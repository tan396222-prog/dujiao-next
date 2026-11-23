package orderhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	paymentpresenter "github.com/dujiao-next/internal/modules/payment/transport/presenter"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"

	"github.com/dujiao-next/internal/i18n"
	captcha "github.com/dujiao-next/internal/modules/captcha/contract"
	captchahttp "github.com/dujiao-next/internal/modules/captcha/transport/http"
	orderpresenter "github.com/dujiao-next/internal/modules/order/transport/presenter"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
)

// OrderCreateService 订单创建端口。
type OrderCreateService interface {
	CreateOrder(input CreateOrderInput) (*orderdomain.Order, error)
	CreateGuestOrder(input CreateGuestOrderInput) (*orderdomain.Order, error)
}

// GuestCreateCaptcha 游客下单验证码端口。
type GuestCreateCaptcha interface {
	VerifyGuestCreateOrder(payload captchahttp.CaptchaPayloadRequest, clientIP string) error
}

// CreatePaymentInput 创建支付输入。
type CreatePaymentInput struct {
	OrderID       uint
	ChannelID     uint
	UseBalance    bool
	ClientIP      string
	Context       context.Context
	RequestScheme string
}

// CreatePaymentResult 创建支付结果。
type CreatePaymentResult struct {
	Payment          *paymentdomain.Payment
	OrderPaid        bool
	WalletPaidAmount money.Amount
	OnlinePayAmount  money.Amount
}

// OrderPaymentCreator 订单支付创建端口。
type OrderPaymentCreator interface {
	CreatePayment(input CreatePaymentInput) (*CreatePaymentResult, error)
}

// CreateOrderAndPayRequest 创建订单并发起支付请求
type CreateOrderAndPayRequest struct {
	Items               []OrderItemRequest      `json:"items" binding:"required"`
	CouponCode          string                  `json:"coupon_code"`
	AffiliateCode       string                  `json:"affiliate_code"`
	AffiliateVisitorKey string                  `json:"affiliate_visitor_key"`
	ManualFormData      map[string]jsonmap.JSON `json:"manual_form_data"`
	ChannelID           uint                    `json:"channel_id"`
	UseBalance          bool                    `json:"use_balance"`
}

// CreateGuestOrderAndPayRequest 游客创建订单并发起支付请求
type CreateGuestOrderAndPayRequest struct {
	Email               string                            `json:"email" binding:"required"`
	OrderPassword       string                            `json:"order_password" binding:"required"`
	Items               []OrderItemRequest                `json:"items" binding:"required"`
	CouponCode          string                            `json:"coupon_code"`
	AffiliateCode       string                            `json:"affiliate_code"`
	AffiliateVisitorKey string                            `json:"affiliate_visitor_key"`
	ManualFormData      map[string]jsonmap.JSON           `json:"manual_form_data"`
	CaptchaPayload      captchahttp.CaptchaPayloadRequest `json:"captcha_payload"`
	ChannelID           uint                              `json:"channel_id"`
}

// CreateHandler 处理前台订单创建与 create-and-pay HTTP。
type CreateHandler struct {
	orders       OrderCreateService
	payments     PaymentChannelPolicy
	captcha      GuestCreateCaptcha
	pay          OrderPaymentCreator
	protection   OrderProtection
	deviceSecret string
}

// OrderProtection 只提供结账预检。它不能代替创建订单时的风控。
type OrderProtection interface {
	InspectOrderAttempt(clientIP string) (bool, int64)
	AcceptProtectionSignal(subject, token, secret string, now time.Time) string
}

func NewCreateHandler(orders OrderCreateService, payments PaymentChannelPolicy, captcha GuestCreateCaptcha, pay OrderPaymentCreator) *CreateHandler {
	if orders == nil {
		panic("order create handler: orders is nil")
	}
	return &CreateHandler{orders: orders, payments: payments, captcha: captcha, pay: pay}
}

// UseDeviceSignal 使用应用密钥签发可选的下单辅助标记。密钥为空时不签发，下单仍然允许。
func (h *CreateHandler) UseDeviceSignal(secret string) {
	if h == nil {
		return
	}
	h.deviceSecret = strings.TrimSpace(secret)
}

func (h *CreateHandler) UseOrderProtection(protection OrderProtection) {
	if h == nil {
		return
	}
	h.protection = protection
}

// CreateOrder 创建订单
func (h *CreateHandler) CreateOrder(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	order, err := h.orders.CreateOrder(CreateOrderInput{
		UserID:              uid,
		Tenant:              tenantFromRequest(c),
		Items:               mapOrderItems(req.Items),
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		DeviceSignal:        h.deviceSignal(c),
		AutomationSignal:    h.protectionSignal(c),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondUserOrderCreateError(c, err)
		return
	}

	orderDetail := orderpresenter.NewOrderDetail(order)
	enrichOrderWithAllowedChannels(h.payments, order, &orderDetail)
	response.Success(c, orderDetail)
}

// CreateGuestOrder 游客创建订单
func (h *CreateHandler) CreateGuestOrder(c *gin.Context) {
	var req CreateGuestOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if !h.verifyGuestCreateCaptcha(c, req.CaptchaPayload) {
		return
	}

	order, err := h.orders.CreateGuestOrder(CreateGuestOrderInput{
		Email:               req.Email,
		OrderPassword:       req.OrderPassword,
		Locale:              i18n.ResolveLocale(c),
		Tenant:              tenantFromRequest(c),
		Items:               mapOrderItems(req.Items),
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		DeviceSignal:        h.deviceSignal(c),
		AutomationSignal:    h.protectionSignal(c),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondGuestOrderCreateError(c, err)
		return
	}
	orderDetail := orderpresenter.NewOrderDetail(order)
	enrichOrderWithAllowedChannels(h.payments, order, &orderDetail)
	response.Success(c, orderDetail)
}

// CreateOrderAndPay 创建订单并发起支付（合并接口）
func (h *CreateHandler) CreateOrderAndPay(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	var req CreateOrderAndPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	order, err := h.orders.CreateOrder(CreateOrderInput{
		UserID:              uid,
		Tenant:              tenantFromRequest(c),
		Items:               mapOrderItems(req.Items),
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		DeviceSignal:        h.deviceSignal(c),
		AutomationSignal:    h.protectionSignal(c),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondUserOrderCreateError(c, err)
		return
	}

	orderResp := orderpresenter.NewOrderDetail(order)
	enrichOrderWithAllowedChannels(h.payments, order, &orderResp)

	if req.ChannelID == 0 && !req.UseBalance {
		response.Success(c, gin.H{
			"order":    orderResp,
			"order_no": order.OrderNo,
		})
		return
	}

	h.respondCreateAndPay(c, order, orderResp, req.ChannelID, req.UseBalance)
}

// CreateGuestOrderAndPay 游客创建订单并发起支付（合并接口）
func (h *CreateHandler) CreateGuestOrderAndPay(c *gin.Context) {
	var req CreateGuestOrderAndPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if !h.verifyGuestCreateCaptcha(c, req.CaptchaPayload) {
		return
	}

	order, err := h.orders.CreateGuestOrder(CreateGuestOrderInput{
		Email:               req.Email,
		OrderPassword:       req.OrderPassword,
		Locale:              i18n.ResolveLocale(c),
		Tenant:              tenantFromRequest(c),
		Items:               mapOrderItems(req.Items),
		CouponCode:          req.CouponCode,
		AffiliateCode:       req.AffiliateCode,
		AffiliateVisitorKey: req.AffiliateVisitorKey,
		ClientIP:            c.ClientIP(),
		DeviceSignal:        h.deviceSignal(c),
		AutomationSignal:    h.protectionSignal(c),
		ManualFormData:      req.ManualFormData,
	})
	if err != nil {
		respondGuestOrderCreateError(c, err)
		return
	}
	orderResp := orderpresenter.NewOrderDetail(order)
	enrichOrderWithAllowedChannels(h.payments, order, &orderResp)

	if req.ChannelID == 0 {
		response.Success(c, gin.H{
			"order":    orderResp,
			"order_no": order.OrderNo,
		})
		return
	}

	h.respondCreateAndPay(c, order, orderResp, req.ChannelID, false)
}

func (h *CreateHandler) deviceSignal(c *gin.Context) string {
	if h == nil || c == nil || h.deviceSecret == "" {
		return ""
	}
	if raw, err := c.Cookie(orderriskcontract.DeviceCookieName); err == nil {
		if id, ok := orderriskcontract.VerifyDeviceToken(h.deviceSecret, raw, time.Now()); ok {
			return id
		}
	}
	token, id, err := orderriskcontract.IssueDeviceToken(h.deviceSecret, time.Now())
	if err != nil {
		return ""
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(orderriskcontract.DeviceCookieName, token, int(24*time.Hour/time.Second), "/", "", secure, true)
	return id
}

func (h *CreateHandler) verifyGuestCreateCaptcha(c *gin.Context, payload captchahttp.CaptchaPayloadRequest) bool {
	if h.captcha == nil {
		return true
	}
	if captchaErr := h.captcha.VerifyGuestCreateOrder(payload, c.ClientIP()); captchaErr != nil {
		switch {
		case errors.Is(captchaErr, captcha.ErrRequired):
			ginutil.RespondError(c, response.CodeBadRequest, "error.captcha_required", nil)
		case errors.Is(captchaErr, captcha.ErrInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.captcha_invalid", nil)
		case errors.Is(captchaErr, captcha.ErrConfigInvalid):
			ginutil.RespondError(c, response.CodeInternal, "error.captcha_config_invalid", captchaErr)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.captcha_verify_failed", captchaErr)
		}
		return false
	}
	return true
}

func (h *CreateHandler) respondCreateAndPay(c *gin.Context, order *orderdomain.Order, orderResp orderpresenter.OrderDetail, channelID uint, useBalance bool) {
	if h.pay == nil {
		response.Success(c, gin.H{
			"order":         orderResp,
			"order_no":      order.OrderNo,
			"payment_error": "payment creator unavailable",
		})
		return
	}
	result, err := h.pay.CreatePayment(CreatePaymentInput{
		OrderID:       order.ID,
		ChannelID:     channelID,
		UseBalance:    useBalance,
		ClientIP:      c.ClientIP(),
		Context:       c.Request.Context(),
		RequestScheme: ginutil.RequestScheme(c),
	})
	if err != nil {
		response.Success(c, gin.H{
			"order":         orderResp,
			"order_no":      order.OrderNo,
			"payment_error": err.Error(),
		})
		return
	}

	resp := gin.H{
		"order":              orderResp,
		"order_no":           order.OrderNo,
		"order_paid":         result.OrderPaid,
		"wallet_paid_amount": result.WalletPaidAmount,
		"online_pay_amount":  result.OnlinePayAmount,
	}
	if result.Payment != nil {
		resp["payment_id"] = result.Payment.ID
		resp["provider_type"] = result.Payment.ProviderType
		resp["channel_type"] = result.Payment.ChannelType
		resp["interaction_mode"] = result.Payment.InteractionMode
		resp["pay_url"] = result.Payment.PayURL
		resp["qr_code"] = result.Payment.QRCode
		if info := paymentpresenter.ExtractCryptoWalletInfo(result.Payment.ProviderType, result.Payment.InteractionMode, result.Payment.ProviderPayload); info.HasAny() {
			if info.Address != "" {
				resp["wallet_address"] = info.Address
			}
			if info.ChainAmount != "" {
				resp["chain_amount"] = info.ChainAmount
			}
			if info.Chain != "" {
				resp["chain"] = info.Chain
			}
			if info.TokenID != "" {
				resp["token_id"] = info.TokenID
			}
		}
		resp["expires_at"] = result.Payment.ExpiredAt
	}
	response.Success(c, resp)
}

type protectionPrecheckRequest struct {
	Bind      string   `json:"bind"`
	Webdriver *bool    `json:"webdriver"`
	Traces    []string `json:"traces"`
}

// PrecheckCheckout 在真正创建订单前做同源预检。它不计数、不锁券、不扣余额。
func (h *CreateHandler) PrecheckCheckout(c *gin.Context) {
	var req protectionPrecheckRequest
	_ = c.ShouldBindJSON(&req)
	traces := make([]string, 0, 4)
	for _, trace := range req.Traces {
		if len(traces) == 4 {
			break
		}
		switch strings.ToLower(strings.TrimSpace(trace)) {
		case "headless", "cdp", "electron":
			traces = append(traces, strings.ToLower(strings.TrimSpace(trace)))
		}
	}
	signal := orderriskcontract.ClassifyAutomation(req.Webdriver, traces)
	banned := false
	var retry int64
	if h != nil && h.protection != nil {
		banned, retry = h.protection.InspectOrderAttempt(c.ClientIP())
	}
	token := ""
	if h != nil && h.deviceSecret != "" && !banned {
		issued, err := orderriskcontract.IssueProtectionToken(h.deviceSecret, h.protectionSubject(c), req.Bind, signal, time.Now())
		if err == nil {
			token = issued
		}
	}
	response.Success(c, gin.H{
		"banned":              banned,
		"retry_after_seconds": retry,
		"signal":              signal,
		"token":               token,
	})
}

func (h *CreateHandler) protectionSubject(c *gin.Context) string {
	if uid := optionalUserID(c); uid > 0 {
		return fmt.Sprintf("user:%d", uid)
	}
	if id := h.deviceSignal(c); id != "" {
		return "device:" + id
	}
	return "guest"
}

func optionalUserID(c *gin.Context) uint {
	if c == nil {
		return 0
	}
	value, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case uint:
		return typed
	case int:
		if typed > 0 {
			return uint(typed)
		}
	case float64:
		if typed > 0 {
			return uint(typed)
		}
	}
	return 0
}

func (h *CreateHandler) protectionSignal(c *gin.Context) string {
	if h == nil || h.protection == nil || h.deviceSecret == "" {
		return ""
	}
	token := strings.TrimSpace(c.GetHeader("X-DJ-Protection"))
	if token == "" {
		return ""
	}
	return h.protection.AcceptProtectionSignal(h.protectionSubject(c), token, h.deviceSecret, time.Now())
}
