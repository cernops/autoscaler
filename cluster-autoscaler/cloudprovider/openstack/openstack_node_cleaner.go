package openstack

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"github.com/golang/glog"
	"net/url"
	"strings"
)

func makeKubeClient() *kube_client.Clientset {
	return kube_client.NewForConfigOrDie(getKubeConfig())
}

func getKubeConfig() *rest.Config {
	url, err := url.Parse("")
	if err != nil {
		glog.Fatalf("Failed to parse Kubernetes url: %v", err)
	}

	kubeConfig, err := config.GetKubeClientConfig(url)
	if err != nil {
		glog.Fatalf("Failed to build Kubernetes client configuration: %v", err)
	}

	return kubeConfig
}

// cleanupNodes removes a set of nodes from the kubernetes node list.
// Attempts to remove all nodes in the list and returns an error
// at the end if any removals failed.
func cleanupNodes(kubeClient *kube_client.Clientset, nodeNames []string) error {
	failedDeletions := []string{}
	for _, nodeName := range nodeNames {
		glog.Infof("cleaning up node %s", nodeName)
		err := kubeClient.CoreV1().Nodes().Delete(nodeName, &metav1.DeleteOptions{})
		if err != nil {
			glog.Errorf("Failed to remove node %s from kubernetes node list: %v", nodeName, err)
			failedDeletions = append(failedDeletions, nodeName)
			continue
		}
		glog.Infof("finished cleaning up node %s", nodeName)
	}
	if len(failedDeletions) > 0 {
		return fmt.Errorf("could not clean up one or more scaled down nodes: %s", strings.Join(failedDeletions, ", "))
	}
	return nil
}


func checkNodesAccess(kubeClient *kube_client.Clientset) error {
	_, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("could not list nodes: %v", err)
	}
	return nil
}