package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	emailverificationdomain "github.com/dujiao-next/internal/modules/identity/emailverification/domain"
	emailverificationstore "github.com/dujiao-next/internal/modules/identity/emailverification/infrastructure/gormstore"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	externalidentitystore "github.com/dujiao-next/internal/modules/identity/externalidentity/infrastructure/gormstore"
	"github.com/dujiao-next/internal/modules/identity/jwttoken"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	userstore "github.com/dujiao-next/internal/modules/identity/user/infrastructure/gormstore"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"
	"github.com/dujiao-next/internal/modules/identity/userauth/challenge"
	usertotpapp "github.com/dujiao-next/internal/modules/identity/userauth/totp/application"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/shared/mailbrand"
	"github.com/dujiao-next/internal/telegramidentity"

	"github.com/glebarez/sqlite"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginCodeMail struct {
	mu          sync.Mutex
	sent        int
	lastCode    string
	lastPurpose string
	fail        bool
}

func (m *loginCodeMail) SendVerifyCode(_, code, purpose, _ string, _ mailbrand.Brand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("smtp down")
	}
	m.sent++
	m.lastCode = code
	m.lastPurpose = purpose
	return nil
}

func (m *loginCodeMail) snapshot() (int, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent, m.lastCode, m.lastPurpose
}

type emailLoginFixture struct {
	svc      *userauthapp.Service
	repo     usercontract.Store
	db       *gorm.DB
	mail     *loginCodeMail
	totp     *usertotpapp.Service
	settings *settingsapp.Service
}

func newEmailLoginFixture(t *testing.T, emailCfg config.EmailConfig) *emailLoginFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:email_login_%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&userdomain.User{},
		&externalidentitydomain.Identity{},
		&emailverificationdomain.Code{},
		&settingsstore.SettingRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if emailCfg.VerifyCode.ExpireMinutes == 0 {
		emailCfg.VerifyCode.ExpireMinutes = 10
	}
	if emailCfg.VerifyCode.SendIntervalSeconds == 0 {
		emailCfg.VerifyCode.SendIntervalSeconds = 60
	}
	if emailCfg.VerifyCode.MaxAttempts == 0 {
		emailCfg.VerifyCode.MaxAttempts = 5
	}
	if emailCfg.VerifyCode.Length == 0 {
		emailCfg.VerifyCode.Length = 6
	}
	cfg := &config.Config{
		App:     config.AppConfig{SecretKey: "email-login-app-secret"},
		UserJWT: config.JWTConfig{SecretKey: "email-login-jwt-secret", ExpireHours: 24},
		Email:   emailCfg,
	}
	mailer := &loginCodeMail{}
	settingSvc := settingsapp.NewService(settingsstore.New(db))
	repo := userstore.New(db)
	svc := userauthapp.NewService(cfg, repo, externalidentitystore.New(db), emailverificationstore.New(db), settingSvc, mailer, nil)
	return &emailLoginFixture{
		svc:      svc,
		repo:     repo,
		db:       db,
		mail:     mailer,
		totp:     usertotpapp.NewService(cfg, repo, nil),
		settings: settingSvc,
	}
}

func readyEmailConfig() config.EmailConfig {
	return config.EmailConfig{Enabled: true, Host: "smtp.example.test", Port: 587, From: "shop@example.test"}
}

func createEmailLoginUser(t *testing.T, repo usercontract.Store, email, password, status string, verified bool) *userdomain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := time.Now()
	user := &userdomain.User{
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  "buyer",
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if verified {
		user.EmailVerifiedAt = &now
	}
	if err := repo.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func activeCodes(t *testing.T, db *gorm.DB, email, purpose string) []emailverificationdomain.Code {
	t.Helper()
	var rows []emailverificationdomain.Code
	if err := db.Where("email = ? AND purpose = ? AND deleted_at IS NULL", email, purpose).Find(&rows).Error; err != nil {
		t.Fatalf("list codes: %v", err)
	}
	return rows
}

func TestLoginEmailCodeRequiresConfiguredSMTP(t *testing.T) {
	fx := newEmailLoginFixture(t, config.EmailConfig{Enabled: false})
	createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	ctx := context.Background()

	err := fx.svc.SendLoginEmailCode(ctx, "buyer@example.com", constants.LocaleZhCN)
	if !errors.Is(err, userauthapp.ErrEmailServiceNotConfigured) {
		t.Fatalf("existing email unconfigured: %v", err)
	}
	err = fx.svc.SendLoginEmailCode(ctx, "missing@example.com", constants.LocaleZhCN)
	if !errors.Is(err, userauthapp.ErrEmailServiceNotConfigured) {
		t.Fatalf("missing email unconfigured: %v", err)
	}
	if sent, _, _ := fx.mail.snapshot(); sent != 0 {
		t.Fatalf("unconfigured smtp sent %d messages", sent)
	}

	enabled := false
	if _, err := fx.settings.PatchSMTPSetting(readyEmailConfig(), settingsmessaging.SMTPSettingPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("patch smtp: %v", err)
	}
	ready := newEmailLoginFixture(t, readyEmailConfig())
	if _, err := ready.settings.PatchSMTPSetting(readyEmailConfig(), settingsmessaging.SMTPSettingPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("patch ready smtp: %v", err)
	}
	createEmailLoginUser(t, ready.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	if err := ready.svc.SendLoginEmailCode(ctx, "buyer@example.com", constants.LocaleZhCN); !errors.Is(err, userauthapp.ErrEmailServiceNotConfigured) {
		t.Fatalf("stored smtp disabled should win over config.yml, got %v", err)
	}
}

func TestLoginEmailCodeSendFailureDoesNotOccupyInterval(t *testing.T) {
	fx := newEmailLoginFixture(t, readyEmailConfig())
	createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	fx.mail.fail = true
	err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN)
	if err == nil || errors.Is(err, userauthapp.ErrEmailServiceNotConfigured) {
		t.Fatalf("expected real send failure, got %v", err)
	}
	if len(activeCodes(t, fx.db, "buyer@example.com", constants.VerifyPurposeLogin)) != 0 {
		t.Fatal("failed send left a code that blocks retry")
	}
	fx.mail.fail = false
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("retry after failed send: %v", err)
	}
	rows := activeCodes(t, fx.db, "buyer@example.com", constants.VerifyPurposeLogin)
	if len(rows) != 1 {
		t.Fatalf("active codes = %d", len(rows))
	}
	_, plain, purpose := fx.mail.snapshot()
	if purpose != constants.VerifyPurposeLogin {
		t.Fatalf("purpose = %s", purpose)
	}
	if rows[0].Code == plain || len(rows[0].Code) != 64 {
		t.Fatal("login code was stored in a recoverable form")
	}
}

func TestLoginEmailCodePurposeIsolation(t *testing.T) {
	fx := newEmailLoginFixture(t, readyEmailConfig())
	createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	ctx := context.Background()
	if err := fx.svc.SendVerifyCode(ctx, "buyer@example.com", constants.VerifyPurposeLogin, constants.LocaleZhCN, ""); !errors.Is(err, userauthapp.ErrInvalidVerifyPurpose) {
		t.Fatalf("public sender accepted login purpose: %v", err)
	}
	now := time.Now()
	for _, purpose := range []string{constants.VerifyPurposeRegister, constants.VerifyPurposeReset, constants.VerifyPurposeChangeEmailOld, constants.VerifyPurposeChangeEmailNew} {
		row := &emailverificationdomain.Code{
			Email: "buyer@example.com", Purpose: purpose, Code: "654321",
			ExpiresAt: now.Add(time.Minute), SentAt: now, CreatedAt: now,
		}
		if err := fx.db.Create(row).Error; err != nil {
			t.Fatalf("insert %s code: %v", purpose, err)
		}
		if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", "654321", false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
			t.Fatalf("%s code logged in: %v", purpose, err)
		}
	}
}

func TestLoginEmailCodeExpiryReuseAttemptsAndRace(t *testing.T) {
	cfg := readyEmailConfig()
	cfg.VerifyCode.MaxAttempts = 2
	cfg.VerifyCode.SendIntervalSeconds = 1
	fx := newEmailLoginFixture(t, cfg)
	createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, plain, _ := fx.mail.snapshot()
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", "000000", false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", "000000", false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("second wrong code: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false); !errors.Is(err, userauthapp.ErrVerifyCodeAttemptsExceeded) {
		t.Fatalf("correct code after max attempts: %v", err)
	}

	fx.mail.mu.Lock()
	fx.mail.sent = 0
	fx.mail.mu.Unlock()
	time.Sleep(1100 * time.Millisecond)
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("resend: %v", err)
	}
	_, plain, _ = fx.mail.snapshot()
	var fresh emailverificationdomain.Code
	if err := fx.db.Where("email = ? AND purpose = ? AND deleted_at IS NULL AND verified_at IS NULL", "buyer@example.com", constants.VerifyPurposeLogin).
		Order("id desc").First(&fresh).Error; err != nil {
		t.Fatalf("missing fresh login code: %v", err)
	}
	if err := fx.db.Model(&fresh).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire code: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false); !errors.Is(err, userauthapp.ErrVerifyCodeExpired) {
		t.Fatalf("expired code: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send for reuse: %v", err)
	}
	_, plain, _ = fx.mail.snapshot()
	res, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false)
	if err != nil || res == nil || res.Token == "" || res.RequiresTOTP {
		t.Fatalf("first consume result=%+v err=%v", res, err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("reused code: %v", err)
	}
	if _, err := fx.svc.LoginStep1("buyer@example.com", "secret123", false); err != nil {
		t.Fatalf("password login after email code: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send for race: %v", err)
	}
	_, plain, _ = fx.mail.snapshot()
	var wg sync.WaitGroup
	results := make(chan *userauthapp.UserLoginResult, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, loginErr := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false)
			if loginErr != nil {
				errs <- loginErr
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	success := 0
	for result := range results {
		if result != nil && result.Token != "" {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent consume successes = %d", success)
	}
}

func TestLoginEmailCodeDoesNotRevealOrAdmitIneligibleAccounts(t *testing.T) {
	cfg := readyEmailConfig()
	cfg.VerifyCode.SendIntervalSeconds = 1
	fx := newEmailLoginFixture(t, cfg)
	createEmailLoginUser(t, fx.repo, "banned@example.com", "secret123", constants.UserStatusDisabled, true)
	createEmailLoginUser(t, fx.repo, "new@example.com", "secret123", constants.UserStatusActive, false)
	createEmailLoginUser(t, fx.repo, telegramidentity.BuildPlaceholderEmail("42"), "secret123", constants.UserStatusActive, true)
	ctx := context.Background()
	for _, email := range []string{"missing@example.com", "banned@example.com", "new@example.com", telegramidentity.BuildPlaceholderEmail("42")} {
		if err := fx.svc.SendLoginEmailCode(ctx, email, constants.LocaleZhCN); err != nil {
			t.Fatalf("send %s: %v", email, err)
		}
	}
	if sent, _, _ := fx.mail.snapshot(); sent != 0 {
		t.Fatalf("ineligible addresses were mailed %d times", sent)
	}
	if err := fx.svc.SendLoginEmailCode(ctx, "missing@example.com", constants.LocaleZhCN); !errors.Is(err, userauthapp.ErrVerifyCodeTooFrequent) {
		t.Fatalf("missing resend: %v", err)
	}

	buyer := createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	if err := fx.svc.SendLoginEmailCode(ctx, "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send buyer: %v", err)
	}
	_, plain, _ := fx.mail.snapshot()
	buyer.Status = constants.UserStatusDisabled
	if err := fx.repo.Update(buyer); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false); !errors.Is(err, userauthapp.ErrEmailLoginRejected) {
		t.Fatalf("disabled login: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	buyer.Status = constants.UserStatusActive
	if err := fx.repo.Update(buyer); err != nil {
		t.Fatalf("reenable: %v", err)
	}
	if err := fx.svc.SendLoginEmailCode(ctx, "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("resend buyer: %v", err)
	}
	_, plain, _ = fx.mail.snapshot()
	if err := fx.db.Model(&userdomain.User{}).Where("id = ?", buyer.ID).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, false); !errors.Is(err, userauthapp.ErrEmailLoginRejected) {
		t.Fatalf("deleted login: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("missing@example.com", plain, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("missing verify: %v", err)
	}
}

func TestLoginEmailCodeKeepsTwoFactorAndPasswordLogin(t *testing.T) {
	fx := newEmailLoginFixture(t, readyEmailConfig())
	user := createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	setup, err := fx.totp.Setup(user.ID)
	if err != nil {
		t.Fatalf("setup totp: %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	if _, err := fx.totp.Enable(user.ID, code); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	if err := fx.svc.SendLoginEmailCode(context.Background(), "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send: %v", err)
	}
	_, plain, _ := fx.mail.snapshot()
	res, err := fx.svc.LoginWithEmailCode("buyer@example.com", plain, true)
	if err != nil {
		t.Fatalf("email login: %v", err)
	}
	if res == nil || !res.RequiresTOTP || res.Token != "" || res.ChallengeToken == "" {
		t.Fatalf("2FA was bypassed: %+v", res)
	}
	claims, err := fx.svc.ParseUserChallengeToken(res.ChallengeToken)
	if err != nil {
		t.Fatalf("parse challenge: %v", err)
	}
	if claims.Purpose != challenge.PurposeTwoFactor || claims.Typ != jwttoken.TypeTwoFactorChallenge || !claims.RememberMe {
		t.Fatalf("challenge claims = %+v", claims)
	}
	if parsed, err := fx.svc.ParseUserJWT(res.ChallengeToken); err == nil && jwttoken.IsAccessType(parsed.Typ) {
		t.Fatalf("challenge token was accepted as an access token, typ=%q", parsed.Typ)
	}
	passwordResult, err := fx.svc.LoginStep1("buyer@example.com", "secret123", false)
	if err != nil {
		t.Fatalf("password login: %v", err)
	}
	if passwordResult == nil || !passwordResult.RequiresTOTP || passwordResult.Token != "" {
		t.Fatalf("password login changed 2FA behavior: %+v", passwordResult)
	}
	stored, err := fx.repo.GetByEmail("buyer@example.com")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored == nil || stored.LastLoginAt != nil {
		t.Fatal("email-code challenge recorded a login before the second factor")
	}
	completed, err := fx.svc.CompleteLoginAfter2FA(user.ID, true)
	if err != nil || completed == nil || completed.Token == "" || completed.RequiresTOTP {
		t.Fatalf("complete 2FA result=%+v err=%v", completed, err)
	}
	access, err := fx.svc.ParseUserJWT(completed.Token)
	if err != nil || access.Typ != jwttoken.TypeAccess || !jwttoken.IsAccessType(access.Typ) {
		t.Fatalf("completed token typ=%v err=%v", access, err)
	}
	stored, err = fx.repo.GetByEmail("buyer@example.com")
	if err != nil || stored == nil || stored.LastLoginAt == nil {
		t.Fatalf("last login after 2FA: user=%+v err=%v", stored, err)
	}
}

func TestLoginEmailCodeDoesNotSubstituteOtherPurposes(t *testing.T) {
	cfg := readyEmailConfig()
	cfg.VerifyCode.SendIntervalSeconds = 1
	fx := newEmailLoginFixture(t, cfg)
	buyer := createEmailLoginUser(t, fx.repo, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	ctx := context.Background()

	if err := fx.svc.SendVerifyCode(ctx, "buyer@example.com", constants.VerifyPurposeReset, constants.LocaleZhCN, ""); err != nil {
		t.Fatalf("send reset: %v", err)
	}
	_, resetCode, resetPurpose := fx.mail.snapshot()
	if resetPurpose != constants.VerifyPurposeReset || resetCode == "" {
		t.Fatalf("reset mail = %s %q", resetPurpose, resetCode)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", resetCode, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("reset code used as login: %v", err)
	}
	if err := fx.svc.ResetPassword("buyer@example.com", resetCode, "secret456"); err != nil {
		t.Fatalf("reset code was consumed by the login attempt: %v", err)
	}

	if err := fx.svc.SendLoginEmailCode(ctx, "buyer@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send login: %v", err)
	}
	_, loginCode, loginPurpose := fx.mail.snapshot()
	if loginPurpose != constants.VerifyPurposeLogin || loginCode == "" || loginCode == resetCode {
		t.Fatalf("login mail = %s %q", loginPurpose, loginCode)
	}
	if err := fx.svc.ResetPassword("buyer@example.com", loginCode, "secret789"); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("login code reset the password: %v", err)
	}

	if err := fx.svc.SendVerifyCode(ctx, "fresh@example.com", constants.VerifyPurposeRegister, constants.LocaleZhCN, ""); err != nil {
		t.Fatalf("send register: %v", err)
	}
	_, registerCode, registerPurpose := fx.mail.snapshot()
	if registerPurpose != constants.VerifyPurposeRegister {
		t.Fatalf("register purpose = %s", registerPurpose)
	}
	if _, err := fx.svc.LoginWithEmailCode("fresh@example.com", registerCode, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("register code used as login: %v", err)
	}
	var registerRow emailverificationdomain.Code
	if err := fx.db.Where("email = ? AND purpose = ? AND deleted_at IS NULL", "fresh@example.com", constants.VerifyPurposeRegister).
		Order("id desc").First(&registerRow).Error; err != nil {
		t.Fatalf("register row: %v", err)
	}
	if registerRow.VerifiedAt != nil {
		t.Fatal("failed login consumed the registration code")
	}
	if _, _, _, err := fx.svc.Register("fresh@example.com", "secret123", loginCode, "", true, true); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("login code registered an account: %v", err)
	}

	if err := fx.svc.SendChangeEmailCode(ctx, buyer.ID, "old", "", constants.LocaleZhCN); err != nil {
		t.Fatalf("send old email code: %v", err)
	}
	_, oldCode, oldPurpose := fx.mail.snapshot()
	if oldPurpose != constants.VerifyPurposeChangeEmailOld {
		t.Fatalf("old email purpose = %s", oldPurpose)
	}
	if err := fx.svc.SendChangeEmailCode(ctx, buyer.ID, "new", "next@example.com", constants.LocaleZhCN); err != nil {
		t.Fatalf("send new email code: %v", err)
	}
	_, newCode, newPurpose := fx.mail.snapshot()
	if newPurpose != constants.VerifyPurposeChangeEmailNew {
		t.Fatalf("new email purpose = %s", newPurpose)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", oldCode, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("old email code used as login: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("next@example.com", newCode, false); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("new email code used as login: %v", err)
	}
	if _, err := fx.svc.LoginWithEmailCode("buyer@example.com", loginCode, false); err != nil {
		t.Fatalf("login code after rejected reset and email-change codes: %v", err)
	}
	changed, err := fx.svc.ChangeEmail(buyer.ID, "next@example.com", oldCode, newCode)
	if err != nil || changed == nil || changed.Email != "next@example.com" {
		t.Fatalf("change email after rejected login: user=%+v err=%v", changed, err)
	}
}
