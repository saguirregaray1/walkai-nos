/*
 * Copyright 2023 nebuly.com.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package mig_test

import (
	"github.com/nebuly-ai/nos/pkg/gpu"
	"github.com/nebuly-ai/nos/pkg/gpu/mig"
	"github.com/nebuly-ai/nos/pkg/test/factory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"testing"
)

func TestGPU__GetMigGeometry(t *testing.T) {
	testCases := []struct {
		name             string
		gpu              mig.GPU
		expectedGeometry gpu.Geometry
	}{
		{
			name:             "Empty GPU",
			gpu:              mig.NewGpuOrPanic(gpu.GPUModel_A30, 0, make(map[mig.ProfileName]int), make(map[mig.ProfileName]int)),
			expectedGeometry: gpu.Geometry{},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedGeometry, tt.gpu.GetGeometry())
		})
	}
}

func TestGPU__Clone(t *testing.T) {
	testCases := []struct {
		name string
		gpu  mig.GPU
	}{
		{
			name: "Empty GPU",
			gpu:  mig.NewGpuOrPanic(gpu.GPUModel_A30, 0, make(map[mig.ProfileName]int), make(map[mig.ProfileName]int)),
		},
		{
			name: "GPU with free and used profiles",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb:  1,
					mig.Profile2g12gb: 3,
				},
				map[mig.ProfileName]int{
					mig.Profile4g20gb: 1,
					mig.Profile7g40gb: 1,
				},
			),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			cloned := tt.gpu.Clone()
			assert.Equal(t, tt.gpu, cloned)
		})
	}
}

func TestGPU__AddPod(t *testing.T) {
	testCases := []struct {
		name string
		gpu  mig.GPU
		pod  v1.Pod

		expectedUsed map[mig.ProfileName]int
		expectedFree map[mig.ProfileName]int
		expectedErr  bool
	}{
		{
			name: "GPU without free MIG resources",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 4,
				},
				make(map[mig.ProfileName]int),
			),
			pod: factory.BuildPod("ns-1", "pd-1").WithContainer(
				factory.BuildContainer("test", "test").
					WithScalarResourceRequest(mig.Profile1g6gb.AsResourceName(), 1).
					Get(),
			).Get(),
			expectedUsed: map[mig.ProfileName]int{
				mig.Profile1g6gb: 4,
			},
			expectedFree: make(map[mig.ProfileName]int),
			expectedErr:  true,
		},
		{
			name: "GPU without enough free MIG resources",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 1,
				},
				map[mig.ProfileName]int{
					mig.Profile2g12gb: 1,
					mig.Profile1g6gb:  1,
				},
			),
			pod: factory.BuildPod("ns-1", "pd-1").WithContainer(
				factory.BuildContainer("test", "test").
					WithScalarResourceRequest(mig.Profile1g6gb.AsResourceName(), 2).
					Get(),
			).Get(),
			expectedUsed: map[mig.ProfileName]int{
				mig.Profile1g6gb: 1,
			},
			expectedFree: map[mig.ProfileName]int{
				mig.Profile2g12gb: 1,
				mig.Profile1g6gb:  1,
			},
			expectedErr: true,
		},
		{
			name: "GPU with enough free MIG resources: both used and free devices should be updated",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 1,
				},
				map[mig.ProfileName]int{
					mig.Profile2g12gb: 1,
					mig.Profile1g6gb:  1,
				},
			),
			pod: factory.BuildPod("ns-1", "pd-1").WithContainer(
				factory.BuildContainer("test", "test").
					WithScalarResourceRequest(mig.Profile1g6gb.AsResourceName(), 1).
					Get(),
			).Get(),
			expectedUsed: map[mig.ProfileName]int{
				mig.Profile1g6gb: 2,
			},
			expectedFree: map[mig.ProfileName]int{
				mig.Profile2g12gb: 1,
				mig.Profile1g6gb:  0,
			},
			expectedErr: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gpu.AddPod(tt.pod)
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedUsed, tt.gpu.GetUsedMigDevices())
			assert.Equal(t, tt.expectedFree, tt.gpu.GetFreeMigDevices())
		})
	}
}

func TestGPU__ApplyGeometry(t *testing.T) {
	testCases := []struct {
		name            string
		gpu             mig.GPU
		geometryToApply gpu.Geometry
		expected        mig.GPU
		expectedErr     bool
	}{
		{
			name: "Empty GPU: geometry should appear as free MIG devices",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			geometryToApply: gpu.Geometry{
				mig.Profile7g40gb: 1,
			},
			expected: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				make(map[mig.ProfileName]int),
				map[mig.ProfileName]int{
					mig.Profile7g40gb: 1,
				},
			),
			expectedErr: false,
		},
		{
			name: "Invalid MIG geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			geometryToApply: gpu.Geometry{
				mig.Profile1g10gb: 12,
			},
			expected: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				make(map[mig.ProfileName]int),
				make(map[mig.ProfileName]int),
			),
			expectedErr: true,
		},
		{
			name: "MIG Geometry requires deleting used MIG devices: should return error and not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 4,
				},
				make(map[mig.ProfileName]int),
			),
			geometryToApply: map[gpu.Slice]int{
				mig.Profile4g24gb: 1,
			},
			expected: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 4,
				},
				make(map[mig.ProfileName]int),
			),
			expectedErr: true,
		},
		{
			name: "Applying new geometry changes only free devices",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
				map[mig.ProfileName]int{
					mig.Profile2g12gb: 1,
				},
			),
			geometryToApply: map[gpu.Slice]int{
				mig.Profile1g6gb: 4,
			},
			expected: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 2,
				},
			),
			expectedErr: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gpu.ApplyGeometry(tt.geometryToApply)
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expected, tt.gpu)
		})
	}
}

func TestGPU__UpdateGeometryFor(t *testing.T) {
	originalConfigs := cloneKnownGeometries(mig.GetKnownGeometries())
	updatedConfigs := cloneKnownGeometries(originalConfigs)
	updatedConfigs[gpu.GPUModel_A100_PCIe_80GB] = []gpu.Geometry{
		{mig.Profile1g10gb: 7},
		{mig.Profile1g10gb: 5, mig.Profile2g20gb: 1},
		{mig.Profile1g10gb: 3, mig.Profile2g20gb: 2},
		{mig.Profile1g10gb: 1, mig.Profile2g20gb: 3},
		{mig.Profile1g10gb: 2, mig.Profile2g20gb: 1, mig.Profile3g40gb: 1},
		{mig.Profile2g20gb: 2, mig.Profile3g40gb: 1},
		{mig.Profile1g10gb: 3, mig.Profile3g40gb: 1},
		{mig.Profile1g10gb: 1, mig.Profile2g20gb: 1, mig.Profile3g40gb: 1},
		{mig.Profile3g40gb: 2},
		{mig.Profile1g10gb: 3, mig.Profile4g40gb: 1},
		{mig.Profile1g10gb: 1, mig.Profile2g20gb: 1, mig.Profile4g40gb: 1},
		{mig.Profile7g79gb: 1},
	}
	require.NoError(t, mig.SetKnownGeometries(updatedConfigs))
	t.Cleanup(func() {
		require.NoError(t, mig.SetKnownGeometries(originalConfigs))
	})

	testCases := []struct {
		name             string
		gpu              mig.GPU
		profiles         map[gpu.Slice]int
		expectedGeometry gpu.Geometry
		expectedUpdated  bool
	}{
		{
			name: "Empty required profiles map, should not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile2g20gb: 1,
				},
				map[mig.ProfileName]int{},
			),
			profiles: map[gpu.Slice]int{},
			expectedGeometry: gpu.Geometry{
				mig.Profile2g20gb: 1, // unchanged
			},
			expectedUpdated: false,
		},
		{
			name: "No geometries can provide the required profiles, should not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile2g20gb: 1,
				},
				map[mig.ProfileName]int{},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile1g10gb: 1,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile2g20gb: 1, // unchanged
			},
			expectedUpdated: false,
		},
		{
			name: "One geometry could provide the required profiles, but applying it would delete used resources, should not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_PCIe_80GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile2g20gb: 1,
				},
				map[mig.ProfileName]int{},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile7g79gb: 1,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile2g20gb: 1, // unchanged
			},
			expectedUpdated: false,
		},
		{
			name: "Current geometry already provides the required profiles, should not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_SXM4_40GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile2g20gb: 1,
				},
				map[mig.ProfileName]int{
					mig.Profile2g20gb: 2,
				},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile2g20gb: 2,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile2g20gb: 3, // unchanged
			},
			expectedUpdated: false,
		},
		{
			name: "Multiple geometries allow to create some of the required profiles, should change geometry using the " +
				"ones that allow to create the highest number of required profiles",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_PCIe_80GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g10gb: 2,
				},
				map[mig.ProfileName]int{},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile1g10gb: 6,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile1g10gb: 7,
			},
			expectedUpdated: true,
		},
		{
			name: "Should update when additional instances of an already present profile are requested",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_PCIe_80GB,
				0,
				map[mig.ProfileName]int{
					mig.Profile3g40gb: 1,
				},
				map[mig.ProfileName]int{
					mig.Profile1g10gb: 3,
				},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile3g40gb: 1,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile3g40gb: 2,
			},
			expectedUpdated: true,
		},
		{
			name: "Prefers geometry that minimally changes current layout while satisfying requirements",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_PCIe_80GB,
				0,
				map[mig.ProfileName]int{},
				map[mig.ProfileName]int{
					mig.Profile1g10gb: 7,
				},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile2g20gb: 1,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile1g10gb: 5,
				mig.Profile2g20gb: 1,
			},
			expectedUpdated: true,
		},
		{
			name: "Keeps existing profiles when multiple geometries satisfy the same demand",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A100_PCIe_80GB,
				0,
				map[mig.ProfileName]int{},
				map[mig.ProfileName]int{
					mig.Profile1g10gb: 1,
					mig.Profile2g20gb: 1,
					mig.Profile4g40gb: 1,
				},
			),
			profiles: map[gpu.Slice]int{
				mig.Profile3g40gb: 1,
			},
			expectedGeometry: gpu.Geometry{
				mig.Profile1g10gb: 2,
				mig.Profile2g20gb: 1,
				mig.Profile3g40gb: 1,
			},
			expectedUpdated: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			updated := tt.gpu.UpdateGeometryFor(tt.profiles)
			assert.Equal(t, tt.expectedUpdated, updated)
			assert.Equal(t, tt.expectedGeometry, tt.gpu.GetGeometry())
		})
	}
}

func TestGeometry__AsResources(t *testing.T) {
	testCases := []struct {
		name     string
		geometry gpu.Geometry
		expected map[v1.ResourceName]int
	}{
		{
			name:     "Empty geometry",
			geometry: gpu.Geometry{},
			expected: make(map[v1.ResourceName]int),
		},
		{
			name: "Multiple resources",
			geometry: gpu.Geometry{
				mig.Profile1g5gb:  3,
				mig.Profile1g10gb: 2,
			},
			expected: map[v1.ResourceName]int{
				mig.Profile1g5gb.AsResourceName():  3,
				mig.Profile1g10gb.AsResourceName(): 2,
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, mig.AsResources(tt.geometry))
		})
	}
}

func TestGPU__ApplyInitialGeometry(t *testing.T) {
	testCases := []struct {
		name             string
		gpu              mig.GPU
		expectedGeometry gpu.Geometry
		errExpected      bool
	}{
		{
			name: "GPU has used devices, should not change geometry",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{
					mig.Profile1g5gb: 5,
				},
				map[mig.ProfileName]int{},
			),
			errExpected: true,
		},
		{
			name: "GPU has no devices, should apply geometry with lowest number of different devices",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{},
				map[mig.ProfileName]int{},
			),
			errExpected: false,
			expectedGeometry: map[gpu.Slice]int{
				mig.Profile4g24gb: 1,
			},
		},
		{
			name: "GPU has free devices, should apply geometry with lowest number of different devices",
			gpu: mig.NewGpuOrPanic(
				gpu.GPUModel_A30,
				0,
				map[mig.ProfileName]int{},
				map[mig.ProfileName]int{
					mig.Profile1g6gb: 1,
				},
			),
			errExpected: false,
			expectedGeometry: map[gpu.Slice]int{
				mig.Profile4g24gb: 1,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gpu.InitGeometry()
			if tc.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedGeometry, tc.gpu.GetGeometry())
			}
		})
	}
}

func cloneKnownGeometries(in map[gpu.Model][]gpu.Geometry) map[gpu.Model][]gpu.Geometry {
	cloned := make(map[gpu.Model][]gpu.Geometry, len(in))
	for model, geometries := range in {
		copiedGeometries := make([]gpu.Geometry, len(geometries))
		for i, geometry := range geometries {
			copiedGeometry := make(gpu.Geometry, len(geometry))
			for profile, quantity := range geometry {
				copiedGeometry[profile] = quantity
			}
			copiedGeometries[i] = copiedGeometry
		}
		cloned[model] = copiedGeometries
	}
	return cloned
}
