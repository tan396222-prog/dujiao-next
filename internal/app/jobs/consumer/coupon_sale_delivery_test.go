package consumer

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/constants"
	couponapp "github.com/dujiao-next/internal/modules/coupon/application"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	coupongormstore "github.com/dujiao-next/internal/modules/coupon/infrastructure/gormstore"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	orderpresenter "github.com/dujiao-next/internal/modules/order/transport/presenter"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestDeliverCouponSaleSkipsRevokedCoupons(t *testing.T) {
	db := openCouponDeliveryDB(t)
	now := time.Now()
	three := money.FromDecimal(decimal.NewFromInt(3))
	order := &orderdomain.Order{
		OrderNo: "COUPON-DELIVER", UserID: 1, Status: constants.OrderStatusPartiallyRefunded, Currency: "CNY",
		OriginalAmount: three, TotalAmount: three, OnlinePaidAmount: three, RefundedAmount: three,
		PaidAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	item := &orderdomain.OrderItem{
		OrderID: order.ID, ProductID: 44, TitleJSON: jsonmap.JSON{"zh-CN": "售券"},
		UnitPrice: three, Quantity: 1, TotalPrice: three, CreatedAt: now, UpdatedAt: now,
	}
	item.MarkCouponSale()
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	repo := coupongormstore.New(db)
	code, err := couponapp.NewService(repo, nil).EnsureSaleCoupon(repo, item.ID, 1)
	if err != nil {
		t.Fatalf("issue coupon: %v", err)
	}
	if err := db.Model(&coupondomain.Coupon{}).Where("code = ?", code).Update("is_active", false).Error; err != nil {
		t.Fatalf("revoke coupon: %v", err)
	}
	worker := &Consumer{Container: &container.Container{
		OrderStore: ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
		CouponRepo: repo,
	}}
	if err := worker.deliverCouponSale(order); err != nil {
		t.Fatalf("delivery should ignore a revoked coupon: %v", err)
	}
	var count int64
	if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatalf("count fulfillment: %v", err)
	}
	if count != 0 {
		t.Fatalf("revoked coupon was delivered %d times", count)
	}
	var stored orderdomain.Order
	if err := db.First(&stored, order.ID).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if stored.Status != constants.OrderStatusPartiallyRefunded {
		t.Fatalf("status = %s", stored.Status)
	}
}

func TestDeliverCouponSaleCompletesPaidQuantityOnce(t *testing.T) {
	db := openCouponDeliveryDB(t)
	repo := ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	parent, child := seedCouponSaleFamily(t, db, repo, "DJ-COUPON-PAID", 7, "", "", constants.OrderStatusPaid)
	worker := couponDeliveryWorker(db, repo)

	if err := worker.deliverCouponSale(child); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if err := worker.deliverCouponSale(child); err != nil {
		t.Fatalf("repeat deliver: %v", err)
	}
	codes := assertCouponSaleDelivered(t, db, child.ID, child.Items[0].ID, 2)
	var storedParent orderdomain.Order
	if err := db.First(&storedParent, parent.ID).Error; err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	if storedParent.Status != constants.OrderStatusCompleted {
		t.Fatalf("parent status = %s", storedParent.Status)
	}

	visible, err := repo.GetByOrderNoAndUser(parent.OrderNo, parent.UserID)
	if err != nil || visible == nil || len(visible.Children) != 1 || visible.Children[0].Fulfillment == nil {
		t.Fatalf("owner cannot read delivered coupons: order=%v err=%v", visible, err)
	}
	detail := orderpresenter.NewOrderDetail(visible)
	if detail.Children[0].Fulfillment == nil || detail.Children[0].Fulfillment.Status != constants.FulfillmentStatusDelivered {
		t.Fatalf("order detail missing delivered fulfillment: %+v", detail.Children)
	}
	for _, code := range codes {
		if !strings.Contains(detail.Children[0].Fulfillment.Payload, code) {
			t.Fatalf("owner payload missing %s: %s", code, detail.Children[0].Fulfillment.Payload)
		}
	}
	if detail.Children[0].Items[0].FulfillmentType != constants.FulfillmentTypeCoupon {
		t.Fatalf("item type = %s", detail.Children[0].Items[0].FulfillmentType)
	}
	other, err := repo.GetByOrderNoAndUser(parent.OrderNo, parent.UserID+1)
	if err != nil || other != nil {
		t.Fatalf("another user read the order: %+v err=%v", other, err)
	}

	_, pendingChild := seedCouponSaleFamily(t, db, repo, "DJ-COUPON-UNPAID", 7, "", "", constants.OrderStatusPendingPayment)
	if err := worker.deliverCouponSale(pendingChild); err != nil {
		t.Fatalf("unpaid deliver: %v", err)
	}
	var unpaidCount int64
	if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", pendingChild.ID).Count(&unpaidCount).Error; err != nil {
		t.Fatalf("count unpaid fulfillment: %v", err)
	}
	if unpaidCount != 0 {
		t.Fatalf("unpaid order was delivered")
	}
	var couponCount int64
	if err := db.Model(&coupondomain.Coupon{}).Count(&couponCount).Error; err != nil {
		t.Fatalf("count coupons: %v", err)
	}
	if couponCount != 2 {
		t.Fatalf("unpaid delivery changed coupon count to %d", couponCount)
	}
}

func TestDeliverCouponSaleGuestRequiresCredential(t *testing.T) {
	db := openCouponDeliveryDB(t)
	repo := ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	order := seedCouponSaleOrder(t, repo, "DJ-COUPON-GUEST", 0, "guest@example.com", "guest-pass", constants.OrderStatusPaid)
	worker := couponDeliveryWorker(db, repo)
	if err := worker.deliverCouponSale(order); err != nil {
		t.Fatalf("deliver guest: %v", err)
	}
	visible, err := repo.GetByOrderNoAndGuest(order.OrderNo, "guest@example.com", "guest-pass")
	if err != nil || visible == nil || visible.Fulfillment == nil || visible.Fulfillment.Payload == "" {
		t.Fatalf("guest with credential cannot read coupon: %+v err=%v", visible, err)
	}
	for _, attempt := range []struct{ email, password string }{
		{"guest@example.com", ""},
		{"guest@example.com", "wrong-pass"},
		{"other@example.com", "guest-pass"},
	} {
		got, err := repo.GetByOrderNoAndGuest(order.OrderNo, attempt.email, attempt.password)
		if err != nil || got != nil {
			t.Fatalf("credential %q/%q read the coupon: %+v err=%v", attempt.email, attempt.password, got, err)
		}
	}
}

func TestDeliverCouponSaleConcurrentCallbackKeepsOneSet(t *testing.T) {
	db := openCouponDeliveryDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	order := seedCouponSaleOrder(t, repo, "DJ-COUPON-RACE", 3, "", "", constants.OrderStatusPaid)
	worker := couponDeliveryWorker(db, repo)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- worker.deliverCouponSale(order)
		}()
	}
	wg.Wait()
	close(errs)
	assertCouponSaleDelivered(t, db, order.ID, order.Items[0].ID, 2)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent deliver: %v", err)
		}
	}
}

func couponDeliveryWorker(db *gorm.DB, repo *ordergormstore.Store) *Consumer {
	return &Consumer{Container: &container.Container{
		OrderStore: repo,
		CouponRepo: coupongormstore.New(db),
	}}
}

func seedCouponSaleFamily(t *testing.T, db *gorm.DB, repo *ordergormstore.Store, orderNo string, userID uint, email, password, status string) (*orderdomain.Order, *orderdomain.Order) {
	t.Helper()
	now := time.Now()
	six := money.FromDecimal(decimal.NewFromInt(6))
	parent := &orderdomain.Order{
		OrderNo: orderNo, UserID: userID, GuestEmail: email, GuestPassword: password,
		Status: status, Currency: "CNY", OriginalAmount: six, TotalAmount: six, OnlinePaidAmount: six,
		CreatedAt: now, UpdatedAt: now,
	}
	if status == constants.OrderStatusPaid {
		parent.PaidAt = &now
	}
	if err := repo.Create(parent, nil); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := seedCouponSaleOrder(t, repo, orderNo+"-1", userID, email, password, status)
	if err := db.Model(child).Update("parent_id", parent.ID).Error; err != nil {
		t.Fatalf("attach child: %v", err)
	}
	child.ParentID = &parent.ID
	return parent, child
}

func seedCouponSaleOrder(t *testing.T, repo *ordergormstore.Store, orderNo string, userID uint, email, password, status string) *orderdomain.Order {
	t.Helper()
	now := time.Now()
	six := money.FromDecimal(decimal.NewFromInt(6))
	three := money.FromDecimal(decimal.NewFromInt(3))
	order := &orderdomain.Order{
		OrderNo: orderNo, UserID: userID, GuestEmail: email, GuestPassword: password,
		Status: status, Currency: "CNY", OriginalAmount: six, TotalAmount: six, OnlinePaidAmount: six,
		CreatedAt: now, UpdatedAt: now,
	}
	if status == constants.OrderStatusPaid {
		order.PaidAt = &now
	}
	item := orderdomain.OrderItem{
		ProductID: 45, TitleJSON: jsonmap.JSON{"zh-CN": "九折优惠券"},
		UnitPrice: three, Quantity: 2, TotalPrice: six, FulfillmentType: constants.FulfillmentTypeManual,
		CreatedAt: now, UpdatedAt: now,
	}
	item.MarkCouponSale()
	items := []orderdomain.OrderItem{item}
	if err := repo.Create(order, items); err != nil {
		t.Fatalf("create order %s: %v", orderNo, err)
	}
	order.Items = items
	return order
}

func assertCouponSaleDelivered(t *testing.T, db *gorm.DB, orderID, itemID uint, quantity int) []string {
	t.Helper()
	var fulfillments []fulfillmentdomain.Fulfillment
	if err := db.Where("order_id = ?", orderID).Find(&fulfillments).Error; err != nil {
		t.Fatalf("load fulfillment: %v", err)
	}
	if len(fulfillments) != 1 || fulfillments[0].Status != constants.FulfillmentStatusDelivered || fulfillments[0].Type != constants.FulfillmentTypeCoupon {
		t.Fatalf("fulfillment = %+v", fulfillments)
	}
	var coupons []coupondomain.Coupon
	if err := db.Order("id").Find(&coupons).Error; err != nil {
		t.Fatalf("load coupons: %v", err)
	}
	if len(coupons) != quantity {
		t.Fatalf("coupon count = %d", len(coupons))
	}
	codes := make([]string, 0, quantity)
	for sequence := 1; sequence <= quantity; sequence++ {
		code := couponapp.SaleCouponCode(itemID, sequence)
		codes = append(codes, code)
		if !strings.Contains(fulfillments[0].Payload, code) {
			t.Fatalf("payload missing %s: %s", code, fulfillments[0].Payload)
		}
	}
	seen := map[string]bool{}
	for _, coupon := range coupons {
		if seen[coupon.Code] || coupon.Type != constants.CouponTypePercent || !coupon.Value.Decimal.Equal(decimal.NewFromInt(10)) || coupon.UsageLimit != 1 || coupon.PerUserLimit != 1 || coupon.EndsAt != nil || !coupon.IsActive || coupon.UsedCount != 0 {
			t.Fatalf("coupon rule drifted: %+v", coupon)
		}
		seen[coupon.Code] = true
	}
	var stored orderdomain.Order
	if err := db.First(&stored, orderID).Error; err != nil {
		t.Fatalf("reload delivered order: %v", err)
	}
	if stored.Status != constants.OrderStatusCompleted {
		t.Fatalf("delivered status = %s", stored.Status)
	}
	return codes
}

func openCouponDeliveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:coupon_delivery_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&orderdomain.Order{}, &orderdomain.OrderItem{}, &fulfillmentdomain.Fulfillment{}, &coupondomain.Coupon{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
