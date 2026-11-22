package integrationtest

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	affiliatedomain "github.com/dujiao-next/internal/modules/affiliate/domain"
	checkinapp "github.com/dujiao-next/internal/modules/checkin/application"
	"github.com/dujiao-next/internal/modules/checkin/contract"
	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"
	checkingormstore "github.com/dujiao-next/internal/modules/checkin/infrastructure/gormstore"
	checkinwallet "github.com/dujiao-next/internal/modules/checkin/infrastructure/walletbalance"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	userstore "github.com/dujiao-next/internal/modules/identity/user/infrastructure/gormstore"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	walletgormstore "github.com/dujiao-next/internal/modules/wallet/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/money"
	dailycheckingormuow "github.com/dujiao-next/internal/workflows/dailycheckin/infrastructure/gormuow"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type fixedCents int64

func (f fixedCents) Cents() (int64, error) { return int64(f), nil }

type countingReward struct {
	cents int64
	calls *int
}

func (c countingReward) Cents() (int64, error) {
	*c.calls++
	return c.cents, nil
}

func openCheckInDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:daily_checkin_%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&userdomain.User{},
		&walletdomain.Account{},
		&walletdomain.Transaction{},
		&checkindomain.CheckIn{},
		&affiliatedomain.Profile{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func newCheckInService(db *gorm.DB, cents int64, now time.Time, rewards checkinapp.RewardSource) *checkinapp.Service {
	if rewards == nil {
		rewards = fixedCents(cents)
	}
	checks := checkingormstore.New(db)
	wallets := walletgormstore.New(db)
	return checkinapp.NewService(checkinapp.Options{
		Users:    userstore.New(db),
		CheckIns: checks,
		Balances: checkinwallet.New(wallets),
		Runner:   dailycheckingormuow.New(checks, walletapp.NewService(walletapp.Options{Repository: wallets, Transactions: wallets})),
		Rewards:  rewards,
		Now:      func() time.Time { return now },
	})
}

func seedUser(t *testing.T, db *gorm.DB, status string, deleted bool) userdomain.User {
	t.Helper()
	now := time.Now()
	user := userdomain.User{
		Email:        fmt.Sprintf("checkin-%d-%s@example.com", time.Now().UnixNano(), status),
		PasswordHash: "not-a-login",
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if deleted {
		user.DeletedAt = &now
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	return user
}

func assertBalance(t *testing.T, db *gorm.DB, userID uint, want string) {
	t.Helper()
	var account walletdomain.Account
	err := db.Where("user_id = ? AND deleted_at IS NULL", userID).First(&account).Error
	if want == "0.00" && errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance.String() != want {
		t.Fatalf("balance %s, want %s", account.Balance.String(), want)
	}
}

func countTxns(t *testing.T, db *gorm.DB, userID uint) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&walletdomain.Transaction{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestClaimBoundariesAreIdempotentAndDoNotOffsetDebt(t *testing.T) {
	db := openCheckInDB(t)
	now := time.Date(2026, 11, 22, 3, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	user := seedUser(t, db, constants.UserStatusActive, false)
	profile := affiliatedomain.Profile{
		UserID:                user.ID,
		AffiliateCode:         "CHKDEBT1",
		Status:                constants.AffiliateProfileStatusActive,
		UnrecoveredCommission: money.FromDecimal(decimal.RequireFromString("9.00")),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	service := newCheckInService(db, 80, now, countingReward{cents: 80, calls: &calls})

	first, err := service.Claim(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reward.String() != "0.80" || first.Balance.String() != "0.80" || first.AlreadyClaimed || first.OffsetsUnrecoveredCommission || first.DebtOffset.String() != "0.00" {
		t.Fatalf("first claim: %+v", first)
	}
	second, err := service.Claim(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Reward.String() != "0.80" || !second.AlreadyClaimed || second.Balance.String() != "0.80" || calls != 1 {
		t.Fatalf("replay reward=%s claimed=%v balance=%s draws=%d", second.Reward, second.AlreadyClaimed, second.Balance, calls)
	}
	assertBalance(t, db, user.ID, "0.80")
	if countTxns(t, db, user.ID) != 1 {
		t.Fatal("replay created another wallet transaction")
	}
	var saved affiliatedomain.Profile
	if err := db.First(&saved, profile.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.UnrecoveredCommission.String() != "9.00" {
		t.Fatalf("check-in changed unrecovered commission to %s", saved.UnrecoveredCommission)
	}
	var txn walletdomain.Transaction
	if err := db.Where("user_id = ?", user.ID).First(&txn).Error; err != nil {
		t.Fatal(err)
	}
	if txn.Type != constants.WalletTxnTypeDailyCheckIn || txn.Reference != fmt.Sprintf("daily-checkin:%d:2026-11-22", user.ID) {
		t.Fatalf("ledger type=%s reference=%s", txn.Type, txn.Reference)
	}
	if !strings.Contains(txn.Remark, "不抵扣未追回推广佣金") {
		t.Fatalf("remark %s", txn.Remark)
	}

	low := newCheckInService(db, 10, now, nil)
	other := seedUser(t, db, constants.UserStatusActive, false)
	lowResult, err := low.Claim(other.ID)
	if err != nil || lowResult.Reward.String() != "0.10" {
		t.Fatalf("low boundary: %+v %v", lowResult, err)
	}
}

func TestClaimUsesShanghaiMidnightAndKeepsOneRowUnderConcurrency(t *testing.T) {
	db := openCheckInDB(t)
	user := seedUser(t, db, constants.UserStatusActive, false)
	earlyNow := time.Date(2026, 11, 22, 15, 59, 30, 0, time.UTC)
	lateNow := time.Date(2026, 11, 22, 16, 0, 30, 0, time.UTC)
	early := newCheckInService(db, 10, earlyNow, nil)
	late := newCheckInService(db, 80, lateNow, nil)
	first, err := early.Claim(user.ID)
	if err != nil || first.CheckinDate != "2026-11-22" || first.Reward.String() != "0.10" {
		t.Fatalf("early: %+v %v", first, err)
	}
	second, err := late.Claim(user.ID)
	if err != nil || second.CheckinDate != "2026-11-23" || second.Reward.String() != "0.80" || second.Balance.String() != "0.90" {
		t.Fatalf("late: %+v %v", second, err)
	}
	if countTxns(t, db, user.ID) != 2 {
		t.Fatal("midnight should create two ledger rows")
	}

	racer := seedUser(t, db, constants.UserStatusActive, false)
	service := newCheckInService(db, 55, earlyNow, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	results := make(chan checkinapp.Result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.Claim(racer.ID)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}
	seen := 0
	for result := range results {
		seen++
		if result.Reward.String() != "0.55" {
			t.Fatalf("concurrent reward %s", result.Reward)
		}
	}
	if seen != 2 || countTxns(t, db, racer.ID) != 1 {
		t.Fatalf("concurrent claims=%d txns=%d", seen, countTxns(t, db, racer.ID))
	}
	assertBalance(t, db, racer.ID, "0.55")
}

func TestClaimRejectsUnavailableAccountsAndForgedRewardRange(t *testing.T) {
	db := openCheckInDB(t)
	now := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	service := newCheckInService(db, 40, now, nil)
	for _, user := range []userdomain.User{
		seedUser(t, db, constants.UserStatusDisabled, false),
		seedUser(t, db, "banned", false),
		seedUser(t, db, constants.UserStatusActive, true),
	} {
		if _, err := service.Claim(user.ID); !errors.Is(err, contract.ErrAccountUnavailable) {
			t.Fatalf("user %d status %s deleted %v: %v", user.ID, user.Status, user.DeletedAt != nil, err)
		}
		if countTxns(t, db, user.ID) != 0 {
			t.Fatalf("unavailable user %d was credited", user.ID)
		}
	}
	active := seedUser(t, db, constants.UserStatusActive, false)
	out := newCheckInService(db, 9, now, nil)
	if _, err := out.Claim(active.ID); !errors.Is(err, contract.ErrRewardOutOfRange) {
		t.Fatal(err)
	}
	if countTxns(t, db, active.ID) != 0 {
		t.Fatal("out of range reward was credited")
	}
	owner := seedUser(t, db, constants.UserStatusActive, false)
	if _, err := service.Claim(owner.ID); err != nil {
		t.Fatal(err)
	}
	other := seedUser(t, db, constants.UserStatusActive, false)
	status, err := service.Status(other.ID)
	if err != nil || status.CheckedIn || status.Balance.String() != "0.00" {
		t.Fatalf("cross user status: %+v %v", status, err)
	}
}

func TestClaimRestoresRowFromExistingLedgerAndRollsBackFailedCredit(t *testing.T) {
	db := openCheckInDB(t)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	user := seedUser(t, db, constants.UserStatusActive, false)
	calls := 0
	service := newCheckInService(db, 40, now, countingReward{cents: 40, calls: &calls})
	if _, err := service.Claim(user.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM daily_checkins WHERE user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	again, err := service.Claim(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Reward.String() != "0.40" || again.Balance.String() != "0.40" || countTxns(t, db, user.ID) != 1 {
		t.Fatalf("restored %+v txns=%d", again, countTxns(t, db, user.ID))
	}
	var rows int64
	if err := db.Model(&checkindomain.CheckIn{}).Where("user_id = ?", user.ID).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("check-in rows %d", rows)
	}

	rollbackUser := seedUser(t, db, constants.UserStatusActive, false)
	checks := checkingormstore.New(db)
	wallets := walletgormstore.New(db)
	runner := dailycheckingormuow.New(checks, walletapp.NewService(walletapp.Options{Repository: wallets, Transactions: wallets}))
	err = runner.WithinClaim(func(tx contract.ClaimTx) error {
		if err := tx.LockActiveUser(rollbackUser.ID); err != nil {
			return err
		}
		if _, err := tx.Credit(contract.CreditInput{
			UserID:    rollbackUser.ID,
			Amount:    money.FromDecimal(decimal.RequireFromString("0.40")),
			Currency:  "CNY",
			Date:      "2026-11-22",
			Reference: fmt.Sprintf("daily-checkin:%d:2026-11-22", rollbackUser.ID),
			Remark:    "应回滚",
		}); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected rollback error")
	}
	assertBalance(t, db, rollbackUser.ID, "0.00")
	if countTxns(t, db, rollbackUser.ID) != 0 {
		t.Fatal("failed transaction left a wallet row")
	}
}

func TestCheckInBalanceCanBeSpentAndCannotBeWithdrawnHere(t *testing.T) {
	db := openCheckInDB(t)
	now := time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC)
	user := seedUser(t, db, constants.UserStatusActive, false)
	service := newCheckInService(db, 50, now, nil)
	if _, err := service.Claim(user.ID); err != nil {
		t.Fatal(err)
	}
	wallets := walletgormstore.New(db)
	walletService := walletapp.NewService(walletapp.Options{Repository: wallets, Transactions: wallets})
	var paid money.Amount
	err := wallets.WithinTransaction(func(tx walletcontract.Transaction) error {
		amount, err := walletService.ApplyOrderBalance(tx, walletcontract.OrderBalanceInput{
			OrderID:     9,
			UserID:      user.ID,
			TotalAmount: money.FromDecimal(decimal.RequireFromString("0.30")),
			Currency:    "CNY",
			UseBalance:  true,
		})
		paid = amount
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if paid.String() != "0.30" {
		t.Fatalf("paid %s", paid)
	}
	assertBalance(t, db, user.ID, "0.20")
	var kinds []string
	if err := db.Model(&walletdomain.Transaction{}).Where("user_id = ?", user.ID).Order("id asc").Pluck("type", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != constants.WalletTxnTypeDailyCheckIn || kinds[1] != constants.WalletTxnTypeOrderPay {
		t.Fatalf("ledger types %#v", kinds)
	}
}
