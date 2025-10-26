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

package mig

import (
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/nebuly-ai/nos/pkg/gpu"
	"github.com/nebuly-ai/nos/pkg/util"
	v1 "k8s.io/api/core/v1"
)

type GPU struct {
	index                int
	model                gpu.Model
	allowedMigGeometries []gpu.Geometry
	usedMigDevices       map[ProfileName]int
	freeMigDevices       map[ProfileName]int
}

func NewGpuOrPanic(model gpu.Model, index int, usedMigDevices, freeMigDevices map[ProfileName]int) GPU {
	g, err := NewGPU(model, index, usedMigDevices, freeMigDevices)
	if err != nil {
		panic(err)
	}
	return g
}

func NewGPU(model gpu.Model, index int, usedMigDevices, freeMigDevices map[ProfileName]int) (GPU, error) {
	allowedGeometries, ok := GetAllowedGeometries(model)
	if !ok {
		return GPU{}, fmt.Errorf("model %q is not associated with any known GPU", model)
	}
	return GPU{
		index:                index,
		model:                model,
		allowedMigGeometries: allowedGeometries,
		usedMigDevices:       usedMigDevices,
		freeMigDevices:       freeMigDevices,
	}, nil
}

func (g *GPU) Clone() GPU {
	cloned := GPU{
		index:                g.index,
		model:                g.model,
		allowedMigGeometries: g.allowedMigGeometries,
		usedMigDevices:       make(map[ProfileName]int),
		freeMigDevices:       make(map[ProfileName]int),
	}
	for k, v := range g.freeMigDevices {
		cloned.freeMigDevices[k] = v
	}
	for k, v := range g.usedMigDevices {
		cloned.usedMigDevices[k] = v
	}
	return cloned
}

func (g *GPU) GetIndex() int {
	return g.index
}

func (g *GPU) GetModel() gpu.Model {
	return g.model
}

func (g *GPU) GetGeometry() gpu.Geometry {
	res := make(gpu.Geometry)

	for profile, quantity := range g.usedMigDevices {
		res[profile] += quantity
	}
	for profile, quantity := range g.freeMigDevices {
		res[profile] += quantity
	}

	return res
}

// CanApplyGeometry returns true if the geometry provided as argument can be applied to the GPU, otherwise it
// returns false and the reason why the geometry cannot be applied.
func (g *GPU) CanApplyGeometry(geometry gpu.Geometry) (bool, string) {
	// Check if geometry is allowed
	if !g.AllowsGeometry(geometry) {
		return false, fmt.Sprintf("GPU model %s does not allow the provided MIG geometry", g.model)
	}
	// Check if new geometry deletes used devices
	for usedProfile, usedQuantity := range g.usedMigDevices {
		if geometry[usedProfile] < usedQuantity {
			return false, "cannot apply MIG geometry: cannot delete MIG devices being used"
		}
	}

	return true, ""
}

// InitGeometry applies the initial MIG geometry of the GPU, so that each MIG GPU has at least one MIG device.
//
// The initial geometry is the one with the largest partitioning (e.g. with fewest slices).
//
// It returns an error if the initial geometry cannot be applied due to used devices that would
// be deleted by the new geometry.
func (g *GPU) InitGeometry() error {
	// Get the geometry with the largest partitioning (e.g. with fewest slices)
	largestGeometry := gpu.GetFewestSlicesGeometry(g.allowedMigGeometries)
	// Apply the geometry
	canApply, reason := g.CanApplyGeometry(largestGeometry)
	if !canApply {
		return errors.New(reason)
	}
	return g.ApplyGeometry(largestGeometry)
}

// ApplyGeometry applies the MIG geometry provided as argument by changing the free devices of the GPU.
// It returns an error if the provided geometry is not allowed or if applying it would require to delete any used
// device of the GPU.
func (g *GPU) ApplyGeometry(geometry gpu.Geometry) error {
	canApply, reason := g.CanApplyGeometry(geometry)
	if !canApply {
		return errors.New(reason)
	}
	// Apply geometry by changing free devices
	for profile, quantity := range geometry {
		migProfile, ok := profile.(ProfileName)
		if ok {
			g.freeMigDevices[migProfile] = quantity - g.usedMigDevices[migProfile]
		}
	}
	// Delete all free devices not included in the new geometry
	for profile := range g.freeMigDevices {
		if _, ok := geometry[profile]; !ok {
			delete(g.freeMigDevices, profile)
		}
	}

	return nil
}

// UpdateGeometryFor tries to update the geometry of the GPU in order to create the highest possible number of required
// profiles provided as argument, without deleting any of the used profiles.
//
// The method returns true if the GPU geometry gets updated, false otherwise.
func (g *GPU) UpdateGeometryFor(requiredProfiles map[gpu.Slice]int) bool {
	currentGeometry := g.GetGeometry()
	var (
		bestGeometry     *gpu.Geometry
		bestScore        geometrySelectionScore
		bestScoreDefined bool
	)

	for _, candidate := range g.GetAllowedGeometries() {
		if canApplyGeometry, _ := g.CanApplyGeometry(candidate); !canApplyGeometry {
			continue
		}
		provided := g.countProvidedProfiles(candidate, requiredProfiles, currentGeometry)
		if provided <= 0 {
			continue
		}
		score := geometrySelectionScore{
			providedProfiles: provided,
			geometryDistance: geometryDistance(currentGeometry, candidate),
			totalSlices:      totalSliceCount(candidate),
			geometryID:       candidate.Id(),
		}
		if !bestScoreDefined || isBetterGeometryScore(score, bestScore) {
			candidateCopy := candidate
			bestGeometry = &candidateCopy
			bestScore = score
			bestScoreDefined = true
		}
	}

	if bestGeometry == nil {
		return false
	}

	_ = g.ApplyGeometry(*bestGeometry)
	return true
}

func (g *GPU) countProvidedProfiles(
	candidate gpu.Geometry,
	requiredProfiles map[gpu.Slice]int,
	currentGeometry gpu.Geometry,
) int {
	var provided int
	for requiredProfile, requiredQuantity := range requiredProfiles {
		requiredMigProfile, ok := requiredProfile.(ProfileName)
		if !ok {
			continue
		}
		if g.freeMigDevices[requiredMigProfile] >= requiredQuantity {
			continue
		}
		needed := requiredQuantity - g.freeMigDevices[requiredMigProfile]
		if needed <= 0 {
			continue
		}
		additionalCapacity := candidate[requiredMigProfile] - currentGeometry[requiredMigProfile]
		if additionalCapacity <= 0 {
			continue
		}
		provided += util.Min(additionalCapacity, needed)
	}
	return provided
}

type geometrySelectionScore struct {
	providedProfiles int
	totalSlices      int
	geometryDistance int
	geometryID       string
}

func isBetterGeometryScore(candidate, current geometrySelectionScore) bool {
	if candidate.providedProfiles != current.providedProfiles {
		return candidate.providedProfiles > current.providedProfiles
	}
	if candidate.totalSlices != current.totalSlices {
		return candidate.totalSlices > current.totalSlices
	}
	if candidate.geometryDistance != current.geometryDistance {
		return candidate.geometryDistance < current.geometryDistance
	}
	return candidate.geometryID < current.geometryID
}

func geometryDistance(current, candidate gpu.Geometry) int {
	sum := 0
	seen := make(map[gpu.Slice]struct{}, len(current))
	for profile, currentCount := range current {
		seen[profile] = struct{}{}
		candidateCount := candidate[profile]
		sum += util.Abs(candidateCount - currentCount)
	}
	for profile, candidateCount := range candidate {
		if _, ok := seen[profile]; ok {
			continue
		}
		sum += util.Abs(candidateCount)
	}
	return sum
}

func totalSliceCount(geometry gpu.Geometry) int {
	sum := 0
	for _, count := range geometry {
		sum += count
	}
	return sum
}

// AllowsGeometry returns true if the geometry provided as argument is allowed by the GPU model
func (g *GPU) AllowsGeometry(geometry gpu.Geometry) bool {
	for _, allowedGeometry := range g.GetAllowedGeometries() {
		if cmp.Equal(geometry, allowedGeometry) {
			return true
		}
	}
	return false
}

// GetAllowedGeometries returns the MIG geometries allowed by the GPU model
func (g *GPU) GetAllowedGeometries() []gpu.Geometry {
	return g.allowedMigGeometries
}

// AddPod adds a Pod to the GPU by updating the free and used MIG devices according to the MIG resources
// requested by the Pod.
//
// AddPod returns an error if the GPU does not have enough free MIG resources for the Pod.
func (g *GPU) AddPod(pod v1.Pod) error {
	for r, q := range GetRequestedProfiles(pod) {
		if g.freeMigDevices[r] < q {
			return fmt.Errorf(
				"not enough free MIG devices (pod requests %d %s, but GPU only has %d)",
				q,
				r,
				g.freeMigDevices[r],
			)
		}
		g.freeMigDevices[r] -= q
		g.usedMigDevices[r] += q
	}
	return nil
}

func (g *GPU) HasFreeMigDevices() bool {
	return len(g.GetFreeMigDevices()) > 0
}

func (g *GPU) GetFreeMigDevices() map[ProfileName]int {
	return g.freeMigDevices
}

func (g *GPU) GetUsedMigDevices() map[ProfileName]int {
	return g.usedMigDevices
}
