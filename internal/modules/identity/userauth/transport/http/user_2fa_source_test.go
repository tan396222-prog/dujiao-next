package userauthhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/gin-gonic/gin"
)

type sourceTestTOTPService struct{}

func (sourceTestTOTPService) GetStatus(uint) (*UserTOTPStatus, error) {
	return &UserTOTPStatus{}, nil
}
func (sourceTestTOTPService) Setup(uint) (*UserTOTPSetupResult, error) {
	return &UserTOTPSetupResult{}, nil
}
func (sourceTestTOTPService) Enable(uint, string) (*UserTOTPEnableResult, error) {
	return &UserTOTPEnableResult{}, nil
}
func (sourceTestTOTPService) Disable(uint, string, bool) error { return nil }
func (sourceTestTOTPService) RegenerateRecoveryCodes(uint, string) ([]string, error) {
	return nil, nil
}
func (sourceTestTOTPService) VerifyChallengeCode(uint, string) error         { return nil }
func (sourceTestTOTPService) VerifyChallengeRecoveryCode(uint, string) error { return nil }

type sourceTest2FAAuth struct {
	source string
}

func (s sourceTest2FAAuth) ParseUserChallengeToken(string) (*UserChallengeClaims, error) {
	return &UserChallengeClaims{
		UserID:      42,
		JTI:         "source-test-jti",
		LoginSource: s.source,
	}, nil
}

func (sourceTest2FAAuth) CompleteLoginAfter2FA(context.Context, uint, bool) (*AuthLoginResult, error) {
	now := time.Now()
	return &AuthLoginResult{
		User: &userdomain.User{
			ID:    42,
			Email: "buyer@example.com",
		},
		Token:     "access-token",
		ExpiresAt: now.Add(time.Hour),
	}, nil
}

func (sourceTest2FAAuth) GetUserEmail(uint) (string, error) {
	return "buyer@example.com", nil
}

type sourceTestChallengeStore struct{}

func (sourceTestChallengeStore) IsRevoked(context.Context, string) bool { return false }
func (sourceTestChallengeStore) BumpFails(context.Context, string) int64 {
	return 0
}
func (sourceTestChallengeStore) Revoke(context.Context, string) {}

type sourceTestLoginRecord struct {
	source string
	status string
}

type sourceTestLoginRecorder struct {
	records []sourceTestLoginRecord
}

func (r *sourceTestLoginRecorder) Record(_ string, _ uint, status, _, source, _, _, _ string) {
	r.records = append(r.records, sourceTestLoginRecord{source: source, status: status})
}

func TestVerifyUser2FAPreservesExternalLoginSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []string{constants.LoginLogSourceGoogle, constants.LoginLogSourceTelegram} {
		t.Run(source, func(t *testing.T) {
			loginRecorder := &sourceTestLoginRecorder{}
			handler := NewUser2FAHandler(
				sourceTestTOTPService{},
				sourceTest2FAAuth{source: source},
				sourceTestChallengeStore{},
				loginRecorder,
			)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/v1/auth/login/verify-2fa",
				bytes.NewBufferString(`{"challenge_token":"challenge","code":"123456"}`),
			)
			context.Request.Header.Set("Content-Type", "application/json")

			handler.VerifyUser2FA(context)

			if len(loginRecorder.records) != 1 {
				t.Fatalf("login records = %d, want 1", len(loginRecorder.records))
			}
			if loginRecorder.records[0].source != source {
				t.Fatalf("login source = %q, want %q", loginRecorder.records[0].source, source)
			}
			if loginRecorder.records[0].status != constants.LoginLogStatusSuccess {
				t.Fatalf("login status = %q", loginRecorder.records[0].status)
			}
		})
	}
}

type emailLoginHTTPSettings struct{}

func (emailLoginHTTPSettings) GetRegistrationEnabled(bool) (bool, error) { return true, nil }
func (emailLoginHTTPSettings) GetEmailVerificationEnabled(bool) (bool, error) {
	return true, nil
}

type emailLoginHTTPAuth struct {
	sendErr  error
	loginErr error
}

func (emailLoginHTTPAuth) Register(context.Context, string, string, string, string, bool, bool) (*userdomain.User, string, time.Time, error) {
	return nil, "", time.Time{}, nil
}
func (emailLoginHTTPAuth) LoginStep1(context.Context, string, string, bool) (*AuthLoginResult, error) {
	return nil, nil
}
func (a emailLoginHTTPAuth) SendLoginEmailCode(context.Context, string, string) error {
	return a.sendErr
}
func (a emailLoginHTTPAuth) LoginWithEmailCode(context.Context, string, string, bool) (*AuthLoginResult, error) {
	return nil, a.loginErr
}

func TestEmailCodeLoginUsesOneClientError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []error{
		ErrVerifyCodeInvalid,
		ErrVerifyCodeExpired,
		ErrVerifyCodeAttemptsExceeded,
		ErrEmailLoginRejected,
		ErrUserDisabled,
		ErrUserNotFound,
	}
	var first string
	for _, loginErr := range cases {
		handler := NewUserLoginHandler(emailLoginHTTPSettings{}, emailLoginHTTPAuth{loginErr: loginErr}, nil, nil)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/auth/login/email-code", bytes.NewBufferString(`{"email":"buyer@example.com","code":"135790"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		handler.UserLoginWithEmailCode(ctx)
		if bytes.Contains(recorder.Body.Bytes(), []byte("135790")) {
			t.Fatalf("response echoed the code for %v: %s", loginErr, recorder.Body.Bytes())
		}
		var payload struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Msg != "验证码无效或账号不可用" {
			t.Fatalf("msg for %v = %q", loginErr, payload.Msg)
		}
		if first == "" {
			first = payload.Msg
		} else if payload.Msg != first {
			t.Fatalf("client message diverged for %v", loginErr)
		}
	}

	handler := NewUserLoginHandler(emailLoginHTTPSettings{}, emailLoginHTTPAuth{sendErr: ErrEmailServiceNotConfigured}, nil, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/auth/login/email-code/send", bytes.NewBufferString(`{"email":"missing@example.com"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.UserSendLoginEmailCode(ctx)
	if bytes.Contains(recorder.Body.Bytes(), []byte(`"sent":true`)) {
		t.Fatalf("unconfigured smtp reported success: %s", recorder.Body.Bytes())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("邮箱服务未配置")) {
		t.Fatalf("missing smtp message: %s", recorder.Body.Bytes())
	}
}
