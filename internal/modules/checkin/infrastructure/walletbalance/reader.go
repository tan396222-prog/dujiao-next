package walletbalance

import (
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// Reader 从钱包账户读取购物余额，不写入。
type Reader struct {
	Wallets walletcontract.Repository
}

func New(wallets walletcontract.Repository) Reader {
	return Reader{Wallets: wallets}
}

func (r Reader) Balance(userID uint) (money.Amount, error) {
	if r.Wallets == nil || userID == 0 {
		return money.FromDecimal(decimal.Zero), nil
	}
	account, err := r.Wallets.GetAccountByUserID(userID)
	if err != nil || account == nil {
		return money.FromDecimal(decimal.Zero), err
	}
	return account.Balance, nil
}
