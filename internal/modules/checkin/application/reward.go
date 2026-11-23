package application

import (
	"crypto/rand"
	"io"
	"math/big"
	"time"

	"github.com/dujiao-next/internal/modules/checkin/contract"
)

const (
	// 非首签每天 1 到 30 分，含两端。
	DailyMinRewardCents int64 = 1
	DailyMaxRewardCents int64 = 30
	// 账号第一次成功签到 180 到 220 分，含两端。
	FirstMinRewardCents int64 = 180
	FirstMaxRewardCents int64 = 220
)

// RewardBand 由服务端在持有账号锁时决定，不接受客户端传入。
type RewardBand int

const (
	RewardBandDaily RewardBand = iota
	RewardBandFirst
)

func (b RewardBand) bounds() (int64, int64, bool) {
	switch b {
	case RewardBandDaily:
		return DailyMinRewardCents, DailyMaxRewardCents, true
	case RewardBandFirst:
		return FirstMinRewardCents, FirstMaxRewardCents, true
	default:
		return 0, 0, false
	}
}

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

// RewardSource 按服务端指定的区间产生均匀的整数分。
type RewardSource interface {
	Cents(band RewardBand) (int64, error)
}

// CryptoReward 使用 crypto/rand。日常区间和首签区间的两端都包含在内。
type CryptoReward struct {
	Reader io.Reader
}

func (r CryptoReward) Cents(band RewardBand) (int64, error) {
	min, max, ok := band.bounds()
	if !ok {
		return 0, contract.ErrRewardOutOfRange
	}
	reader := r.Reader
	if reader == nil {
		reader = rand.Reader
	}
	span := max - min + 1
	n, err := rand.Int(reader, big.NewInt(span))
	if err != nil {
		return 0, err
	}
	cents := n.Int64() + min
	if cents < min || cents > max {
		return 0, contract.ErrRewardOutOfRange
	}
	return cents, nil
}
