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
	"io"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/config/dynamic"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	"k8s.io/klog"
	"os"
	"sync"
)

const (
	// ProviderName is the cloud provider name for OpenStack
	ProviderName = "openstack"
)

// OpenstackNodeGroup implements CloudProvider interface from cluster-autoscaler/cloudprovider module.
type openstackCloudProvider struct {
	openstackManager *OpenstackManager
	resourceLimiter  *cloudprovider.ResourceLimiter
	nodeGroups       []OpenstackNodeGroup
}

func buildOpenstackCloudProvider(openstackManager OpenstackManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	os := &openstackCloudProvider{
		openstackManager: &openstackManager,
		resourceLimiter:  resourceLimiter,
		nodeGroups:       []OpenstackNodeGroup{},
	}
	return os, nil
}

// Name returns the name of the cloud provider.
func (os *openstackCloudProvider) Name() string {
	return ProviderName
}

// NodeGroups returns all node groups managed by this cloud provider.
func (os *openstackCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	groups := make([]cloudprovider.NodeGroup, len(os.nodeGroups))
	for i, group := range os.nodeGroups {
		groups[i] = &group
	}
	return groups
}

// AddNodeGroup appends a node group to the list of node groups managed by this cloud provider.
func (os *openstackCloudProvider) AddNodeGroup(group OpenstackNodeGroup) {
	os.nodeGroups = append(os.nodeGroups, group)
}

// NodeGroupForNode returns the node group that a given node belongs to.
//
// Since only a single node group is currently supported, the first node group is always returned.
func (os *openstackCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {
	// TODO: wait for magnum nodegroup support
	return &(os.nodeGroups[0]), nil
}

// Pricing is not implemented.
func (os *openstackCloudProvider) Pricing() (cloudprovider.PricingModel, errors.AutoscalerError) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetAvailableMachineTypes is not implemented.
func (os *openstackCloudProvider) GetAvailableMachineTypes() ([]string, error) {
	return []string{}, nil
}

// NewNodeGroup is not implemented.
func (os *openstackCloudProvider) NewNodeGroup(machineType string, labels map[string]string, systemLabels map[string]string,
	taints []apiv1.Taint, extraResources map[string]resource.Quantity) (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetResourceLimiter returns resource constraints for the cloud provider
func (os *openstackCloudProvider) GetResourceLimiter() (*cloudprovider.ResourceLimiter, error) {
	return os.resourceLimiter, nil
}

// Refresh is called before every autoscaler main loop.
//
// Currently only prints debug information.
func (os *openstackCloudProvider) Refresh() error {
	for _, nodegroup := range os.nodeGroups {
		klog.V(3).Info(nodegroup.Debug())
	}
	return nil
}

// Cleanup currently does nothing.
func (os *openstackCloudProvider) Cleanup() error {
	return nil
}

// BuildOpenstack is called by the autoscaler to build an openstack cloud provider.
//
// The OpenstackManager is created here, and the node groups are created
// based on the specs provided via the command line parameters.
func BuildOpenstack(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) cloudprovider.CloudProvider {
	var config io.ReadCloser

	// Should be loaded with --cloud-config /etc/kubernetes/kube_openstack_config from master node
	if opts.CloudConfig != "" {
		var err error
		config, err = os.Open(opts.CloudConfig)
		if err != nil {
			klog.Fatalf("Couldn't open cloud provider configuration %s: %#v", opts.CloudConfig, err)
		}
		defer config.Close()
	}

	manager, err := CreateOpenstackManager(config, do, opts)
	if err != nil {
		klog.Fatalf("Failed to create openstack manager: %v", err)
	}

	provider, err := buildOpenstackCloudProvider(manager, rl)
	if err != nil {
		klog.Fatalf("Failed to create openstack cloud provider: %v", err)
	}

	if len(do.NodeGroupSpecs) == 0 {
		klog.Fatalf("Must specify at least one node group with --nodes=<min>:<max>:<name>,...")
	}

	// Temporary, only makes sense to have one nodegroup until magnum nodegroups are implemented
	if len(do.NodeGroupSpecs) > 1 {
		klog.Fatalf("Openstack autoscaler only supports a single nodegroup for now")
	}

	clusterUpdateLock := sync.Mutex{}

	for _, nodegroupSpec := range do.NodeGroupSpecs {
		spec, err := dynamic.SpecFromString(nodegroupSpec, scaleToZeroSupported)
		if err != nil {
			klog.Fatalf("Could not parse node group spec %s: %v", nodegroupSpec, err)
		}

		ng := OpenstackNodeGroup{
			openstackManager:   manager,
			id:                 spec.Name,
			clusterUpdateMutex: &clusterUpdateLock,
			minSize:            spec.MinSize,
			maxSize:            spec.MaxSize,
			targetSize:         new(int),
			waitTimeStep:       waitForStatusTimeStep,
		}
		*ng.targetSize, err = ng.openstackManager.NodeGroupSize(ng.id)
		if err != nil {
			klog.Fatalf("Could not set current nodes in node group: %v", err)
		}
		provider.(*openstackCloudProvider).AddNodeGroup(ng)
	}

	return provider
}
