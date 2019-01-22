package openstack

import (
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"io"
	"os"
	"fmt"
)

const (
	DefaultManager = "heat"
)

// OpenstackManager is an interface representing the
type OpenstackManager interface {
	CurrentTotalNodes() (int, error)
	UpdateNodeCount(nodes int) error
	GetNodes() ([]string, error)
	DeleteNodes(minionsToRemove string, updatedNodeCount int) (int, error)
	GetClusterStatus() (string, error)
	CanUpdate() (bool, string, error)
}


func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions, opts config.AutoscalingOptions) (OpenstackManager, error) {
	// For now get manager from env var, can consider adding flag later
	manager, ok := os.LookupEnv("OSMANAGER")
	if !ok {
		manager = DefaultManager
	}

	switch manager {
	case "heat":
		return CreateOpenstackManagerHeat(configReader, discoverOpts, opts)
	}

	return nil, fmt.Errorf("openstack manager does not exist: %s", manager)
}