// Copyright 2020 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package member

import (
	"fmt"
	"strconv"
	"time"

	"github.com/pingcap/tidb-operator/pkg/apis/label"
	"github.com/pingcap/tidb-operator/pkg/apis/pingcap/v1alpha1"
	"github.com/pingcap/tidb-operator/pkg/controller"
	apps "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
)

type tiflashScaler struct {
	generalScaler
}

// NewTiFlashScaler returns a tiflash Scaler
func NewTiFlashScaler(deps *controller.Dependencies) Scaler {
	return &tiflashScaler{generalScaler: generalScaler{deps: deps}}
}

func (s *tiflashScaler) Scale(meta metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	scaling, _, _, _ := scaleOne(oldSet, newSet)
	if scaling > 0 {
		return s.ScaleOut(meta, oldSet, newSet)
	} else if scaling < 0 {
		return s.ScaleIn(meta, oldSet, newSet)
	}
	// we only sync auto scaler annotations when we are finishing syncing scaling
	return nil
}

func (s *tiflashScaler) ScaleOut(meta metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	tc, ok := meta.(*v1alpha1.TidbCluster)
	if !ok {
		return nil
	}

	_, ordinal, replicas, deleteSlots := scaleOne(oldSet, newSet)
	resetReplicas(newSet, oldSet)

	klog.Infof("scaling out tiflash statefulset %s/%s, ordinal: %d (replicas: %d, delete slots: %v)", oldSet.Namespace, oldSet.Name, ordinal, replicas, deleteSlots.List())
	_, err := s.deleteDeferDeletingPVC(tc, v1alpha1.TiFlashMemberType, ordinal)
	if err != nil {
		return err
	}

	setReplicasAndDeleteSlots(newSet, replicas, deleteSlots)
	return nil
}

func (s *tiflashScaler) ScaleIn(meta metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	tc, ok := meta.(*v1alpha1.TidbCluster)
	if !ok {
		return nil
	}

	ns := tc.GetNamespace()
	tcName := tc.GetName()
	// we can only remove one member at a time when scaling in
	_, ordinal, replicas, deleteSlots := scaleOne(oldSet, newSet)
	resetReplicas(newSet, oldSet)

	klog.Infof("scaling in tiflash statefulset %s/%s, ordinal: %d (replicas: %d, delete slots: %v)", oldSet.Namespace, oldSet.Name, ordinal, replicas, deleteSlots.List())
	// We need delete store from cluster before decreasing the statefulset replicas
	podName := ordinalPodName(v1alpha1.TiFlashMemberType, tcName, ordinal)
	pod, err := s.deps.PodLister.Pods(ns).Get(podName)
	if err != nil {
		return fmt.Errorf("tiflashScaler.ScaleIn: failed to get pods %s for cluster %s/%s, error: %s", podName, ns, tcName, err)
	}

	// TODO: Update Webhook to support TiFlash
	// if controller.PodWebhookEnabled {
	// 	setReplicasAndDeleteSlots(newSet, replicas, deleteSlots)
	// 	return nil
	// }

	for _, store := range tc.Status.TiFlash.Stores {
		if store.PodName == podName {
			state := store.State
			id, err := strconv.ParseUint(store.ID, 10, 64)
			if err != nil {
				return err
			}
			if state != v1alpha1.TiKVStateOffline {
				if err := controller.GetPDClient(s.deps.PDControl, tc).DeleteStore(id); err != nil {
					klog.Errorf("tiflash scale in: failed to delete store %d, %v", id, err)
					return err
				}
				klog.Infof("tiflash scale in: delete store %d for tiflash %s/%s successfully", id, ns, podName)
			}
			return controller.RequeueErrorf("TiFlash %s/%s store %d is still in cluster, state: %s", ns, podName, id, state)
		}
	}
	for id, store := range tc.Status.TiFlash.TombstoneStores {
		if store.PodName == podName && pod.Labels[label.StoreIDLabelKey] == id {
			id, err := strconv.ParseUint(store.ID, 10, 64)
			if err != nil {
				return err
			}

			// TODO: double check if store is really not in Up/Offline/Down state
			klog.Infof("TiFlash %s/%s store %d becomes tombstone", ns, podName, id)

			err = s.updateDeferDeletingPVC(tc, v1alpha1.TiFlashMemberType, ordinal)
			if err != nil {
				return err
			}
			setReplicasAndDeleteSlots(newSet, replicas, deleteSlots)
			return nil
		}
	}

	// When store not found in TidbCluster status, there are two situations as follows:
	// 1. This can happen when TiFlash joins cluster but we haven't synced its status.
	//    In this situation return error to wait another round for safety.
	//
	// 2. This can happen when TiFlash pod has not been successfully registered in the cluster, such as always pending.
	//    In this situation we should delete this TiFlash pod immediately to avoid blocking the subsequent operations.
	if !podutil.IsPodReady(pod) {
		safeTimeDeadline := pod.CreationTimestamp.Add(5 * s.deps.CLIConfig.ResyncDuration)
		if time.Now().Before(safeTimeDeadline) {
			// Wait for 5 resync periods to ensure that the following situation does not occur:
			//
			// The tiflash pod starts for a while, but has not synced its status, and then the pod becomes not ready.
			// Here we wait for 5 resync periods to ensure that the status of this tiflash pod has been synced.
			// After this period of time, if there is still no information about this tiflash in TidbCluster status,
			// then we can be sure that this tiflash has never been added to the tidb cluster.
			// So we can scale in this tiflash pod safely.
			resetReplicas(newSet, oldSet)
			return fmt.Errorf("TiFlash %s/%s is not ready, wait for some resync periods to synced its status", ns, podName)
		}
		klog.Infof("Pod %s/%s not ready for more than %v and no store for it, scale in it",
			ns, podName, 5*s.deps.CLIConfig.ResyncDuration)
		err = s.updateDeferDeletingPVC(tc, v1alpha1.TiFlashMemberType, ordinal)
		if err != nil {
			return err
		}
		setReplicasAndDeleteSlots(newSet, replicas, deleteSlots)
		return nil
	}
	return fmt.Errorf("tiflash %s/%s no store found in cluster", ns, podName)
}

type fakeTiFlashScaler struct{}

// NewFakeTiFlashScaler returns a fake tiflash Scaler
func NewFakeTiFlashScaler() Scaler {
	return &fakeTiFlashScaler{}
}

func (s *fakeTiFlashScaler) Scale(meta metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	if *newSet.Spec.Replicas > *oldSet.Spec.Replicas {
		return s.ScaleOut(meta, oldSet, newSet)
	} else if *newSet.Spec.Replicas < *oldSet.Spec.Replicas {
		return s.ScaleIn(meta, oldSet, newSet)
	}
	return nil
}

func (s *fakeTiFlashScaler) ScaleOut(_ metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	setReplicasAndDeleteSlots(newSet, *oldSet.Spec.Replicas+1, nil)
	return nil
}

func (s *fakeTiFlashScaler) ScaleIn(_ metav1.Object, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	setReplicasAndDeleteSlots(newSet, *oldSet.Spec.Replicas-1, nil)
	return nil
}
