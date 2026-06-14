package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ClusterConfig holds the configuration for a single Kubernetes cluster.
type ClusterConfig struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Token  string `json:"token"`
}

// ClustersConfig holds the configuration for multiple clusters.
type ClustersConfig struct {
	Clusters []ClusterConfig `json:"clusters"`
}

// LoadClustersConfig loads cluster configurations from a JSON file.
func LoadClustersConfig(path string) (*ClustersConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var cfg ClustersConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if len(cfg.Clusters) == 0 {
		return nil, fmt.Errorf("no clusters defined in config file")
	}

	return &cfg, nil
}

// GetCluster returns the cluster configuration by name.
func (c *ClustersConfig) GetCluster(name string) (*ClusterConfig, error) {
	for _, cluster := range c.Clusters {
		if cluster.Name == name {
			return &cluster, nil
		}
	}
	return nil, fmt.Errorf("cluster %q not found", name)
}
