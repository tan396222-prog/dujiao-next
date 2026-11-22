package integrationtest

import (
	"strings"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	affiliateapp "github.com/dujiao-next/internal/modules/affiliate/application"
	affiliatedomain "github.com/dujiao-next/internal/modules/affiliate/domain"
	affiliategormstore "github.com/dujiao-next/internal/modules/affiliate/infrastructure/gormstore"
	userstore "github.com/dujiao-next/internal/modules/identity/user/infrastructure/gormstore"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	walletgormstore "github.com/dujiao-next/internal/modules/wallet/infrastructure/gormstore"
	"github.com/dujiao-next/internal/testkit/memorysettings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type enabledProducts map[uint]bool

func (m enabledProducts) ListByIDs(ids []uint) ([]productdomain.Product, error) {
	rows := make([]productdomain.Product, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, productdomain.Product{ID: id, IsAffiliateEnabled: m[id]})
	}
	return rows, nil
}

func TestCommissionUsesDiscountedActualPayment(t *testing.T) {
	cases := []struct {
		name       string
		order      *orderdomain.Order
		enabled    enabledProducts
		rate       float64
		wantBase   string
		wantAmount string
		wantNone   bool
		self       bool
	}{
		{
			name: "mixed wallet and online is actual payment once",
			order: paidOrder(100, 30, 70, []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
			}),
			enabled: enabledProducts{1: true}, rate: 20, wantBase: "100", wantAmount: "20",
		},
		{
			name: "child prefilled online amount is not added again",
			order: func() *orderdomain.Order {
				order := paidOrder(100, 30, 0, nil)
				order.Children = []orderdomain.Order{{
					OnlinePaidAmount: money.FromDecimal(decimal.NewFromInt(100)),
					Items:            []orderdomain.OrderItem{{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))}},
				}}
				return order
			}(),
			enabled: enabledProducts{1: true}, rate: 20, wantBase: "30", wantAmount: "6",
		},
		{
			name: "wallet plus online cannot exceed discounted goods",
			order: paidOrder(100, 100, 100, []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
			}),
			enabled: enabledProducts{1: true}, rate: 20, wantBase: "100", wantAmount: "20",
		},
		{
			name: "only affiliate products and only after coupon",
			order: paidOrder(100, 0, 100, []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100)), CouponDiscount: money.FromDecimal(decimal.NewFromInt(10))},
				{ProductID: 2, TotalPrice: money.FromDecimal(decimal.NewFromInt(10))},
			}),
			enabled: enabledProducts{1: true, 2: false}, rate: 20, wantBase: "90", wantAmount: "18",
		},
		{
			name: "three percent rounds half away from zero to cents",
			order: paidOrderAmount("33.33", "0", "33.33", []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.RequireFromString("33.33"))},
			}),
			enabled: enabledProducts{1: true}, rate: 3, wantBase: "33.33", wantAmount: "1",
		},
		{
			name: "half cent rounds away from zero",
			order: paidOrderAmount("0.50", "0.50", "0", []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.RequireFromString("0.50"))},
			}),
			enabled: enabledProducts{1: true}, rate: 3, wantBase: "0.50", wantAmount: "0.02",
		},
		{
			name: "unpaid pending creates nothing",
			order: func() *orderdomain.Order {
				order := paidOrder(100, 0, 100, []orderdomain.OrderItem{{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))}})
				order.Status = constants.OrderStatusPendingPayment
				return order
			}(),
			enabled: enabledProducts{1: true}, rate: 20, wantNone: true,
		},
		{
			name: "canceled creates nothing",
			order: func() *orderdomain.Order {
				order := paidOrder(100, 0, 100, []orderdomain.OrderItem{{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))}})
				order.Status = constants.OrderStatusCanceled
				return order
			}(),
			enabled: enabledProducts{1: true}, rate: 20, wantNone: true,
		},
		{
			name: "zero actual payment creates nothing",
			order: paidOrder(100, 0, 0, []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
			}),
			enabled: enabledProducts{1: true}, rate: 20, wantNone: true,
		},
		{
			name: "self invite creates nothing",
			order: paidOrder(100, 0, 100, []orderdomain.OrderItem{
				{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
			}),
			enabled: enabledProducts{1: true}, rate: 20, wantNone: true, self: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _, promoterID, profileID := newCommissionService(t, tc.order, tc.enabled, tc.rate, true)
			if tc.self {
				tc.order.UserID = promoterID
			}
			if err := svc.HandleOrderPaid(tc.order.ID); err != nil {
				t.Fatalf("HandleOrderPaid: %v", err)
			}
			got, err := store.GetCommissionByOrderAndProfile(tc.order.ID, profileID, constants.AffiliateCommissionTypeOrder)
			if err != nil {
				t.Fatalf("load commission: %v", err)
			}
			if tc.wantNone {
				if got != nil {
					t.Fatalf("expected no commission, got %s", got.CommissionAmount.Decimal)
				}
				return
			}
			if got == nil {
				t.Fatal("expected commission")
			}
			if !got.BaseAmount.Decimal.Equal(decimal.RequireFromString(tc.wantBase)) || !got.CommissionAmount.Decimal.Equal(decimal.RequireFromString(tc.wantAmount)) {
				t.Fatalf("base %s amount %s, want %s / %s", got.BaseAmount.Decimal, got.CommissionAmount.Decimal, tc.wantBase, tc.wantAmount)
			}
			if got.Status != constants.AffiliateCommissionStatusWalletCredited {
				t.Fatalf("confirmed commission must enter the shoppable wallet, status %s", got.Status)
			}
		})
	}
}

func TestCommissionWalletCreditIsIdempotentAndRefundClawsBack(t *testing.T) {
	order := paidOrder(100, 30, 70, []orderdomain.OrderItem{
		{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
	})
	svc, store, db, promoterID, profileID := newCommissionService(t, order, enabledProducts{1: true}, 20, true)
	if err := svc.HandleOrderPaid(order.ID); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if err := svc.HandleOrderPaid(order.ID); err != nil {
		t.Fatalf("duplicate callback: %v", err)
	}
	var credits int64
	if err := db.Model(&walletdomain.Transaction{}).
		Where("user_id = ? AND type = ?", promoterID, constants.WalletTxnTypeAffiliateCommission).
		Count(&credits).Error; err != nil {
		t.Fatalf("count credits: %v", err)
	}
	if credits != 1 {
		t.Fatalf("duplicate callback created %d wallet credits", credits)
	}
	walletStore := walletgormstore.New(db)
	account, err := walletStore.GetAccountByUserID(promoterID)
	if err != nil || account == nil || !account.Balance.Decimal.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("balance after credit: %+v err %v", account, err)
	}

	if err := svc.HandleOrderRefunded(store, walletStore, order, decimal.NewFromInt(50), decimal.Zero, "partial"); err != nil {
		t.Fatalf("partial refund: %v", err)
	}
	account, _ = walletStore.GetAccountByUserID(promoterID)
	if !account.Balance.Decimal.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("half refund should claw back 10, balance %s", account.Balance.Decimal)
	}
	got, _ := store.GetCommissionByOrderAndProfile(order.ID, profileID, constants.AffiliateCommissionTypeOrder)
	if got.CommissionAmount.Decimal.String() != "10" || got.Status != constants.AffiliateCommissionStatusWalletCredited {
		t.Fatalf("partial commission = %+v", got)
	}
	if err := svc.HandleOrderRefunded(store, walletStore, order, decimal.NewFromInt(50), decimal.Zero, "partial"); err != nil {
		t.Fatalf("repeat partial refund: %v", err)
	}
	account, _ = walletStore.GetAccountByUserID(promoterID)
	if !account.Balance.Decimal.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("repeat partial refund clawed back again, balance %s", account.Balance.Decimal)
	}
	got, _ = store.GetCommissionByOrderAndProfile(order.ID, profileID, constants.AffiliateCommissionTypeOrder)
	if got.CommissionAmount.Decimal.String() != "10" {
		t.Fatalf("repeat partial refund changed commission to %s", got.CommissionAmount.Decimal)
	}

	account.Balance = money.FromDecimal(decimal.NewFromInt(1))
	if err := db.Save(account).Error; err != nil {
		t.Fatalf("lower balance: %v", err)
	}
	if err := svc.HandleOrderRefunded(store, walletStore, order, decimal.NewFromInt(50), decimal.NewFromInt(50), "rest"); err != nil {
		t.Fatalf("buyer refund must succeed when wallet is short: %v", err)
	}
	account, _ = walletStore.GetAccountByUserID(promoterID)
	if account.Balance.Decimal.IsNegative() || !account.Balance.Decimal.Equal(decimal.Zero) {
		t.Fatalf("balance must stop at zero, got %s", account.Balance.Decimal)
	}
	got, _ = store.GetCommissionByOrderAndProfile(order.ID, profileID, constants.AffiliateCommissionTypeOrder)
	if got.Status != constants.AffiliateCommissionStatusRejected {
		t.Fatalf("fully refunded commission status %s", got.Status)
	}
	if !strings.Contains(got.InvalidReason, "wallet_shortfall=") || !strings.Contains(got.InvalidReason, "reversal=") {
		t.Fatalf("shortfall was not audited: %s", got.InvalidReason)
	}
	if err := svc.HandleOrderRefunded(store, walletStore, order, decimal.NewFromInt(50), decimal.NewFromInt(50), "rest"); err != nil {
		t.Fatalf("repeat short refund: %v", err)
	}
	account, _ = walletStore.GetAccountByUserID(promoterID)
	if !account.Balance.Decimal.Equal(decimal.Zero) {
		t.Fatalf("repeat short refund changed balance to %s", account.Balance.Decimal)
	}
	profile, err := store.GetProfileByID(profileID)
	if err != nil || profile == nil || !profile.UnrecoveredCommission.Decimal.Equal(decimal.NewFromInt(9)) {
		t.Fatalf("unrecovered commission want 9, got %+v err %v", profile, err)
	}
}

func TestFutureCommissionOffsetsUnrecoveredDebtWithoutTouchingOtherWallets(t *testing.T) {
	first := paidOrder(100, 0, 100, []orderdomain.OrderItem{
		{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
	})
	second := paidOrder(100, 0, 100, []orderdomain.OrderItem{
		{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
	})
	third := paidOrder(100, 0, 100, []orderdomain.OrderItem{
		{ProductID: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(100))},
	})
	first.ID = 11
	second.ID = 12
	third.ID = 13
	orders := orderMapReader{first.ID: first, second.ID: second, third.ID: third}

	_, db, _ := setupAffiliateServiceTest(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&affiliatedomain.Commission{}, &walletdomain.Account{}, &walletdomain.Transaction{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	promoter := createAffiliateTestUser(t, db, "promoter-debt@example.com")
	buyer := createAffiliateTestUser(t, db, "buyer-debt@example.com")
	other := createAffiliateTestUser(t, db, "other-wallet@example.com")
	profile := createAffiliateTestProfile(t, db, promoter.ID, "PROMODEBT", constants.AffiliateProfileStatusActive)
	for _, order := range []*orderdomain.Order{first, second, third} {
		order.UserID = buyer.ID
		order.AffiliateProfileID = &profile.ID
	}
	settings := settingsapp.NewService(memorysettings.New())
	if _, err := settings.UpdateAffiliateSetting(settingsintegration.AffiliateSetting{Enabled: true, CommissionRate: 20}); err != nil {
		t.Fatalf("setting: %v", err)
	}
	store := affiliategormstore.New(db)
	walletStore := walletgormstore.New(db)
	svc := affiliateapp.NewService(store, userstore.New(db), orders, enabledProducts{1: true}, settings)
	svc.SetWallet(walletapp.NewService(walletapp.Options{Repository: walletStore, Transactions: walletStore}), walletStore)

	if err := svc.HandleOrderPaid(first.ID); err != nil {
		t.Fatalf("credit first commission: %v", err)
	}
	account, err := walletStore.GetAccountByUserID(promoter.ID)
	if err != nil || account == nil {
		t.Fatalf("load promoter wallet: %+v err %v", account, err)
	}
	account.Balance = money.FromDecimal(decimal.Zero)
	if err := db.Save(account).Error; err != nil {
		t.Fatalf("empty promoter wallet: %v", err)
	}
	if err := svc.HandleOrderRefunded(store, walletStore, first, decimal.NewFromInt(100), decimal.Zero, "full"); err != nil {
		t.Fatalf("buyer refund must succeed when the promoter wallet is empty: %v", err)
	}
	reloaded, err := store.GetProfileByID(profile.ID)
	if err != nil || reloaded == nil || !reloaded.UnrecoveredCommission.Decimal.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("debt want 20, got %+v err %v", reloaded, err)
	}
	if otherAccount, err := walletStore.GetAccountByUserID(other.ID); err != nil || (otherAccount != nil && !otherAccount.Balance.Decimal.IsZero()) {
		t.Fatalf("unrelated wallet changed: %+v err %v", otherAccount, err)
	}

	if err := svc.HandleOrderPaid(second.ID); err != nil {
		t.Fatalf("offset commission: %v", err)
	}
	if err := svc.HandleOrderPaid(second.ID); err != nil {
		t.Fatalf("repeat offset commission: %v", err)
	}
	reloaded, _ = store.GetProfileByID(profile.ID)
	if reloaded == nil || !reloaded.UnrecoveredCommission.Decimal.IsZero() {
		t.Fatalf("debt should be cleared by the next commission, got %+v", reloaded)
	}
	account, _ = walletStore.GetAccountByUserID(promoter.ID)
	if account == nil || !account.Balance.Decimal.IsZero() {
		t.Fatalf("offset commission must not pay the promoter again, balance %+v", account)
	}
	var credits int64
	if err := db.Model(&walletdomain.Transaction{}).
		Where("user_id = ? AND type = ?", promoter.ID, constants.WalletTxnTypeAffiliateCommission).
		Count(&credits).Error; err != nil {
		t.Fatalf("count credits: %v", err)
	}
	if credits != 1 {
		t.Fatalf("wallet credits = %d, want the original commission only", credits)
	}

	if err := svc.HandleOrderPaid(third.ID); err != nil {
		t.Fatalf("credit third commission: %v", err)
	}
	if err := svc.HandleOrderPaid(third.ID); err != nil {
		t.Fatalf("repeat third commission: %v", err)
	}
	account, _ = walletStore.GetAccountByUserID(promoter.ID)
	if account == nil || !account.Balance.Decimal.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("third commission should pay 20 after the debt is gone, got %+v", account)
	}
	if err := db.Model(&walletdomain.Transaction{}).
		Where("user_id = ? AND type = ?", promoter.ID, constants.WalletTxnTypeAffiliateCommission).
		Count(&credits).Error; err != nil {
		t.Fatalf("count credits after third: %v", err)
	}
	if credits != 2 {
		t.Fatalf("wallet credits = %d, want 2", credits)
	}
	if otherAccount, _ := walletStore.GetAccountByUserID(other.ID); otherAccount != nil && !otherAccount.Balance.Decimal.IsZero() {
		t.Fatalf("unrelated wallet changed after later commissions: %+v", otherAccount)
	}
}

type orderMapReader map[uint]*orderdomain.Order

func (m orderMapReader) GetByID(id uint) (*orderdomain.Order, error) {
	return m[id], nil
}

func paidOrder(total, walletPaid, onlinePaid int64, items []orderdomain.OrderItem) *orderdomain.Order {
	return paidOrderAmount(
		decimal.NewFromInt(total).String(),
		decimal.NewFromInt(walletPaid).String(),
		decimal.NewFromInt(onlinePaid).String(),
		items,
	)
}

func paidOrderAmount(total, walletPaid, onlinePaid string, items []orderdomain.OrderItem) *orderdomain.Order {
	return &orderdomain.Order{
		ID:               10,
		Status:           constants.OrderStatusPaid,
		Currency:         "CNY",
		TotalAmount:      money.FromDecimal(decimal.RequireFromString(total)),
		WalletPaidAmount: money.FromDecimal(decimal.RequireFromString(walletPaid)),
		OnlinePaidAmount: money.FromDecimal(decimal.RequireFromString(onlinePaid)),
		Items:            items,
	}
}

func newCommissionService(t *testing.T, order *orderdomain.Order, products enabledProducts, rate float64, withWallet bool) (*affiliateapp.Service, *affiliategormstore.Store, *gorm.DB, uint, uint) {
	t.Helper()
	_, db, _ := setupAffiliateServiceTest(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&affiliatedomain.Commission{}, &walletdomain.Account{}, &walletdomain.Transaction{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	promoter := createAffiliateTestUser(t, db, "promoter@example.com")
	buyer := createAffiliateTestUser(t, db, "buyer@example.com")
	profile := createAffiliateTestProfile(t, db, promoter.ID, "PROMO1", constants.AffiliateProfileStatusActive)
	order.UserID = buyer.ID
	order.AffiliateProfileID = &profile.ID
	settings := settingsapp.NewService(memorysettings.New())
	if _, err := settings.UpdateAffiliateSetting(settingsintegration.AffiliateSetting{Enabled: true, CommissionRate: rate}); err != nil {
		t.Fatalf("setting: %v", err)
	}
	store := affiliategormstore.New(db)
	svc := affiliateapp.NewService(store, userstore.New(db), staticOrderReader{order}, products, settings)
	if withWallet {
		walletStore := walletgormstore.New(db)
		svc.SetWallet(walletapp.NewService(walletapp.Options{Repository: walletStore, Transactions: walletStore}), walletStore)
	}
	return svc, store, db, promoter.ID, profile.ID
}
