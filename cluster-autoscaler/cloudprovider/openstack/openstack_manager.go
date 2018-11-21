package openstack

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	"gopkg.in/gcfg.v1"
	"os"

	"io"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"
)

type OpenstackManager struct {
	clusterClient *gophercloud.ServiceClient
	clusterName string

}

func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions) (*OpenstackManager, error) {
	var cfg provider_os.Config
	if configReader != nil {
		if err := gcfg.ReadInto(&cfg, configReader); err != nil {
			glog.Errorf("Couldn't read config: %v", err)
			return nil, err
		}
		glog.Infof("TrustID: %s", cfg.Global.TrustID)
	}

	opts := toAuthOptions(cfg)

	/*opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		return nil, fmt.Errorf("Could not get env auth options: %v", err)
	}*/

	glog.Infof("%#v", opts)

	//provider, err := openstack.AuthenticatedClient(opts)
	provider, err := openstack.NewClient(cfg.Global.AuthURL)
	if err != nil {
		return nil, fmt.Errorf("Could not authenticate client: %v", err)
	}

	err = openstack.AuthenticateV3(provider, opts, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("Bad")
	}

	clusterClient, err := openstack.NewContainerInfraV1(provider, gophercloud.EndpointOpts{Type: "container-infra", Name: "magnum", Region: "cern"})
	if err != nil {
		return nil, fmt.Errorf("Could not create container-infra client: %v", err)
	}

	// Temporary, can get from CLUSTER_UUID from
	// /etc/sysconfig/heat-params in master node.
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		return nil, fmt.Errorf("Please set env var CLUSTER_NAME of cluster to manage")
	}

	manager := OpenstackManager{
		clusterClient: clusterClient,
		clusterName: clusterName,
	}

	return &manager, nil
}

// Taken from https://github.com/kubernetes/cloud-provider-openstack/
// pkg/cloudprovider/providers/openstack/openstack.go
func toAuthOptions(cfg provider_os.Config) trusts.AuthOptsExt {
	opts:=  gophercloud.AuthOptions{
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
	result, err := clusters.Get(osm.clusterClient, osm.clusterName).Extract()
	if err != nil {
		return 0, fmt.Errorf("Could not get current total nodes: %v", err)
	}
	return result.NodeCount, nil
}