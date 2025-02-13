// Copyright 2018 PingCAP, Inc.
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

package meta

import (
	"fmt"

	"github.com/pingcap/tidb-operator/pkg/apis/label"
	"github.com/pingcap/tidb-operator/pkg/apis/pingcap/v1alpha1"
	"github.com/pingcap/tidb-operator/pkg/controller"
	"github.com/pingcap/tidb-operator/pkg/manager"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

type reclaimPolicyManager struct {
	deps *controller.Dependencies
}

// NewReclaimPolicyManager returns a *reclaimPolicyManager
func NewReclaimPolicyManager(deps *controller.Dependencies) *reclaimPolicyManager {
	return &reclaimPolicyManager{
		deps: deps,
	}
}

func (m *reclaimPolicyManager) Sync(tc *v1alpha1.TidbCluster) error {
	return m.sync(v1alpha1.TiDBClusterKind, tc, tc.IsPVReclaimEnabled(), *tc.Spec.PVReclaimPolicy)
}

func (m *reclaimPolicyManager) SyncMonitor(tm *v1alpha1.TidbMonitor) error {
	return m.sync(v1alpha1.TiDBMonitorKind, tm, false, *tm.Spec.PVReclaimPolicy)
}

func (m *reclaimPolicyManager) SyncTiDBNGMonitoring(tngm *v1alpha1.TidbNGMonitoring) error {
	return m.sync(v1alpha1.TiDBNGMonitoringKind, tngm, false, *tngm.Spec.PVReclaimPolicy)
}

func (m *reclaimPolicyManager) SyncDM(dc *v1alpha1.DMCluster) error {
	return m.sync(v1alpha1.DMClusterKind, dc, dc.IsPVReclaimEnabled(), *dc.Spec.PVReclaimPolicy)
}

func (m *reclaimPolicyManager) sync(kind string, obj runtime.Object, isPVReclaimEnabled bool, policy corev1.PersistentVolumeReclaimPolicy) error {
	if m.deps.PVLister == nil {
		klog.V(4).Infof("Persistent volumes lister is unavailable, skip syncing reclaim policy for %s. This may be caused by no relevant permissions", kind)
		return nil
	}

	var (
		meta         = obj.(metav1.ObjectMetaAccessor).GetObjectMeta()
		ns           = meta.GetNamespace()
		instanceName = meta.GetName()
		selector     labels.Selector
		err          error
	)

	switch kind {
	case v1alpha1.TiDBClusterKind:
		selector, err = label.New().Instance(instanceName).Selector()
	case v1alpha1.TiDBMonitorKind:
		selector, err = label.NewMonitor().Instance(instanceName).Monitor().Selector()
	case v1alpha1.DMClusterKind:
		selector, err = label.NewDM().Instance(instanceName).Selector()
	case v1alpha1.TiDBNGMonitoringKind:
		selector, err = label.NewTiDBNGMonitoring().Instance(instanceName).Selector()
	default:
		return fmt.Errorf("unsupported kind %s", kind)
	}
	if err != nil {
		return err
	}

	pvcs, err := m.deps.PVCLister.PersistentVolumeClaims(ns).List(selector)
	if err != nil {
		return fmt.Errorf("reclaimPolicyManager.sync: failed to list pvc for %s %s/%s, selector %s, error: %s", kind, ns, instanceName, selector, err)
	}
	for _, pvc := range pvcs {
		if pvc.Spec.VolumeName == "" {
			continue
		}
		if isPVReclaimEnabled && len(pvc.Annotations[label.AnnPVCDeferDeleting]) != 0 {
			// If the PV reclaim setting is enabled, and when PV is a candidate to be reclaimed, skip patching this PV.
			continue
		}
		if l := label.Label(pvc.Labels); kind == v1alpha1.TiDBClusterKind && (!l.IsPD() && !l.IsTiDB() && !l.IsTiKV() && !l.IsTiFlash() && !l.IsPump()) {
			continue
		}
		pv, err := m.deps.PVLister.Get(pvc.Spec.VolumeName)
		if err != nil {
			return fmt.Errorf("reclaimPolicyManager.sync: failed to get pvc %s for %s %s/%s, error: %s", pvc.Spec.VolumeName, kind, ns, instanceName, err)
		}

		if pv.Spec.PersistentVolumeReclaimPolicy == policy {
			continue
		}
		err = m.deps.PVControl.PatchPVReclaimPolicy(obj, pv, policy)
		if err != nil {
			return err
		}
	}

	return nil
}

var _ manager.Manager = &reclaimPolicyManager{}

type FakeReclaimPolicyManager struct {
	err error
}

func NewFakeReclaimPolicyManager() *FakeReclaimPolicyManager {
	return &FakeReclaimPolicyManager{}
}

func (m *FakeReclaimPolicyManager) SetSyncError(err error) {
	m.err = err
}

func (m *FakeReclaimPolicyManager) Sync(_ *v1alpha1.TidbCluster) error {
	return m.err
}

func (m *FakeReclaimPolicyManager) SyncDM(_ *v1alpha1.DMCluster) error {
	return m.err
}
