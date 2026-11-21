package contract

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
)

const (
	DeviceCookieName = "dj_device"
	deviceTokenTTL   = 24 * time.Hour
	AttemptReason    = "order_attempt_limit"
)

// AttemptSnapshot 是仍在计数窗口或封禁期内的一个 IP。
type AttemptSnapshot struct {
	IP         string `json:"ip"`
	Count      int64  `json:"count"`
	Limit      int    `json:"limit"`
	Banned     bool   `json:"banned"`
	RetryAfter int64  `json:"retry_after_seconds"`
	Device     string `json:"device,omitempty"`
	Signal     string `json:"signal,omitempty"`
	UserID     uint   `json:"user_id,omitempty"`
	Guest      bool   `json:"guest"`
	Backend    string `json:"backend"`
	Reason     string `json:"reason"`
}

// AttemptEvent 记录封禁或人工解封，不记录普通浏览。
type AttemptEvent struct {
	At         time.Time `json:"at"`
	IP         string    `json:"ip"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	Count      int64     `json:"count"`
	RetryAfter int64     `json:"retry_after_seconds"`
	Device     string    `json:"device,omitempty"`
	Backend    string    `json:"backend"`
}

// AttemptReport 是后台查看的当前计数、封禁和近期动作。
type AttemptReport struct {
	Limit   settingssecurity.OrderRateLimitConfig `json:"limit"`
	Active  []AttemptSnapshot                     `json:"active"`
	Events  []AttemptEvent                        `json:"events"`
	Storage string                                `json:"storage"`
}

// OrderItem 是风控所需的最小订单项快照。
type OrderItem struct {
	ProductID uint
	Quantity  int
}

// CheckInput 是订单风控检查所需的调用上下文。
type CheckInput struct {
	UserID           uint
	ClientIP         string
	RiskIP           string
	IsGuest          bool
	SkipIPCheck      bool
	ConsumeRateLimit bool
	DeviceSignal     string
	AutomationSignal string
	Items            []OrderItem
}

// CheckResult 返回创建订单后续步骤需要复用的不可变风控上下文。
type CheckResult struct {
	RiskIP               string
	PaymentExpireMinutes int
	ConfigSnapshot       settingssecurity.OrderRiskControlConfig
}

// NormalizeRiskIP 生成游客风控键：IPv4 使用完整地址，IPv6 按 /64 前缀聚合。
func NormalizeRiskIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	network := ip.Mask(net.CIDRMask(64, 128))
	if network == nil {
		return ""
	}
	return network.String() + "/64"
}

// ParseAttemptKey 只接受单个 IP，或与封禁键一致的 IPv6 /64。其它网段不能用来批量解封。
func ParseAttemptKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "/") {
		ip, network, err := net.ParseCIDR(raw)
		if err != nil || ip == nil || network == nil {
			return ""
		}
		ones, bits := network.Mask.Size()
		if bits != 128 || ones != 64 {
			return ""
		}
		key := NormalizeRiskIP(ip.String())
		if key == "" || key != network.IP.String()+"/64" {
			return ""
		}
		return key
	}
	return NormalizeRiskIP(raw)
}

// IssueDeviceToken 签发本站下单辅助标记。它不是浏览器指纹，也不能当作登录身份。
func IssueDeviceToken(secret string, now time.Time) (token string, id string, err error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", "", fmt.Errorf("missing device token secret")
	}
	buf := make([]byte, 8)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(buf)
	issuedAt := now.Unix()
	return fmt.Sprintf("v1.%s.%d.%s", id, issuedAt, deviceMAC(secret, id, issuedAt)), id, nil
}

// VerifyDeviceToken 校验标记的签名和 24 小时有效期。伪造或过期都视为没有标记。
func VerifyDeviceToken(secret, token string, now time.Time) (string, bool) {
	secret = strings.TrimSpace(secret)
	parts := strings.Split(strings.TrimSpace(token), ".")
	if secret == "" || len(parts) != 4 || parts[0] != "v1" || len(parts[1]) != 16 {
		return "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", false
	}
	issuedAt, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", false
	}
	if now.Unix()+120 < issuedAt || now.Unix()-issuedAt > int64(deviceTokenTTL/time.Second) {
		return "", false
	}
	expected := deviceMAC(secret, parts[1], issuedAt)
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(parts[3]))) {
		return "", false
	}
	return parts[1], true
}

func deviceMAC(secret, id string, issuedAt int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "v1.%s.%d", id, issuedAt)
	return hex.EncodeToString(mac.Sum(nil))
}

const protectionTokenTTL = 2 * time.Minute

// ClassifyAutomation 只保留 who-are-you 里能在本店同源使用的自动化矛盾判断。
// 来源：https://github.com/yt1988/who-are-you
// commit 0836962988ff1cb92e086fa45499a7fb0de5b4ad
// MIT License, Copyright (c) 2026 yt1988
// 对应 src/infer.mjs 的 bot 结论，以及 src/app.js 的 autoTraces。
// 未使用地理定位、VPN 指责、WebRTC、canvas、字体、第三方探测和 Durable Object。
// 结果只是下单证据，不是封禁键，也不能证明是同一个人。
func ClassifyAutomation(webdriver *bool, traces []string) string {
	if webdriver == nil {
		return "absent"
	}
	if *webdriver {
		return "webdriver"
	}
	for _, trace := range traces {
		switch strings.ToLower(strings.TrimSpace(trace)) {
		case "headless":
			return "headless"
		case "cdp":
			return "cdp"
		case "electron":
			return "electron"
		}
	}
	return "ok"
}

// ProtectionClaims 是一次结账预检的短时绑定，不能代替下单时的风控。
type ProtectionClaims struct {
	Subject string
	Bind    string
	Signal  string
	Nonce   string
}

func IssueProtectionToken(secret, subject, bind, signal string, now time.Time) (string, error) {
	secret = strings.TrimSpace(secret)
	subject = strings.TrimSpace(subject)
	if secret == "" || subject == "" || strings.Contains(subject, "|") {
		return "", fmt.Errorf("missing protection token secret")
	}
	signal = normalizeProtectionSignal(signal)
	bind = sanitizeProtectionBind(bind)
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(buf)
	issuedAt := now.Unix()
	mac := protectionMAC(secret, subject, bind, signal, issuedAt, nonce)
	return fmt.Sprintf("p1|%s|%s|%s|%d|%s|%s", subject, bind, signal, issuedAt, nonce, mac), nil
}

func VerifyProtectionToken(secret, token, subject string, now time.Time) (ProtectionClaims, bool) {
	secret = strings.TrimSpace(secret)
	subject = strings.TrimSpace(subject)
	parts := strings.Split(strings.TrimSpace(token), "|")
	if secret == "" || subject == "" || len(parts) != 7 || parts[0] != "p1" || parts[1] != subject {
		return ProtectionClaims{}, false
	}
	signal := normalizeProtectionSignal(parts[3])
	if signal != parts[3] {
		return ProtectionClaims{}, false
	}
	issuedAt, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || now.Unix()+30 < issuedAt || now.Unix()-issuedAt > int64(protectionTokenTTL/time.Second) {
		return ProtectionClaims{}, false
	}
	if len(parts[5]) != 16 {
		return ProtectionClaims{}, false
	}
	if _, err := hex.DecodeString(parts[5]); err != nil {
		return ProtectionClaims{}, false
	}
	expected := protectionMAC(secret, parts[1], parts[2], parts[3], issuedAt, parts[5])
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(parts[6]))) {
		return ProtectionClaims{}, false
	}
	return ProtectionClaims{Subject: parts[1], Bind: parts[2], Signal: signal, Nonce: parts[5]}, true
}

func normalizeProtectionSignal(signal string) string {
	switch signal {
	case "ok", "absent", "webdriver", "headless", "cdp", "electron":
		return signal
	default:
		return "absent"
	}
}

func sanitizeProtectionBind(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	if b.Len() == 0 {
		return "checkout"
	}
	return b.String()
}

func protectionMAC(secret, subject, bind, signal string, issuedAt int64, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "p1|%s|%s|%s|%d|%s", subject, bind, signal, issuedAt, nonce)
	return hex.EncodeToString(mac.Sum(nil))
}
