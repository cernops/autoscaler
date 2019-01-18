# Cluster Autoscaler on OpenStack
The cluster autoscaler on OpenStack scales worker nodes within any
specified nodegroup. It will run as a `Deployment` in your cluster.
This README will go over some of the necessary steps required to get
the cluster autoscaler up and running.

## Permissions and credentials

The autoscaler needs a `ServiceAccount` with permissions for Kubernetes and
requires credentials for interacting with OpenStack.

An example `ServiceAccount` is given in `examples/os-autoscaler-svcaccount.yaml`.
(Currently all permissions are granted, the example will be changed later
with more restrictive permissions)

The required credentials for OpenStack can be obtained from your master node.
The `kube_openstack_config` file is stored in a secret and then mounted into
the autoscaler container.

```
$ scp fedora@scaler-01-d5hgjhjra4wc-master-0:/etc/kubernetes/kube_openstack_config .
$ kubectl -n kube-system create secret generic autoscaler-os-creds --from-file=kube_openstack_config
```

## Autoscaler deployment

The deployment in `examples/os-autoscaler-deployment.yaml` can be used,
but the arguments passed to the autoscaler will need to be changed
to match your cluster.

| Argument         | Usage                                                                                                                                |
|------------------|--------------------------------------------------------------------------------------------------------------------------------------|
| --cluster-name   | The name of your Magnum cluster. If there are multiple clusters sharing the same name then the cluster IDs should be used instead.   |
| --cloud-provider | Can be omitted if the autoscaler is built with `BUILD_TAGS=openstack`.                                                               |
| --nodes          | Of the form `min:max:NodegroupName`. Nodegroups are not yet implemented in Magnum so only a single nodegroup is supported currently. |

## Notes

The autoscaler will not remove nodes which have non-default kube-system pods.
This prevents the node that the autoscaler is running on from being scaled down.
If you are deploying the autoscaler into a cluster which already has more than one node,
it is best to deploy it onto any node which already has non-default kube-system pods,
to minimise the number of nodes which cannot be removed when scaling.