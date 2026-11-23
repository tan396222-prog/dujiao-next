package checkinhttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	checkinapp "github.com/dujiao-next/internal/modules/checkin/application"
	"github.com/dujiao-next/internal/modules/checkin/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

type checker interface {
	Status(userID uint) (checkinapp.Result, error)
	Claim(userID uint) (checkinapp.Result, error)
}

// UserHandler 提供每日签到查询和领取。用户、日期和金额只来自服务端。
type UserHandler struct {
	svc checker
}

func NewUserHandler(svc *checkinapp.Service) *UserHandler {
	return &UserHandler{svc: svc}
}

func RegisterUserRoutes(user *gin.RouterGroup, handler *UserHandler, claimLimit gin.HandlerFunc) {
	if user == nil || handler == nil {
		return
	}
	user.GET("/daily-checkins/today", handler.Today)
	claimed := user.Group("")
	if claimLimit != nil {
		claimed.Use(claimLimit)
	}
	claimed.POST("/daily-checkins", handler.Claim)
}

func (h *UserHandler) Today(c *gin.Context) {
	userID, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	if rejectClientFields(c) {
		return
	}
	result, err := h.svc.Status(userID)
	if err != nil {
		writeCheckInError(c, err)
		return
	}
	response.Success(c, present(result))
}

func (h *UserHandler) Claim(c *gin.Context) {
	userID, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	if rejectClientFields(c) || rejectClaimBody(c) {
		return
	}
	result, err := h.svc.Claim(userID)
	if err != nil {
		writeCheckInError(c, err)
		return
	}
	response.Success(c, present(result))
}

func present(result checkinapp.Result) gin.H {
	payload := gin.H{
		"checked_in":                     result.CheckedIn,
		"already_claimed":                result.AlreadyClaimed,
		"balance":                        result.Balance.String(),
		"currency":                       result.Currency,
		"checkin_date":                   result.CheckinDate,
		"next_checkin_date":              result.NextCheckinDate,
		"debt_offset_amount":             result.DebtOffset.String(),
		"offsets_unrecovered_commission": result.OffsetsUnrecoveredCommission,
	}
	if result.CheckedIn {
		payload["reward_amount"] = result.Reward.String()
	}
	return payload
}

func writeCheckInError(c *gin.Context, err error) {
	if errors.Is(err, contract.ErrAccountUnavailable) {
		ginutil.RespondError(c, response.CodeForbidden, "error.checkin_unavailable", err)
		return
	}
	ginutil.RespondError(c, response.CodeInternal, "error.checkin_failed", err)
}

var forbiddenClientKeys = map[string]struct{}{
	"user_id":       {},
	"userid":        {},
	"amount":        {},
	"date":          {},
	"checkin_date":  {},
	"reward":        {},
	"reward_amount": {},
	"first":         {},
	"first_checkin": {},
	"is_first":      {},
	"bonus":         {},
}

func rejectClientFields(c *gin.Context) bool {
	for key := range c.Request.URL.Query() {
		if _, found := forbiddenClientKeys[strings.ToLower(strings.TrimSpace(key))]; found {
			ginutil.RespondError(c, response.CodeBadRequest, "error.checkin_invalid", nil)
			return true
		}
	}
	return false
}

func rejectClaimBody(c *gin.Context) bool {
	if c.Request.Body == nil {
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var payload struct{}
	err := decoder.Decode(&payload)
	if errors.Is(err, io.EOF) {
		return false
	}
	if err != nil || decoder.More() {
		ginutil.RespondError(c, response.CodeBadRequest, "error.checkin_invalid", nil)
		return true
	}
	return false
}
