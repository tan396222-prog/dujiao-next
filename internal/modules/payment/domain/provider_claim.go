package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/shared/jsonmap"
)

// ProviderClaim 记录某个支付商户下已经使用过的外部交易号。
// 同一商户的交易号不能再绑定到另一笔订单或充值。
type ProviderClaim struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	ScopeKey    string    `gorm:"size:191;not null;uniqueIndex:ux_payment_provider_claim,priority:1" json:"scope_key"`
	ProviderRef string    `gorm:"size:191;not null;uniqueIndex:ux_payment_provider_claim,priority:2" json:"provider_ref"`
	PaymentID   uint      `gorm:"not null;index" json:"payment_id"`
	OrderID     uint      `gorm:"not null;default:0;index" json:"order_id"`
	ChannelID   uint      `gorm:"not null;index" json:"channel_id"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

// TableName 指定表名。
func (ProviderClaim) TableName() string {
	return "payment_provider_claims"
}

// ProviderClaimScope 用支付渠道类型和商户号组成交易号的唯一范围。
// 只读取商户号、应用号这类公开标识，不读取密钥。没有商户号时退回渠道 ID。
func ProviderClaimScope(providerType string, config jsonmap.JSON, channelID uint) string {
	provider := strings.ToLower(strings.TrimSpace(providerType))
	merchant := strings.ToLower(providerMerchantID(config))
	scope := fmt.Sprintf("%s|channel:%d", provider, channelID)
	if merchant != "" {
		scope = fmt.Sprintf("%s|merchant:%s", provider, merchant)
	}
	if len(scope) <= 191 {
		return scope
	}
	sum := sha256.Sum256([]byte(scope))
	return fmt.Sprintf("%s|merchant:%s", provider, hex.EncodeToString(sum[:16]))
}

func providerMerchantID(config jsonmap.JSON) string {
	for _, key := range []string{"merchant_id", "app_id", "mchid", "pid"} {
		if text := configString(config, key); text != "" {
			return text
		}
	}
	return ""
}

func configString(config jsonmap.JSON, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" || text == "<nil>" {
			return ""
		}
		return text
	}
}
