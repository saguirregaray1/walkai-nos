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

package gpupartitioner

import (
	"context"
	partitionermig "github.com/nebuly-ai/nos/internal/partitioning/mig"
	"github.com/nebuly-ai/nos/pkg/api/nos.nebuly.com/v1alpha1"
	"github.com/nebuly-ai/nos/pkg/gpu"
	gpumig "github.com/nebuly-ai/nos/pkg/gpu/mig"
	podutil "github.com/nebuly-ai/nos/pkg/util/pod"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Controller reacts to pending Pods that request MIG resources and, when none of the MIG-enabled nodes currently
// expose the requested MIG profile, it updates the node partitioning so that the profile becomes available.
type Controller struct {
	client.Client
	Scheme       *runtime.Scheme
	partitioner  *partitionermig.Partitioner
	planIDSource func() string
}

func NewController(client client.Client, scheme *runtime.Scheme) *Controller {
	return &Controller{
		Client:       client,
		Scheme:       scheme,
		partitioner:  partitionermig.NewPartitioner(client),
		planIDSource: partitionermig.NewPartitioningPlanID,
	}
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;patch

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pod v1.Pod
	if err := c.Get(ctx, client.ObjectKey{Name: req.Name, Namespace: req.Namespace}, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !c.shouldConsiderPod(pod) {
		return ctrl.Result{}, nil
	}

	requestedProfiles := gpumig.GetRequestedProfiles(pod)
	if len(requestedProfiles) == 0 {
		return ctrl.Result{}, nil
	}

	nodes, err := c.listMigPartitionedNodes(ctx)
	if err != nil {
		logger.Error(err, "unable to list MIG partitioned nodes")
		return ctrl.Result{}, err
	}
	if len(nodes) == 0 {
		logger.V(1).Info("no MIG partitioned nodes found, nothing to do")
		return ctrl.Result{}, nil
	}

	if c.profileAlreadyPresent(nodes, requestedProfiles) {
		logger.V(1).Info("requested MIG profiles already present in the cluster, skipping", "profiles", requestedProfiles)
		return ctrl.Result{}, nil
	}

	updated, err := c.tryRepartition(ctx, nodes, requestedProfiles)
	if err != nil {
		logger.Error(err, "failed to update MIG partitioning")
		return ctrl.Result{}, err
	}
	if !updated {
		logger.Info("unable to find a node that can provide requested MIG profiles", "profiles", requestedProfiles)
	}

	return ctrl.Result{}, nil
}

func (c *Controller) shouldConsiderPod(pod v1.Pod) bool {
	if !podutil.IsPending(pod) {
		return false
	}
	if podutil.IsScheduled(pod) {
		return false
	}
	if !podutil.IsUnschedulable(pod) {
		return false
	}
	return true
}

func (c *Controller) listMigPartitionedNodes(ctx context.Context) ([]v1.Node, error) {
	var nodeList v1.NodeList
	if err := c.List(ctx, &nodeList, client.MatchingLabels{v1alpha1.LabelGpuPartitioning: gpu.PartitioningKindMig.String()}); err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (c *Controller) profileAlreadyPresent(nodes []v1.Node, requested map[gpumig.ProfileName]int) bool {
	for profile := range requested {
		if c.profilePresentOnAnyNode(nodes, profile) {
			continue
		}
		return false
	}
	return true
}

func (c *Controller) profilePresentOnAnyNode(nodes []v1.Node, profile gpumig.ProfileName) bool {
	for _, node := range nodes {
		nodeInfo := framework.NewNodeInfo()
		nodeInfo.SetNode(node.DeepCopy())
		migNode, err := gpumig.NewNode(*nodeInfo)
		if err != nil {
			continue
		}
		if migNode.Geometry()[profile] > 0 {
			return true
		}
	}
	return false
}

func (c *Controller) tryRepartition(
	ctx context.Context,
	nodes []v1.Node,
	requested map[gpumig.ProfileName]int,
) (bool, error) {
	logger := log.FromContext(ctx)

	for _, node := range nodes {
		updated, err := c.updateNodeGeometry(ctx, node, requested)
		if err != nil {
			logger.Error(err, "failed updating node MIG geometry", "node", node.Name)
			continue
		}
		if updated {
			logger.Info("updated MIG partitioning", "node", node.Name)
			return true, nil
		}
	}

	return false, nil
}

func (c *Controller) updateNodeGeometry(
	ctx context.Context,
	node v1.Node,
	requested map[gpumig.ProfileName]int,
) (bool, error) {
	nodeInfo := framework.NewNodeInfo()
	nodeInfo.SetNode(node.DeepCopy())
	migNode, err := gpumig.NewNode(*nodeInfo)
	if err != nil {
		return false, err
	}

	requiredSlices := make(map[gpu.Slice]int, len(requested))
	for profile, quantity := range requested {
		requiredSlices[profile] = quantity
	}

	anyUpdated, err := migNode.UpdateGeometryFor(requiredSlices)
	if err != nil || !anyUpdated {
		return false, err
	}

	partitioning := partitionermig.BuildNodePartitioning(migNode)
	planID := c.planIDSource()

	if err := c.partitioner.ApplyPartitioning(ctx, node, planID, partitioning); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager, name string) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1.Pod{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(c)
}

// InjectFunc allows to override the plan ID generator for tests.
func (c *Controller) InjectFunc(f func() string) {
	if f != nil {
		c.planIDSource = f
	}
}
