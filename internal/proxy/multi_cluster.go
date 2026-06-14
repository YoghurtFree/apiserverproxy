package proxy

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"

	"apiserverproxy/internal/cache"
	"apiserverproxy/internal/config"
)

// MultiClusterHandler proxies requests to multiple Kubernetes clusters.
type MultiClusterHandler struct {
	clusters *config.ClustersConfig
	clients  map[string]*http.Client
	cache    *cache.Manager
}

// NewMultiClusterHandler creates a new multi-cluster proxy handler.
func NewMultiClusterHandler(clusters *config.ClustersConfig, cacheManager *cache.Manager) *MultiClusterHandler {
	clients := make(map[string]*http.Client)
	for _, cluster := range clusters.Clusters {
		clients[cluster.Name] = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				TLSHandshakeTimeout: 10 * time.Second,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			},
		}
	}

	return &MultiClusterHandler{
		clusters: clusters,
		clients:  clients,
		cache:    cacheManager,
	}
}

// Handle is the Gin handler that proxies requests to the appropriate cluster.
func (h *MultiClusterHandler) Handle(c *gin.Context) {
	// Extract cluster name from path: /{cluster}/...
	clusterName := c.Param("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster name is required"})
		return
	}

	// Get cluster configuration
	cluster, err := h.clusters.GetCluster(clusterName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Get the HTTP client for this cluster
	client, ok := h.clients[clusterName]
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("no client for cluster %q", clusterName)})
		return
	}

	// Extract the actual API path (remove /{cluster} prefix)
	apiPath := c.Param("path")
	if apiPath == "" {
		apiPath = "/"
	}

	// Check if this is a list pods request (not watch) and cache is available
	if h.isListPodsRequest(c.Request, apiPath) && !isWatchRequest(c.Request) && h.cache.HasCache(clusterName) {
		h.handleListPods(c, clusterName, apiPath)
		return
	}

	// Check if this is a list services request (not watch) and cache is available
	if h.isListServicesRequest(c.Request, apiPath) && !isWatchRequest(c.Request) && h.cache.HasCache(clusterName) {
		h.handleListServices(c, clusterName, apiPath)
		return
	}

	// Build target URL
	targetURL := fmt.Sprintf("%s%s", cluster.Server, apiPath)
	if c.Request.URL.RawQuery != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, c.Request.URL.RawQuery)
	}

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL,
		c.Request.Body,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create request: %v", err)})
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, c.Request.Header)
	removeHopByHopHeaders(proxyReq.Header)

	// Set bearer token
	if cluster.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+cluster.Token)
	}

	// Check if this is a watch request
	if isWatchRequest(c.Request) {
		h.handleWatch(c, client, proxyReq)
		return
	}

	// Forward regular request
	resp, err := client.Do(proxyReq)
	if err != nil {
		if strings.Contains(err.Error(), "context canceled") {
			c.Status(http.StatusRequestTimeout)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("forward request: %v", err)})
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, v := range resp.Header {
		c.Header(k, strings.Join(v, ", "))
	}

	// Write status and body
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

// isListPodsRequest checks if the request is a list pods request.
func (h *MultiClusterHandler) isListPodsRequest(req *http.Request, apiPath string) bool {
	if req.Method != "GET" {
		return false
	}

	// Match /api/v1/pods or /api/v1/namespaces/{namespace}/pods
	podPathRegex := regexp.MustCompile(`^/api/v1/(namespaces/[^/]+/)?pods$`)
	return podPathRegex.MatchString(apiPath)
}

// isListServicesRequest checks if the request is a list services request.
func (h *MultiClusterHandler) isListServicesRequest(req *http.Request, apiPath string) bool {
	if req.Method != "GET" {
		return false
	}

	// Match /api/v1/services or /api/v1/namespaces/{namespace}/services
	servicePathRegex := regexp.MustCompile(`^/api/v1/(namespaces/[^/]+/)?services$`)
	return servicePathRegex.MatchString(apiPath)
}

// handleListPods handles list pods requests using cache.
func (h *MultiClusterHandler) handleListPods(c *gin.Context, cluster, apiPath string) {
	// Extract namespace from path if present
	namespace := extractNamespace(apiPath)

	// Extract labelSelector from query parameters
	labelSelector := c.Query("labelSelector")
	ls, err := labels.Parse(labelSelector)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid labelSelector: %v", err)})
		return
	}

	// Extract fieldSelector from query parameters
	fieldSelector := c.Query("fieldSelector")
	var fs fields.Selector
	if fieldSelector != "" {
		fs, err = fields.ParseSelector(fieldSelector)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid fieldSelector: %v", err)})
			return
		}
	}

	pods, err := h.cache.ListPods(c.Request.Context(), cluster, namespace, ls, fs)
	if err != nil {
		// Fallback to API server if cache fails
		h.proxyRequest(c, cluster, apiPath)
		return
	}

	// Convert to JSON response
	data, err := json.Marshal(pods)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("marshal pods: %v", err)})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("X-Cache", "HIT")
	c.Status(http.StatusOK)
	c.Writer.Write(data)
}

// handleListServices handles list services requests using cache.
func (h *MultiClusterHandler) handleListServices(c *gin.Context, cluster, apiPath string) {
	// Extract namespace from path if present
	namespace := extractNamespace(apiPath)

	// Extract labelSelector from query parameters
	labelSelector := c.Query("labelSelector")
	ls, err := labels.Parse(labelSelector)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid labelSelector: %v", err)})
		return
	}

	// Extract fieldSelector from query parameters
	fieldSelector := c.Query("fieldSelector")
	var fs fields.Selector
	if fieldSelector != "" {
		fs, err = fields.ParseSelector(fieldSelector)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid fieldSelector: %v", err)})
			return
		}
	}

	services, err := h.cache.ListServices(c.Request.Context(), cluster, namespace, ls, fs)
	if err != nil {
		// Fallback to API server if cache fails
		h.proxyRequest(c, cluster, apiPath)
		return
	}

	// Convert to JSON response
	data, err := json.Marshal(services)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("marshal services: %v", err)})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("X-Cache", "HIT")
	c.Status(http.StatusOK)
	c.Writer.Write(data)
}

// extractNamespace extracts namespace from API path.
func extractNamespace(apiPath string) string {
	// Match /api/v1/namespaces/{namespace}/pods or /api/v1/namespaces/{namespace}/services
	re := regexp.MustCompile(`^/api/v1/namespaces/([^/]+)/(pods|services)$`)
	matches := re.FindStringSubmatch(apiPath)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// parseLabelSelector parses labelSelector string into map.
// Example: "app=nginx,env=prod" -> {"app": "nginx", "env": "prod"}
func parseLabelSelector(selector string) map[string]string {
	if selector == "" {
		return nil
	}

	labels := make(map[string]string)
	pairs := strings.Split(selector, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			labels[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return labels
}

// proxyRequest proxies the request to the API server (fallback).
func (h *MultiClusterHandler) proxyRequest(c *gin.Context, clusterName, apiPath string) {
	cluster, err := h.clusters.GetCluster(clusterName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	client := h.clients[clusterName]

	// Build target URL
	targetURL := fmt.Sprintf("%s%s", cluster.Server, apiPath)
	if c.Request.URL.RawQuery != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, c.Request.URL.RawQuery)
	}

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL,
		c.Request.Body,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create request: %v", err)})
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, c.Request.Header)
	removeHopByHopHeaders(proxyReq.Header)

	// Set bearer token
	if cluster.Token != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+cluster.Token)
	}

	// Forward request
	resp, err := client.Do(proxyReq)
	if err != nil {
		if strings.Contains(err.Error(), "context canceled") {
			c.Status(http.StatusRequestTimeout)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("forward request: %v", err)})
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, v := range resp.Header {
		c.Header(k, strings.Join(v, ", "))
	}

	// Write status and body
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

// isWatchRequest checks if the request is a Kubernetes watch request.
func isWatchRequest(req *http.Request) bool {
	return req.URL.Query().Get("watch") == "true"
}

// copyHeaders copies headers from src to dst.
func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

// removeHopByHopHeaders removes headers that should not be forwarded.
func removeHopByHopHeaders(h http.Header) {
	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, k := range hopByHop {
		h.Del(k)
	}
}

// handleWatch handles watch requests with streaming.
func (h *MultiClusterHandler) handleWatch(c *gin.Context, client *http.Client, req *http.Request) {
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "context canceled") {
			c.Status(http.StatusRequestTimeout)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("forward watch request: %v", err)})
		return
	}
	defer resp.Body.Close()

	// Copy response headers for streaming
	for k, v := range resp.Header {
		c.Header(k, strings.Join(v, ", "))
	}

	// Ensure chunked encoding for streaming
	c.Header("Transfer-Encoding", "chunked")
	c.Header("Cache-Control", "no-cache")

	// Write status
	c.Status(resp.StatusCode)

	// Stream response body with flushing
	streamResponse(c.Writer, resp.Body)
}
