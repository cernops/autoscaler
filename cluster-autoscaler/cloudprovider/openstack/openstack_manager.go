package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"gopkg.in/gcfg.v1"

	"io"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"
)

type OpenstackManager struct {
	clusterClient *gophercloud.ServiceClient
	clusterName string

}

func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions) (*OpenstackManager, error) {
	if configReader != nil {
		var cfg provider_os.Config
		if err := gcfg.ReadInto(&cfg, configReader); err != nil {
			glog.Errorf("Couldn't read config: %v", err)
			return nil, err
		}
		fmt.Println(cfg)
	}

	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		return nil, fmt.Errorf("Could not get env auth options: %v", err)
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, fmt.Errorf("Could not authenticate client: %v", err)
	}

	clusterClient, err := openstack.NewContainerInfraV1(provider, gophercloud.EndpointOpts{Type: "container-infra", Name: "magnum", Region: "cern"})
	if err != nil {
		return nil, fmt.Errorf("Could not create cluster client: %v", err)
	}

	manager := OpenstackManager{
		clusterClient: clusterClient,
	}

	return &manager, nil
}


func (osm *OpenstackManager) CurrentTotalNodes() (int, error) {
	result, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return 0, fmt.Errorf("Could not get current total nodes: %v", err)
	}
	return result.NodeCount, nil
}