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
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OpenstackManagerMock struct {
	mock.Mock
}

func (m *OpenstackManagerMock) NodeGroupSize(nodegroup string) (int, error) {
	args := m.Called(nodegroup)
	return args.Int(0), args.Error(1)
}

func (m *OpenstackManagerMock) UpdateNodeCount(nodegroup string, nodes int) error {
	args := m.Called(nodegroup, nodes)
	return args.Error(0)
}

func (m *OpenstackManagerMock) GetNodes(nodegroup string) ([]string, error) {
	args := m.Called(nodegroup)
	return args.Get(0).([]string), args.Error(1)
}

func (m *OpenstackManagerMock) DeleteNodes(nodegroup string, minionsToRemove []string, updatedNodeCount int) error {
	args := m.Called(nodegroup, minionsToRemove, updatedNodeCount)
	return args.Error(0)
}

func (m *OpenstackManagerMock) GetClusterStatus() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *OpenstackManagerMock) CanUpdate() (bool, string, error) {
	args := m.Called()
	return args.Bool(0), args.String(1), args.Error(2)
}

func CreateTestNodeGroup(manager OpenstackManager) *OpenstackNodeGroup {
	current := 1
	ng := OpenstackNodeGroup{
		openstackManager:   manager,
		id:                 "TestNodeGroup",
		clusterUpdateMutex: &sync.Mutex{},
		minSize:            1,
		maxSize:            10,
		targetSize:         &current,
		waitTimeStep:       100 * time.Millisecond,
	}
	return &ng
}

func TestWaitForClusterStatus(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodeGroup(manager)

	// Test all working normally
	manager.On("GetClusterStatus").Return(clusterStatusUpdateComplete, nil).Once()
	err := ng.WaitForClusterStatus(clusterStatusUpdateComplete, 200*time.Millisecond)
	assert.NoError(t, err)

	// Test timeout
	manager.On("GetClusterStatus").Return(clusterStatusUpdateInProgress, nil).Times(2)
	err = ng.WaitForClusterStatus(clusterStatusUpdateComplete, 200*time.Millisecond)
	assert.Error(t, err)
	assert.Equal(t, "timeout (200ms) waiting for UPDATE_COMPLETE status", err.Error())

	// Test error returned from manager
	manager.On("GetClusterStatus").Return("", errors.New("manager error")).Once()
	err = ng.WaitForClusterStatus(clusterStatusUpdateComplete, 200*time.Millisecond)
	assert.Error(t, err)
	assert.Equal(t, "error waiting for UPDATE_COMPLETE status: manager error", err.Error())
}

func TestIncreaseSize(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodeGroup(manager)

	// Test all working normally
	manager.On("NodeGroupSize", "TestNodeGroup").Return(1, nil).Once()
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("UpdateNodeCount", "TestNodeGroup", 2).Return(nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateComplete, nil).Once()
	err := ng.IncreaseSize(1)
	assert.NoError(t, err)
	assert.Equal(t, 2, *ng.targetSize, "target size not updated")

	// Test negative increase
	err = ng.IncreaseSize(-1)
	assert.Error(t, err)
	assert.Equal(t, "size increase must be positive", err.Error())

	// Test zero increase
	err = ng.IncreaseSize(0)
	assert.Error(t, err)
	assert.Equal(t, "size increase must be positive", err.Error())

	// Test current total nodes fails
	manager.On("NodeGroupSize", "TestNodeGroup").Return(0, errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "could not check current nodegroup size: manager error", err.Error())

	// Test increase too large
	manager.On("NodeGroupSize", "TestNodeGroup").Return(1, nil).Once()
	err = ng.IncreaseSize(10)
	assert.Error(t, err)
	assert.Equal(t, "size increase too large, desired:11 max:10", err.Error())

	// Test cluster status prevents update
	manager.On("NodeGroupSize", "TestNodeGroup").Return(1, nil).Once()
	manager.On("CanUpdate").Return(false, clusterStatusUpdateInProgress, nil).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "can not add nodes, cluster is in UPDATE_IN_PROGRESS status", err.Error())

	// Test cluster status check fails
	manager.On("NodeGroupSize", "TestNodeGroup").Return(1, nil).Once()
	manager.On("CanUpdate").Return(false, "", errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "can not increase node count: manager error", err.Error())

	// Test update node count fails
	*ng.targetSize = 1
	manager.On("NodeGroupSize", "TestNodeGroup").Return(1, nil).Once()
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("UpdateNodeCount", "TestNodeGroup", 2).Return(errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "could not increase cluster size: manager error", err.Error())
}

var nodesToDelete []*apiv1.Node
var nodeIPs []string

func init() {
	for i := 1; i <= 5; i++ {
		node := apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("cluster-abc-minion-%d", i),
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("10.0.0.%d", i),
					},
				},
			},
		}

		nodesToDelete = append(nodesToDelete, &node)
	}

	for _, node := range nodesToDelete {
		nodeIPs = append(nodeIPs, node.Status.Addresses[0].Address)
	}
}

func TestDeleteNodes(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodeGroup(manager)
	*ng.targetSize = 10

	// Test all working normally
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(10, nil).Once()
	manager.On("DeleteNodes", "TestNodeGroup", nodeIPs, 5).Return(nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(5, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateComplete, nil).Once()
	err := ng.DeleteNodes(nodesToDelete)
	assert.NoError(t, err)
	assert.Equal(t, 5, *ng.targetSize)
}

func TestDeleteNodesBatching(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodeGroup(manager)
	*ng.targetSize = 10

	// Test all working normally
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(10, nil).Once()
	manager.On("DeleteNodes", "TestNodeGroup", nodeIPs, 5).Return(nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(5, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateComplete, nil).Once()

	go func() {
		time.Sleep(time.Second)
		err := ng.DeleteNodes(nodesToDelete[3:5])
		assert.NoError(t, err, "Delete call that should have been batched did not return nil")
	}()

	err := ng.DeleteNodes(nodesToDelete[0:3])
	assert.NoError(t, err)
	assert.Equal(t, 5, *ng.targetSize)
	manager.AssertExpectations(t)
}

func TestDeleteNodesBelowMin(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodeGroup(manager)
	ng.minSize = 8
	*ng.targetSize = 10

	// Try to batch 5 nodes for deletion when minSize only allows for two to be deleted

	var twoIPs []string
	var twoNames []string

	for i := 0; i < 2; i++ {
		twoIPs = append(twoIPs, nodesToDelete[i].Status.Addresses[0].Address)
		twoNames = append(twoNames, nodesToDelete[i].ObjectMeta.Name)
	}

	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(10, nil).Once()
	manager.On("DeleteNodes", "TestNodeGroup", twoIPs, 8).Return(nil).Once()
	manager.On("NodeGroupSize", "TestNodeGroup").Return(8, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(clusterStatusUpdateComplete, nil).Once()

	for i := 1; i < 5; i++ {
		go func(i int) {
			// Wait and preserve order
			time.Sleep(time.Second + 100*time.Millisecond*time.Duration(i))
			err := ng.DeleteNodes(nodesToDelete[i : i+1])
			if i == 1 {
				// One node should be added to the batch
				assert.NoError(t, err, "Delete call that should have been batched did not return nil")
			} else {
				// The rest should fail
				assert.Error(t, err)
				assert.Equal(t, "deleting nodes would take nodegroup below minimum size", err.Error())
			}
		}(i)
	}

	err := ng.DeleteNodes(nodesToDelete[0:1])
	assert.NoError(t, err)
	manager.AssertExpectations(t)
}
