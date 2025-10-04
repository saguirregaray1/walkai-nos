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

package gpu_test

import (
	"github.com/nebuly-ai/nos/pkg/api/nos.nebuly.com/v1alpha1"
	"github.com/nebuly-ai/nos/pkg/gpu"
	"github.com/nebuly-ai/nos/pkg/gpu/mig"
	"github.com/nebuly-ai/nos/pkg/test/factory"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"testing"
)

func TestIsMigPartitioningEnabled(t *testing.T) {
	testCases := []struct {
		name     string
		node     v1.Node
		expected bool
	}{
		{
			name:     "Node without partitioning label",
			node:     factory.BuildNode("node-1").Get(),
			expected: false,
		},
		{
			name: "Noe with partitioning label, MIG",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				v1alpha1.LabelGpuPartitioning: gpu.PartitioningKindMig.String(),
			}).Get(),
			expected: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			enabled := gpu.IsMigPartitioningEnabled(tt.node)
			assert.Equal(t, tt.expected, enabled)
		})
	}
}

func TestGetPartitioningKind(t *testing.T) {
	testCases := []struct {
		name       string
		node       v1.Node
		expected   gpu.PartitioningKind
		expectedOk bool
	}{
		{
			name:       "Node without GPU partitioning label",
			node:       factory.BuildNode("node-1").Get(),
			expected:   "",
			expectedOk: false,
		},
		{
			name: "Node with GPU partitioning label, but value is not a valid kind",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				v1alpha1.LabelGpuPartitioning: "invalid-kind",
			}).Get(),
			expected:   "",
			expectedOk: false,
		},
		{
			name: "Node with MIG partitioning kind",
			node: factory.BuildNode("node-1").WithLabels(map[string]string{
				v1alpha1.LabelGpuPartitioning: gpu.PartitioningKindMig.String(),
			}).Get(),
			expected:   gpu.PartitioningKindMig,
			expectedOk: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			kind, ok := gpu.GetPartitioningKind(tt.node)
			assert.Equal(t, tt.expected, kind)
			assert.Equal(t, tt.expectedOk, ok)
		})
	}
}

func Test_GetFewestSlicesGeometry(t *testing.T) {
	testCases := []struct {
		name       string
		geometries []gpu.Geometry
		expected   gpu.Geometry
	}{
		{
			name:       "Empty geometries",
			geometries: []gpu.Geometry{},
			expected:   gpu.Geometry(nil),
		},
		{
			name: "Multiple geometries",
			geometries: []gpu.Geometry{
				{
					mig.Profile1g10gb: 1,
					mig.Profile2g20gb: 2,
				},
				{
					mig.Profile7g40gb: 1,
					mig.Profile2g20gb: 1,
				},
				{
					mig.Profile7g40gb: 7,
				},
			},
			expected: gpu.Geometry{
				mig.Profile7g40gb: 7, // Should consider only the number of *different* slices
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			geometry := gpu.GetFewestSlicesGeometry(tt.geometries)
			assert.Equal(t, tt.expected, geometry)
		})
	}
}
