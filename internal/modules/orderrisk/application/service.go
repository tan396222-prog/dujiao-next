package application

import (
	"crypto/sha256"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/logger"
	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

type parsedIPBlacklist struct {
	exactIPs map[string]struct{}
	cidrs    []*net.IPNet
	hash     string
}

// Options 声明订单风控应用服务的全部端口。
type Options struct {
	Settings       orderriskcontract.SettingReader
	RateLimiter    orderriskcontract.RateLimiter
	AttemptLimiter orderriskcontract.AttemptLimiter
}

// Service 编排身份分流、黑名单、商品数量、待支付库存配额和下单频率检查。
type Service struct {
	settings       orderriskcontract.SettingReader
	rateLimiter    orderriskcontract.RateLimiter
	attemptLimiter orderriskcontract.AttemptLimiter

	mu              sync.RWMutex
	cachedBlacklist *parsedIPBlacklist
	nonces          sync.Map
}

var _ orderriskcontract.Controller = (*Service)(nil)

func NewService(options Options) *Service {
	return &Service{settings: options.Settings, rateLimiter: options.RateLimiter, attemptLimiter: options.AttemptLimiter}
}

// CheckOrderAllowed 在订单事务外读取配置并执行前置风控，返回后续事务应复用的配置与规范化 IP。
func (s *Service) CheckOrderAllowed(input orderriskcontract.CheckInput) (orderriskcontract.CheckResult, error) {
	result := orderriskcontract.CheckResult{
		RiskIP:         orderriskcontract.NormalizeRiskIP(input.ClientIP),
		ConfigSnapshot: settingssecurity.DefaultOrderRiskControlConfig(),
	}
	if s == nil || s.settings == nil {
		return result, nil
	}
	cfg, err := s.settings.GetOrderRiskControlConfig()
	if err != nil {
		logger.Warnw("risk_control_get_config_error", "error", err)
		if input.ConsumeRateLimit {
			return result, err
		}
		return result, nil
	}
	result.ConfigSnapshot = cfg
	if err := s.consumeOrderAttempts(input, result.RiskIP, cfg); err != nil {
		return result, err
	}
	if !cfg.Enabled {
		return result, nil
	}

	if !input.SkipIPCheck && input.ClientIP != "" && len(cfg.Common.IPBlacklist) > 0 && s.isIPInBlacklist(input.ClientIP, cfg.Common.IPBlacklist) {
		return result, orderriskcontract.ErrIPBlacklisted
	}
	if input.IsGuest {
		return s.checkGuestOrder(input, result, cfg)
	}
	return s.checkMemberOrder(input, result, cfg)
}

func (s *Service) consumeOrderAttempts(input orderriskcontract.CheckInput, riskIP string, cfg settingssecurity.OrderRiskControlConfig) error {
	if s == nil || s.attemptLimiter == nil || !input.ConsumeRateLimit || input.SkipIPCheck || riskIP == "" || !cfg.Common.AttemptLimit.Enabled {
		return nil
	}
	return s.attemptLimiter.Consume(riskIP, cfg.Common.AttemptLimit, orderriskcontract.AttemptMeta{
		Device: input.DeviceSignal,
		Signal: input.AutomationSignal,
		UserID: input.UserID,
		Guest:  input.IsGuest,
	})
}

// ListOrderAttempts 返回仍在窗口或封禁期内的下单 IP，以及封禁和解封记录。
func (s *Service) ListOrderAttempts() (orderriskcontract.AttemptReport, error) {
	cfg := settingssecurity.DefaultOrderRiskControlConfig()
	storage := "memory"
	if s != nil && s.settings != nil {
		if loaded, err := s.settings.GetOrderRiskControlConfig(); err == nil {
			cfg = loaded
		}
	}
	report := orderriskcontract.AttemptReport{Limit: cfg.Common.AttemptLimit, Storage: storage}
	if s == nil || s.attemptLimiter == nil {
		return report, nil
	}
	report.Active, report.Events = s.attemptLimiter.Snapshot(cfg.Common.AttemptLimit.MaxRequests)
	if len(report.Active) > 0 {
		report.Storage = report.Active[0].Backend
	}
	return report, nil
}

// InspectOrderAttempt 只查看当前 IP 是否已经在封禁窗口内，不增加计数，也不创建订单。
func (s *Service) InspectOrderAttempt(clientIP string) (bool, int64) {
	riskIP := orderriskcontract.NormalizeRiskIP(clientIP)
	if s == nil || s.attemptLimiter == nil || riskIP == "" {
		return false, 0
	}
	cfg := settingssecurity.DefaultOrderRiskControlConfig()
	if s.settings != nil {
		if loaded, err := s.settings.GetOrderRiskControlConfig(); err == nil {
			cfg = loaded
		}
	}
	if !cfg.Common.AttemptLimit.Enabled {
		return false, 0
	}
	_, retry, banned := s.attemptLimiter.Peek(riskIP, cfg.Common.AttemptLimit.MaxRequests)
	if !banned {
		return false, 0
	}
	return true, retry
}

// AcceptProtectionSignal 校验结账预检令牌。无效、过期或重放都只是没有证据，不会因此放行或封禁。
func (s *Service) AcceptProtectionSignal(subject, token, secret string, now time.Time) string {
	claims, ok := orderriskcontract.VerifyProtectionToken(secret, token, subject, now)
	if !ok || s == nil {
		return ""
	}
	if !s.consumeNonce(claims.Nonce) {
		return ""
	}
	return claims.Signal
}

func (s *Service) consumeNonce(nonce string) bool {
	if nonce == "" {
		return false
	}
	if _, loaded := s.nonces.LoadOrStore(nonce, time.Now().Add(2*time.Minute)); loaded {
		return false
	}
	return true
}

// UnbanOrderAttempt 清除一个规范化 IP 的临时下单封禁。它不修改账户状态。
func (s *Service) UnbanOrderAttempt(raw string) (bool, error) {
	key := orderriskcontract.ParseAttemptKey(raw)
	if key == "" {
		return false, orderriskcontract.ErrAttemptKeyInvalid
	}
	if s == nil || s.attemptLimiter == nil {
		return false, nil
	}
	return s.attemptLimiter.Clear(key)
}

func (s *Service) checkGuestOrder(input orderriskcontract.CheckInput, result orderriskcontract.CheckResult, cfg settingssecurity.OrderRiskControlConfig) (orderriskcontract.CheckResult, error) {
	policy := cfg.Guest
	if !policy.Enabled {
		return result, nil
	}
	result.PaymentExpireMinutes = policy.PaymentExpireMinutes
	if !input.SkipIPCheck && guestPolicyRequiresIP(policy) && result.RiskIP == "" {
		return result, orderriskcontract.ErrClientIPUnavailable
	}
	if err := checkProductQuantityLimit(input.Items, policy.MaxQuantityPerProductPerOrder); err != nil {
		return result, err
	}
	if input.ConsumeRateLimit && policy.RateLimit.Enabled && s.rateLimiter != nil {
		input.RiskIP = result.RiskIP
		if err := s.rateLimiter.Check(input, policy.RateLimit); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) checkMemberOrder(input orderriskcontract.CheckInput, result orderriskcontract.CheckResult, cfg settingssecurity.OrderRiskControlConfig) (orderriskcontract.CheckResult, error) {
	policy := cfg.Member
	if !policy.Enabled {
		return result, nil
	}
	if !input.SkipIPCheck && policy.MaxPendingOrdersPerIP > 0 && result.RiskIP == "" {
		return result, orderriskcontract.ErrClientIPUnavailable
	}
	if err := checkProductQuantityLimit(input.Items, policy.MaxQuantityPerProductPerOrder); err != nil {
		return result, err
	}
	if input.ConsumeRateLimit && policy.RateLimit.Enabled && s.rateLimiter != nil {
		input.RiskIP = result.RiskIP
		if err := s.rateLimiter.Check(input, policy.RateLimit); err != nil {
			return result, err
		}
	}
	return result, nil
}

func guestPolicyRequiresIP(policy settingssecurity.OrderRiskGuestPolicy) bool {
	return policy.MaxPendingOrdersPerIP > 0 ||
		policy.MaxPendingQuantityPerIPProduct > 0 ||
		policy.RateLimit.Enabled
}

func checkProductQuantityLimit(items []orderriskcontract.OrderItem, maximum int) error {
	if maximum <= 0 {
		return nil
	}
	for _, quantity := range aggregateProductQuantities(items) {
		if quantity > int64(maximum) {
			return orderriskcontract.ErrProductQuantityLimit
		}
	}
	return nil
}

func aggregateProductQuantities(items []orderriskcontract.OrderItem) map[uint]int64 {
	result := make(map[uint]int64, len(items))
	for _, item := range items {
		if item.ProductID == 0 || item.Quantity <= 0 {
			continue
		}
		result[item.ProductID] += int64(item.Quantity)
	}
	return result
}

// CheckPendingOrderAllowed 必须在订单事务内、创建父订单和锁库存之前调用。
// prepared 来自事务外的 CheckOrderAllowed；事务内禁止再次读取独立设置仓储。
func (s *Service) CheckPendingOrderAllowed(input orderriskcontract.CheckInput, prepared orderriskcontract.CheckResult, gate orderriskcontract.PendingOrderGate) error {
	if s == nil || gate == nil {
		return nil
	}
	cfg := prepared.ConfigSnapshot
	if !cfg.Enabled {
		return nil
	}
	if input.RiskIP == "" {
		input.RiskIP = prepared.RiskIP
		if input.RiskIP == "" {
			input.RiskIP = orderriskcontract.NormalizeRiskIP(input.ClientIP)
		}
	}
	if input.IsGuest {
		return checkGuestPendingOrder(input, cfg.Guest, gate)
	}
	return checkMemberPendingOrder(input, cfg.Member, gate)
}

func checkGuestPendingOrder(input orderriskcontract.CheckInput, policy settingssecurity.OrderRiskGuestPolicy, gate orderriskcontract.PendingOrderGate) error {
	if !policy.Enabled || input.SkipIPCheck {
		return nil
	}
	if !guestPolicyRequiresIP(policy) {
		return nil
	}
	if input.RiskIP == "" {
		return orderriskcontract.ErrClientIPUnavailable
	}
	if err := gate.LockRiskKeys([]string{"guest:ip:" + input.RiskIP}); err != nil {
		return err
	}
	if policy.MaxPendingOrdersPerIP > 0 {
		count, err := gate.CountPendingGuestByRiskIP(input.RiskIP)
		if err != nil {
			return err
		}
		if count >= int64(policy.MaxPendingOrdersPerIP) {
			return orderriskcontract.ErrTooManyPendingOrders
		}
	}
	if policy.MaxPendingQuantityPerIPProduct <= 0 {
		return nil
	}
	requested := aggregateProductQuantities(input.Items)
	productIDs := make([]uint, 0, len(requested))
	for productID := range requested {
		productIDs = append(productIDs, productID)
	}
	sort.Slice(productIDs, func(i, j int) bool { return productIDs[i] < productIDs[j] })
	pending, err := gate.SumPendingGuestQuantityByRiskIP(input.RiskIP, productIDs)
	if err != nil {
		return err
	}
	for productID, quantity := range requested {
		if pending[productID]+quantity > int64(policy.MaxPendingQuantityPerIPProduct) {
			return orderriskcontract.ErrPendingProductQuantityLimit
		}
	}
	return nil
}

func checkMemberPendingOrder(input orderriskcontract.CheckInput, policy settingssecurity.OrderRiskMemberPolicy, gate orderriskcontract.PendingOrderGate) error {
	if !policy.Enabled {
		return nil
	}
	keys := make([]string, 0, 2)
	if input.UserID > 0 && policy.MaxPendingOrdersPerUser > 0 {
		keys = append(keys, fmt.Sprintf("member:user:%d", input.UserID))
	}
	if !input.SkipIPCheck && input.RiskIP != "" && policy.MaxPendingOrdersPerIP > 0 {
		keys = append(keys, "member:ip:"+input.RiskIP)
	}
	if len(keys) > 0 {
		if err := gate.LockRiskKeys(keys); err != nil {
			return err
		}
	}
	if input.UserID > 0 && policy.MaxPendingOrdersPerUser > 0 {
		count, err := gate.CountPendingByUserID(input.UserID)
		if err != nil {
			return err
		}
		if count >= int64(policy.MaxPendingOrdersPerUser) {
			return orderriskcontract.ErrTooManyPendingOrders
		}
	}
	if !input.SkipIPCheck && input.RiskIP != "" && policy.MaxPendingOrdersPerIP > 0 {
		count, err := gate.CountPendingMemberByRiskIP(input.RiskIP)
		if err != nil {
			return err
		}
		if count >= int64(policy.MaxPendingOrdersPerIP) {
			return orderriskcontract.ErrTooManyPendingOrders
		}
	}
	return nil
}

func (s *Service) getOrBuildBlacklist(blacklist []string) *parsedIPBlacklist {
	hash := hashBlacklist(blacklist)
	s.mu.RLock()
	if s.cachedBlacklist != nil && s.cachedBlacklist.hash == hash {
		cached := s.cachedBlacklist
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	parsed := &parsedIPBlacklist{exactIPs: make(map[string]struct{}, len(blacklist)), hash: hash}
	for _, entry := range blacklist {
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err == nil {
				parsed.cidrs = append(parsed.cidrs, cidr)
			}
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			parsed.exactIPs[ip.String()] = struct{}{}
		}
	}

	s.mu.Lock()
	if s.cachedBlacklist == nil || s.cachedBlacklist.hash != hash {
		s.cachedBlacklist = parsed
	}
	s.mu.Unlock()
	return parsed
}

func hashBlacklist(list []string) string {
	hash := sha256.New()
	for _, value := range list {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (s *Service) isIPInBlacklist(clientIP string, blacklist []string) bool {
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}
	parsed := s.getOrBuildBlacklist(blacklist)
	if _, exists := parsed.exactIPs[ip.String()]; exists {
		return true
	}
	for _, cidr := range parsed.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
