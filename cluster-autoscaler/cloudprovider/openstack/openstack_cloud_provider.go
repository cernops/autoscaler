package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"io"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	"k8s.io/kubernetes/pkg/scheduler/cache"
	"os"
	"strings"
	"time"
)

const (
	ProviderName = "openstack"
	WaitForUpdateStateTimeout = 30
)

type openstackCloudProvider struct {
	openstackManager *OpenstackManager
	resourceLimiter *cloudprovider.ResourceLimiter
	nodeGroups []OpenstackNodeGroup
}

func BuildOpenstackCloudProvider(openstackManager *OpenstackManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	glog.Info("Building openstack cloud provider")
	os := &openstackCloudProvider{
		openstackManager: openstackManager,
		resourceLimiter: resourceLimiter,
		nodeGroups: make([]OpenstackNodeGroup, 1),
	}
	os.nodeGroups[0] = OpenstackNodeGroup{
		openstackManager: os.openstackManager,
		id: "Default",
	}

	os.nodeGroups[0].openstackManager.minSize = 1
	os.nodeGroups[0].openstackManager.maxSize = 10

	nodes, err := os.openstackManager.CurrentTotalNodes()
	if err != nil {
		return nil, fmt.Errorf("could not get current number of nodes: %v", err)
	}
	os.nodeGroups[0].openstackManager.targetSize = nodes
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
	//glog.Info("Calling Refresh()")
	/*nodes, err := os.openstackManager.CurrentTotalNodes()
	if err != nil {
		return err
	}
	glog.Infof("Current nodes: %d", nodes)
	os.nodeGroups[0].targetSize = nodes*/
	return nil
}

func (os *openstackCloudProvider) Cleanup() error {
	return nil
}


type OpenstackNodeGroup struct {
	openstackManager *OpenstackManager
	id string
	// TODO: have something like aws' ASG instead of having to store sizes on the manager?
	// They are stored there so that when autoscaler copies the nodegroup it can still update the target size
}

func (ng *OpenstackNodeGroup) WaitForUpdateState(timeout int) error {
	var i int
	for i=0; i<timeout; i++ {
		time.Sleep(time.Second)
		clusterStatus, err := ng.openstackManager.GetClusterStatus()
		if err != nil {
			return fmt.Errorf("error waiting for update state: %v", err)
		}
		if clusterStatus == StatusUpdateInProgress {
			glog.Infof("Waited for update state, took %d seconds", i)
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for update state")
}

func (ng *OpenstackNodeGroup) IncreaseSize(delta int) error {
	ng.openstackManager.UpdateMutex.Lock()
	defer ng.openstackManager.UpdateMutex.Unlock()

	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}
	possible, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("can not increase node count: %v", err)
	}
	if !possible {
		return fmt.Errorf("can not increase node count, cluster is already updating")
	}
	glog.Infof("Increasing size by %d, %d->%d", delta, ng.openstackManager.targetSize, ng.openstackManager.targetSize+delta)
	ng.openstackManager.targetSize += delta

	err = ng.openstackManager.UpdateNodeCount(ng.openstackManager.targetSize)
	if err != nil {
		return fmt.Errorf("could not increase cluster size: %v", err)
	}

	// Keep holding the mutex lock so that the cluster status can change to UPDATE_IN_PROGRESS
	return ng.WaitForUpdateState(WaitForUpdateStateTimeout)
}


func (ng *OpenstackNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {
	// TODO: wait for magnum support for scaling down by deleting specific nodes
	// Until then we are deleting the VM via nova client and scaling down.
	// This leads to issues with multiple nodes being deleted at once though
	glog.Info("Deleting nodes, acquiring lock")
	ng.openstackManager.UpdateMutex.Lock()
	defer ng.openstackManager.UpdateMutex.Unlock()

	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	glog.Infof("Deleting nodes: %v", nodeNames)

	canDelete, err := ng.openstackManager.CanUpdate()
	if err != nil {
		return fmt.Errorf("could not check if cluster is ready to delete nodes: %v", err)
	}
	if !canDelete {
		return fmt.Errorf("can not decrease node count, cluster is already updating")
	}

	/*for _, node := range nodes {
		err := ng.openstackManager.DeleteNode(node.Status.NodeInfo.SystemUUID)
		if err != nil {
			return fmt.Errorf("could not delete node %s: %v", node.Status.NodeInfo.SystemUUID, err)
		}
	}*/

	minionCount, err := ng.openstackManager.CurrentTotalNodes()
	if err != nil {
		return fmt.Errorf("could not get current node count")
	}

	var minionIPs []string
	for _, node := range nodes {
		for _, addr := range node.Status.Addresses {
			glog.Infof("Node address: %#v", addr)
			if addr.Type == apiv1.NodeInternalIP {
				minionIPs = append(minionIPs, addr.Address)
			}
		}
	}

	minionsToRemove := strings.Join(minionIPs, ",")

	err = ng.openstackManager.DeleteViaHeat(minionsToRemove, minionCount-len(nodes))
	if err != nil {
		return fmt.Errorf("error deleting nodes via heat: %v", err)
	}

	// Wait for the stack to do it's thing before updating the cluster node_count
	err = ng.openstackManager.WaitForStackStatus("UPDATE_IN_PROGRESS")
	if err != nil {
		return fmt.Errorf("error waiting for stack UPDATE_IN_PROGRESS: %v", err)
	}
	err = ng.openstackManager.WaitForStackStatus("UPDATE_COMPLETE")
	if err != nil {
		return fmt.Errorf("error waiting for stack UPDATE_COMPLETE: %v", err)
	}

	/*for _, node := range nodes {
		minionCount -= 1
		err := ng.openstackManager.DeleteViaHeat(node.GetName(), minionCount)
		if err != nil {
			return fmt.Errorf("could not delete node %s: %v", node.Status.NodeInfo.SystemUUID, err)
		}
	}*/


	/*err = ng.DecreaseTargetSize(-len(nodes))
	if err != nil {
		return fmt.Errorf("could not decrease cluster size: %v", err)
	}*/

	// Keep holding the mutex lock so that the cluster status can change to UPDATE_IN_PROGRESS
	return ng.WaitForUpdateState(WaitForUpdateStateTimeout)
}


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
	// TODO: manager.GetNodes() does not return anything yet
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
	glog.Infof("Calling TargetSize(), getting %d", ng.openstackManager.targetSize)
	return ng.openstackManager.targetSize, nil
}


type OpenstackRef struct {
	Name string
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

	manager, err := CreateOpenstackManager(config, do)
	if err != nil {
		glog.Fatalf("Failed to create openstack manager: %v", err)
	}

	provider, err := BuildOpenstackCloudProvider(manager, rl)
	if err != nil {
		glog.Fatalf("Failed to create openstack cloud provider: %v", err)
	}
	return provider
}