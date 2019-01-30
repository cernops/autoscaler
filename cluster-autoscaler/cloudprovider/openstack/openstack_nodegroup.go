/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package openstack

import (
	"fmt"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/cache"
	"sync"
	"time"
)

// OpenstackNodeGroup implements NodeGroup interface from cluster-autoscaler/cloudprovider.
//
// Represents a homogeneous collection of nodes within a cluster,
// which can be dynamically resized between a minimum and maximum
// number of nodes.
type OpenstackNodeGroup struct {
	openstackManager OpenstackManager
	id               string

	clusterUpdateMutex *sync.Mutex

	minSize int
	maxSize int
	// Stored as a pointer so that when autoscaler copies the nodegroup it can still update the target size
	targetSize *int

	nodesToDelete      []*apiv1.Node
	nodesToDeleteMutex sync.Mutex
}

// WaitForClusterStatus checks once per second to see if the cluster has entered a given status.
// Returns when the status is observed or the timeout is reached.
func (ng *OpenstackNodeGroup) WaitForClusterStatus(status string, timeout time.Duration) error {
	klog.V(2).Infof("Waiting for cluster %s status", status)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(time.Second) {
		clusterStatus, err := ng.openstackManager.GetClusterStatus()
		if err != nil {
			return fmt.Errorf("error waiting for %s status: %v", status, err)
		}
		if clusterStatus == status {
			klog.V(0).Infof("Waited for cluster %s status, took %d seconds", status, int(time.Since(start).Seconds()))
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for %s status", status)
}

// IncreaseSize increases the number of nodes by replacing the cluster's node_count.
//
// Takes precautions so that the cluster is not modified while in an UPDATE_IN_PROGRESS state.
// Blocks until the cluster has reached UPDATE_COMPLETE.
func (ng *OpenstackNodeGroup) IncreaseSize(delta int) error {
	ng.clusterUpdateMutex.Lock()
	defer ng.clusterUpdateMutex.Unlock()

	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}

	size, err := ng.openstackManager.NodeGroupSize(ng.id)
	if err != nil {
		return fmt.Errorf("could not check current nodegroup size: %v", err)
	}
	if size+delta > ng.MaxSize() {
		return fmt.Errorf("size increase too large, desired:%d max:%d", size+delta, ng.MaxSize())
	}

	updatePossible, currentStatus, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("can not increase node count: %v", err)
	}
	if !updatePossible {
		return fmt.Errorf("can not add nodes, cluster is in %s status", currentStatus)
	}
	klog.V(0).Infof("Increasing size by %d, %d->%d", delta, *ng.targetSize, *ng.targetSize+delta)
	*ng.targetSize += delta

	err = ng.openstackManager.UpdateNodeCount(ng.id, *ng.targetSize)
	if err != nil {
		return fmt.Errorf("could not increase cluster size: %v", err)
	}

	// Block until cluster has gone into update status and then completed
	err = ng.WaitForClusterStatus(clusterStatusUpdateInProgress, waitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("scale up error: %v", err)
	}
	err = ng.WaitForClusterStatus(clusterStatusUpdateComplete, waitForCompleteStatusTimout)
	if err != nil {
		return fmt.Errorf("scale up error: %v", err)
	}
	return nil
}

// DeleteNodes deletes a set of nodes chosen by the autoscaler.
//
// The process of deletion depends on the implementation of OpenstackManager,
// but this function handles what should be common between all implementations:
//   - simultaneous but separate calls from the autoscaler are batched together
//   - does not allow scaling while the cluster is already in an UPDATE_IN_PROGRESS state
//   - after scaling down, blocks until the cluster has reached UPDATE_COMPLETE
func (ng *OpenstackNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {

	// Batch simultaneous deletes on individual nodes
	ng.nodesToDeleteMutex.Lock()
	// Check that these nodes would not make the batch delete more nodes than the minimum would allow
	if *ng.targetSize-len(ng.nodesToDelete)-len(nodes) < ng.MinSize() {
		ng.nodesToDeleteMutex.Unlock()
		return fmt.Errorf("deleting nodes would take nodegroup below minimum size")
	}
	ng.nodesToDelete = append(ng.nodesToDelete, nodes...)
	ng.nodesToDeleteMutex.Unlock()

	ng.clusterUpdateMutex.Lock()
	defer ng.clusterUpdateMutex.Unlock()

	ng.nodesToDeleteMutex.Lock()
	if len(ng.nodesToDelete) == 0 {
		// Deletion was handled by another goroutine
		ng.nodesToDeleteMutex.Unlock()
		return nil
	}
	ng.nodesToDeleteMutex.Unlock()

	// This goroutine has the clusterUpdateMutex, so will be the one
	// to actually delete the nodes. While this goroutine waits, others
	// will add their nodes to nodesToDelete and block at acquiring
	// the clusterUpdateMutex lock. One they get it, the deletion will
	// already be done and they will return above at the check
	// for len(ng.nodesToDelete) == 0.
	time.Sleep(deleteNodesBatchingDelay)

	ng.nodesToDeleteMutex.Lock()
	nodes = make([]*apiv1.Node, len(ng.nodesToDelete))
	copy(nodes, ng.nodesToDelete)
	ng.nodesToDelete = nil
	ng.nodesToDeleteMutex.Unlock()

	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	klog.V(1).Infof("Deleting nodes: %v", nodeNames)

	updatePossible, currentStatus, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("could not check if cluster is ready to delete nodes: %v", err)
	}
	if !updatePossible {
		return fmt.Errorf("can not delete nodes, cluster is in %s status", currentStatus)
	}

	minionCount, err := ng.openstackManager.NodeGroupSize(ng.id)
	if err != nil {
		return fmt.Errorf("could not get current node count")
	}

	if minionCount-len(nodes) < ng.MinSize() {
		return fmt.Errorf("size decrease too large, desired:%d min:%d", minionCount-len(nodes), ng.MinSize())
	}

	var minionIPs []string
	for _, node := range nodes {
		for _, addr := range node.Status.Addresses {
			if addr.Type == apiv1.NodeInternalIP {
				minionIPs = append(minionIPs, addr.Address)
				break
			}
		}
	}

	err = ng.openstackManager.DeleteNodes(ng.id, minionIPs, minionCount-len(nodes))
	if err != nil {
		return fmt.Errorf("manager error deleting nodes: %v", err)
	}

	// Block until cluster has gone into update status and then completed
	err = ng.WaitForClusterStatus(clusterStatusUpdateInProgress, waitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("scale down error: %v", err)
	}
	err = ng.WaitForClusterStatus(clusterStatusUpdateComplete, waitForCompleteStatusTimout)
	if err != nil {
		return fmt.Errorf("scale down error: %v", err)
	}

	*ng.targetSize, err = ng.openstackManager.NodeGroupSize(ng.id)
	if err != nil {
		return fmt.Errorf("could not update target size after scale down: %v", err)
	}

	return nil
}

// DecreaseTargetSize decreases the cluster node_count in magnum.
func (ng *OpenstackNodeGroup) DecreaseTargetSize(delta int) error {
	if delta >= 0 {
		return fmt.Errorf("size decrease must be negative")
	}
	klog.V(0).Infof("Decreasing target size by %d, %d->%d", delta, *ng.targetSize, *ng.targetSize+delta)
	*ng.targetSize += delta
	return ng.openstackManager.UpdateNodeCount(ng.id, *ng.targetSize)
}

// Id returns the node group ID
func (ng *OpenstackNodeGroup) Id() string {
	return ng.id
}

// Debug returns a string formatted with the node group's min, max and target sizes.
func (ng *OpenstackNodeGroup) Debug() string {
	return fmt.Sprintf("%s min=%d max=%d target=%d", ng.id, ng.minSize, ng.maxSize, *ng.targetSize)
}

// Nodes returns a list of nodes that belong to this node group.
func (ng *OpenstackNodeGroup) Nodes() ([]cloudprovider.Instance, error) {
	nodes, err := ng.openstackManager.GetNodes(ng.id)
	if err != nil {
		return nil, fmt.Errorf("could not get nodes: %v", err)
	}
	var instances []cloudprovider.Instance
	for _, node := range nodes {
		instances = append(instances, cloudprovider.Instance{Id: node})
	}
	return instances, nil
}

// TemplateNodeInfo returns a node template for this node group.
// Currently not implemented.
func (ng *OpenstackNodeGroup) TemplateNodeInfo() (*cache.NodeInfo, error) {
	return &cache.NodeInfo{}, nil
}

// Exist returns if this node group exists.
// Currently always returns true.
func (ng *OpenstackNodeGroup) Exist() bool {
	return true
}

// Create creates the node group on the cloud provider side.
func (ng *OpenstackNodeGroup) Create() (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrAlreadyExist
}

// Delete deletes the node group on the cloud provider side.
func (ng *OpenstackNodeGroup) Delete() error {
	return cloudprovider.ErrNotImplemented
}

// Autoprovisioned returns if the nodegroup is autoprovisioned.
func (ng *OpenstackNodeGroup) Autoprovisioned() bool {
	return false
}

// MaxSize returns the maximum allowed size of the node group.
func (ng *OpenstackNodeGroup) MaxSize() int {
	return ng.maxSize
}

// MinSize returns the minimum allowed size of the node group.
func (ng *OpenstackNodeGroup) MinSize() int {
	return ng.minSize
}

// TargetSize returns the target size of the node group.
func (ng *OpenstackNodeGroup) TargetSize() (int, error) {
	return *ng.targetSize, nil
}
