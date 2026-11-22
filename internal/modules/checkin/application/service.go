package application

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/checkin/contract"
	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// Result 是一次签到查询或领取的结果。
// 签到奖励全额进入购物余额，DebtOffset 始终为 0，也不抵扣未追回推广佣金。
type Result struct {
	CheckedIn                    bool
	AlreadyClaimed               bool
	Reward                       money.Amount
	Balance                      money.Amount
	Currency                     string
	CheckinDate                  string
	NextCheckinDate              string
	DebtOffset                   money.Amount
	OffsetsUnrecoveredCommission bool
}

type Options struct {
	Users    contract.AccountDirectory
	CheckIns contract.Directory
	Balances contract.BalanceReader
	Runner   contract.ClaimRunner
	Rewards  RewardSource
	Now      func() time.Time
}

type Service struct {
	users    contract.AccountDirectory
	checkIns contract.Directory
	balances contract.BalanceReader
	runner   contract.ClaimRunner
	rewards  RewardSource
	now      func() time.Time
}

func NewService(options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	rewards := options.Rewards
	if rewards == nil {
		rewards = CryptoReward{}
	}
	return &Service{
		users:    options.Users,
		checkIns: options.CheckIns,
		balances: options.Balances,
		runner:   options.Runner,
		rewards:  rewards,
		now:      now,
	}
}

// Status 只读取今日是否已领，不会入账。
func (s *Service) Status(userID uint) (Result, error) {
	if s == nil || userID == 0 {
		return Result{}, contract.ErrFailed
	}
	if _, err := s.requireActive(userID); err != nil {
		return Result{}, err
	}
	date := BeijingDate(s.now())
	row, err := s.checkIns.Find(userID, date)
	if err != nil {
		return Result{}, err
	}
	balance, err := s.balance(userID)
	if err != nil {
		return Result{}, err
	}
	if row == nil {
		return idleResult(balance, date), nil
	}
	return claimedResult(row, balance, true, date), nil
}

// Claim 在一个事务里生成奖励、写入签到行并增加购物余额。
// 重复请求、并发和支付式重试都返回同一笔奖励，不重复入账。
func (s *Service) Claim(userID uint) (Result, error) {
	if s == nil || s.runner == nil || userID == 0 {
		return Result{}, contract.ErrFailed
	}
	now := s.now()
	date := BeijingDate(now)
	var result Result
	err := s.runner.WithinClaim(func(tx contract.ClaimTx) error {
		if err := tx.LockActiveUser(userID); err != nil {
			return err
		}
		row, err := tx.FindCheckIn(userID, date)
		if err != nil {
			return err
		}
		if row != nil {
			balance, err := tx.Balance(userID)
			if err != nil {
				return err
			}
			result = claimedResult(row, balance, true, date)
			return nil
		}
		cents, err := s.rewards.Cents()
		if err != nil {
			return contract.ErrFailed
		}
		if cents < MinRewardCents || cents > MaxRewardCents {
			return contract.ErrRewardOutOfRange
		}
		amount := money.FromDecimal(decimal.NewFromInt(cents).Shift(-2))
		credited, err := tx.Credit(contract.CreditInput{
			UserID:    userID,
			Amount:    amount,
			Currency:  constants.SiteCurrencyDefault,
			Date:      date,
			Reference: fmt.Sprintf("daily-checkin:%d:%s", userID, date),
			Remark:    fmt.Sprintf("每日签到 %s 奖励进入购物余额，不抵扣未追回推广佣金", date),
		})
		if err != nil {
			return err
		}
		if credited.AlreadyRecorded {
			amount = credited.Amount
		}
		created := &checkindomain.CheckIn{
			UserID:      userID,
			CheckinDate: date,
			Amount:      amount,
			Currency:    constants.SiteCurrencyDefault,
			WalletTxnID: credited.TransactionID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.InsertCheckIn(created); err != nil {
			return err
		}
		result = claimedResult(created, credited.Balance, credited.AlreadyRecorded, date)
		return nil
	})
	if err == nil {
		return result, nil
	}
	if errors.Is(err, contract.ErrAccountUnavailable) || errors.Is(err, contract.ErrRewardOutOfRange) {
		return Result{}, err
	}
	// 钱包流水唯一约束会被包成通用创建错误，并发失败的一方改读已提交的同一笔奖励。
	if isUniqueViolation(err) || errors.Is(err, walletcontract.ErrTransactionCreateFailed) {
		if recovered, recErr := s.reloadClaim(userID, date); recErr == nil {
			return recovered, nil
		}
	}
	return Result{}, err
}

func (s *Service) requireActive(userID uint) (*struct{}, error) {
	if s.users == nil {
		return nil, contract.ErrFailed
	}
	user, err := s.users.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !checkindomain.AccountCanCheckIn(user.Status, user.DeletedAt != nil) {
		return nil, contract.ErrAccountUnavailable
	}
	return nil, nil
}

func (s *Service) balance(userID uint) (money.Amount, error) {
	if s.balances == nil {
		return money.FromDecimal(decimal.Zero), nil
	}
	return s.balances.Balance(userID)
}

func (s *Service) reloadClaim(userID uint, date string) (Result, error) {
	if s.checkIns == nil {
		return Result{}, contract.ErrFailed
	}
	row, err := s.checkIns.Find(userID, date)
	if err != nil {
		return Result{}, err
	}
	if row == nil {
		return Result{}, contract.ErrFailed
	}
	balance, err := s.balance(userID)
	if err != nil {
		return Result{}, err
	}
	return claimedResult(row, balance, true, date), nil
}

func idleResult(balance money.Amount, date string) Result {
	return Result{
		Balance:                      balance,
		Currency:                     constants.SiteCurrencyDefault,
		CheckinDate:                  date,
		NextCheckinDate:              NextBeijingDate(date),
		DebtOffset:                   money.FromDecimal(decimal.Zero),
		OffsetsUnrecoveredCommission: false,
	}
}

func claimedResult(row *checkindomain.CheckIn, balance money.Amount, already bool, date string) Result {
	reward := money.FromDecimal(decimal.Zero)
	if row != nil {
		reward = row.Amount
	}
	return Result{
		CheckedIn:                    true,
		AlreadyClaimed:               already,
		Reward:                       reward,
		Balance:                      balance,
		Currency:                     constants.SiteCurrencyDefault,
		CheckinDate:                  date,
		NextCheckinDate:              NextBeijingDate(date),
		DebtOffset:                   money.FromDecimal(decimal.Zero),
		OffsetsUnrecoveredCommission: false,
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
