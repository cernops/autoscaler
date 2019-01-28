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
	"strings"
)

type OpenstackManagerMock struct {
	mock.Mock
}

func (m *OpenstackManagerMock) CurrentTotalNodes() (int, error) {
	args := m.Called()
	return args.Int(0), args.Error(1)
}

func (m *OpenstackManagerMock) UpdateNodeCount(nodes int) error {
	args := m.Called(nodes)
	return args.Error(0)
}

func (m *OpenstackManagerMock) GetNodes() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *OpenstackManagerMock) DeleteNodes(minionsToRemove string, updatedNodeCount int) (int, error) {
	args := m.Called(minionsToRemove, updatedNodeCount)
	return args.Int(0), args.Error(1)
}

func (m *OpenstackManagerMock) GetClusterStatus() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *OpenstackManagerMock) CanUpdate() (bool, string, error) {
	args := m.Called()
	return args.Bool(0), args.String(1), args.Error(2)
}


type NodeCleanerMock struct {
	mock.Mock
}

func (nc *NodeCleanerMock) CheckNodesAccess() error {
	args := nc.Called()
	return args.Error(0)
}

func (nc *NodeCleanerMock) CleanupNodes(nodeNames []string) error {
	args := nc.Called(nodeNames)
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
	manager.On("UpdateNodeCount", 2).Return(nil).Once()
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
	*ng.targetSize = 1
	manager.On("CurrentTotalNodes").Return(1, nil).Once()
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("UpdateNodeCount", 2).Return(errors.New("manager error")).Once()
	err = ng.IncreaseSize(1)
	assert.Error(t, err)
	assert.Equal(t, "could not increase cluster size: manager error", err.Error())
}




var nodesToDelete []*apiv1.Node
var joinedIPs string
var nameList []string

func init() {
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
		nameList = append(nameList, node.ObjectMeta.Name)
	}

	IPs := []string{}
	for _, node := range nodesToDelete {
		IPs = append(IPs, node.Status.Addresses[0].Address)
	}
	joinedIPs = strings.Join(IPs, ",")
}

func TestDeleteNodes(t *testing.T) {
	manager := &OpenstackManagerMock{}
	nodeCleaner := &NodeCleanerMock{}
	ng := CreateTestNodegroup(manager)
	ng.nodeCleaner = nodeCleaner
	*ng.targetSize = 10

	// Test all working normally
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("CurrentTotalNodes").Return(10, nil).Once()
	manager.On("DeleteNodes", joinedIPs, 5).Return(5, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
	nodeCleaner.On("CleanupNodes", nameList).Return(nil).Once()
	err := ng.DeleteNodes(nodesToDelete)
	assert.NoError(t, err)
	assert.Equal(t, 5, *ng.targetSize)
}

func TestDeleteNodesBatching(t *testing.T) {
	manager := &OpenstackManagerMock{}
	nodeCleaner := &NodeCleanerMock{}
	ng := CreateTestNodegroup(manager)
	ng.nodeCleaner = nodeCleaner
	*ng.targetSize = 10

	// Test all working normally
	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("CurrentTotalNodes").Return(10, nil).Once()
	manager.On("DeleteNodes", joinedIPs, 5).Return(5, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
	nodeCleaner.On("CleanupNodes", nameList).Return(nil).Once()

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
	nodeCleaner := &NodeCleanerMock{}
	ng := CreateTestNodegroup(manager)
	ng.nodeCleaner = nodeCleaner
	ng.minSize = 8
	*ng.targetSize = 10

	// Try to batch 5 nodes for deletion when minSize only allows for two to be deleted

	var twoIPs []string
	var twoJoinedIPs string
	var twoNames []string

	for i := 0; i < 2; i++ {
		twoIPs = append(twoIPs, nodesToDelete[i].Status.Addresses[0].Address)
		twoNames = append(twoNames, nodesToDelete[i].ObjectMeta.Name)
	}
	twoJoinedIPs = strings.Join(twoIPs, ",")

	manager.On("CanUpdate").Return(true, "", nil).Once()
	manager.On("CurrentTotalNodes").Return(10, nil).Once()
	manager.On("DeleteNodes", twoJoinedIPs, 8).Return(5, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateInProgress, nil).Once()
	manager.On("GetClusterStatus").Return(ClusterStatusUpdateComplete, nil).Once()
	nodeCleaner.On("CleanupNodes", twoNames).Return(nil).Once()

	for i := 1; i < 5; i++ {
		go func(i int) {
			// Wait and preserve order
			time.Sleep(time.Second + 100*time.Millisecond*time.Duration(i))
			err := ng.DeleteNodes(nodesToDelete[i:i+1])
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