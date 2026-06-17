package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"apiserverproxy/internal/cache"
	"apiserverproxy/internal/config"
	ilog "apiserverproxy/internal/log"
	"apiserverproxy/internal/proxy"
	"apiserverproxy/internal/router"
)

func main() {
	configFile := flag.String("config-file", "", "Path to clusters config file (config.json)")
	listenAddr := flag.String("listen", ":8080", "Proxy listen address")
	flag.Parse()

	if *configFile == "" {
		log.Fatal("--config-file is required")
	}

	clusters, err := config.LoadClustersConfig(*configFile)
	if err != nil {
		log.Fatalf("load clusters config: %v", err)
	}

	ilog.Setup(clusters.Log)

	log.Infof("Loaded %d clusters from %s", len(clusters.Clusters), *configFile)
	for _, c := range clusters.Clusters {
		log.Infof("  - %s: %s", c.Name, c.Server)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cacheManager := cache.InitFromConfig(ctx, clusters.Clusters)
	engine, handler := router.New(clusters, cacheManager)

	// Start config watcher for hot-reload
	watcher := config.NewWatcher(*configFile, func(newCfg *config.ClustersConfig) {
		reloadClusters(ctx, cacheManager, handler, clusters, newCfg)
		clusters = newCfg
	})
	go func() {
		if err := watcher.Start(ctx); err != nil {
			log.Errorf("config watcher stopped: %v", err)
		}
	}()

	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	go func() {
		log.Infof("Listening on %s", *listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down proxy...")
	cancel() // stop config watcher

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Server shutdown error: %v", err)
	}

	cacheManager.StopAll()
	log.Info("Proxy stopped")
}

// reloadClusters diffs old and new config, then starts/stops caches accordingly.
func reloadClusters(ctx context.Context, mgr *cache.Manager, handler *proxy.MultiClusterHandler, oldCfg, newCfg *config.ClustersConfig) {
	oldSet := make(map[string]config.ClusterConfig)
	for _, c := range oldCfg.Clusters {
		oldSet[c.Name] = c
	}
	newSet := make(map[string]config.ClusterConfig)
	for _, c := range newCfg.Clusters {
		newSet[c.Name] = c
	}

	// Stop removed clusters
	for name := range oldSet {
		if _, exists := newSet[name]; !exists {
			log.Infof("stopping cache for removed cluster: %s", name)
			mgr.Stop(name)
		}
	}

	// Start or restart changed/added clusters
	for name, newCluster := range newSet {
		old, exists := oldSet[name]
		changed := !exists || old.Server != newCluster.Server || old.Token != newCluster.Token

		if !changed {
			continue
		}

		if exists {
			log.Infof("restarting cache for changed cluster: %s", name)
			mgr.Stop(name)
		} else {
			log.Infof("starting cache for new cluster: %s", name)
		}

		cfg := &rest.Config{
			Host:        newCluster.Server,
			BearerToken: newCluster.Token,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		if err := mgr.Start(ctx, name, cfg); err != nil {
			log.Errorf("start cache for %s: %v", name, err)
		}
	}

	handler.Reload(newCfg)
}
