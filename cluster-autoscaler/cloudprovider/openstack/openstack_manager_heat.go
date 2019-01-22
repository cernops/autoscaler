package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"time"

	"github.com/gophercloud/gophercloud/openstack/orchestration/v1/stacks"
	"gopkg.in/gcfg.v1"
	"io"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"
)

const (
	StackStatusUpdateInProgress = "UPDATE_IN_PROGRESS"
	StackStatusUpdateComplete = "UPDATE_COMPLETE"
)

var StatusesPreventingUpdate = sets.NewString(
	ClusterStatusUpdateInProgress,
	ClusterStatusUpdateFailed,
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

func CreateOpenstackManagerHeat(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions, opts config.AutoscalingOptions) (*OpenstackManagerHeat, error) {
	var cfg provider_os.Config
	if configReader != nil {
		if err := gcfg.ReadInto(&cfg, configReader); err != nil {
			glog.Errorf("Couldn't read config: %v", err)
			return nil, err
		}
	}

	if opts.ClusterName == "" {
		glog.Fatalf("The cluster-name parameter must be set")
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

func (osm *OpenstackManagerHeat) CurrentTotalNodes() (int, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return 0, fmt.Errorf("could not get cluster to get current total nodes: %v", err)
	}
	return cluster.NodeCount, nil
}

// UpdateNodeCount replaces the cluster node_count in magnum.
func (osm *OpenstackManagerHeat) UpdateNodeCount(nodes int) error {
	updateOpts := []clusters.UpdateOptsBuilder{
		clusters.UpdateOpts{Op: clusters.ReplaceOp, Path: "/node_count", Value: fmt.Sprintf("%d", nodes)},
	}
	_, err := clusters.Update(osm.clusterClient, osm.clusterName, updateOpts).Extract()
	if err != nil {
		return fmt.Errorf("could not update cluster node count: %v", err)
	}
	return nil
}

func (osm *OpenstackManagerHeat) GetNodes() ([]string, error) {
	/*cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get cluster to list nodes: %v", err)
	}*/
	/*clusterStackPages, err := stackresources.List(osm.heatClient, osm.stackName, osm.stackID, stackresources.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("could not get cluster stack resources pages: %v", err)
	}
	clusterStackResources, err := stackresources.ExtractResources(clusterStackPages)
	if err != nil {
		return nil, fmt.Errorf("could not extract cluster stack resources: %v", err)
	}

	glog.Infof("%#v", clusterStackResources)*/

	// I don't know what exactly should be returned in this.
	// GKE has fmt.Sprintf("gce://%s/%s/%s", ref.Project, ref.Zone, ref.Name))
	// But I don't know what it's used for anyway

	var minionIPs []string

	stack, err := stacks.Get(osm.heatClient, osm.stackName, osm.stackID).Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get stack: %v", err)
	}
	for _, output := range stack.Outputs {
		if output["output_key"] == "kube_minions" {
			outputValue := output["output_value"].([]interface{})
			for _, ip := range outputValue {
				// This value is nil for newly spawned nodes, then "", then finally the IP
				if ip != nil {
					if ip != "" {
						minionIPs = append(minionIPs, ip.(string))
					}
				}
			}
		}
	}
	//glog.Infof("minion IPs: %#v", minionIPs)

	/*minions, err := stackresources.Get(osm.heatClient, osm.stackName, osm.stackID, "kube_minions").Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get kube_minions resource: %v", err)
	}

	glog.Infof("minions: %#v", minions)*/

	/*metadata, err := stackresources.Metadata(osm.heatClient, osm.stackName, osm.stackID, "kube_minions").Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get kube_minions metadata: %v", err)
	}
	glog.Infof("metadata: %#v", metadata)*/

	/*resources, err := stackresources.Find(osm.heatClient, osm.stackName).Extract()
	if err != nil {
		return nil, fmt.Errorf("could not find for stacks: %v", err)
	}
	glog.Infof("find: %#v", resources)*/

	/*var nodes []string
	for _, resource := range clusterStackResources {
		glog.Infof("Stack resource: %#v", resource)
		name := resource.Name
		nodes = append(nodes, name)
	}*/
	// TODO: get nodes from heat? Wait for proper nodegroups?
	// This works fine being empty for now anyway
	return []string{}, nil
}

// DeleteNodes deletes nodes by passing a comma separated list of names or IPs
// of minions to remove to heat, and simultaneously sets the new number of minions on the stack.
// The magnum node_count is then set to the new value (does not cause any more nodes to be removed).
func (osm *OpenstackManagerHeat) DeleteNodes(minionsToRemove string, updatedNodeCount int) (int, error) {
	updateOpts := stacks.UpdateOpts{
		Parameters: map[string]interface{}{
			"minions_to_remove": minionsToRemove,
			"number_of_minions": updatedNodeCount,
		},
	}

	updateResult := stacks.UpdatePatch(osm.heatClient, osm.stackName, osm.stackID, updateOpts)
	err := updateResult.ExtractErr()
	if err != nil {
		return 0, fmt.Errorf("stack patch failed: %v", err)
	}

	// Wait for the stack to do its thing before updating the cluster node_count
	err = osm.WaitForStackStatus(StackStatusUpdateInProgress, WaitForUpdateStatusTimeout)
	if err != nil {
		return 0, fmt.Errorf("error waiting for stack %s status: %v", StackStatusUpdateInProgress, err)
	}
	err = osm.WaitForStackStatus(StackStatusUpdateComplete, WaitForCompleteStatusTimout)
	if err != nil {
		return 0, fmt.Errorf("error waiting for stack %s status: %v", StackStatusUpdateComplete, err)
	}

	err = osm.UpdateNodeCount(updatedNodeCount)
	if err != nil {
		return 0, fmt.Errorf("could not set new cluster size: %v", err)
	}
	return updatedNodeCount, nil
}

func (osm *OpenstackManagerHeat) GetClusterStatus() (string, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get cluster to check status: %v", err)
	}
	return cluster.Status, nil
}

// CanUpdate checks if the cluster status is present in a set of statuses that
// prevent the cluster from being updated.
// Returns if updating is possible and the status for convenience.
func (osm *OpenstackManagerHeat) CanUpdate() (bool, string, error) {
	clusterStatus, err := osm.GetClusterStatus()
	if err != nil {
		return false, "", fmt.Errorf("could not get cluster update ability: %v", err)
	}
	return !StatusesPreventingUpdate.Has(clusterStatus), clusterStatus, nil
}

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
	glog.Infof("Waiting for stack %s status", status)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(time.Second) {
		currentStatus, err := osm.GetStackStatus()
		if err != nil {
			return fmt.Errorf("error waiting for stack status: %v", err)
		}
		if currentStatus == status {
			glog.Infof("Waited for stack %s status, took %d seconds", status, int(time.Since(start).Seconds()))
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout (%v) waiting for stack status %s", timeout, status)
}

func (osm *OpenstackManagerHeat) GetStackID() (string, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get cluster to get stack ID: %v", err)
	}
	return cluster.StackID, nil
}

func (osm *OpenstackManagerHeat) GetStackName() (string, error) {
	stack, err := stacks.Get(osm.heatClient, "", osm.stackID).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get stack from heat: %v", err)
	}
	return stack.Name, nil
}
