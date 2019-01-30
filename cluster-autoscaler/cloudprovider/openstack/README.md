# Cluster Autoscaler on OpenStack
The cluster autoscaler on OpenStack scales worker nodes within any
specified nodegroup. It will run as a `Deployment` in your cluster.
This README will go over some of the necessary steps required to get
the cluster autoscaler up and running.

## Permissions and credentials

The autoscaler needs a `ServiceAccount` with permissions for Kubernetes and
requires credentials for interacting with OpenStack.

An example `ServiceAccount` is given in [examples/os-autoscaler-svcaccount.yaml](examples/os-autoscaler-svcaccount.yaml).

The credentials for authenticating with OpenStack are stored in a secret and
mounted as a file inside the container. [examples/os-autoscaler-secret](examples/os-autoscaler-secret.yaml)
can be modified with the contents of your cloud-config. This file can be obtained from your master node,
in `/etc/kubernetes` (may be named `kube_openstack_config` instead of `cloud-config`).

## Autoscaler deployment

The deployment in `examples/os-autoscaler-deployment.yaml` can be used,
but the arguments passed to the autoscaler will need to be changed
to match your cluster.

| Argument         | Usage                                                                                                                                      |
|------------------|--------------------------------------------------------------------------------------------------------------------------------------------|
| --cluster-name   | The name of your Kubernetes cluster. If there are multiple clusters sharing the same name then the cluster IDs should be used instead.     |
| --cloud-provider | Can be omitted if the autoscaler is built with `BUILD_TAGS=openstack`.                                                                     |
| --nodes          | Of the form `min:max:NodeGroupName`. Node groups are not yet implemented in Magnum so only a single node group is currently supported.     |

## Notes

The OpenStack cloud provider currently only supports Magnum clusters using Heat.
This can be extended with other implementations of OpenstackManager,
which is the interface providing the required interactions with the cluster.

The autoscaler will not remove nodes which have non-default kube-system pods.
This prevents the node that the autoscaler is running on from being scaled down.
If you are deploying the autoscaler into a cluster which already has more than one node,
it is best to deploy it onto any node which already has non-default kube-system pods,
to minimise the number of nodes which cannot be removed when scaling.

Or, if you are using a Magnum version which supports scheduling on the master node, then
the example deployment file
[examples/os-autoscaler-deployment-master.yaml](examples/os-autoscaler-deployment-master.yaml)
can be used.