package checkinhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	checkinapp "github.com/dujiao-next/internal/modules/checkin/application"
	"github.com/dujiao-next/internal/modules/checkin/contract"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type checkInStub struct {
	statusCalls int
	claimCalls  int
	err         error
	result      checkinapp.Result
}

func (s *checkInStub) Status(uint) (checkinapp.Result, error) {
	s.statusCalls++
	return s.result, s.err
}

func (s *checkInStub) Claim(uint) (checkinapp.Result, error) {
	s.claimCalls++
	return s.result, s.err
}

func performCheckIn(handler *UserHandler, method, target, body string, userID *uint) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if userID != nil {
			c.Set("user_id", *userID)
		}
		c.Next()
	})
	RegisterUserRoutes(router.Group("/api/v1"), handler, nil)
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeStatus(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	return payload
}

func TestCheckInRejectsMissingUserAndForgedFields(t *testing.T) {
	stub := &checkInStub{}
	handler := &UserHandler{svc: stub}

	missing := performCheckIn(handler, http.MethodPost, "/api/v1/daily-checkins", `{}`, nil)
	payload := decodeStatus(t, missing)
	if payload["status_code"] != float64(401) {
		t.Fatalf("missing user status: %#v", payload)
	}
	if stub.claimCalls != 0 || stub.statusCalls != 0 {
		t.Fatalf("unauthenticated request touched the service: %+v", stub)
	}

	userID := uint(7)
	forged := performCheckIn(handler, http.MethodPost, "/api/v1/daily-checkins", `{"user_id":2,"amount":"80","date":"2020-01-01"}`, &userID)
	payload = decodeStatus(t, forged)
	if payload["status_code"] != float64(400) || !strings.Contains(payload["msg"].(string), "不能指定") {
		t.Fatalf("forged body: %#v", payload)
	}
	if stub.claimCalls != 0 {
		t.Fatal("forged body reached claim")
	}

	query := performCheckIn(handler, http.MethodPost, "/api/v1/daily-checkins?amount=80&user_id=2", `{}`, &userID)
	payload = decodeStatus(t, query)
	if payload["status_code"] != float64(400) || stub.claimCalls != 0 {
		t.Fatalf("forged query reached claim: %#v calls=%d", payload, stub.claimCalls)
	}
}

func TestCheckInTodayDoesNotClaim(t *testing.T) {
	userID := uint(3)
	stub := &checkInStub{result: checkinapp.Result{
		Balance:                      money.FromDecimal(decimal.RequireFromString("1.20")),
		Currency:                     "CNY",
		CheckinDate:                  "2026-11-22",
		NextCheckinDate:              "2026-11-23",
		DebtOffset:                   money.FromDecimal(decimal.Zero),
		OffsetsUnrecoveredCommission: false,
	}}
	handler := &UserHandler{svc: stub}
	recorder := performCheckIn(handler, http.MethodGet, "/api/v1/daily-checkins/today", "", &userID)
	payload := decodeStatus(t, recorder)
	if payload["status_code"] != float64(0) {
		t.Fatalf("today status: %#v", payload)
	}
	if stub.statusCalls != 1 || stub.claimCalls != 0 {
		t.Fatalf("today called status=%d claim=%d", stub.statusCalls, stub.claimCalls)
	}
	data := payload["data"].(map[string]any)
	if data["checked_in"] != false || data["debt_offset_amount"] != "0.00" || data["offsets_unrecovered_commission"] != false {
		t.Fatalf("today payload: %#v", data)
	}
	if _, ok := data["reward_amount"]; ok {
		t.Fatal("unchecked status should not invent a reward")
	}

	missing := performCheckIn(handler, http.MethodGet, "/api/v1/daily-checkins", "", &userID)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("list route should stay absent, got %d", missing.Code)
	}
}

func TestCheckInUnavailableDoesNotPay(t *testing.T) {
	userID := uint(4)
	stub := &checkInStub{err: contract.ErrAccountUnavailable}
	handler := &UserHandler{svc: stub}
	recorder := performCheckIn(handler, http.MethodPost, "/api/v1/daily-checkins", `{}`, &userID)
	payload := decodeStatus(t, recorder)
	if payload["status_code"] != float64(403) {
		t.Fatalf("unavailable: %#v", payload)
	}
	if stub.claimCalls != 1 {
		t.Fatalf("claim calls: %d", stub.claimCalls)
	}
}
