package openstack

import (
	"github.com/golang/glog"
	"io"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/config/dynamic"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	"os"
	"sync"
)

const (
	ProviderName = "openstack"
)


// OpenstackNodeGroup implements CloudProvider interface from cluster-autoscaler/cloudprovider module.
type openstackCloudProvider struct {
	openstackManager *OpenstackManager
	resourceLimiter  *cloudprovider.ResourceLimiter
	nodeGroups       []OpenstackNodeGroup
}

func BuildOpenstackCloudProvider(openstackManager OpenstackManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	glog.Info("Building openstack cloud provider")
	os := &openstackCloudProvider{
		openstackManager: &openstackManager,
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
	glog.Infof(os.nodeGroups[0].Debug())
	return nil
}

func (os *openstackCloudProvider) Cleanup() error {
	return nil
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


	clusterUpdateLock := sync.Mutex{}

	for _, nodegroupSpec := range do.NodeGroupSpecs {
		spec, err := dynamic.SpecFromString(nodegroupSpec, scaleToZeroSupported)
		if err != nil {
			glog.Fatalf("Could not parse node group spec %s: %v", nodegroupSpec, err)
		}

		ng := OpenstackNodeGroup{
			openstackManager: manager,
			id:               spec.Name,
			kubeClient:       makeKubeClient(),
			clusterUpdateMutex: &clusterUpdateLock,
			minSize: &spec.MinSize,
			maxSize: &spec.MaxSize,
			targetSize: new(int),
		}
		*ng.targetSize, err = ng.openstackManager.CurrentTotalNodes()
		if err != nil {
			glog.Fatalf("Could not set current nodes in node group: %v", err)
		}
		provider.(*openstackCloudProvider).AddNodeGroup(ng)
	}

	return provider
}
