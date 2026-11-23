package contract

import settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"

type SettingReader interface {
	GetOrderRiskControlConfig() (settingssecurity.OrderRiskControlConfig, error)
}

// PendingOrderGate 在订单事务中序列化相同风控身份并读取真实待支付库存占用。
type PendingOrderGate interface {
	LockRiskKeys(keys []string) error
	CountPendingByUserID(userID uint) (int64, error)
	CountPendingGuestByRiskIP(riskIP string) (int64, error)
	CountPendingMemberByRiskIP(riskIP string) (int64, error)
	SumPendingGuestQuantityByRiskIP(riskIP string, productIDs []uint) (map[uint]int64, error)
}

// RateLimiter 执行具有外部状态的下单频率限制。
type RateLimiter interface {
	Check(input CheckInput, config settingssecurity.OrderRateLimitConfig) error
}

// AttemptMeta 是一次真实下单尝试附带的辅助证据，不参与封禁键。
type AttemptMeta struct {
	Device string
	Signal string
	UserID uint
	Guest  bool
}

// AttemptLimiter 按规范化 IP 对真实下单尝试做原子计数。封禁键只有 IP。
type AttemptLimiter interface {
	Consume(riskIP string, config settingssecurity.OrderRateLimitConfig, meta AttemptMeta) error
	Peek(riskIP string, limit int) (count int64, retryAfter int64, banned bool)
	Snapshot(limit int) ([]AttemptSnapshot, []AttemptEvent)
	Clear(riskIP string) (bool, error)
}

// Controller 是订单上下文调用风控所需的用例端口。
type Controller interface {
	CheckOrderAllowed(input CheckInput) (CheckResult, error)
	CheckPendingOrderAllowed(input CheckInput, prepared CheckResult, gate PendingOrderGate) error
}
