package contract

import (
	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/shared/money"
)

// AccountDirectory 读取账号状态。领取时的加锁发生在事务内，不走这个端口。
type AccountDirectory interface {
	GetByID(uint) (*userdomain.User, error)
}

// Directory 按账号和北京时间日期查找已提交的签到。
type Directory interface {
	Find(userID uint, date string) (*checkindomain.CheckIn, error)
}

// BalanceReader 读取当前购物余额。领取事务内的余额走 ClaimTx，避免另开连接。
type BalanceReader interface {
	Balance(userID uint) (money.Amount, error)
}

// CreditInput 是签到入账请求。金额、日期和用户都由服务端决定。
type CreditInput struct {
	UserID    uint
	Amount    money.Amount
	Currency  string
	Date      string
	Reference string
	Remark    string
}

// CreditResult 是事务内入账结果。AlreadyRecorded 为真时不得再加一次钱。
type CreditResult struct {
	TransactionID   uint
	Amount          money.Amount
	Balance         money.Amount
	AlreadyRecorded bool
}

// ClaimTx 是已经打开的签到事务。
type ClaimTx interface {
	LockActiveUser(userID uint) error
	FindCheckIn(userID uint, date string) (*checkindomain.CheckIn, error)
	InsertCheckIn(row *checkindomain.CheckIn) error
	Credit(input CreditInput) (CreditResult, error)
	Balance(userID uint) (money.Amount, error)
}

// ClaimRunner 把账号锁、签到行和钱包入账放进同一个事务。
type ClaimRunner interface {
	WithinClaim(fn func(tx ClaimTx) error) error
}
