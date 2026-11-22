package application

import (
	"crypto/rand"
	"io"
	"math/big"
	"time"

	"github.com/dujiao-next/internal/modules/checkin/contract"
)

const (
	MinRewardCents = 10
	MaxRewardCents = 80
)

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// BeijingDate 返回上海时区的日历日，格式 YYYY-MM-DD。
func BeijingDate(now time.Time) string {
	return now.In(shanghai).Format("2006-01-02")
}

// NextBeijingDate 返回下一个上海日历日。跨日按日历加一天，不按 24 小时累加。
func NextBeijingDate(date string) string {
	parsed, err := time.ParseInLocation("2006-01-02", date, shanghai)
	if err != nil {
		return ""
	}
	return parsed.AddDate(0, 0, 1).Format("2006-01-02")
}

// RewardSource 产生 10 到 80 之间的整数分。
type RewardSource interface {
	Cents() (int64, error)
}

// CryptoReward 使用 crypto/rand，端点 10 和 80 都包含在内。
type CryptoReward struct {
	Reader io.Reader
}

func (r CryptoReward) Cents() (int64, error) {
	reader := r.Reader
	if reader == nil {
		reader = rand.Reader
	}
	span := int64(MaxRewardCents - MinRewardCents + 1)
	n, err := rand.Int(reader, big.NewInt(span))
	if err != nil {
		return 0, err
	}
	cents := n.Int64() + MinRewardCents
	if cents < MinRewardCents || cents > MaxRewardCents {
		return 0, contract.ErrRewardOutOfRange
	}
	return cents, nil
}
