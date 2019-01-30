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
	"io"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"os"
)

const (
	defaultManager = "heat"
)

// OpenstackManager is an interface for the basic interactions with the cluster.
type OpenstackManager interface {
	NodeGroupSize(nodegroup string) (int, error)
	UpdateNodeCount(nodegroup string, nodes int) error
	GetNodes(nodegroup string) ([]string, error)
	DeleteNodes(nodegroup string, minionsToRemove []string, updatedNodeCount int) error
	GetClusterStatus() (string, error)
	CanUpdate() (bool, string, error)
}

// CreateOpenstackManager creates the desired implementation of OpenstackManager.
// Currently reads the environment variable OSMANAGER to find which to create,
// and falls back to a default if the variable is not found.
func CreateOpenstackManager(configReader io.Reader, discoverOpts cloudprovider.NodeGroupDiscoveryOptions, opts config.AutoscalingOptions) (OpenstackManager, error) {
	// For now get manager from env var, can consider adding flag later
	manager, ok := os.LookupEnv("OSMANAGER")
	if !ok {
		manager = defaultManager
	}

	switch manager {
	case "heat":
		return CreateOpenstackManagerHeat(configReader, discoverOpts, opts)
	}

	return nil, fmt.Errorf("openstack manager does not exist: %s", manager)
}
