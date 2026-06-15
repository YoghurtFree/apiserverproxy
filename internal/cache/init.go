package cache

import (
	"context"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"apiserverproxy/internal/config"
)

// InitFromConfig creates a cache manager and starts caches for all clusters.
func InitFromConfig(ctx context.Context, clusters []config.ClusterConfig) *Manager {
	mgr := NewManager()

	for _, cluster := range clusters {
		cfg := &rest.Config{
			Host:        cluster.Server,
			BearerToken: cluster.Token,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		if err := mgr.Start(ctx, cluster.Name, cfg); err != nil {
			log.Warnf("failed to start cache for %s: %v", cluster.Name, err)
		} else {
			log.Infof("Cache started for cluster %s", cluster.Name)
		}
	}

	return mgr
}
