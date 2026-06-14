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

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"

	"apiserverproxy/internal/cache"
	"apiserverproxy/internal/config"
	"apiserverproxy/internal/proxy"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config-file", "", "Path to clusters config file (config.json)")
	listenAddr := flag.String("listen", ":8080", "Proxy listen address")
	flag.Parse()

	if *configFile == "" {
		log.Fatal("--config-file is required")
	}

	// Load clusters config
	clusters, err := config.LoadClustersConfig(*configFile)
	if err != nil {
		log.Fatalf("load clusters config: %v", err)
	}

	log.Infof("Loaded %d clusters from %s", len(clusters.Clusters), *configFile)
	for _, c := range clusters.Clusters {
		log.Infof("  - %s: %s", c.Name, c.Server)
	}

	// Create Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// Create cache manager
	cacheManager := cache.NewManager()

	// Start cache for each cluster
	for _, cluster := range clusters.Clusters {
		cfg := &rest.Config{
			Host:        cluster.Server,
			BearerToken: cluster.Token,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
		if err := cacheManager.Start(context.Background(), cluster.Name, cfg); err != nil {
			log.Warnf("failed to start cache for %s: %v", cluster.Name, err)
		} else {
			log.Infof("Cache started for cluster %s", cluster.Name)
		}
	}

	handler := proxy.NewMultiClusterHandler(clusters, cacheManager)

	// 原有路由: /{cluster}/api/v1/pods
	router.GET("/:cluster/*path", handler.Handle)
	router.POST("/:cluster/*path", handler.Handle)
	router.PUT("/:cluster/*path", handler.Handle)
	router.DELETE("/:cluster/*path", handler.Handle)
	router.PATCH("/:cluster/*path", handler.Handle)
	router.HEAD("/:cluster/*path", handler.Handle)
	router.OPTIONS("/:cluster/*path", handler.Handle)

	// SDK 兼容路由: /clusters/{cluster}/api/v1/pods
	// K8s SDK 可将 host 设为 http://proxy:8080/clusters/{cluster} 来使用
	router.GET("/clusters/:cluster/*path", handler.Handle)
	router.POST("/clusters/:cluster/*path", handler.Handle)
	router.PUT("/clusters/:cluster/*path", handler.Handle)
	router.DELETE("/clusters/:cluster/*path", handler.Handle)
	router.PATCH("/clusters/:cluster/*path", handler.Handle)
	router.HEAD("/clusters/:cluster/*path", handler.Handle)
	router.OPTIONS("/clusters/:cluster/*path", handler.Handle)

	// Create HTTP server
	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}

	// Start server in goroutine
	go func() {
		log.Infof("Listening on %s", *listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down proxy...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Errorf("Server shutdown error: %v", err)
	}

	log.Info("Proxy stopped")
}
