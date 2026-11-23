package application

import (
	"testing"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/shopspring/decimal"
)

func TestMergeCreateOrderItems(t *testing.T) {
	items := []CreateOrderItem{
		{ProductID: 1, SKUID: 10, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 10, Quantity: 2, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 11, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 2, SKUID: 20, Quantity: 1, FulfillmentType: ""},
	}
	merged, err := mergeCreateOrderItems(items)
	if err != nil {
		t.Fatalf("mergeCreateOrderItems error: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 items, got %d", len(merged))
	}
	if merged[0].ProductID != 1 || merged[0].SKUID != 10 || merged[0].Quantity != 3 {
		t.Fatalf("unexpected merged item: %+v", merged[0])
	}
	if merged[0].FulfillmentType != "" {
		t.Fatalf("expected empty fulfillment type, got: %s", merged[0].FulfillmentType)
	}
}

func TestMergeCreateOrderItemsConflict(t *testing.T) {
	items := []CreateOrderItem{
		{ProductID: 1, SKUID: 10, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 11, Quantity: 1, FulfillmentType: "manual"},
	}
	merged, err := mergeCreateOrderItems(items)
	if err != nil {
		t.Fatalf("expected no error for conflicting fulfillment type input, got: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("unexpected merged result: %+v", merged)
	}
}

func TestApplyCouponDiscountToItems(t *testing.T) {
	plans := []childOrderPlan{
		{Item: orderdomain.OrderItem{ProductID: 1}, TotalAmount: decimal.NewFromInt(100)},
		{Item: orderdomain.OrderItem{ProductID: 2}, TotalAmount: decimal.NewFromInt(50)},
		{Item: orderdomain.OrderItem{ProductID: 3}, TotalAmount: decimal.NewFromInt(50)},
	}
	coupon := &coupondomain.Coupon{
		ScopeType:   constants.ScopeTypeProduct,
		ScopeRefIDs: "[1,2]",
	}
	if err := applyCouponDiscountToItems(plans, coupon, decimal.NewFromInt(30)); err != nil {
		t.Fatalf("applyCouponDiscountToItems error: %v", err)
	}
	if !plans[0].CouponDiscount.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("expected 20, got %s", plans[0].CouponDiscount.String())
	}
	if !plans[1].CouponDiscount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected 10, got %s", plans[1].CouponDiscount.String())
	}
	if !plans[2].CouponDiscount.Equal(decimal.Zero) {
		t.Fatalf("expected 0, got %s", plans[2].CouponDiscount.String())
	}

	// 限定了规格时，分摊必须和优惠总额算的是同一批订单项：
	// 未勾选规格的订单项不能分到任何优惠。
	skuPlans := []childOrderPlan{
		{Item: orderdomain.OrderItem{ProductID: 1, SKUID: 1}, TotalAmount: decimal.NewFromInt(80)},
		{Item: orderdomain.OrderItem{ProductID: 1, SKUID: 2}, TotalAmount: decimal.NewFromInt(40)},
	}
	skuCoupon := &coupondomain.Coupon{
		ScopeType:   constants.ScopeTypeProduct,
		ScopeRefIDs: "[1]",
		ScopeSKUIDs: "[2]",
	}
	if err := applyCouponDiscountToItems(skuPlans, skuCoupon, decimal.NewFromInt(20)); err != nil {
		t.Fatalf("applyCouponDiscountToItems with sku scope error: %v", err)
	}
	if !skuPlans[0].CouponDiscount.Equal(decimal.Zero) {
		t.Fatalf("expected sku 1 discount 0, got %s", skuPlans[0].CouponDiscount.String())
	}
	if !skuPlans[1].CouponDiscount.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("expected sku 2 discount 20, got %s", skuPlans[1].CouponDiscount.String())
	}
}

func TestApplyCouponDiscountSkipsCouponSale(t *testing.T) {
	sale := &productdomain.Product{ID: 9, CouponSale: true}
	plans := []childOrderPlan{
		{Product: sale, Item: orderdomain.OrderItem{ProductID: 9}, TotalAmount: decimal.NewFromInt(100)},
		{Product: &productdomain.Product{ID: 2}, Item: orderdomain.OrderItem{ProductID: 2}, TotalAmount: decimal.NewFromInt(100)},
	}
	coupon := &coupondomain.Coupon{ScopeType: constants.ScopeTypeStore}
	if err := applyCouponDiscountToItems(plans, coupon, decimal.NewFromInt(10)); err != nil {
		t.Fatalf("store coupon should discount ordinary items: %v", err)
	}
	if !plans[0].CouponDiscount.Equal(decimal.Zero) || !plans[1].CouponDiscount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("coupon sale must not receive the discount: sale=%s ordinary=%s", plans[0].CouponDiscount, plans[1].CouponDiscount)
	}

	onlySale := []childOrderPlan{{Product: sale, Item: orderdomain.OrderItem{ProductID: 9}, TotalAmount: decimal.NewFromInt(3)}}
	if err := applyCouponDiscountToItems(onlySale, coupon, decimal.NewFromInt(1)); err == nil {
		t.Fatal("a cart of only coupon-sale products must not accept a coupon")
	}

	scoped := &coupondomain.Coupon{ScopeType: constants.ScopeTypeProduct, ScopeRefIDs: "[9,2]"}
	mixed := []childOrderPlan{
		{Product: sale, Item: orderdomain.OrderItem{ProductID: 9}, TotalAmount: decimal.NewFromInt(50)},
		{Product: &productdomain.Product{ID: 2}, Item: orderdomain.OrderItem{ProductID: 2}, TotalAmount: decimal.NewFromInt(50)},
	}
	if err := applyCouponDiscountToItems(mixed, scoped, decimal.NewFromInt(5)); err != nil {
		t.Fatalf("product-scoped coupon should still discount the ordinary product: %v", err)
	}
	if !mixed[0].CouponDiscount.Equal(decimal.Zero) || !mixed[1].CouponDiscount.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("product scope leaked onto coupon sale: sale=%s ordinary=%s", mixed[0].CouponDiscount, mixed[1].CouponDiscount)
	}
}

func TestManualLineNeedsPresale(t *testing.T) {
	product := &productdomain.Product{FulfillmentType: constants.FulfillmentTypeManual}
	empty := &productdomain.ProductSKU{ManualStockTotal: 0}
	if !manualLineNeedsPresale(product, empty, 1) {
		t.Fatal("manual stock 0 must be presale")
	}
	if manualLineNeedsPresale(product, &productdomain.ProductSKU{ManualStockTotal: 5}, 5) {
		t.Fatal("enough manual stock must be reserved")
	}
	if manualLineNeedsPresale(product, &productdomain.ProductSKU{ManualStockTotal: constants.ManualStockUnlimited}, 9) {
		t.Fatal("unlimited stock is not presale")
	}
	sale := &productdomain.Product{CouponSale: true, FulfillmentType: constants.FulfillmentTypeManual}
	if manualLineNeedsPresale(sale, empty, 1) {
		t.Fatal("coupon sale does not reserve card stock")
	}
	if manualLineNeedsPresale(product, nil, 1) {
		t.Fatal("missing sku is not a presale line")
	}
}

func TestResolveManualFormSubmissionPreferOrderItemKey(t *testing.T) {
	data := map[string]jsonmap.JSON{
		"1":    {"legacy": "legacy"},
		"1:10": {"current": "current"},
	}
	got := resolveManualFormSubmission(data, 1, 10)
	if got["current"] != "current" {
		t.Fatalf("expected order item key value, got: %+v", got)
	}
}

func TestResolveManualFormSubmissionFallbackLegacyProductKey(t *testing.T) {
	data := map[string]jsonmap.JSON{
		"1": {"legacy": "legacy"},
	}
	got := resolveManualFormSubmission(data, 1, 99)
	if got["legacy"] != "legacy" {
		t.Fatalf("expected legacy product key value, got: %+v", got)
	}
}
