package application

import (
	"time"

	affiliatecontract "github.com/dujiao-next/internal/modules/affiliate/contract"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
)

const (
	affiliateCodeLength        = 8
	affiliateSplitTypePrefix   = "sp"
	affiliateAttributionWindow = 30 * 24 * time.Hour
	affiliateClickDedupeWindow = 10 * time.Minute
)

// Service 推广返利业务服务
type Service struct {
	repo        affiliatecontract.Store
	userRepo    usercontract.Store
	orderRepo   affiliatecontract.OrderReader
	productRepo affiliatecontract.ProductReader
	settings    affiliatecontract.SettingsReader
	wallet      walletcontract.UseCase
	walletTx    walletcontract.UnitOfWork
}

// NewService 创建推广返利服务
func NewService(
	repo affiliatecontract.Store,
	userRepo usercontract.Store,
	orderRepo affiliatecontract.OrderReader,
	productRepo affiliatecontract.ProductReader,
	settings affiliatecontract.SettingsReader,
) *Service {
	return &Service{
		repo:        repo,
		userRepo:    userRepo,
		orderRepo:   orderRepo,
		productRepo: productRepo,
		settings:    settings,
	}
}

// SetWallet 把确认后的佣金转入可购物余额。未设置时佣金仍留在原提现账户。
func (s *Service) SetWallet(use walletcontract.UseCase, tx walletcontract.UnitOfWork) {
	if s == nil {
		return
	}
	s.wallet = use
	s.walletTx = tx
}
