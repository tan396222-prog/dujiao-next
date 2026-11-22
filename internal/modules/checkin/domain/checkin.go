package domain

import (
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"
)

// CheckIn 是一个账号在一个北京时间自然日的签到记录。
// 同一天重复领取由 (user_id, checkin_date) 唯一约束拦住。
// 回滚代码时不要删除 wallet_transactions 里 daily-checkin: 开头的流水，否则同一天可能再次入账。
type CheckIn struct {
	ID          uint         `gorm:"primarykey"`
	UserID      uint         `gorm:"not null;uniqueIndex:idx_daily_checkins_user_date"`
	CheckinDate string       `gorm:"type:varchar(10);not null;uniqueIndex:idx_daily_checkins_user_date"`
	Amount      money.Amount `gorm:"type:decimal(20,2);not null"`
	Currency    string       `gorm:"type:varchar(16);not null;default:'CNY'"`
	WalletTxnID uint         `gorm:"not null;default:0"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (CheckIn) TableName() string { return "daily_checkins" }

// AccountCanCheckIn 只允许未删除的 active 账号领取。
func AccountCanCheckIn(status string, deleted bool) bool {
	if deleted {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(status), constants.UserStatusActive)
}
