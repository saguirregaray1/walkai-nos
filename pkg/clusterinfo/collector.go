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

package clusterinfo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nebuly-ai/nos/pkg/gpu"
	"github.com/nebuly-ai/nos/pkg/gpu/mig"
	"github.com/nebuly-ai/nos/pkg/resource"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// Collector retrieves information about GPU inventory and GPU-consuming pods from the cluster.
type Collector struct {
	kube  kubernetes.Interface
	clock clock
}

func NewCollector(kube kubernetes.Interface) Collector {
	return Collector{
		kube:  kube,
		clock: realClock{},
	}
}

func NewCollectorWithClock(kube kubernetes.Interface, c clock) Collector {
	return Collector{
		kube:  kube,
		clock: c,
	}
}

func (c Collector) Collect(ctx context.Context) (Snapshot, error) {
	nodes, err := c.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return Snapshot{}, err
	}
	pods, err := c.kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{
		Timestamp: c.clock.Now().UTC(),
		GPUs:      buildGPUInventory(nodes.Items),
		Pods:      buildPodSummaries(pods.Items),
	}
	return snapshot, nil
}

type gpuTotals struct {
	Allocated int
	Available int
}

func buildGPUInventory(nodes []v1.Node) []GPUInventory {
	result := make(map[string]gpuTotals)
	for _, node := range nodes {
		statusAnnotations, _ := gpu.ParseNodeAnnotations(node)
		for _, annotation := range statusAnnotations {
			entry := result[annotation.ProfileName]
			switch annotation.Status {
			case resource.StatusUsed:
				entry.Allocated += annotation.Quantity
			case resource.StatusFree:
				entry.Available += annotation.Quantity
			}
			result[annotation.ProfileName] = entry
		}
	}

	profiles := make([]string, 0, len(result))
	for profile := range result {
		if profile == "" {
			continue
		}
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)

	inventory := make([]GPUInventory, 0, len(profiles))
	for _, profile := range profiles {
		entry := result[profile]
		inventory = append(inventory, GPUInventory{
			Profile:   profile,
			Allocated: entry.Allocated,
			Available: entry.Available,
		})
	}
	return inventory
}

func buildPodSummaries(pods []v1.Pod) []PodSummary {
	summaries := make([]PodSummary, 0)
	for _, pod := range pods {
		profiles := mig.GetRequestedProfiles(pod)
		if len(profiles) == 0 {
			continue
		}
		summaries = append(summaries, PodSummary{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Status:    podStatus(pod),
			GPU:       formatProfiles(profiles),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Namespace == summaries[j].Namespace {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].Namespace < summaries[j].Namespace
	})
	return summaries
}

func podStatus(pod v1.Pod) string {
	if status := containerStatusesReason(pod.Status.ContainerStatuses); status != "" {
		return status
	}
	if status := containerStatusesReason(pod.Status.InitContainerStatuses); status != "" {
		return status
	}
	if phase := pod.Status.Phase; phase != "" {
		return string(phase)
	}
	return "Unknown"
}

func containerStatusesReason(statuses []v1.ContainerStatus) string {
	for _, status := range statuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
			return status.State.Terminated.Reason
		}
		if status.State.Running != nil {
			return "Running"
		}
	}
	return ""
}

func formatProfiles(profiles map[mig.ProfileName]int) string {
	if len(profiles) == 0 {
		return ""
	}
	keys := make([]string, 0, len(profiles))
	for profile := range profiles {
		keys = append(keys, profile.String())
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		name := mig.ProfileName(key)
		qty, ok := profiles[name]
		if !ok {
			continue
		}
		label := key
		if qty > 1 {
			label = fmt.Sprintf("%s x%d", label, qty)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}
