package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"

	"sync"

	//"github.com/gophercloud/gophercloud/openstack/orchestration/v1/stackresources"
	"gopkg.in/gcfg.v1"
	"os"

	"io"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"
)

const (
	StatusUpdateInProgress = "UPDATE_IN_PROGRESS"
)

var StatesPreventingUpdate = sets.NewString(StatusUpdateInProgress)

type OpenstackManager struct {
	clusterClient *gophercloud.ServiceClient
	novaClient *gophercloud.ServiceClient
	heatClient *gophercloud.ServiceClient
	clusterName string

	UpdateMutex sync.Mutex

	minSize int
	maxSize int
	targetSize int

}

func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions) (*OpenstackManager, error) {
	var cfg provider_os.Config
	if configReader != nil {
		if err := gcfg.ReadInto(&cfg, configReader); err != nil {
			glog.Errorf("Couldn't read config: %v", err)
			return nil, err
		}
	}

	opts := toAuthOptsExt(cfg)

	provider, err := openstack.NewClient(cfg.Global.AuthURL)
	if err != nil {
		return nil, fmt.Errorf("could not authenticate client: %v", err)
	}

	err = openstack.AuthenticateV3(provider, opts, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("could not authenticate: %v", err)
	}

	clusterClient, err := openstack.NewContainerInfraV1(provider, gophercloud.EndpointOpts{Type: "container-infra", Name: "magnum", Region: "cern"})
	if err != nil {
		return nil, fmt.Errorf("could not create container-infra client: %v", err)
	}

	novaClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{Type: "compute", Name: "nova", Region: "cern"})
	if err != nil {
		return nil, fmt.Errorf("could not create compute client: %v", err)
	}

	heatClient, err := openstack.NewOrchestrationV1(provider, gophercloud.EndpointOpts{Type: "orchestration", Name: "heat", Region: "cern"})
	if err != nil {
		return nil, fmt.Errorf("could not create orchestration client: %v", err)
	}

	// Temporary, can get from CLUSTER_UUID from
	// /etc/sysconfig/heat-params in master node.
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		return nil, fmt.Errorf("please set env var CLUSTER_NAME of cluster to manage")
	}

	manager := OpenstackManager{
		clusterClient: clusterClient,
		clusterName: clusterName,
		novaClient: novaClient,
		heatClient: heatClient,
	}

	return &manager, nil
}

func toAuthOptsExt(cfg provider_os.Config) trusts.AuthOptsExt {
	opts :=  gophercloud.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthURL,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserID,
		Password:         cfg.Global.Password,
		TenantID:         cfg.Global.TenantID,
		TenantName:       cfg.Global.TenantName,
		DomainID:         cfg.Global.DomainID,
		DomainName:       cfg.Global.DomainName,

		// Persistent service, so we need to be able to renew tokens.
		AllowReauth: true,
	}

	return trusts.AuthOptsExt{
		TrustID: cfg.Global.TrustID,
		AuthOptionsBuilder: &opts,
	}
}


func (osm *OpenstackManager) CurrentTotalNodes() (int, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return 0, fmt.Errorf("could not get cluster to get current total nodes: %v", err)
	}
	return cluster.NodeCount, nil
}

func (osm *OpenstackManager) UpdateNodeCount(nodes int) error {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return fmt.Errorf("could not get cluster to update node count: %v", err)
	}
	clusterUUID := cluster.UUID
	updateOpts := []clusters.UpdateOptsBuilder{
		clusters.UpdateOpts{Op: clusters.ReplaceOp, Path: "/node_count", Value: fmt.Sprintf("%d", nodes)},
	}
	_, err = clusters.Update(osm.clusterClient, clusterUUID, updateOpts).Extract()
	if err != nil {
		return fmt.Errorf("could not update cluster node count: %v", err)
	}
	glog.Infof("Set current node count to %d", nodes)
	return nil
}

func (osm *OpenstackManager) GetNodes() ([]string, error) {
	/*cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get cluster to list nodes: %v", err)
	}*/
	/*clusterStackPages, err := stackresources.List(osm.heatClient, "", cluster.StackID, stackresources.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("could not get cluster stack resources pages: %v", err)
	}
	clusterStackResources, err := stackresources.ExtractResources(clusterStackPages)
	if err != nil {
		return nil, fmt.Errorf("could not extract cluster stack resources: %v", err)
	}*/

	/*minions, err := stackresources.Get(osm.heatClient, "", cluster.StackID, "kube_minions").Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get kube_minions resource: %v", err)
	}

	glog.Infof("minions: %#v", minions)*/

	/*var nodes []string
	for _, resource := range clusterStackResources {
		glog.Infof("Stack resource: %#v", resource)
		name := resource.Name
		nodes = append(nodes, name)
	}*/
	// TODO: get nodes from heat?
	return []string{}, nil
}


func (osm *OpenstackManager) DeleteNode(UID string) error {
	/*cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return fmt.Errorf("Could not get cluster to delete node: %v", err)
	}
	clusterStackPages, err := stackresources.List(osm.heatClient, "", cluster.StackID, stackresources.ListOpts{}).AllPages()
	if err != nil {
		return fmt.Errorf("Could not get cluster stack resources pages: %v", err)
	}
	clusterStackResources, err := stackresources.ExtractResources(clusterStackPages)
	if err != nil {
		return fmt.Errorf("Could not extract cluster stack resources: %v", err)
	}
	glog.Infof("Stack resources: %#v", clusterStackResources)*/
	deleteResult := servers.Delete(osm.novaClient, UID)
	errResult := deleteResult.ExtractErr()
	glog.Infof("Delete result for %s: err=%v", UID, errResult)
	return errResult
}

func (osm *OpenstackManager) GetClusterStatus() (string, error) {
	cluster, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return "", fmt.Errorf("could not get cluster to check status: %v", err)
	}
	return cluster.Status, nil
}

func (osm *OpenstackManager) CanUpdate() (bool, error) {
	clusterStatus, err := osm.GetClusterStatus()
	if err != nil {
		return false, fmt.Errorf("could not get cluster update ability: %v", err)
	}
	return !StatesPreventingUpdate.Has(clusterStatus), nil
}