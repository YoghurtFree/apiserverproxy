package router

import (
	"github.com/gin-gonic/gin"

	"apiserverproxy/internal/cache"
	"apiserverproxy/internal/config"
	"apiserverproxy/internal/middleware"
	"apiserverproxy/internal/proxy"
)

// New creates a Gin engine with all routes registered and returns the handler for hot-reload.
func New(clusters *config.ClustersConfig, cacheManager *cache.Manager) (*gin.Engine, *proxy.MultiClusterHandler) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.TraceID())
	router.Use(middleware.AccessLog())

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
	router.GET("/clusters/:cluster/*path", handler.Handle)
	router.POST("/clusters/:cluster/*path", handler.Handle)
	router.PUT("/clusters/:cluster/*path", handler.Handle)
	router.DELETE("/clusters/:cluster/*path", handler.Handle)
	router.PATCH("/clusters/:cluster/*path", handler.Handle)
	router.HEAD("/clusters/:cluster/*path", handler.Handle)
	router.OPTIONS("/clusters/:cluster/*path", handler.Handle)

	return router, handler
}
