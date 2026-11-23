package gormuow

import (
	"errors"
	"strings"

	"github.com/dujiao-next/internal/constants"
	checkincontract "github.com/dujiao-next/internal/modules/checkin/contract"
	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"
	checkingormstore "github.com/dujiao-next/internal/modules/checkin/infrastructure/gormstore"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	walletgormstore "github.com/dujiao-next/internal/modules/wallet/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Runner 把签到行和购物余额入账放进同一个数据库事务。
type Runner struct {
	checkIns *checkingormstore.Store
	wallet   *walletapp.Service
}

func New(checkIns *checkingormstore.Store, wallet *walletapp.Service) *Runner {
	return &Runner{checkIns: checkIns, wallet: wallet}
}

func (r *Runner) WithinClaim(fn func(tx checkincontract.ClaimTx) error) error {
	if r == nil || r.checkIns == nil || r.wallet == nil || fn == nil {
		return checkincontract.ErrFailed
	}
	return r.checkIns.Transaction(func(db *gorm.DB) error {
		return fn(&claimTx{
			db:       db,
			checkIns: r.checkIns.WithTx(db),
			wallets:  walletgormstore.UseTransaction(db).Wallets(),
			wallet:   r.wallet,
		})
	})
}

type claimTx struct {
	db       *gorm.DB
	checkIns *checkingormstore.Store
	wallets  walletcontract.Repository
	wallet   *walletapp.Service
}

func (tx *claimTx) LockActiveUser(userID uint) error {
	if tx == nil || tx.db == nil || userID == 0 {
		return checkincontract.ErrAccountUnavailable
	}
	var user userdomain.User
	err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND deleted_at IS NULL", userID).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return checkincontract.ErrAccountUnavailable
	}
	if err != nil {
		return err
	}
	if !checkindomain.AccountCanCheckIn(user.Status, false) {
		return checkincontract.ErrAccountUnavailable
	}
	return nil
}

func (tx *claimTx) FindCheckIn(userID uint, date string) (*checkindomain.CheckIn, error) {
	return tx.checkIns.Find(userID, date)
}

func (tx *claimTx) InsertCheckIn(row *checkindomain.CheckIn) error {
	return tx.checkIns.Insert(row)
}

func (tx *claimTx) ExistingCredit(userID uint, reference string) (checkincontract.CreditResult, bool, error) {
	if tx == nil || tx.wallets == nil || userID == 0 {
		return checkincontract.CreditResult{}, false, checkincontract.ErrFailed
	}
	existing, err := tx.wallets.GetTransactionByReference(reference)
	if err != nil || existing == nil {
		return checkincontract.CreditResult{}, false, err
	}
	if existing.UserID != userID || existing.Type != constants.WalletTxnTypeDailyCheckIn || existing.Direction != constants.WalletTxnDirectionIn {
		return checkincontract.CreditResult{}, false, checkincontract.ErrFailed
	}
	balance, err := tx.Balance(userID)
	if err != nil {
		return checkincontract.CreditResult{}, false, err
	}
	return checkincontract.CreditResult{
		TransactionID:   existing.ID,
		Amount:          existing.Amount,
		Balance:         balance,
		AlreadyRecorded: true,
	}, true, nil
}

func (tx *claimTx) HasPriorReward(userID uint) (bool, error) {
	if tx == nil || tx.db == nil || userID == 0 {
		return false, checkincontract.ErrFailed
	}
	var checkInID uint
	err := tx.db.Unscoped().Model(&checkindomain.CheckIn{}).
		Select("id").
		Where("user_id = ?", userID).
		Limit(1).
		Scan(&checkInID).Error
	if err != nil {
		return false, err
	}
	if checkInID != 0 {
		return true, nil
	}
	var txnID uint
	err = tx.db.Unscoped().Model(&walletdomain.Transaction{}).
		Select("id").
		Where("user_id = ? AND type = ?", userID, constants.WalletTxnTypeDailyCheckIn).
		Limit(1).
		Scan(&txnID).Error
	if err != nil {
		return false, err
	}
	return txnID != 0, nil
}

func (tx *claimTx) Balance(userID uint) (money.Amount, error) {
	if tx == nil || tx.wallets == nil {
		return money.FromDecimal(decimal.Zero), checkincontract.ErrFailed
	}
	account, err := tx.wallets.GetAccountByUserID(userID)
	if err != nil {
		return money.FromDecimal(decimal.Zero), err
	}
	if account == nil {
		return money.FromDecimal(decimal.Zero), nil
	}
	return account.Balance, nil
}

func (tx *claimTx) Credit(input checkincontract.CreditInput) (checkincontract.CreditResult, error) {
	if tx == nil || tx.wallet == nil || tx.wallets == nil {
		return checkincontract.CreditResult{}, checkincontract.ErrFailed
	}
	reference := strings.TrimSpace(input.Reference)
	if reference == "" || input.UserID == 0 {
		return checkincontract.CreditResult{}, checkincontract.ErrFailed
	}
	existing, err := tx.wallets.GetTransactionByReference(reference)
	if err != nil {
		return checkincontract.CreditResult{}, err
	}
	if existing != nil {
		if existing.UserID != input.UserID || existing.Type != constants.WalletTxnTypeDailyCheckIn || existing.Direction != constants.WalletTxnDirectionIn {
			return checkincontract.CreditResult{}, checkincontract.ErrFailed
		}
		balance, err := tx.Balance(input.UserID)
		if err != nil {
			return checkincontract.CreditResult{}, err
		}
		return checkincontract.CreditResult{
			TransactionID:   existing.ID,
			Amount:          existing.Amount,
			Balance:         balance,
			AlreadyRecorded: true,
		}, nil
	}
	account, txn, err := tx.wallet.CreditInTransaction(walletgormstore.UseTransaction(tx.db), walletcontract.CreditInput{
		UserID:    input.UserID,
		Amount:    input.Amount,
		Currency:  input.Currency,
		Type:      constants.WalletTxnTypeDailyCheckIn,
		Reference: reference,
		Remark:    input.Remark,
	})
	if err != nil {
		return checkincontract.CreditResult{}, err
	}
	if txn == nil || account == nil {
		return checkincontract.CreditResult{}, checkincontract.ErrFailed
	}
	return checkincontract.CreditResult{
		TransactionID: txn.ID,
		Amount:        txn.Amount,
		Balance:       account.Balance,
	}, nil
}

var _ checkincontract.ClaimRunner = (*Runner)(nil)
var _ checkincontract.ClaimTx = (*claimTx)(nil)
