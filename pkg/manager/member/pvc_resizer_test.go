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
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pingcap/tidb-operator/pkg/apis/label"
	"github.com/pingcap/tidb-operator/pkg/apis/pingcap/v1alpha1"
	"github.com/pingcap/tidb-operator/pkg/controller"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func newFullPVC(name, component, storageClass, storageRequest, nameLabel, instance string) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: v1.NamespaceDefault,
			Name:      name,
			Labels: map[string]string{
				label.NameLabelKey:      nameLabel,
				label.ManagedByLabelKey: label.TiDBOperator,
				label.InstanceLabelKey:  instance,
				label.ComponentLabelKey: component,
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse(storageRequest),
				},
			},
			StorageClassName: pointer.StringPtr(storageClass),
		},
	}
}

func newPVCWithStorage(name string, component string, storageClass, storageRequest string) *v1.PersistentVolumeClaim {
	return newFullPVC(name, component, storageClass, storageRequest, "tidb-cluster", "tc")
}

func newDMPVCWithStorage(name string, component string, storageClass, storageRequest string) *v1.PersistentVolumeClaim {
	return newFullPVC(name, component, storageClass, storageRequest, "dm-cluster", "dc")
}

func newStorageClass(name string, volumeExpansion bool) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		AllowVolumeExpansion: pointer.BoolPtr(volumeExpansion),
	}
}

func TestPVCResizer(t *testing.T) {
	tests := []struct {
		name     string
		tc       *v1alpha1.TidbCluster
		sc       *storagev1.StorageClass
		pvcs     []*v1.PersistentVolumeClaim
		wantPVCs []*v1.PersistentVolumeClaim
		wantErr  error
	}{
		{
			name: "no PVCs",
			tc: &v1alpha1.TidbCluster{
				Spec: v1alpha1.TidbClusterSpec{},
			},
		},
		{
			name: "resize PD PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					PD: &v1alpha1.PDSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
						StorageVolumes: []v1alpha1.StorageVolume{
							{
								Name:        "log",
								StorageSize: "2Gi",
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-tc-pd-0", label.PDLabelVal, "sc", "1Gi"),
				newPVCWithStorage("pd-tc-pd-1", label.PDLabelVal, "sc", "1Gi"),
				newPVCWithStorage("pd-tc-pd-2", label.PDLabelVal, "sc", "1Gi"),
				newPVCWithStorage("pd-log-tc-pd-0", label.PDLabelVal, "sc", "1Gi"),
				newPVCWithStorage("pd-log-tc-pd-1", label.PDLabelVal, "sc", "1Gi"),
				newPVCWithStorage("pd-log-tc-pd-2", label.PDLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-tc-pd-0", label.PDLabelVal, "sc", "2Gi"),
				newPVCWithStorage("pd-tc-pd-1", label.PDLabelVal, "sc", "2Gi"),
				newPVCWithStorage("pd-tc-pd-2", label.PDLabelVal, "sc", "2Gi"),
				newPVCWithStorage("pd-log-tc-pd-0", label.PDLabelVal, "sc", "2Gi"),
				newPVCWithStorage("pd-log-tc-pd-1", label.PDLabelVal, "sc", "2Gi"),
				newPVCWithStorage("pd-log-tc-pd-2", label.PDLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "resize TiDB PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					TiDB: &v1alpha1.TiDBSpec{
						StorageVolumes: []v1alpha1.StorageVolume{
							{
								Name:        "log",
								StorageSize: "2Gi",
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("tidb-log-tc-tidb-0", label.TiDBLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tidb-log-tc-tidb-1", label.TiDBLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tidb-log-tc-tidb-2", label.TiDBLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("tidb-log-tc-tidb-0", label.TiDBLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tidb-log-tc-tidb-1", label.TiDBLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tidb-log-tc-tidb-2", label.TiDBLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "resize TiKV PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					TiKV: &v1alpha1.TiKVSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
						StorageVolumes: []v1alpha1.StorageVolume{
							{
								Name:        "log",
								StorageSize: "2Gi",
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("tikv-tc-tikv-0", label.TiKVLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tikv-tc-tikv-1", label.TiKVLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tikv-tc-tikv-2", label.TiKVLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-0", label.TiKVLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-1", label.TiKVLabelVal, "sc", "1Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-2", label.TiKVLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("tikv-tc-tikv-0", label.TiKVLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tikv-tc-tikv-1", label.TiKVLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tikv-tc-tikv-2", label.TiKVLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-0", label.TiKVLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-1", label.TiKVLabelVal, "sc", "2Gi"),
				newPVCWithStorage("tikv-log-tc-tikv-2", label.TiKVLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "resize TiFlash PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					TiFlash: &v1alpha1.TiFlashSpec{
						StorageClaims: []v1alpha1.StorageClaim{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceStorage: resource.MustParse("2Gi"),
									},
								},
								StorageClassName: pointer.StringPtr("sc"),
							},
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceStorage: resource.MustParse("3Gi"),
									},
								},
								StorageClassName: pointer.StringPtr("sc"),
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("data0-tc-tiflash-0", label.TiFlashLabelVal, "sc", "1Gi"),
				newPVCWithStorage("data1-tc-tiflash-0", label.TiFlashLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("data0-tc-tiflash-0", label.TiFlashLabelVal, "sc", "2Gi"),
				newPVCWithStorage("data1-tc-tiflash-0", label.TiFlashLabelVal, "sc", "3Gi"),
			},
		},
		{
			name: "resize TiCDC PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					TiCDC: &v1alpha1.TiCDCSpec{
						StorageVolumes: []v1alpha1.StorageVolume{
							{
								Name:        "sort-dir",
								StorageSize: "2Gi",
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-0", label.TiCDCLabelVal, "sc", "1Gi"),
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-1", label.TiCDCLabelVal, "sc", "1Gi"),
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-2", label.TiCDCLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-0", label.TiCDCLabelVal, "sc", "2Gi"),
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-1", label.TiCDCLabelVal, "sc", "2Gi"),
				newPVCWithStorage("ticdc-sort-dir-tc-ticdc-2", label.TiCDCLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "resize Pump PVCs",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					Pump: &v1alpha1.PumpSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("data-tc-pump-0", label.PumpLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("data-tc-pump-0", label.PumpLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "storage class is missing",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					PD: &v1alpha1.PDSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-tc-pd-0", label.PDLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-tc-pd-0", label.PDLabelVal, "sc", "1Gi"),
			},
			wantErr: apierrors.NewNotFound(storagev1.Resource("storageclass"), "sc"),
		},
		{
			name: "storage class does not support volume expansion",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					PD: &v1alpha1.PDSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", false),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-0", label.PDLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-0", label.PDLabelVal, "sc", "1Gi"),
			},
			wantErr: nil,
		},
		{
			name: "shrinking is not supported",
			tc: &v1alpha1.TidbCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "tc",
				},
				Spec: v1alpha1.TidbClusterSpec{
					PD: &v1alpha1.PDSpec{
						ResourceRequirements: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			sc: newStorageClass("sc", false),
			pvcs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-0", label.PDLabelVal, "sc", "2Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newPVCWithStorage("pd-0", label.PDLabelVal, "sc", "2Gi"),
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDeps := controller.NewFakeDependencies()
			for _, pvc := range tt.pvcs {
				fakeDeps.KubeClientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
			}
			if tt.sc != nil {
				fakeDeps.KubeClientset.StorageV1().StorageClasses().Create(context.TODO(), tt.sc, metav1.CreateOptions{})
			}

			resizer := NewPVCResizer(fakeDeps)

			informerFactory := fakeDeps.KubeInformerFactory
			informerFactory.Start(ctx.Done())
			informerFactory.WaitForCacheSync(ctx.Done())

			err := resizer.Resize(tt.tc)
			if !reflect.DeepEqual(tt.wantErr, err) {
				t.Errorf("want %v, got %v", tt.wantErr, err)
			}

			for i, pvc := range tt.pvcs {
				wantPVC := tt.wantPVCs[i]
				got, err := fakeDeps.KubeClientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(wantPVC, got); diff != "" {
					t.Errorf("unexpected (-want, +got): %s", diff)
				}
			}
		})
	}
}

func TestDMPVCResizer(t *testing.T) {
	tests := []struct {
		name     string
		dc       *v1alpha1.DMCluster
		sc       *storagev1.StorageClass
		pvcs     []*v1.PersistentVolumeClaim
		wantPVCs []*v1.PersistentVolumeClaim
		wantErr  error
	}{
		{
			name: "no PVCs",
			dc: &v1alpha1.DMCluster{
				Spec: v1alpha1.DMClusterSpec{},
			},
		},
		{
			name: "resize dm-master PVCs",
			dc: &v1alpha1.DMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "dc",
				},
				Spec: v1alpha1.DMClusterSpec{
					Master: v1alpha1.MasterSpec{
						StorageSize: "2Gi",
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newDMPVCWithStorage("dm-master-dc-dm-master-0", label.DMMasterLabelVal, "sc", "1Gi"),
				newDMPVCWithStorage("dm-master-dc-dm-master-1", label.DMMasterLabelVal, "sc", "1Gi"),
				newDMPVCWithStorage("dm-master-dc-dm-master-2", label.DMMasterLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newDMPVCWithStorage("dm-master-dc-dm-master-0", label.DMMasterLabelVal, "sc", "2Gi"),
				newDMPVCWithStorage("dm-master-dc-dm-master-1", label.DMMasterLabelVal, "sc", "2Gi"),
				newDMPVCWithStorage("dm-master-dc-dm-master-2", label.DMMasterLabelVal, "sc", "2Gi"),
			},
		},
		{
			name: "resize dm-worker PVCs",
			dc: &v1alpha1.DMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Name:      "dc",
				},
				Spec: v1alpha1.DMClusterSpec{
					Worker: &v1alpha1.WorkerSpec{
						StorageSize: "2Gi",
					},
				},
			},
			sc: newStorageClass("sc", true),
			pvcs: []*v1.PersistentVolumeClaim{
				newDMPVCWithStorage("dm-worker-dc-dm-worker-0", label.DMWorkerLabelVal, "sc", "1Gi"),
				newDMPVCWithStorage("dm-worker-dc-dm-worker-1", label.DMWorkerLabelVal, "sc", "1Gi"),
				newDMPVCWithStorage("dm-worker-dc-dm-worker-2", label.DMWorkerLabelVal, "sc", "1Gi"),
			},
			wantPVCs: []*v1.PersistentVolumeClaim{
				newDMPVCWithStorage("dm-worker-dc-dm-worker-0", label.DMWorkerLabelVal, "sc", "2Gi"),
				newDMPVCWithStorage("dm-worker-dc-dm-worker-1", label.DMWorkerLabelVal, "sc", "2Gi"),
				newDMPVCWithStorage("dm-worker-dc-dm-worker-2", label.DMWorkerLabelVal, "sc", "2Gi"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDeps := controller.NewFakeDependencies()

			for _, pvc := range tt.pvcs {
				fakeDeps.KubeClientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
			}
			if tt.sc != nil {
				fakeDeps.KubeClientset.StorageV1().StorageClasses().Create(context.TODO(), tt.sc, metav1.CreateOptions{})
			}

			informerFactory := fakeDeps.KubeInformerFactory
			resizer := NewPVCResizer(fakeDeps)

			informerFactory.Start(ctx.Done())
			informerFactory.WaitForCacheSync(ctx.Done())

			err := resizer.ResizeDM(tt.dc)
			if !reflect.DeepEqual(tt.wantErr, err) {
				t.Errorf("want %v, got %v", tt.wantErr, err)
			}

			for i, pvc := range tt.pvcs {
				wantPVC := tt.wantPVCs[i]
				got, err := fakeDeps.KubeClientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(wantPVC, got); diff != "" {
					t.Errorf("unexpected (-want, +got): %s", diff)
				}
			}
		})
	}
}
