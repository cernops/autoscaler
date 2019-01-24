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

func (m *OpenstackManagerMock) CurrentTotalNodes() (int, error) {
	args := m.Called()
	return args.Get(0).(int), args.Error(1)
}

func (m *OpenstackManagerMock) UpdateNodeCount(nodes int) error {
	args := m.Called()
	return args.Error(0)
}

func (m *OpenstackManagerMock) GetNodes() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *OpenstackManagerMock) DeleteNodes(minionsToRemove string, updatedNodeCount int) (int, error) {
	args := m.Called()
	return args.Get(0).(int), args.Error(1)
}

func (m *OpenstackManagerMock) GetClusterStatus() (string, error) {
	args := m.Called()
	return args.Get(0).(string), args.Error(1)
}

func (m *OpenstackManagerMock) CanUpdate() (bool, string, error) {
	args := m.Called()
	return args.Get(0).(bool), args.Get(1).(string), args.Error(2)
}


type NodeCleanerMock struct {
	mock.Mock
}

func (nc *NodeCleanerMock) CheckNodesAccess() error {
	args := nc.Called()
	return args.Error(0)
}

func (nc *NodeCleanerMock) CleanupNodes(nodeNames []string) error {
	args := nc.Called()
	return args.Error(0)
}


func CreateTestNodegroup(manager OpenstackManager) *OpenstackNodeGroup {
	current := 1
	ng := OpenstackNodeGroup{
		openstackManager:   manager,
		id:                 "TestNodegroup",
		clusterUpdateMutex: &sync.Mutex{},
		minSize:            1,
		maxSize:            10,
		targetSize:         &current,
	}
	return &ng
}

func TestWaitForClusterStatus(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodegroup(manager)

	// Test all working normally
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
	err := ng.WaitForClusterStatus(ClusterStatusUpdateComplete, 2*time.Second)
	assert.NoError(t, err)

	// Test timeout
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Times(2)
	err = ng.WaitForClusterStatus(ClusterStatusUpdateComplete, 2*time.Second)
	assert.Error(t, err)
	assert.Equal(t, "timeout waiting for UPDATE_COMPLETE status", err.Error())

	// Test error returned from manager
	manager.On("GetClusterStatus").Return("", errors.New("manager error")).Once()
	err = ng.WaitForClusterStatus(ClusterStatusUpdateComplete, 2*time.Second)
	assert.Error(t, err)
	assert.Equal(t, "error waiting for UPDATE_COMPLETE status: manager error", err.Error())
}

func TestIncreaseSize(t *testing.T) {
	manager := &OpenstackManagerMock{}
	ng := CreateTestNodegroup(manager)

	// Test all working normally
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("UpdateNodeCount").Return(nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
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
	manager.On("CurrentTotalNodes").Return(0, errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "could not check current nodegroup size: manager error", err.Error())

	// Test increase too large
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	err = ng.IncreaseSize(10)
	assert.Error(t, err)
	assert.Equal(t, "size increase too large, desired:11 max:10", err.Error())

	// Test cluster status prevents update
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	manager.On("CanUpdate").Return(false, ClusterStatusUpdateInProgress, nil).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "can not add nodes, cluster is in UPDATE_IN_PROGRESS status", err.Error())

	// Test cluster status check fails
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	manager.On("CanUpdate").Return(false, "", errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "can not increase node count: manager error", err.Error())

	// Test update node count fails
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("UpdateNodeCount").Return(errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "could not increase cluster size: manager error", err.Error())
}

func TestDeleteNodes(t *testing.T) {
	manager := &OpenstackManagerMock{}
	nodeCleaner := &NodeCleanerMock{}
	ng := CreateTestNodegroup(manager)
	ng.nodeCleaner = nodeCleaner
	*ng.targetSize = 10

	var nodesToDelete []*apiv1.Node
	for i := 1; i <= 5; i++ {
		node := apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("cluster-abc-minion-%d", i),
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					apiv1.NodeAddress{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("10.0.0.%d", i),
					},
				},
			},
		}

		nodesToDelete = append(nodesToDelete, &node)
	}

	// Test all working normally
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("CurrentTotalNodes").Return(10, nil)
	manager.On("DeleteNodes").Return(5, nil)
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
	nodeCleaner.On("CleanupNodes").Return(nil)
	err := ng.DeleteNodes(nodesToDelete)
	assert.NoError(t, err)
	assert.Equal(t, 5, *ng.targetSize)

	// to test batching
	// Can let the goroutine DeleteNode calls fail after they add to the batch group
	// go func() { time.Sleep(0.5); ng.DeleteNodes(node2); ng.DeleteNodes(node3) }
	// ng.DeleteNodes(node1)
}