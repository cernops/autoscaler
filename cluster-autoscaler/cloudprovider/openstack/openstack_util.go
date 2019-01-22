package openstack

import (
	"time"

	provider_os "k8s.io/kubernetes/pkg/cloudprovider/providers/openstack"

	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	"github.com/gophercloud/gophercloud"
)

const (
	ClusterStatusUpdateInProgress = "UPDATE_IN_PROGRESS"
	ClusterStatusUpdateComplete   = "UPDATE_COMPLETE"
	ClusterStatusUpdateFailed     = "UPDATE_FAILED"

	WaitForUpdateStatusTimeout  = 30 * time.Second
	WaitForCompleteStatusTimout = 10 * time.Minute
)

func toAuthOptsExt(cfg provider_os.Config) trusts.AuthOptsExt {
	opts := gophercloud.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthURL,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserID,
		Password:         cfg.Global.Password,
		TenantID:         cfg.Global.TenantID,
		TenantName:       cfg.Global.TenantName,
		DomainID:         cfg.Global.DomainID,
		DomainName:       cfg.Global.DomainName,

		// Persistent service, so we need to be able to renew tokens.
		AllowReauth: true,
	}

	return trusts.AuthOptsExt{
		TrustID:            cfg.Global.TrustID,
		AuthOptionsBuilder: &opts,
	}
}