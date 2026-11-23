package application

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	orderstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"
	orderriskdomain "github.com/dujiao-next/internal/modules/orderrisk/domain"
	orderrisklimiter "github.com/dujiao-next/internal/modules/orderrisk/infrastructure/redislimiter"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type settingReaderStub struct {
	config settingssecurity.OrderRiskControlConfig
	err    error
}

func (s settingReaderStub) GetOrderRiskControlConfig() (settingssecurity.OrderRiskControlConfig, error) {
	return s.config, s.err
}

type countingSettingReader struct {
	config settingssecurity.OrderRiskControlConfig
	err    error
	calls  int
}

func (s *countingSettingReader) GetOrderRiskControlConfig() (settingssecurity.OrderRiskControlConfig, error) {
	s.calls++
	return s.config, s.err
}

type rateLimiterStub struct {
	calls  int
	input  orderriskcontract.CheckInput
	config settingssecurity.OrderRateLimitConfig
	err    error
}

func (s *rateLimiterStub) Check(input orderriskcontract.CheckInput, config settingssecurity.OrderRateLimitConfig) error {
	s.calls++
	s.input = input
	s.config = config
	return s.err
}

type pendingGateStub struct {
	lockedKeys       []string
	pendingByUser    int64
	pendingGuestIP   int64
	pendingMemberIP  int64
	pendingByProduct map[uint]int64
	err              error
}

func (s *pendingGateStub) LockRiskKeys(keys []string) error {
	s.lockedKeys = append([]string(nil), keys...)
	return s.err
}
func (s *pendingGateStub) CountPendingByUserID(uint) (int64, error) {
	return s.pendingByUser, s.err
}
func (s *pendingGateStub) CountPendingGuestByRiskIP(string) (int64, error) {
	return s.pendingGuestIP, s.err
}
func (s *pendingGateStub) CountPendingMemberByRiskIP(string) (int64, error) {
	return s.pendingMemberIP, s.err
}
func (s *pendingGateStub) SumPendingGuestQuantityByRiskIP(string, []uint) (map[uint]int64, error) {
	return s.pendingByProduct, s.err
}

func testConfig() settingssecurity.OrderRiskControlConfig {
	cfg := settingssecurity.DefaultOrderRiskControlConfig()
	cfg.Enabled = true
	return cfg
}

func TestCheckOrderAllowed_DisabledByDefault(t *testing.T) {
	svc := NewService(Options{Settings: settingReaderStub{config: settingssecurity.DefaultOrderRiskControlConfig()}})
	result, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		IsGuest:  true,
		ClientIP: "2001:db8:abcd:12::1234",
		Items:    []orderriskcontract.OrderItem{{ProductID: 1, Quantity: 999}},
	})
	if err != nil {
		t.Fatalf("expected nil when globally disabled, got %v", err)
	}
	if result.RiskIP != "2001:db8:abcd:12::/64" {
		t.Fatalf("expected normalized risk IP even while disabled, got %q", result.RiskIP)
	}
}

func TestCheckPendingOrderAllowedDoesNotReadSettingsInsideSingleConnectionTransaction(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "risk-single-connection.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&settingsstore.SettingRecord{},
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&orderriskdomain.LockKey{},
	); err != nil {
		t.Fatalf("migrate risk tables: %v", err)
	}
	cfg := testConfig()
	if err := db.Create(&settingsstore.SettingRecord{
		Key:       constants.SettingKeyOrderRiskControlConfig,
		ValueJSON: settingssecurity.EncodeOrderRiskControlConfig(cfg),
	}).Error; err != nil {
		t.Fatalf("seed enabled risk config: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	settings := settingsapp.NewService(settingsstore.New(db))
	svc := NewService(Options{Settings: settings})
	orders := orderstore.New(db, "risk-single-connection-secret-with-32-bytes")
	input := orderriskcontract.CheckInput{
		IsGuest:  true,
		ClientIP: "1.2.3.4",
		Items:    []orderriskcontract.OrderItem{{ProductID: 7, Quantity: 1}},
	}
	prepared, err := svc.CheckOrderAllowed(input)
	if err != nil {
		t.Fatalf("prepare risk check: %v", err)
	}
	if !prepared.ConfigSnapshot.Enabled {
		t.Fatal("expected enabled risk config snapshot")
	}

	done := make(chan error, 1)
	go func() {
		done <- orders.WithinTransaction(func(tx ordercontract.Transaction) error {
			return svc.CheckPendingOrderAllowed(input, prepared, tx.Orders())
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pending risk check: %v", err)
		}
	case <-time.After(2 * time.Second):
		stats := sqlDB.Stats()
		// 释放旧实现中等待第二条连接的 goroutine，避免失败测试泄漏。
		sqlDB.SetMaxOpenConns(2)
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatalf("pending risk check blocked inside transaction: in_use=%d open=%d wait_count=%d", stats.InUse, stats.OpenConnections, stats.WaitCount)
	}
}

func TestPendingRiskCheckReusesPreparedConfigSnapshot(t *testing.T) {
	cfg := testConfig()
	settings := &countingSettingReader{config: cfg}
	svc := NewService(Options{Settings: settings})
	input := orderriskcontract.CheckInput{IsGuest: true, ClientIP: "1.2.3.4"}

	prepared, err := svc.CheckOrderAllowed(input)
	if err != nil {
		t.Fatalf("prepare risk check: %v", err)
	}
	if err := svc.CheckPendingOrderAllowed(input, prepared, &pendingGateStub{}); err != nil {
		t.Fatalf("pending risk check: %v", err)
	}
	if settings.calls != 1 {
		t.Fatalf("settings reads=%d, want exactly one before the transaction", settings.calls)
	}
}

func TestCheckOrderAllowed_SettingsReadFailureFailsClosedOnlyForCreate(t *testing.T) {
	settingsErr := errors.New("settings unavailable")
	svc := NewService(Options{Settings: settingReaderStub{err: settingsErr}})

	preview, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		IsGuest:  true,
		ClientIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("preview-style check should preserve fail-open behavior, got %v", err)
	}
	if preview.ConfigSnapshot.Enabled {
		t.Fatal("preview fallback config must keep risk control disabled")
	}

	_, err = svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		IsGuest:          true,
		ClientIP:         "1.2.3.4",
		ConsumeRateLimit: true,
	})
	if !errors.Is(err, settingsErr) {
		t.Fatalf("create-style check must fail closed, got %v", err)
	}
}

func TestCheckOrderAllowed_IPBlacklistCanonicalizesAddress(t *testing.T) {
	cfg := testConfig()
	cfg.Common.IPBlacklist = []string{"1.2.3.4", "10.0.0.0/8"}
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	for _, ip := range []string{"1.2.3.4", "::ffff:1.2.3.4", "10.2.3.4"} {
		if _, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{IsGuest: true, ClientIP: ip}); !errors.Is(err, orderriskcontract.ErrIPBlacklisted) {
			t.Fatalf("expected %q to be blacklisted, got %v", ip, err)
		}
	}
}

func TestCheckOrderAllowed_GuestQuantityAggregatesProductAcrossSKUs(t *testing.T) {
	cfg := testConfig()
	cfg.Guest.MaxQuantityPerProductPerOrder = 2
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	_, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		IsGuest:  true,
		ClientIP: "1.2.3.4",
		Items: []orderriskcontract.OrderItem{
			{ProductID: 7, Quantity: 1},
			{ProductID: 7, Quantity: 2},
		},
	})
	if !errors.Is(err, orderriskcontract.ErrProductQuantityLimit) {
		t.Fatalf("expected aggregate guest quantity to be rejected, got %v", err)
	}
}

func TestCheckOrderAllowed_MemberUsesIndependentQuantityPolicy(t *testing.T) {
	cfg := testConfig()
	cfg.Guest.MaxQuantityPerProductPerOrder = 1
	cfg.Member.MaxQuantityPerProductPerOrder = 5
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	if _, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		UserID: 3,
		Items:  []orderriskcontract.OrderItem{{ProductID: 1, Quantity: 5}},
	}); err != nil {
		t.Fatalf("member quantity must not use guest limit: %v", err)
	}
	if _, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{
		UserID: 3,
		Items:  []orderriskcontract.OrderItem{{ProductID: 1, Quantity: 6}},
	}); !errors.Is(err, orderriskcontract.ErrProductQuantityLimit) {
		t.Fatalf("expected member quantity limit, got %v", err)
	}
}

func TestCheckOrderAllowed_GuestRateLimitUsesRiskIPOnlyWhenConsuming(t *testing.T) {
	cfg := testConfig()
	limited := &orderriskcontract.RateLimitedError{RetryAfter: 42}
	limiter := &rateLimiterStub{err: limited}
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}, RateLimiter: limiter})
	input := orderriskcontract.CheckInput{IsGuest: true, ClientIP: "2001:db8:1:2::abcd"}

	if _, err := svc.CheckOrderAllowed(input); err != nil {
		t.Fatalf("preview-style check must not consume rate limit: %v", err)
	}
	if limiter.calls != 0 {
		t.Fatalf("expected no rate limiter call, got %d", limiter.calls)
	}
	input.ConsumeRateLimit = true
	if _, err := svc.CheckOrderAllowed(input); err != limited {
		t.Fatalf("expected limiter error, got %v", err)
	}
	if limiter.calls != 1 || limiter.input.RiskIP != "2001:db8:1:2::/64" || limiter.input.UserID != 0 {
		t.Fatalf("unexpected guest limiter input: calls=%d input=%+v", limiter.calls, limiter.input)
	}
}

func TestCheckPendingOrderAllowed_GuestLocksIPAndCountsOrders(t *testing.T) {
	cfg := testConfig()
	cfg.Guest.MaxPendingOrdersPerIP = 2
	gate := &pendingGateStub{pendingGuestIP: 2}
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	err := svc.CheckPendingOrderAllowed(orderriskcontract.CheckInput{
		IsGuest: true,
		RiskIP:  "1.2.3.4",
		Items:   []orderriskcontract.OrderItem{{ProductID: 1, Quantity: 1}},
	}, orderriskcontract.CheckResult{ConfigSnapshot: cfg}, gate)
	if !errors.Is(err, orderriskcontract.ErrTooManyPendingOrders) {
		t.Fatalf("expected guest pending order limit, got %v", err)
	}
	if !reflect.DeepEqual(gate.lockedKeys, []string{"guest:ip:1.2.3.4"}) {
		t.Fatalf("unexpected lock keys: %#v", gate.lockedKeys)
	}
}

func TestCheckPendingOrderAllowed_GuestCountsPendingProductQuantity(t *testing.T) {
	cfg := testConfig()
	cfg.Guest.MaxPendingOrdersPerIP = 10
	cfg.Guest.MaxPendingQuantityPerIPProduct = 2
	gate := &pendingGateStub{pendingByProduct: map[uint]int64{7: 1}}
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	err := svc.CheckPendingOrderAllowed(orderriskcontract.CheckInput{
		IsGuest: true,
		RiskIP:  "1.2.3.4",
		Items: []orderriskcontract.OrderItem{
			{ProductID: 7, Quantity: 1},
			{ProductID: 7, Quantity: 1},
		},
	}, orderriskcontract.CheckResult{ConfigSnapshot: cfg}, gate)
	if !errors.Is(err, orderriskcontract.ErrPendingProductQuantityLimit) {
		t.Fatalf("expected pending product quantity limit, got %v", err)
	}
}

func TestCheckPendingOrderAllowed_MemberUsesUserAndOptionalIP(t *testing.T) {
	cfg := testConfig()
	cfg.Member.MaxPendingOrdersPerUser = 5
	cfg.Member.MaxPendingOrdersPerIP = 3
	gate := &pendingGateStub{pendingByUser: 4, pendingMemberIP: 3}
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	err := svc.CheckPendingOrderAllowed(
		orderriskcontract.CheckInput{UserID: 9, RiskIP: "1.2.3.4"},
		orderriskcontract.CheckResult{ConfigSnapshot: cfg},
		gate,
	)
	if !errors.Is(err, orderriskcontract.ErrTooManyPendingOrders) {
		t.Fatalf("expected member IP pending limit, got %v", err)
	}
	if !reflect.DeepEqual(gate.lockedKeys, []string{"member:user:9", "member:ip:1.2.3.4"}) {
		t.Fatalf("unexpected member lock keys: %#v", gate.lockedKeys)
	}
}

func TestCheckOrderAllowed_GuestReturnsExpiryOverride(t *testing.T) {
	cfg := testConfig()
	cfg.Guest.PaymentExpireMinutes = 8
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}})

	result, err := svc.CheckOrderAllowed(orderriskcontract.CheckInput{IsGuest: true, ClientIP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if result.PaymentExpireMinutes != 8 {
		t.Fatalf("expected guest expiry 8, got %d", result.PaymentExpireMinutes)
	}
}

func TestOrderAttemptLimitAppliesWhenMasterSwitchIsOff(t *testing.T) {
	limiter := orderrisklimiter.New()
	svc := NewService(Options{
		Settings:       settingReaderStub{config: settingssecurity.DefaultOrderRiskControlConfig()},
		AttemptLimiter: limiter,
	})
	for i := 0; i < 20; i++ {
		if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.20", true, true)); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.20", false, true)); !errors.Is(err, orderriskcontract.ErrOrderRateLimited) {
		t.Fatalf("member on the same IP must share the ban, got %v", err)
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.21", true, true)); err != nil {
		t.Fatalf("a different IP must stay allowed, got %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.20", true, false)); err != nil {
			t.Fatalf("preview must not be blocked or counted, got %v", err)
		}
	}
}

func TestOrderAttemptLimitSkipsChannelOrdersAndEmptyIP(t *testing.T) {
	limiter := orderrisklimiter.New()
	svc := NewService(Options{
		Settings:       settingReaderStub{config: settingssecurity.DefaultOrderRiskControlConfig()},
		AttemptLimiter: limiter,
	})
	for i := 0; i < 30; i++ {
		input := attemptInput("198.51.100.30", true, true)
		input.SkipIPCheck = true
		if _, err := svc.CheckOrderAllowed(input); err != nil {
			t.Fatalf("channel order must skip IP attempts: %v", err)
		}
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.30", true, true)); err != nil {
		t.Fatalf("skipped attempts must not consume the IP quota, got %v", err)
	}
}

func TestOrderAttemptBanExpiresAndCanBeCleared(t *testing.T) {
	limiter := orderrisklimiter.New()
	cfg := settingssecurity.DefaultOrderRiskControlConfig()
	cfg.Common.AttemptLimit.BlockSeconds = 1
	svc := NewService(Options{Settings: settingReaderStub{config: cfg}, AttemptLimiter: limiter})
	for i := 0; i < 20; i++ {
		if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.40", true, true)); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.40", true, true)); !errors.Is(err, orderriskcontract.ErrOrderRateLimited) {
		t.Fatalf("expected ban, got %v", err)
	}
	cleared, err := svc.UnbanOrderAttempt("198.51.100.40")
	if err != nil || !cleared {
		t.Fatalf("unban cleared=%v err=%v", cleared, err)
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.40", true, true)); err != nil {
		t.Fatalf("unban must allow the next order, got %v", err)
	}
	for i := 0; i < 19; i++ {
		if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.41", true, true)); err != nil {
			t.Fatalf("refill %d: %v", i, err)
		}
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.41", true, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.41", true, true)); !errors.Is(err, orderriskcontract.ErrOrderRateLimited) {
		t.Fatalf("expected fresh ban, got %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := svc.CheckOrderAllowed(attemptInput("198.51.100.41", true, true)); err != nil {
		t.Fatalf("ban must expire, got %v", err)
	}
}

func TestOrderAttemptLimitIsAtomicAndDoesNotPunishAnotherIPForTheSameDevice(t *testing.T) {
	limiter := orderrisklimiter.New()
	cfg := settingssecurity.OrderRateLimitConfig{Enabled: true, WindowSeconds: 60, MaxRequests: 20, BlockSeconds: 86400}
	meta := orderriskcontract.AttemptMeta{Device: "0123456789abcdef"}
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Consume("203.0.113.50", cfg, meta); err == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 20 {
		t.Fatalf("allowed %d concurrent attempts, want 20", allowed.Load())
	}
	if err := limiter.Consume("203.0.113.51", cfg, meta); err != nil {
		t.Fatalf("the same device mark from another IP must not be banned, got %v", err)
	}
	if err := limiter.Consume("203.0.113.50", cfg, meta); !errors.Is(err, orderriskcontract.ErrOrderRateLimited) {
		t.Fatalf("the busy IP must stay banned, got %v", err)
	}
}

func TestOrderAttemptIPv6PrefixIsShared(t *testing.T) {
	limiter := orderrisklimiter.New()
	svc := NewService(Options{
		Settings:       settingReaderStub{config: settingssecurity.DefaultOrderRiskControlConfig()},
		AttemptLimiter: limiter,
	})
	for i := 0; i < 20; i++ {
		if _, err := svc.CheckOrderAllowed(attemptInput("2001:db8:abcd:12::"+string(rune('a'+i%6)), true, true)); err != nil {
			t.Fatalf("ipv6 attempt %d: %v", i, err)
		}
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("2001:db8:abcd:12::ffff", true, true)); !errors.Is(err, orderriskcontract.ErrOrderRateLimited) {
		t.Fatalf("same /64 must share the counter, got %v", err)
	}
	if _, err := svc.CheckOrderAllowed(attemptInput("2001:db8:eeee:12::1", true, true)); err != nil {
		t.Fatalf("different /64 must stay allowed, got %v", err)
	}
}

func TestDeviceSignalRejectsForgeryAndExpiry(t *testing.T) {
	secret := "test-device-secret"
	now := time.Unix(1_700_000_000, 0)
	token, id, err := orderriskcontract.IssueDeviceToken(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := orderriskcontract.VerifyDeviceToken(secret, token, now.Add(time.Hour))
	if !ok || got != id {
		t.Fatalf("valid token id=%q ok=%v", got, ok)
	}
	if _, ok := orderriskcontract.VerifyDeviceToken("other-secret", token, now); ok {
		t.Fatal("token must not verify under another secret")
	}
	dot := strings.LastIndex(token, ".")
	replacement := "1"
	if token[dot+1] == '1' {
		replacement = "2"
	}
	forged := token[:dot+1] + replacement + token[dot+2:]
	if _, ok := orderriskcontract.VerifyDeviceToken(secret, forged, now); ok {
		t.Fatal("tampered token must be ignored")
	}
	if _, ok := orderriskcontract.VerifyDeviceToken(secret, token, now.Add(25*time.Hour)); ok {
		t.Fatal("expired token must be ignored")
	}
	if key := orderriskcontract.ParseAttemptKey("10.0.0.0/8"); key != "" {
		t.Fatalf("cidr unban key = %q", key)
	}
	full := "2001:db8:abcd:12::99"
	if orderriskcontract.ParseAttemptKey(full) != orderriskcontract.NormalizeRiskIP(full) {
		t.Fatal("ipv6 unban must use the stored /64 key")
	}
}

func attemptInput(ip string, guest bool, consume bool) orderriskcontract.CheckInput {
	input := orderriskcontract.CheckInput{IsGuest: guest, ClientIP: ip, ConsumeRateLimit: consume, DeviceSignal: "0123456789abcdef"}
	if !guest {
		input.UserID = 7
	}
	return input
}
