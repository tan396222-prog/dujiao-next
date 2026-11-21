package refund_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	couponapp "github.com/dujiao-next/internal/modules/coupon/application"
	couponcontract "github.com/dujiao-next/internal/modules/coupon/contract"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	coupongormstore "github.com/dujiao-next/internal/modules/coupon/infrastructure/gormstore"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	. "github.com/dujiao-next/internal/modules/order/application/refund"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestCouponSaleRefundRevokesWholeUnusedCouponsOnly(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-MULTI", decimal.NewFromInt(6), 2, false)
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.RequireFromString("4")),
	}); !errors.Is(err, ErrCouponSaleBenefitConsumed) {
		t.Fatalf("partial coupon amount error = %v", err)
	}
	assertCouponActive(t, db, codes[0], true)
	assertCouponActive(t, db, codes[1], true)
	assertRefunded(t, db, order.ID, "0.00")

	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err != nil {
		t.Fatalf("refund first coupon: %v", err)
	}
	assertCouponActive(t, db, codes[0], false)
	assertCouponActive(t, db, codes[1], true)
	assertRefunded(t, db, order.ID, "3.00")
	assertFulfillmentPayload(t, db, order.ID, codes[1])

	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err != nil {
		t.Fatalf("refund second coupon: %v", err)
	}
	assertCouponActive(t, db, codes[1], false)
	assertRefunded(t, db, order.ID, "6.00")
	assertFulfillmentPayload(t, db, order.ID, "")

	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); !errors.Is(err, walletcontract.ErrRefundExceeded) {
		t.Fatalf("repeat refund error = %v", err)
	}
}

func TestUsedCouponSaleCannotBeRefunded(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-USED", decimal.NewFromInt(6), 2, false)
	if err := db.Model(&coupondomain.Coupon{}).Where("code = ?", codes[0]).Update("used_count", 1).Error; err != nil {
		t.Fatalf("mark coupon used: %v", err)
	}
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(6)),
	}); !errors.Is(err, ErrCouponSaleBenefitConsumed) {
		t.Fatalf("full refund of a used coupon error = %v", err)
	}
	assertRefunded(t, db, order.ID, "0.00")
	assertCouponActive(t, db, codes[0], true)
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err != nil {
		t.Fatalf("unused sibling should still be refundable: %v", err)
	}
	assertCouponActive(t, db, codes[0], true)
	assertCouponActive(t, db, codes[1], false)
	assertRefunded(t, db, order.ID, "3.00")
}

func TestCouponSaleRefundLeavesOrdinaryAmountUntilCouponPrice(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-MIXED", decimal.NewFromInt(3), 1, true)
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(7)),
	}); err != nil {
		t.Fatalf("refund ordinary amount: %v", err)
	}
	assertCouponActive(t, db, codes[0], true)
	assertRefunded(t, db, order.ID, "7.00")
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err != nil {
		t.Fatalf("refund coupon amount: %v", err)
	}
	assertCouponActive(t, db, codes[0], false)
	assertRefunded(t, db, order.ID, "10.00")
}

func TestManualCouponSaleRefundRevokesUnusedCoupon(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-MANUAL", decimal.NewFromInt(3), 1, false)
	if _, _, err := svc.AdminManualRefund(AdminManualRefundInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err != nil {
		t.Fatalf("manual refund: %v", err)
	}
	assertCouponActive(t, db, codes[0], false)
	assertRefunded(t, db, order.ID, "3.00")
}

func TestCouponSaleRefundRollsBackWhenWalletWriteFails(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-ROLLBACK", decimal.NewFromInt(3), 1, false)
	fail := true
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_wallet_once", func(tx *gorm.DB) {
		if !fail {
			return
		}
		table := ""
		if tx.Statement != nil && tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		if table == "wallet_transactions" {
			fail = false
			_ = tx.AddError(errors.New("forced wallet failure"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
		OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
	}); err == nil {
		t.Fatal("refund should roll back when the wallet write fails")
	}
	assertCouponActive(t, db, codes[0], true)
	assertRefunded(t, db, order.ID, "0.00")
}

func TestCouponSaleRefundAndRedemptionCannotBothSucceed(t *testing.T) {
	svc, db := setupCouponSaleRefundTest(t)
	order, codes := createCouponSaleOrder(t, db, "SALE-RACE", decimal.NewFromInt(3), 1, false)
	store := ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{
			OrderID: order.ID, Amount: money.FromDecimal(decimal.NewFromInt(3)),
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		errs <- redeemSaleCoupon(store, codes[0])
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrCouponSaleBenefitConsumed) && !errors.Is(err, couponcontract.ErrInactive) {
			t.Fatalf("race error = %v", err)
		}
	}
	var coupon coupondomain.Coupon
	if err := db.Where("code = ?", codes[0]).First(&coupon).Error; err != nil {
		t.Fatalf("reload coupon: %v", err)
	}
	var stored orderdomain.Order
	if err := db.First(&stored, order.ID).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if stored.RefundedAmount.Decimal.GreaterThan(decimal.Zero) && coupon.IsActive && coupon.UsedCount > 0 {
		t.Fatalf("refund and redemption both stuck: refunded %s used %d active %v", stored.RefundedAmount.Decimal, coupon.UsedCount, coupon.IsActive)
	}
}

func setupCouponSaleRefundTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	svc, db := setupOrderRefundWalletTest(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&coupondomain.Coupon{}, &coupondomain.CouponUsage{}); err != nil {
		t.Fatalf("migrate coupons: %v", err)
	}
	return svc, db
}

func createCouponSaleOrder(t *testing.T, db *gorm.DB, orderNo string, couponTotal decimal.Decimal, quantity int, withGoods bool) (*orderdomain.Order, []string) {
	t.Helper()
	userID := uint(time.Now().UnixNano()%100000) + 2000
	createTestUser(t, db, userID)
	total := couponTotal
	if withGoods {
		total = couponTotal.Add(decimal.NewFromInt(7))
	}
	order := createTestOrder(t, db, userID, orderNo, total)
	now := time.Now()
	if err := db.Model(order).Updates(map[string]interface{}{
		"status": constants.OrderStatusPaid, "paid_at": now,
	}).Error; err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	item := &orderdomain.OrderItem{
		OrderID: order.ID, ProductID: 44, SKUID: 1, TitleJSON: jsonmap.JSON{"zh-CN": "售券"},
		UnitPrice: money.FromDecimal(couponTotal), Quantity: quantity, TotalPrice: money.FromDecimal(couponTotal),
		CreatedAt: now, UpdatedAt: now,
	}
	item.MarkCouponSale()
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create coupon item: %v", err)
	}
	if withGoods {
		goods := &orderdomain.OrderItem{
			OrderID: order.ID, ProductID: 45, SKUID: 2, TitleJSON: jsonmap.JSON{"zh-CN": "普通商品"},
			UnitPrice: money.FromDecimal(decimal.NewFromInt(7)), Quantity: 1, TotalPrice: money.FromDecimal(decimal.NewFromInt(7)),
			FulfillmentType: constants.FulfillmentTypeManual, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(goods).Error; err != nil {
			t.Fatalf("create goods item: %v", err)
		}
	}
	repo := coupongormstore.New(db)
	codes := make([]string, 0, quantity)
	for sequence := 1; sequence <= quantity; sequence++ {
		code, err := couponapp.NewService(repo, nil).EnsureSaleCoupon(repo, item.ID, sequence)
		if err != nil {
			t.Fatalf("issue coupon: %v", err)
		}
		codes = append(codes, code)
	}
	if err := db.Create(&fulfillmentdomain.Fulfillment{
		OrderID: order.ID, Type: constants.FulfillmentTypeCoupon, Status: constants.FulfillmentStatusDelivered,
		Payload: joinLines(codes), CreatedAt: now, UpdatedAt: now, DeliveredAt: &now,
	}).Error; err != nil {
		t.Fatalf("create fulfillment: %v", err)
	}
	return order, codes
}

func redeemSaleCoupon(store ordercontract.Store, code string) error {
	return store.WithinTransaction(func(tx ordercontract.Transaction) error {
		existing, err := tx.Coupons().GetByCode(code)
		if err != nil {
			return err
		}
		if existing == nil || !existing.IsActive {
			return couponcontract.ErrInactive
		}
		locked, err := tx.Coupons().GetByIDForUpdate(existing.ID)
		if err != nil {
			return err
		}
		if locked == nil || !locked.IsActive || locked.UsedCount > 0 {
			return couponcontract.ErrInactive
		}
		return tx.Coupons().IncrementUsedCount(locked.ID, 1)
	})
}

func assertCouponActive(t *testing.T, db *gorm.DB, code string, active bool) {
	t.Helper()
	var coupon coupondomain.Coupon
	if err := db.Where("code = ?", code).First(&coupon).Error; err != nil {
		t.Fatalf("load coupon %s: %v", code, err)
	}
	if coupon.IsActive != active {
		t.Fatalf("coupon %s active = %v, want %v", code, coupon.IsActive, active)
	}
}

func assertRefunded(t *testing.T, db *gorm.DB, orderID uint, amount string) {
	t.Helper()
	var order orderdomain.Order
	if err := db.First(&order, orderID).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if order.RefundedAmount.Decimal.StringFixed(2) != amount {
		t.Fatalf("refunded = %s, want %s", order.RefundedAmount.Decimal.StringFixed(2), amount)
	}
}

func assertFulfillmentPayload(t *testing.T, db *gorm.DB, orderID uint, payload string) {
	t.Helper()
	var record fulfillmentdomain.Fulfillment
	if err := db.Where("order_id = ?", orderID).First(&record).Error; err != nil {
		t.Fatalf("load fulfillment: %v", err)
	}
	if record.Payload != payload {
		t.Fatalf("payload = %q, want %q", record.Payload, payload)
	}
}

func joinLines(lines []string) string {
	out := ""
	for index, line := range lines {
		if index > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
