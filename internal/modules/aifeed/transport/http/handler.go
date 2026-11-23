package aifeedhttp

import (
	"context"
	"time"

	aifeedcontract "github.com/dujiao-next/internal/modules/aifeed/contract"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// Reader is the public feed cache. It must not create orders or payments.
type Reader interface {
	Snapshot(context.Context) aifeedcontract.Snapshot
}

// Handler serves the sanitized storefront feed.
type Handler struct {
	feed Reader
}

func NewHandler(feed Reader) *Handler {
	if feed == nil {
		panic("ai feed handler: feed is nil")
	}
	return &Handler{feed: feed}
}

// Get returns the cached public summaries. Callers do not choose the upstream URL.
func (h *Handler) Get(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	c.Header("Cache-Control", "public, max-age=30")
	response.Success(c, h.feed.Snapshot(ctx))
}
