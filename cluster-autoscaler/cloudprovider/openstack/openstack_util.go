package openstack

import "time"

const (
	ClusterStatusUpdateInProgress = "UPDATE_IN_PROGRESS"
	ClusterStatusUpdateComplete   = "UPDATE_COMPLETE"
	ClusterStatusUpdateFailed     = "UPDATE_FAILED"

	WaitForUpdateStatusTimeout  = 30 * time.Second
	WaitForCompleteStatusTimout = 10 * time.Minute
)
