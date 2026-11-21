package consumer

import (
	"fmt"
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
