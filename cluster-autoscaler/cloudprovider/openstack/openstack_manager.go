package openstack

import (
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"io"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
)

type OpenstackManager struct {
	clusterClient gophercloud.ServiceClient

}

func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions) (*OpenstackManager, error) {
	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		return nil, fmt.Errorf("Could not get env auth options: %v", err)
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		return nil, fmt.Errorf("Could not authenticate client: %v", err)
	}

	secretServiceClient, err := openstack.NewBlockStorageV1()

	return &OpenstackManager{}, nil
}