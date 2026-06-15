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

	"apiserverproxy/internal/cache"
	"apiserverproxy/internal/config"
	ilog "apiserverproxy/internal/log"
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

	cacheManager := cache.InitFromConfig(context.Background(), clusters.Clusters)
	engine := router.New(clusters, cacheManager)

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Errorf("Server shutdown error: %v", err)
	}

	log.Info("Proxy stopped")
}
