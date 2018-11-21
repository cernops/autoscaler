package openstack

import (
	"io"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
)

type OpenstackManager struct {
}

func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions) (*OpenstackManager, error) {
	return &OpenstackManager{}, nil
}