package aifeedhttp

import "github.com/gin-gonic/gin"

// RegisterPublicRoutes registers the read-only storefront feed.
func RegisterPublicRoutes(public gin.IRoutes, handler *Handler) {
	if public == nil || handler == nil {
		panic("ai feed public route: required dependency is nil")
	}
	public.GET("/ai-feed", handler.Get)
}
