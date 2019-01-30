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
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/klog"
	"time"

	"github.com/gophercloud/gophercloud/openstack/orchestration/v1/stacks"
	"gopkg.in/gcfg.v1"
	"io"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"
	"strings"
)

const (
	stackStatusUpdateInProgress = "UPDATE_IN_PROGRESS"
	stackStatusUpdateComplete   = "UPDATE_COMPLETE"
)

// StatusesPreventingUpdate is a set of statuses that would prevent
// the cluster from successfully scaling.
var StatusesPreventingUpdate = sets.NewString(
	clusterStatusUpdateInProgress,
	clusterStatusUpdateFailed,
)

// OpenstackManagerHeat implements the OpenstackManager interface.
//
// Most interactions with the cluster are done directly with magnum,
// but scaling down requires an intermediate step using heat to
// delete the specific nodes that the autoscaler has picked for removal.
type OpenstackManagerHeat struct {
	clusterClient *gophercloud.ServiceClient
	heatClient    *gophercloud.ServiceClient
	clusterName   string

	stackName string
	stackID   string
}

// CreateOpenstackManagerHeat sets up cluster and stack clients and returns
// an OpenstackManagerHeat.
func CreateOpenstackManagerHeat(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions, opts config.AutoscalingOptions) (*OpenstackManagerHeat, error) {
	var cfg provider_os.Config
	if configReader != nil {
		if err := gcfg.ReadInto(&cfg, configReader); err != nil {
			klog.Errorf("Couldn't read config: %v", err)
			return nil, err
		}
	}

	if opts.ClusterName == "" {
		klog.Fatalf("The cluster-name parameter must be set")
	}

	authOpts := toAuthOptsExt(cfg)

	provider, err := openstack.NewClient(cfg.Global.AuthURL)
	if err != nil {
		return nil, fmt.Errorf("could not authenticate client: %v", err)
	}

	err = openstack.AuthenticateV3(provider, authOpts, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("could not authenticate: %v", err)
	}

	clusterClient, err := openstack.NewContainerInfraV1(provider, gophercloud.EndpointOpts{Type: "container-infra", Name: "magnum", Region: cfg.Global.Region})
	if err != nil {
		return nil, fmt.Errorf("could not create container-infra client: %v", err)
	}

	heatClient, err := openstack.NewOrchestrationV1(provider, gophercloud.EndpointOpts{Type: "orchestration", Name: "heat", Region: cfg.Global.Region})
	if err != nil {
		return nil, fmt.Errorf("could not create orchestration client: %v", err)
	}

	manager := OpenstackManagerHeat{
		clusterClient: clusterClient,
		clusterName:   opts.ClusterName,
		heatClient:    heatClient,
	}

	manager.stackID, err = manager.GetStackID()
	if err != nil {
		return nil, fmt.Errorf("could not store stack ID on manager: %v", err)
	}
	manager.stackName, err = manager.GetStackName()
	if err != nil {
		return nil, fmt.Errorf("could not store stack name on manager: %v", err)
	}

	return &manager, nil
}

// NodeGroupSize gets the current cluster size as reported by magnum.
// The nodegroup argument is ignored as this implementation of OpenstackManager
// assumes that only a single node group exists.
func (osm *OpenstackManagerHeat) NodeGroupSize(nodegroup string) (int, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return 0, fmt.Errorf("could not get cluster: %v", err)
	}
	return cluster.NodeCount, nil
}

// UpdateNodeCount replaces the cluster node_count in magnum.
func (osm *OpenstackManagerHeat) UpdateNodeCount(nodegroup string, nodes int) error {
	updateOpts := []clusters.UpdateOptsBuilder{
		clusters.UpdateOpts{Op: clusters.ReplaceOp, Path: "/node_count", Value: fmt.Sprintf("%d", nodes)},
	}
	_, err := clusters.Update(osm.clusterClient, osm.clusterName, updateOpts).Extract()
	if err != nil {
		return fmt.Errorf("could not update cluster: %v", err)
	}
	return nil
}

// GetNodes currently returns nothing and this seems to work fine.
func (osm *OpenstackManagerHeat) GetNodes(nodegroup string) ([]string, error) {
	// TODO: get nodes from heat?
	// This works fine being empty for now anyway
	return []string{}, nil
}

// DeleteNodes deletes nodes by passing a comma separated list of names or IPs
// of minions to remove to heat, and simultaneously sets the new number of minions on the stack.
// The magnum node_count is then set to the new value (does not cause any more nodes to be removed).
func (osm *OpenstackManagerHeat) DeleteNodes(nodegroup string, minionsToRemove []string, updatedNodeCount int) error {
	minionsList := strings.Join(minionsToRemove, ",")
	updateOpts := stacks.UpdateOpts{
		Parameters: map[string]interface{}{
			"minions_to_remove": minionsList,
			"number_of_minions": updatedNodeCount,
		},
	}

	updateResult := stacks.UpdatePatch(osm.heatClient, osm.stackName, osm.stackID, updateOpts)
	err := updateResult.ExtractErr()
	if err != nil {
		return fmt.Errorf("stack patch failed: %v", err)
	}

	// Wait for the stack to do its thing before updating the cluster node_count
	err = osm.WaitForStackStatus(stackStatusUpdateInProgress, waitForUpdateStatusTimeout)
	if err != nil {
		return fmt.Errorf("error waiting for stack %s status: %v", stackStatusUpdateInProgress, err)
	}
	err = osm.WaitForStackStatus(stackStatusUpdateComplete, waitForCompleteStatusTimout)
	if err != nil {
		return fmt.Errorf("error waiting for stack %s status: %v", stackStatusUpdateComplete, err)
	}

	err = osm.UpdateNodeCount(nodegroup, updatedNodeCount)
	if err != nil {
		return fmt.Errorf("could not set new cluster size: %v", err)
	}
	return nil
}

// GetClusterStatus returns the current status of the magnum cluster.
func (osm *OpenstackManagerHeat) GetClusterStatus() (string, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get cluster: %v", err)
	}
	return cluster.Status, nil
}

// CanUpdate checks if the cluster status is present in a set of statuses that
// prevent the cluster from being updated.
// Returns if updating is possible and the status for convenience.
func (osm *OpenstackManagerHeat) CanUpdate() (bool, string, error) {
	clusterStatus, err := osm.GetClusterStatus()
	if err != nil {
		return false, "", fmt.Errorf("could not get cluster status: %v", err)
	}
	return !StatusesPreventingUpdate.Has(clusterStatus), clusterStatus, nil
}

// GetStackStatus returns the current status of the heat stack used by the magnum cluster.
func (osm *OpenstackManagerHeat) GetStackStatus() (string, error) {
	stack, err := stacks.Get(osm.heatClient, osm.stackName, osm.stackID).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get stack from heat: %v", err)
	}
	return stack.Status, nil
}

// WaitForStackStatus checks once per second to see if the heat stack has entered a given status.
// waits for heat stack to change to a given status.
// Returns when the status is observed or the timeout is reached.
func (osm *OpenstackManagerHeat) WaitForStackStatus(status string, timeout time.Duration) error {
	klog.V(2).Infof("Waiting for stack %s status", status)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(time.Second) {
		currentStatus, err := osm.GetStackStatus()
		if err != nil {
			return fmt.Errorf("error waiting for stack status: %v", err)
		}
		if currentStatus == status {
			klog.V(0).Infof("Waited for stack %s status, took %d seconds", status, int(time.Since(start).Seconds()))
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout (%v) waiting for stack status %s", timeout, status)
}

// GetStackID gets the heat stack ID from the magnum cluster.
func (osm *OpenstackManagerHeat) GetStackID() (string, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get cluster: %v", err)
	}
	klog.V(0).Infof("Cluster stack ID: %s", cluster.StackID)
	return cluster.StackID, nil
}

// GetStackName lists all heat stacks and finds the one with a matching ID
// to the heat stack ID obtained from the magnum cluster.
func (osm *OpenstackManagerHeat) GetStackName() (string, error) {
	// Calls to stacks.Get require both ID and name, so to get the name
	// we have to inspect all stacks and find the one with the matching ID.

	allStacksPages, err := stacks.List(osm.heatClient, nil).AllPages()
	if err != nil {
		return "", fmt.Errorf("could not list all stacks: %v", err)
	}

	allStacks, err := stacks.ExtractStacks(allStacksPages)
	if err != nil {
		return "", fmt.Errorf("could not extract stack pages: %v", err)
	}

	for _, stack := range allStacks {
		if stack.ID == osm.stackID {
			klog.V(0).Infof("For stack ID %s, stack name is %s", osm.stackID, stack.Name)
			return stack.Name, nil
		}
	}

	return "", fmt.Errorf("stack with ID %s not found", osm.stackID)
}
