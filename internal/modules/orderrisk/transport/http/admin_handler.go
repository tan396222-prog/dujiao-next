package orderriskhttp

import (
	"errors"
	"net"
	"strings"

	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// AttemptAdmin 是后台查看和解封下单尝试所需的用例。
type AttemptAdmin interface {
	ListOrderAttempts() (orderriskcontract.AttemptReport, error)
	UnbanOrderAttempt(raw string) (bool, error)
}

type AdminHandler struct {
	attempts AttemptAdmin
}

func NewAdminHandler(attempts AttemptAdmin) *AdminHandler {
	return &AdminHandler{attempts: attempts}
}

func RegisterAdminRoutes(admin gin.IRoutes, handler *AdminHandler) {
	admin.GET("/order-risk/attempts", handler.ListAttempts)
	admin.POST("/order-risk/attempts/unban", handler.Unban)
	admin.GET("/order-risk/client-ip", handler.ClientIdentity)
}

func (h *AdminHandler) ListAttempts(c *gin.Context) {
	if h == nil || h.attempts == nil {
		response.Success(c, orderriskcontract.AttemptReport{})
		return
	}
	report, err := h.attempts.ListOrderAttempts()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, report)
}

type unbanRequest struct {
	IP string `json:"ip"`
}

func (h *AdminHandler) Unban(c *gin.Context) {
	if h == nil || h.attempts == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", nil)
		return
	}
	var req unbanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	cleared, err := h.attempts.UnbanOrderAttempt(req.IP)
	if errors.Is(err, orderriskcontract.ErrAttemptKeyInvalid) {
		ginutil.RespondError(c, response.CodeBadRequest, "error.risk_attempt_key_invalid", nil)
		return
	}
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}
	response.Success(c, gin.H{"cleared": cleared, "ip": orderriskcontract.ParseAttemptKey(req.IP)})
}

// ClientIdentity 只返回这次管理请求自己被服务器识别出的地址，忽略任何查询参数。
func (h *AdminHandler) ClientIdentity(c *gin.Context) {
	peer := canonicalIP(c.RemoteIP())
	resolved := canonicalIP(c.ClientIP())
	source := "remote"
	if resolved == "" {
		source = "unavailable"
	} else if peer != "" && resolved != peer {
		source = "forwarded"
	}
	response.Success(c, gin.H{
		"ip":      resolved,
		"risk_ip": orderriskcontract.NormalizeRiskIP(c.ClientIP()),
		"source":  source,
		"peer":    peer,
	})
}

func canonicalIP(raw string) string {
	parsed := net.ParseIP(strings.TrimSpace(raw))
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.String()
}
