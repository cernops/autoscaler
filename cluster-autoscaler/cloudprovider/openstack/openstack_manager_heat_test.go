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
	"github.com/gophercloud/gophercloud"
	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/stretchr/testify/assert"
	"net/http"
	"testing"
)

var clusterUUID = "732851e1-f792-4194-b966-4cbfa5f30093"
var clusterNodeCount = 5
var clusterStatus = clusterStatusUpdateComplete
var clusterStackID = "2e35472f-d3c1-40b1-93fe-a421db19cc89"

var clusterGetResponseSuccess = fmt.Sprintf(`
{
    "create_timeout":60,
    "links":[
        {
            "href":"http://10.100.101.102:9511/v1/clusters/732851e1-f792-4194-b966-4cbfa5f30093",
            "rel":"self"
        },
        {
            "href":"http://10.100.101.102:9511/clusters/732851e1-f792-4194-b966-4cbfa5f30093",
            "rel":"bookmark"
        }
    ],
    "labels":{
    },
    "updated_at":"2019-02-07T14:07:33+00:00",
    "keypair":"test-key",
    "master_flavor_id":"m2.medium",
    "user_id":"tester",
    "uuid":"%s",
    "api_address":"https://172.24.4.6:6443",
    "master_addresses":[
        "172.24.4.6"
    ],
    "node_count":%d,
    "project_id":"d656bd69-82a2-4efe-bbee-abd0b0a83252",
    "status":"%s",
    "docker_volume_size":null,
    "master_count":1,
    "node_addresses":[
        "172.24.4.10"
    ],
    "status_reason":"Stack UPDATE completed successfully",
    "coe_version":"v1.9.3",
    "cluster_template_id":"af067ca7-d0a2-43ee-9529-40e538072d86",
    "name":"cluster-01",
    "stack_id":"%s",
    "created_at":"2019-02-01T13:13:48+00:00",
    "discovery_url":"https://discovery.etcd.io/efa82529a12040a19deec2e7addd3183",
    "flavor_id":"m2.medium",
    "container_version":"1.12.6"
}`, clusterUUID, clusterNodeCount, clusterStatus, clusterStackID)

var badClusterUUID = "fa9887b8-e8d8-46d1-a82f-3c28618f8db1"

var clusterGetResponseFail = fmt.Sprintf(`
{
    "errors":[
        {
            "status":404,
            "code":"client",
            "links":[

            ],
            "title":"Cluster %s could not be found",
            "detail":"Cluster %s could not be found.",
            "request_id":""
        }
    ]
}`, badClusterUUID, badClusterUUID)

var stackName = "cluster-01-aftjnjwdczjr"
var stackID = "2e35472f-d3c1-40b1-93fe-a421db19cc89"
var stackStatus = "UPDATE_COMPLETE"

var badStackID = "708389ff-10fa-4008-9e0f-a3d5f979e88c"

var stackListReponse = fmt.Sprintf(`
{
    "stacks":[
        {
            "description":"",
            "parent":null,
            "stack_status_reason":"Stack UPDATE completed successfully",
            "stack_name":"%s",
            "stack_user_project_id":"0aa1d684229d4b5985b090e5b2c651d6",
            "deletion_time":null,
            "creation_time":"2019-02-01T13:13:55Z",
            "links":[
                {
                    "href":"https://10.100.101.102:8004/v1/d656bd69-82a2-4efe-bbee-abd0b0a83252/stacks/cluster-01-aftjnjwdczjr/2e35472f-d3c1-40b1-93fe-a421db19cc89",
                    "rel":"self"
                }
            ],
            "updated_time":"2019-02-07T14:06:29Z",
            "stack_owner":null,
            "stack_status":"%s",
            "id":"%s",
            "tags":null
        },
        {
            "description":"",
            "parent":null,
            "stack_status_reason":"Stack CREATE completed successfully",
            "stack_name":"other-02-bpadbo73s3cw",
            "stack_user_project_id":"466128b394ef4c07a75a3cdac9cf6441",
            "deletion_time":null,
            "creation_time":"2018-12-11T14:19:41Z",
            "links":[
                {
                    "href":"https://10.100.101.102:8004/v1/d656bd69-82a2-4efe-bbee-abd0b0a83252/stacks/other-02-bpadbo73s3cw/d8cd9889-5c12-4622-964e-71177b49144f",
                    "rel":"self"
                }
            ],
            "updated_time":null,
            "stack_owner":null,
            "stack_status":"CREATE_COMPLETE",
            "id":"d8cd9889-5c12-4622-964e-71177b49144f",
            "tags":null
        }
    ]
}`, stackName, stackStatus, stackID)

var stackGetResponse = `
{
    "stack":{
        "parent":null,
        "disable_rollback":true,
        "description":"This template will boot a Kubernetes cluster with one or more minions.\n",
        "parameters":{
            "OS::stack_id":"2e35472f-d3c1-40b1-93fe-a421db19cc89",
            "OS::project_id":"d656bd69-82a2-4efe-bbee-abd0b0a83252",
            "OS::stack_name":"cluster-01-aftjnjwdczjr"
        },
        "deletion_time":null,
        "stack_name":"%s",
        "stack_user_project_id":"0aa1d684229d4b5985b090e5b2c651d6",
        "stack_status_reason":"Stack UPDATE completed successfully",
        "creation_time":"2019-02-01T13:13:55Z",
        "links":[
            {
                "href":"https://10.100.101.102:8004/v1/d656bd69-82a2-4efe-bbee-abd0b0a83252/stacks/cluster-01-aftjnjwdczjr/2e35472f-d3c1-40b1-93fe-a421db19cc89",
                "rel":"self"
            }
        ],
        "capabilities":[

        ],
        "notification_topics":[

        ],
        "tags":null,
        "timeout_mins":60,
        "stack_status":"%s",
        "stack_owner":null,
        "updated_time":"2019-02-07T14:06:29Z",
        "id":"%s",
        "outputs":[
        ]
    }
}`

var stackGetResponseSuccess = fmt.Sprintf(stackGetResponse, stackName, stackStatus, stackID)

var stackGetResponseNotFound = fmt.Sprintf(`
{
    "explanation":"The resource could not be found.",
    "code":404,
    "error":{
        "message":"The Stack (%s) could not be found.",
        "traceback":null,
        "type":"EntityNotFound"
    },
    "title":"Not Found"
}`, badStackID)

var patchResponseSuccess = `{"uuid": "732851e1-f792-4194-b966-4cbfa5f30093"}`

var stackUpdateResponseFail = fmt.Sprintf(`
{
    "errors":[
        {
            "status":404,
            "code":"client",
            "links":[

            ],
            "title":"Cluster %s could not be found",
            "detail":"Cluster %s could not be found.",
            "request_id":""
        }
    ]
}`, badClusterUUID, badClusterUUID)

var stackBadPatchResponse = `
{
    "explanation":"The server could not comply with the request since it is either malformed or otherwise incorrect.",
    "code":400,
    "error":{
        "message":"Property error: : resources.kube_minions.properties.count: : -1 is out of range (min: 0, max: None)",
        "traceback":null,
        "type":"StackValidationFailed"
    },
    "title":"Bad Request"
}`

func CreateTestServiceClient() *gophercloud.ServiceClient {
	return &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{TokenID: "cbc36478b0bd8e67e89469c7749d4127"},
		Endpoint:       th.Endpoint() + "v1/",
	}
}

func CreateTestOpenstackManagerHeat(client *gophercloud.ServiceClient) *OpenstackManagerHeat {
	return &OpenstackManagerHeat{
		clusterClient: client,
		heatClient:    client,
	}
}

func CreateManagerGetClusterSuccess() *OpenstackManagerHeat {
	th.Mux.HandleFunc("/v1/clusters/"+clusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, clusterGetResponseSuccess)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID

	return osm
}

func CreateManagerGetClusterFail() *OpenstackManagerHeat {
	th.Mux.HandleFunc("/v1/clusters/"+clusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)

		fmt.Fprint(w, clusterGetResponseFail)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = badClusterUUID

	return osm
}

func TestNodeGroupSizeSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterSuccess()

	nodeCount, err := osm.NodeGroupSize("default")
	assert.NoError(t, err)
	assert.Equal(t, clusterNodeCount, nodeCount)
}

func TestNodeGroupSizeFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterFail()

	_, err := osm.NodeGroupSize("default")
	assert.Error(t, err)
	assert.Equal(t, "could not get cluster: Resource not found", err.Error())
}

func TestGetClusterStatusSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterSuccess()

	gotStatus, err := osm.GetClusterStatus()
	assert.NoError(t, err)
	assert.Equal(t, clusterStatus, gotStatus)
}

func TestGetClusterStatusFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterFail()

	_, err := osm.GetClusterStatus()
	assert.Error(t, err)
	assert.Equal(t, "could not get cluster: Resource not found", err.Error())
}

func TestCanUpdateSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterSuccess()

	can, status, err := osm.CanUpdate()
	assert.NoError(t, err)
	assert.Equal(t, true, can)
	assert.Equal(t, clusterStatus, status)
}

func TestCanUpdateFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterFail()

	_, _, err := osm.CanUpdate()
	assert.Error(t, err)
	assert.Equal(t, "could not get cluster status: could not get cluster: Resource not found", err.Error())
}

func TestGetStackIDSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterSuccess()

	ID, err := osm.GetStackID()
	assert.NoError(t, err)
	assert.Equal(t, clusterStackID, ID)
}

func TestGetStackIDFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	osm := CreateManagerGetClusterFail()

	_, err := osm.GetStackID()
	assert.Error(t, err)
	assert.Equal(t, "could not get cluster: Resource not found", err.Error())
}

func TestGetStackNameSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, stackListReponse)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = stackID

	gotStackName, err := osm.GetStackName()
	assert.NoError(t, err)
	assert.Equal(t, stackName, gotStackName)
}

func TestGetStackNameNotFound(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, stackListReponse)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = badStackID

	_, err := osm.GetStackName()
	assert.Error(t, err)
	assert.Equal(t, fmt.Sprintf("stack with ID %s not found", badStackID), err.Error())
}

func TestGetStackStatusSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, stackGetResponseSuccess)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = stackID
	osm.stackName = stackName

	status, err := osm.GetStackStatus()
	assert.NoError(t, err)
	assert.Equal(t, stackStatus, status)
}

func TestGetStackStatusFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)

		fmt.Fprint(w, stackGetResponseNotFound)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = badStackID
	osm.stackName = stackName

	_, err := osm.GetStackStatus()
	assert.Error(t, err)
	assert.Equal(t, "could not get stack from heat: Resource not found", err.Error())
}

func TestUpdateNodeCountSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/clusters/"+clusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)

		fmt.Fprint(w, patchResponseSuccess)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID

	err := osm.UpdateNodeCount("default", 2)
	assert.NoError(t, err)
}

func TestUpdateNodeCountFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/clusters/"+badClusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)

		fmt.Fprint(w, stackUpdateResponseFail)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = badClusterUUID

	err := osm.UpdateNodeCount("default", 2)
	assert.Error(t, err)
	assert.Equal(t, "could not update cluster: Resource not found", err.Error())
}

func TestDeleteNodesSuccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	GETRequestCount := 0

	// Handle stack PATCH, then two stack GETS (for checking UPDATE_IN_PROGRESS -> UPDATE_COMPLETE)
	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.Method == "GET" {
			if GETRequestCount == 0 {
				GETRequestCount += 1
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, stackGetResponse, stackName, stackStatusUpdateInProgress, stackID)
			} else if GETRequestCount == 1 {
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, stackGetResponse, stackName, stackStatusUpdateComplete, stackID)
			}
		}
	})

	// Handle cluster node_count PATCH
	th.Mux.HandleFunc("/v1/clusters/"+clusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, patchResponseSuccess)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = stackID
	osm.stackName = stackName

	err := osm.DeleteNodes("default", []string{"172.24.4.15"}, 1)
	assert.NoError(t, err)
}

func TestDeleteNodesStackPatchFail(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	GETRequestCount := 0

	// Handle bad stack PATCH, then two stack GETS (for checking UPDATE_IN_PROGRESS -> UPDATE_COMPLETE)
	th.Mux.HandleFunc("/v1/stacks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, stackBadPatchResponse)
			return
		}
		if r.Method == http.MethodGet {
			if GETRequestCount == 0 {
				GETRequestCount += 1
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, stackGetResponse, stackName, stackStatusUpdateInProgress, stackID)
			} else if GETRequestCount == 1 {
				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, stackGetResponse, stackName, stackStatusUpdateComplete, stackID)
			}
		}
	})

	// Handle cluster node_count PATCH
	th.Mux.HandleFunc("/v1/clusters/"+clusterUUID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
	})

	sc := CreateTestServiceClient()

	osm := CreateTestOpenstackManagerHeat(sc)
	osm.clusterName = clusterUUID
	osm.stackID = stackID
	osm.stackName = stackName

	err := osm.DeleteNodes("default", []string{"172.24.4.15"}, -1)
	assert.Error(t, err)

	expectedErrString := fmt.Sprintf("stack patch failed: Bad request with: [PATCH %s/v1/stacks/%s/%s], error message: %s",
		th.Server.URL, stackName, stackID, stackBadPatchResponse)

	assert.Equal(t, expectedErrString, err.Error())
}
