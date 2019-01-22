package openstack

import (
	"fmt"
	"github.com/golang/glog"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/cache"
	"strings"
	"time"
	"sync"
)


type OpenstackNodeGroup struct {
	openstackManager OpenstackManager
	id               string

	kubeClient *kube_client.Clientset


	clusterUpdateMutex *sync.Mutex

	// Stored as pointers so that when autoscaler copies the nodegroup it can still update the target size
	minSize *int
	maxSize *int
	targetSize *int

	nodesToDelete      []*apiv1.Node
	nodesToDeleteMutex sync.Mutex
}

// WaitForClusterStatus checks once per second to see if the cluster has entered a given status.
// Returns when the status is observed or the timeout is reached.
func (ng *OpenstackNodeGroup) WaitForClusterStatus(status string, timeout time.Duration) error {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(time.Second) {
		clusterStatus, err := ng.openstackManager.GetClusterStatus()
		if err != nil {
			return fmt.Errorf("error waiting for %s status: %v", status, err)
		}
		if clusterStatus == status {
			glog.Infof("Waited for cluster %s status, took %d seconds", status, int(time.Since(start).Seconds()))
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for %s status", status)
}

// IncreaseSize increases the number of nodes by replacing the cluster's node_count.
//
// Takes precautions so that the cluster is not modified while in  an UPDATE_IN_PROGRESS state.
// Blocks until the cluster has reached UPDATE_COMPLETE.
func (ng *OpenstackNodeGroup) IncreaseSize(delta int) error {
	ng.clusterUpdateMutex.Lock()
	defer ng.clusterUpdateMutex.Unlock()

	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}
	updatePossible, currentStatus, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("can not increase node count: %v", err)
	}
	if !updatePossible {
		return fmt.Errorf("can not add nodes, cluster is in %s status", currentStatus)
	}
	glog.Infof("Increasing size by %d, %d->%d", delta, *ng.targetSize, *ng.targetSize+delta)
	*ng.targetSize += delta

	err = ng.openstackManager.UpdateNodeCount(*ng.targetSize)
	if err != nil {
		return fmt.Errorf("could not increase cluster size: %v", err)
	}

	// Block until cluster has gone into update status and then completed
	err = ng.WaitForClusterStatus(ClusterStatusUpdateInProgress, WaitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("scale up error: %v", err)
	}
	err = ng.WaitForClusterStatus(ClusterStatusUpdateComplete, WaitForCompleteStatusTimout)
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

	// Attempt at batching simultaneous deletes on individual nodes
	ng.nodesToDeleteMutex.Lock()
	ng.nodesToDelete = append(ng.nodesToDelete, nodes...)
	ng.nodesToDeleteMutex.Unlock()

	ng.clusterUpdateMutex.Lock()
	defer ng.clusterUpdateMutex.Unlock()

	if len(ng.nodesToDelete) == 0 {
		// Deletion was handled by another goroutine
		return nil
	}

	time.Sleep(2 * time.Second)

	ng.nodesToDeleteMutex.Lock()
	nodes = make([]*apiv1.Node, len(ng.nodesToDelete))
	copy(nodes, ng.nodesToDelete)
	ng.nodesToDelete = nil
	ng.nodesToDeleteMutex.Unlock()

	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	glog.Infof("Deleting nodes: %v", nodeNames)

	updatePossible, currentStatus, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("could not check if cluster is ready to delete nodes: %v", err)
	}
	if !updatePossible {
		return fmt.Errorf("can not delete nodes, cluster is in %s status", currentStatus)
	}

	minionCount, err := ng.openstackManager.CurrentTotalNodes()
	if err != nil {
		return fmt.Errorf("could not get current node count")
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

	minionsToRemove := strings.Join(minionIPs, ",")

	newNodeCount, err := ng.openstackManager.DeleteNodes(minionsToRemove, minionCount-len(nodes))
	if err != nil {
		return fmt.Errorf("manager error deleting nodes: %v", err)
	}

	*ng.targetSize = newNodeCount

	// Block until cluster has gone into update status and then completed
	err = ng.WaitForClusterStatus(ClusterStatusUpdateInProgress, WaitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("scale down error: %v", err)
	}
	err = ng.WaitForClusterStatus(ClusterStatusUpdateComplete, WaitForCompleteStatusTimout)
	if err != nil {
		return fmt.Errorf("scale down error: %v", err)
	}

	// Delete nodes from kubernetes node list after scale down is complete
	return cleanupNodes(ng.kubeClient, nodeNames)
}

// DecreaseTargetSize decreases the cluster node_count in magnum.
func (ng *OpenstackNodeGroup) DecreaseTargetSize(delta int) error {
	if delta >= 0 {
		return fmt.Errorf("size decrease must be negative")
	}
	glog.Infof("Decreasing target size by %d, %d->%d", delta, *ng.targetSize, *ng.targetSize+delta)
	*ng.targetSize += delta
	return ng.openstackManager.UpdateNodeCount(*ng.targetSize)
}

func (ng *OpenstackNodeGroup) Id() string {
	return ng.id
}

func (ng *OpenstackNodeGroup) Debug() string {
	return fmt.Sprintf("%s min=%d max=%d target=%d", ng.id, *ng.minSize, *ng.maxSize, *ng.targetSize)
}

func (ng *OpenstackNodeGroup) Nodes() ([]cloudprovider.Instance, error) {
	nodes, err := ng.openstackManager.GetNodes()
	if err != nil {
		return nil, fmt.Errorf("could not get nodes: %v", err)
	}
	var instances []cloudprovider.Instance
	for _, node := range nodes {
		instances = append(instances, cloudprovider.Instance{Id: node})
	}
	return instances, nil
}

func (ng *OpenstackNodeGroup) TemplateNodeInfo() (*cache.NodeInfo, error) {
	return &cache.NodeInfo{}, nil
}

func (ng *OpenstackNodeGroup) Exist() bool {
	return true
}

func (ng *OpenstackNodeGroup) Create() (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrAlreadyExist
}

func (ng *OpenstackNodeGroup) Delete() error {
	return cloudprovider.ErrNotImplemented
}

func (ng *OpenstackNodeGroup) Autoprovisioned() bool {
	return false
}

func (ng *OpenstackNodeGroup) MaxSize() int {
	//glog.Infof("Calling MaxSize(), getting %d", ng.maxSize)
	return *ng.maxSize
}

func (ng *OpenstackNodeGroup) MinSize() int {
	//glog.Infof("Calling MinSize(), getting %d", ng.minSize)
	return *ng.minSize
}

func (ng *OpenstackNodeGroup) TargetSize() (int, error) {
	return *ng.targetSize, nil
}
