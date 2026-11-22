package producthttp

import (
	"encoding/json"
	"strings"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/constants"
)

func TestDecorateProductStock_AutoSkipsInactiveSKUs(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:              1,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []productdomain.ProductSKU{
			{
				ID:                 11,
				SKUCode:            productdomain.DefaultSKUCode,
				IsActive:           true,
				AutoStockAvailable: 2,
				AutoStockTotal:     3,
				AutoStockLocked:    1,
				AutoStockSold:      4,
			},
			{
				ID:                 12,
				SKUCode:            "DISABLED",
				IsActive:           false,
				AutoStockAvailable: 100,
				AutoStockTotal:     120,
				AutoStockLocked:    20,
				AutoStockSold:      50,
			},
		},
	}

	item := publicProductView{Product: *product}
	h.decorateProductStock(product, &item)

	if item.AutoStockAvailable != 2 {
		t.Fatalf("expected auto_stock_available=2, got %d", item.AutoStockAvailable)
	}
	if item.AutoStockTotal != 3 {
		t.Fatalf("expected auto_stock_total=3, got %d", item.AutoStockTotal)
	}
	if item.AutoStockLocked != 1 {
		t.Fatalf("expected auto_stock_locked=1, got %d", item.AutoStockLocked)
	}
	if item.AutoStockSold != 4 {
		t.Fatalf("expected auto_stock_sold=4, got %d", item.AutoStockSold)
	}
	if item.IsSoldOut {
		t.Fatalf("expected product not sold out when active sku has stock")
	}
	if !item.Backorder {
		t.Fatal("local auto stock can be ordered before restock")
	}
}

func TestDecorateProductStock_ManualZeroStaysPurchasable(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:              2,
		FulfillmentType: constants.FulfillmentTypeManual,
		SKUs: []productdomain.ProductSKU{{
			ID:               21,
			SKUCode:          productdomain.DefaultSKUCode,
			IsActive:         true,
			ManualStockTotal: 0,
		}},
	}
	item := publicProductView{Product: *product}
	h.decorateProductStock(product, &item)
	if !item.Backorder || item.IsSoldOut {
		t.Fatalf("zero local stock is presale, backorder=%v soldout=%v", item.Backorder, item.IsSoldOut)
	}
	if item.StockStatus != constants.ProductStockStatusOutOfStock {
		t.Fatalf("stock status %s", item.StockStatus)
	}
}

func TestPublicProductResponseStatusModeMasksExactStock(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:               1,
		FulfillmentType:  constants.FulfillmentTypeManual,
		StockDisplayMode: constants.ProductStockDisplayStatus,
		SKUs: []productdomain.ProductSKU{
			{
				ID:               11,
				SKUCode:          productdomain.DefaultSKUCode,
				IsActive:         true,
				ManualStockTotal: 37,
			},
		},
	}

	resp, err := h.decoratePublicProduct(product, nil)
	if err != nil {
		t.Fatalf("decoratePublicProduct failed: %v", err)
	}

	if resp.StockDisplayMode != constants.ProductStockDisplayStatus {
		t.Fatalf("expected stock_display_mode=status, got %q", resp.StockDisplayMode)
	}
	if !resp.StockQuantityHidden {
		t.Fatalf("expected product stock quantity to be hidden")
	}
	if resp.ManualStockAvailable == 37 {
		t.Fatalf("expected product manual stock to be masked, got exact value %d", resp.ManualStockAvailable)
	}
	if resp.StockDisplay != constants.ProductStockStatusInStock {
		t.Fatalf("expected product stock display in_stock, got %q", resp.StockDisplay)
	}
	if len(resp.SKUs) != 1 {
		t.Fatalf("expected one sku, got %d", len(resp.SKUs))
	}
	sku := resp.SKUs[0]
	if !sku.StockQuantityHidden {
		t.Fatalf("expected sku stock quantity to be hidden")
	}
	if sku.ManualStockTotal == 37 {
		t.Fatalf("expected sku manual stock to be masked, got exact value %d", sku.ManualStockTotal)
	}
	if sku.StockStatus != constants.ProductStockStatusInStock {
		t.Fatalf("expected sku stock status in_stock, got %q", sku.StockStatus)
	}
	if sku.IsSoldOut {
		t.Fatalf("expected sku to remain purchasable")
	}
}

func TestPublicProductResponseRangeModeReturnsBucketOnly(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:               1,
		FulfillmentType:  constants.FulfillmentTypeManual,
		StockDisplayMode: constants.ProductStockDisplayRange,
		SKUs: []productdomain.ProductSKU{
			{
				ID:               11,
				SKUCode:          productdomain.DefaultSKUCode,
				IsActive:         true,
				ManualStockTotal: 42,
			},
		},
	}

	resp, err := h.decoratePublicProduct(product, nil)
	if err != nil {
		t.Fatalf("decoratePublicProduct failed: %v", err)
	}

	if resp.StockDisplay != constants.ProductStockDisplayRange21To50 {
		t.Fatalf("expected product stock range 21-50, got %q", resp.StockDisplay)
	}
	if resp.StockRangeMin == nil || *resp.StockRangeMin != 21 {
		t.Fatalf("expected range min 21, got %+v", resp.StockRangeMin)
	}
	if resp.StockRangeMax == nil || *resp.StockRangeMax != 50 {
		t.Fatalf("expected range max 50, got %+v", resp.StockRangeMax)
	}
	if resp.ManualStockAvailable == 42 {
		t.Fatalf("expected product manual stock to be masked, got exact value %d", resp.ManualStockAvailable)
	}
	if len(resp.SKUs) != 1 {
		t.Fatalf("expected one sku, got %d", len(resp.SKUs))
	}
	sku := resp.SKUs[0]
	if sku.StockDisplay != constants.ProductStockDisplayRange21To50 {
		t.Fatalf("expected sku stock range 21-50, got %q", sku.StockDisplay)
	}
	if sku.StockRangeMin == nil || *sku.StockRangeMin != 21 {
		t.Fatalf("expected sku range min 21, got %+v", sku.StockRangeMin)
	}
	if sku.StockRangeMax == nil || *sku.StockRangeMax != 50 {
		t.Fatalf("expected sku range max 50, got %+v", sku.StockRangeMax)
	}
	if sku.ManualStockTotal == 42 {
		t.Fatalf("expected sku manual stock to be masked, got exact value %d", sku.ManualStockTotal)
	}
	if resp.ManualStockAvailable != 0 || sku.ManualStockTotal != 0 || sku.ManualStockSold != 0 {
		t.Fatalf("range mode still published a positive count product=%d sku=%d sold=%d", resp.ManualStockAvailable, sku.ManualStockTotal, sku.ManualStockSold)
	}
}

func TestPublicProductResponseHidesFiniteCardCountsButKeepsTiers(t *testing.T) {
	h := &PublicHandler{}
	product := &productdomain.Product{
		ID:               7,
		FulfillmentType:  constants.FulfillmentTypeAuto,
		StockDisplayMode: constants.ProductStockDisplayRange,
		SKUs: []productdomain.ProductSKU{
			{ID: 1, SKUCode: "single", IsActive: true, AutoStockAvailable: 1, AutoStockTotal: 1, AutoStockSold: 4},
			{ID: 2, SKUCode: "mid", IsActive: true, AutoStockAvailable: 9, AutoStockTotal: 12, AutoStockSold: 3},
		},
	}

	resp, err := h.decoratePublicProduct(product, nil)
	if err != nil {
		t.Fatalf("decoratePublicProduct failed: %v", err)
	}
	if resp.StockStatus != constants.ProductStockStatusInStock || resp.StockDisplay != constants.ProductStockDisplayRange6To20 {
		t.Fatalf("product tier mismatch status=%s display=%s", resp.StockStatus, resp.StockDisplay)
	}
	if !resp.StockQuantityHidden || !resp.Backorder || resp.IsSoldOut {
		t.Fatalf("local auto stock must stay orderable without exposing a count: %+v", resp)
	}
	if len(resp.SKUs) != 2 {
		t.Fatalf("expected two skus, got %d", len(resp.SKUs))
	}
	if resp.SKUs[0].StockStatus != constants.ProductStockStatusLowStock || resp.SKUs[0].StockDisplay != constants.ProductStockDisplayRange1To5 {
		t.Fatalf("single card sku tier mismatch: %+v", resp.SKUs[0])
	}
	if resp.SKUs[1].StockStatus != constants.ProductStockStatusInStock || resp.SKUs[1].StockDisplay != constants.ProductStockDisplayRange6To20 {
		t.Fatalf("mid card sku tier mismatch: %+v", resp.SKUs[1])
	}

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal public product: %v", err)
	}
	assertNoPositivePublicStock(t, body)
	if strings.Contains(string(body), `"auto_stock_available":1`) || strings.Contains(string(body), `"auto_stock_available":9`) {
		t.Fatalf("public json still contains an exact auto stock count: %s", body)
	}

	hidden := *product
	hidden.StockDisplayMode = constants.ProductStockDisplayHidden
	hiddenResp, err := h.decoratePublicProduct(&hidden, nil)
	if err != nil {
		t.Fatalf("decorate hidden product: %v", err)
	}
	hiddenBody, err := json.Marshal(hiddenResp)
	if err != nil {
		t.Fatalf("marshal hidden product: %v", err)
	}
	assertNoPositivePublicStock(t, hiddenBody)
	if hiddenResp.StockDisplay != constants.ProductStockDisplayHidden || !hiddenResp.StockQuantityHidden {
		t.Fatalf("hidden mode metadata mismatch: %+v", hiddenResp)
	}

	empty := &productdomain.Product{
		ID:               8,
		FulfillmentType:  constants.FulfillmentTypeAuto,
		StockDisplayMode: constants.ProductStockDisplayRange,
		SKUs: []productdomain.ProductSKU{
			{ID: 3, SKUCode: productdomain.DefaultSKUCode, IsActive: true, AutoStockAvailable: 0},
		},
	}
	emptyResp, err := h.decoratePublicProduct(empty, nil)
	if err != nil {
		t.Fatalf("decorate empty product: %v", err)
	}
	if emptyResp.StockStatus != constants.ProductStockStatusOutOfStock || !emptyResp.Backorder || emptyResp.IsSoldOut {
		t.Fatalf("zero local cards must stay presale, got status=%s backorder=%v soldout=%v", emptyResp.StockStatus, emptyResp.Backorder, emptyResp.IsSoldOut)
	}
	if emptyResp.AutoStockAvailable != 0 || emptyResp.SKUs[0].AutoStockAvailable != 0 {
		t.Fatalf("zero stock changed value product=%d sku=%d", emptyResp.AutoStockAvailable, emptyResp.SKUs[0].AutoStockAvailable)
	}

	unlimited := &productdomain.Product{
		ID:               9,
		FulfillmentType:  constants.FulfillmentTypeManual,
		StockDisplayMode: constants.ProductStockDisplayHidden,
		SKUs: []productdomain.ProductSKU{
			{ID: 4, SKUCode: productdomain.DefaultSKUCode, IsActive: true, ManualStockTotal: constants.ManualStockUnlimited},
		},
	}
	unlimitedResp, err := h.decoratePublicProduct(unlimited, nil)
	if err != nil {
		t.Fatalf("decorate unlimited product: %v", err)
	}
	if unlimitedResp.ManualStockAvailable != constants.ManualStockUnlimited || unlimitedResp.SKUs[0].ManualStockTotal != constants.ManualStockUnlimited {
		t.Fatalf("already unlimited stock must stay unlimited, product=%d sku=%d", unlimitedResp.ManualStockAvailable, unlimitedResp.SKUs[0].ManualStockTotal)
	}
	if unlimitedResp.StockStatus != constants.ProductStockStatusUnlimited {
		t.Fatalf("unlimited status mismatch: %s", unlimitedResp.StockStatus)
	}

	exact := &productdomain.Product{
		ID:               10,
		FulfillmentType:  constants.FulfillmentTypeAuto,
		StockDisplayMode: constants.ProductStockDisplayExact,
		SKUs: []productdomain.ProductSKU{
			{ID: 5, SKUCode: productdomain.DefaultSKUCode, IsActive: true, AutoStockAvailable: 1, AutoStockTotal: 6},
		},
	}
	exactResp, err := h.decoratePublicProduct(exact, nil)
	if err != nil {
		t.Fatalf("decorate exact product: %v", err)
	}
	if exactResp.StockQuantityHidden || exactResp.AutoStockAvailable != 1 || exactResp.SKUs[0].AutoStockAvailable != 1 {
		t.Fatalf("exact mode must keep the real count, product=%d sku=%d hidden=%v", exactResp.AutoStockAvailable, exactResp.SKUs[0].AutoStockAvailable, exactResp.StockQuantityHidden)
	}
	if exactResp.StockDisplay != constants.ProductStockDisplayExact || exactResp.SKUs[0].StockStatus != constants.ProductStockStatusLowStock {
		t.Fatalf("exact display mismatch product=%s sku=%s", exactResp.StockDisplay, exactResp.SKUs[0].StockStatus)
	}
}

func assertNoPositivePublicStock(t *testing.T, raw []byte) {
	t.Helper()
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode public product: %v", err)
	}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, value := range typed {
				switch key {
				case "auto_stock_available", "auto_stock_total", "auto_stock_locked", "auto_stock_sold",
					"manual_stock_available", "manual_stock_total", "manual_stock_locked", "manual_stock_sold",
					"upstream_stock", "stock_count":
					number, ok := value.(float64)
					if !ok {
						t.Fatalf("%s is not a number: %#v", key, value)
					}
					if number > 0 {
						t.Fatalf("%s leaked a positive quantity %v", key, number)
					}
				}
				walk(value)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(payload)
}
