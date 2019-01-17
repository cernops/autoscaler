package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"io"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/config/dynamic"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/cache"
	"os"
	"strings"
	"time"
)

const (
	ProviderName = "openstack"
)

type openstackCloudProvider struct {
	openstackManager *OpenstackManager
	resourceLimiter  *cloudprovider.ResourceLimiter
	nodeGroups       []OpenstackNodeGroup
}

func BuildOpenstackCloudProvider(openstackManager *OpenstackManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	glog.Info("Building openstack cloud provider")
	os := &openstackCloudProvider{
		openstackManager: openstackManager,
		resourceLimiter:  resourceLimiter,
		nodeGroups:       []OpenstackNodeGroup{},
	}
	return os, nil
}

func (os *openstackCloudProvider) Name() string {
	return ProviderName
}

func (os *openstackCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	groups := make([]cloudprovider.NodeGroup, len(os.nodeGroups))
	for i, group := range os.nodeGroups {
		groups[i] = &group
	}
	return groups
}

func (os *openstackCloudProvider) AddNodeGroup(group OpenstackNodeGroup) {
	os.nodeGroups = append(os.nodeGroups, group)
}

func (os *openstackCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {
	return &(os.nodeGroups[0]), nil
}

func (os *openstackCloudProvider) Pricing() (cloudprovider.PricingModel, errors.AutoscalerError) {
	return nil, cloudprovider.ErrNotImplemented
}

func (os *openstackCloudProvider) GetAvailableMachineTypes() ([]string, error) {
	return []string{}, nil
}

func (os *openstackCloudProvider) NewNodeGroup(machineType string, labels map[string]string, systemLabels map[string]string,
	taints []apiv1.Taint, extraResources map[string]resource.Quantity) (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrNotImplemented
}

func (os *openstackCloudProvider) GetResourceLimiter() (*cloudprovider.ResourceLimiter, error) {
	return os.resourceLimiter, nil
}

func (os *openstackCloudProvider) Refresh() error {
	return nil
}

func (os *openstackCloudProvider) Cleanup() error {
	return nil
}

type OpenstackNodeGroup struct {
	openstackManager *OpenstackManager
	id               string
	// TODO: have something like aws' ASG instead of having to store sizes on the manager?
	// They are stored there so that when autoscaler copies the nodegroup it can still update the target size

	kubeClient *kube_client.Clientset
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
// Takes precautions so that the cluster is not modified while in
// an UPDATE_IN_PROGRESS state.
func (ng *OpenstackNodeGroup) IncreaseSize(delta int) error {
	ng.openstackManager.UpdateMutex.Lock()
	defer ng.openstackManager.UpdateMutex.Unlock()

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
	glog.Infof("Increasing size by %d, %d->%d", delta, ng.openstackManager.targetSize, ng.openstackManager.targetSize+delta)
	ng.openstackManager.targetSize += delta

	err = ng.openstackManager.UpdateNodeCount(ng.openstackManager.targetSize)
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

// DeleteNodes deletes a set of nodes by removing them from the heat stack
// and then scaling down the cluster to match the stack's new minion count.
// Takes precautions so that the cluster/stack are not modified while in
// an UPDATE_IN_PROGRESS state.
//
// Simultaneous but separate calls from the autoscaler to delete
// multiple nodes are batched together to scale down as quickly as possible.
func (ng *OpenstackNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {

	// Attempt at batching simultaneous deletes on individual nodes
	ng.openstackManager.nodesToDeleteMutex.Lock()
	ng.openstackManager.nodesToDelete = append(ng.openstackManager.nodesToDelete, nodes...)
	ng.openstackManager.nodesToDeleteMutex.Unlock()

	ng.openstackManager.UpdateMutex.Lock()
	defer ng.openstackManager.UpdateMutex.Unlock()

	if len(ng.openstackManager.nodesToDelete) == 0 {
		// Deletion was handled by another goroutine
		return nil
	}

	time.Sleep(2 * time.Second)

	ng.openstackManager.nodesToDeleteMutex.Lock()
	nodes = make([]*apiv1.Node, len(ng.openstackManager.nodesToDelete))
	copy(nodes, ng.openstackManager.nodesToDelete)
	ng.openstackManager.nodesToDelete = nil
	ng.openstackManager.nodesToDeleteMutex.Unlock()

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

	err = ng.openstackManager.DeleteNodes(minionsToRemove, minionCount-len(nodes))
	if err != nil {
		return fmt.Errorf("error deleting nodes via heat: %v", err)
	}

	// Wait for the stack to do it's thing before updating the cluster node_count
	err = ng.openstackManager.WaitForStackStatus("UPDATE_IN_PROGRESS", WaitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("error waiting for stack UPDATE_IN_PROGRESS: %v", err)
	}
	err = ng.openstackManager.WaitForStackStatus("UPDATE_COMPLETE", WaitForCompleteStatusTimout)
	if err != nil {
		return fmt.Errorf("error waiting for stack UPDATE_COMPLETE: %v", err)
	}

	err = ng.DecreaseTargetSize(-len(nodes))
	if err != nil {
		return fmt.Errorf("could not decrease cluster size: %v", err)
	}

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
// Should be used after removing specific nodes via heat,
// only to correct the node count in magnum to match the heat stack.
func (ng *OpenstackNodeGroup) DecreaseTargetSize(delta int) error {
	if delta >= 0 {
		return fmt.Errorf("size decrease must be negative")
	}
	glog.Infof("Decreasing target size by %d, %d->%d", delta, ng.openstackManager.targetSize, ng.openstackManager.targetSize+delta)
	ng.openstackManager.targetSize += delta
	return ng.openstackManager.UpdateNodeCount(ng.openstackManager.targetSize)
}

func (ng *OpenstackNodeGroup) Id() string {
	return ng.id
}

func (ng *OpenstackNodeGroup) Debug() string {
	return fmt.Sprintf("%s min=%d max=%d target=%d", ng.id, ng.openstackManager.minSize, ng.openstackManager.maxSize, ng.openstackManager.targetSize)
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
	return ng.openstackManager.maxSize
}

func (ng *OpenstackNodeGroup) MinSize() int {
	//glog.Infof("Calling MinSize(), getting %d", ng.minSize)
	return ng.openstackManager.minSize
}

func (ng *OpenstackNodeGroup) TargetSize() (int, error) {
	return ng.openstackManager.targetSize, nil
}

func BuildOpenstack(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) cloudprovider.CloudProvider {
	var config io.ReadCloser

	// Should be loaded with --cloud-config /etc/kubernetes/kube_openstack_config in master node
	if opts.CloudConfig != "" {
		var err error
		config, err = os.Open(opts.CloudConfig)
		if err != nil {
			glog.Fatalf("Couldn't open cloud provider configuration %s: %#v", opts.CloudConfig, err)
		}
		defer config.Close()
	}

	manager, err := CreateOpenstackManager(config, do, opts)
	if err != nil {
		glog.Fatalf("Failed to create openstack manager: %v", err)
	}

	provider, err := BuildOpenstackCloudProvider(manager, rl)
	if err != nil {
		glog.Fatalf("Failed to create openstack cloud provider: %v", err)
	}

	if len(do.NodeGroupSpecs) == 0 {
		glog.Fatalf("Must specify at least one node group with --nodes=<min>:<max>:<name>,...")
	}

	// Temporary, only makes sense to have one nodegroup until magnum nodegroups are implemented
	if len(do.NodeGroupSpecs) > 1 {
		glog.Fatalf("Openstack autoscaler only supports a single nodegroup for now")
	}

	kubeClient := makeKubeClient()
	err = checkNodesAccess(kubeClient)
	if err != nil {
		glog.Fatalf("kubeClient auth error: %v", err)
	}

	for _, nodegroupSpec := range do.NodeGroupSpecs {
		spec, err := dynamic.SpecFromString(nodegroupSpec, true)
		if err != nil {
			glog.Fatalf("Could not parse node group spec %s: %v", nodegroupSpec, err)
		}

		ng := OpenstackNodeGroup{
			openstackManager: manager,
			id:               spec.Name,
			kubeClient:       makeKubeClient(),
		}
		ng.openstackManager.minSize = spec.MinSize
		ng.openstackManager.maxSize = spec.MaxSize
		ng.openstackManager.targetSize, err = ng.openstackManager.CurrentTotalNodes()
		if err != nil {
			glog.Fatalf("Could not set current nodes in node group: %v", err)
		}
		provider.(*openstackCloudProvider).AddNodeGroup(ng)
	}

	return provider
}
