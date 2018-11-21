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
)

const (
	ProviderName = "openstack"
)

type openstackCloudProvider struct {
	openstackManager *OpenstackManager
	resourceLimiter *cloudprovider.ResourceLimiter
}

func BuildOpenstackCloudProvider(openstackManager *OpenstackManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	glog.Info("Building openstack cloud provider")
	os := &openstackCloudProvider{
		openstackManager: openstackManager,
		resourceLimiter: resourceLimiter,
	}
	return os, nil
}

func (os *openstackCloudProvider) Name() string {
	return ProviderName
}

func (os *openstackCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	glog.Info("Getting NodeGroups()")
	groups := []cloudprovider.NodeGroup{}
	ng := &OpenstackNodeGroup{
		openstackManager: os.openstackManager,
		minSize: 0,
		maxSize: 10,
		targetSize: 5,
		id: "Default",
	}
	groups = append(groups, ng)
	return groups
}

func (os *openstackCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {
	glog.Infof("Getting NodeGroupForNode(%s)", node.Name)
	return &OpenstackNodeGroup{
		openstackManager: os.openstackManager,
		minSize: 0,
		maxSize: 10,
		targetSize: 5,
		id: "Default",
	}, nil
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
	glog.Info("Calling Refresh()")
	return nil
}

func (os *openstackCloudProvider) Cleanup() error {
	glog.Info("Calling Cleanup()")
	return nil
}



type OpenstackNodeGroup struct {
	openstackManager *OpenstackManager

	minSize int
	maxSize int
	targetSize int
	id string
}

func (ng OpenstackNodeGroup) IncreaseSize(delta int) error {
	glog.Infof("Increasing size by %d", delta)
	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}

	return nil
}

func (ng OpenstackNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {
	glog.Infof("Deleting nodes: %v", nodes)
	return nil
}

func (ng OpenstackNodeGroup) DecreaseTargetSize(delta int) error {
	glog.Infof("Decreasing target size by %d", delta)
	return nil
}

func (ng OpenstackNodeGroup) Id() string {
	return ng.id
}

func (ng OpenstackNodeGroup) Debug() string {
	return fmt.Sprintf("%s min=%d max=%d target=%d", ng.id, ng.minSize, ng.maxSize, ng.targetSize)
}

func (ng OpenstackNodeGroup) Nodes() ([]cloudprovider.Instance, error) {
	glog.Info("Getting Nodes()")
	return []cloudprovider.Instance{
		cloudprovider.Instance{Id: "scaler-01-lsrc3drq5vy5-minion-0"},
	}, nil
}

func (ng OpenstackNodeGroup) TemplateNodeInfo() (*cache.NodeInfo, error) {
	glog.Info("Getting TemplateNodeInfo()")
	return &cache.NodeInfo{}, nil
}

func (ng OpenstackNodeGroup) Exist() bool {
	return true
}

func (ng OpenstackNodeGroup) Create() (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrAlreadyExist
}

func (ng OpenstackNodeGroup) Delete() error {
	return cloudprovider.ErrNotImplemented
}

func (ng OpenstackNodeGroup) Autoprovisioned() bool {
	return false
}

func (ng OpenstackNodeGroup) MaxSize() int {
	glog.Info("Calling MaxSize()")
	return ng.maxSize
}

func (ng OpenstackNodeGroup) MinSize() int {
	glog.Info("Calling MinSize()")
	return ng.minSize
}

func (ng OpenstackNodeGroup) TargetSize() (int, error) {
	glog.Info("Calling TargetSize()")
	return ng.targetSize, nil
}






func BuildOpenstack(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) cloudprovider.CloudProvider {
	var config io.ReadCloser

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