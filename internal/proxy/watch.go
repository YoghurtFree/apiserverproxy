package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// WatchHandler handles Kubernetes watch requests with streaming.
type WatchHandler struct {
	target      *url.URL
	client      *http.Client
	bearerToken string
}

// NewWatchHandler creates a new watch handler.
func NewWatchHandler(target *url.URL, tlsConfig *tls.Config, bearerToken string) *WatchHandler {
	transport := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}

	return &WatchHandler{
		target: target,
		client: &http.Client{
			Timeout:   0, // No timeout for long-running watch
			Transport: transport,
		},
		bearerToken: bearerToken,
	}
}

// Handle streams watch events from the Kubernetes API server.
func (h *WatchHandler) Handle(c *gin.Context) {
	// Build target URL with watch parameters
	targetURL := h.target.JoinPath(c.Request.URL.Path)
	targetURL.RawQuery = c.Request.URL.RawQuery

	// Create watch request
	watchReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL.String(),
		c.Request.Body,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create watch request: %v", err)})
		return
	}

	// Copy headers
	copyHeaders(watchReq.Header, c.Request.Header)
	removeHopByHopHeaders(watchReq.Header)

	// Set bearer token if configured
	if h.bearerToken != "" {
		watchReq.Header.Set("Authorization", "Bearer "+h.bearerToken)
	}

	// Forward watch request
	resp, err := h.client.Do(watchReq)
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

// streamResponse copies response body with immediate flushing for each chunk.
func streamResponse(w gin.ResponseWriter, r io.Reader) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: just copy without flushing
		io.Copy(w, r)
		return
	}

	buf := bufio.NewReader(r)
	for {
		line, err := buf.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				// Connection closed or error
			}
			return
		}

		// Write the line
		w.Write(line)

		// Flush immediately for streaming
		flusher.Flush()
	}
}