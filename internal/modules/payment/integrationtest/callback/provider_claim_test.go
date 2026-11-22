package paymentcallback_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	paymentgormstore "github.com/dujiao-next/internal/modules/payment/infrastructure/gormstore"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	walletgormstore "github.com/dujiao-next/internal/modules/wallet/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestProviderTradeNumberCannotBeReusedAcrossOrders(t *testing.T) {
	svc, db := setupProviderClaimTest(t)
	channel := createClaimChannel(t, db, "pid-shared", constants.PaymentChannelTypeAlipay)
	otherChannel := createClaimChannel(t, db, "pid-shared", constants.PaymentChannelTypeWxpay)
	otherMerchant := createClaimChannel(t, db, "pid-other", constants.PaymentChannelTypeAlipay)
	firstOrder := createClaimOrder(t, db, "CLAIM-ORDER-1", constants.OrderStatusPendingPayment)
	secondOrder := createClaimOrder(t, db, "CLAIM-ORDER-2", constants.OrderStatusPendingPayment)
	thirdOrder := createClaimOrder(t, db, "CLAIM-ORDER-3", constants.OrderStatusPendingPayment)
	first := createClaimPayment(t, db, firstOrder, channel, "")
	second := createClaimPayment(t, db, secondOrder, otherChannel, "")
	third := createClaimPayment(t, db, thirdOrder, otherMerchant, "")

	if _, err := svc.HandleCallback(claimCallback(first, firstOrder, "TRADE-1", "10.00", "CNY", constants.PaymentStatusSuccess)); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	if _, err := svc.HandleCallback(claimCallback(first, firstOrder, "TRADE-1", "10.00", "CNY", constants.PaymentStatusSuccess)); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if _, err := svc.HandleCallback(claimCallback(second, secondOrder, "TRADE-1", "10.00", "CNY", constants.PaymentStatusSuccess)); err != paymentapp.ErrPaymentProviderRefRejected {
		t.Fatalf("same merchant reuse = %v", err)
	}
	if _, err := svc.HandleCallback(claimCallback(third, thirdOrder, "TRADE-1", "10.00", "CNY", constants.PaymentStatusSuccess)); err != nil {
		t.Fatalf("different merchant should be a separate namespace: %v", err)
	}

	assertOrderStatus(t, db, firstOrder.ID, constants.OrderStatusPaid)
	assertOrderStatus(t, db, secondOrder.ID, constants.OrderStatusPendingPayment)
	assertOrderStatus(t, db, thirdOrder.ID, constants.OrderStatusPaid)
	if got := countClaims(t, db); got != 2 {
		t.Fatalf("claims = %d, want 2", got)
	}
	var paid int64
	if err := db.Model(&paymentdomain.Payment{}).Where("status = ?", constants.PaymentStatusSuccess).Count(&paid).Error; err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if paid != 2 {
		t.Fatalf("successful payments = %d, want the two distinct merchant payments", paid)
	}
}

func TestProviderTradeNumberConcurrentCallbacksClaimOnce(t *testing.T) {
	svc, db := setupProviderClaimTest(t)
	channel := createClaimChannel(t, db, "pid-race", constants.PaymentChannelTypeAlipay)
	leftOrder := createClaimOrder(t, db, "CLAIM-RACE-1", constants.OrderStatusPendingPayment)
	rightOrder := createClaimOrder(t, db, "CLAIM-RACE-2", constants.OrderStatusPendingPayment)
	left := createClaimPayment(t, db, leftOrder, channel, "")
	right := createClaimPayment(t, db, rightOrder, channel, "")

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for _, item := range []struct {
		payment *paymentdomain.Payment
		order   *orderdomain.Order
	}{{left, leftOrder}, {right, rightOrder}} {
		wg.Add(1)
		go func(payment *paymentdomain.Payment, order *orderdomain.Order) {
			defer wg.Done()
			<-start
			_, err := svc.HandleCallback(claimCallback(payment, order, "TRADE-RACE", "10.00", "CNY", constants.PaymentStatusSuccess))
			errs <- err
		}(item.payment, item.order)
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	rejects := 0
	for err := range errs {
		switch err {
		case nil:
			successes++
		case paymentapp.ErrPaymentProviderRefRejected:
			rejects++
		default:
			t.Fatalf("concurrent callback: %v", err)
		}
	}
	if successes != 1 || rejects != 1 {
		t.Fatalf("successes=%d rejects=%d", successes, rejects)
	}
	if got := countClaims(t, db); got != 1 {
		t.Fatalf("claims = %d, want 1", got)
	}
	var paidOrders int64
	if err := db.Model(&orderdomain.Order{}).Where("status = ?", constants.OrderStatusPaid).Count(&paidOrders).Error; err != nil {
		t.Fatalf("count paid orders: %v", err)
	}
	if paidOrders != 1 {
		t.Fatalf("paid orders = %d, want 1", paidOrders)
	}
}

func TestSuccessfulCallbackRejectsBadBindingWithoutClaiming(t *testing.T) {
	svc, db := setupProviderClaimTest(t)
	channel := createClaimChannel(t, db, "pid-bind", constants.PaymentChannelTypeAlipay)
	order := createClaimOrder(t, db, "CLAIM-BIND", constants.OrderStatusPendingPayment)
	payment := createClaimPayment(t, db, order, channel, "")

	cases := []paymentapp.PaymentCallbackInput{
		claimCallback(payment, order, "", "10.00", "CNY", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-BAD-AMOUNT", "10.001", "CNY", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-FEN", "1000.00", "CNY", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-CENT", "0.01", "CNY", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-CURRENCY", "10.00", "USD", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-ORDER", "10.00", "CNY", constants.PaymentStatusSuccess),
		claimCallback(payment, order, "TRADE-EXPIRED", "10.00", "CNY", constants.PaymentStatusExpired),
	}
	cases[5].OrderNo = "SOME-OTHER-ORDER"
	wants := []error{
		paymentapp.ErrPaymentProviderRefRequired,
		paymentapp.ErrPaymentAmountMismatch,
		paymentapp.ErrPaymentAmountMismatch,
		paymentapp.ErrPaymentAmountMismatch,
		paymentapp.ErrPaymentCurrencyMismatch,
		paymentapp.ErrPaymentInvalid,
		nil,
	}
	for index, input := range cases {
		if _, err := svc.HandleCallback(input); err != wants[index] {
			t.Fatalf("case %d error = %v, want %v", index, err, wants[index])
		}
	}
	if got := countClaims(t, db); got != 0 {
		t.Fatalf("rejected callbacks created %d claims", got)
	}
	assertOrderStatus(t, db, order.ID, constants.OrderStatusPendingPayment)

	pinned := claimCallback(payment, order, "TRADE-PINNED", "10.00", "", constants.PaymentStatusSuccess)
	if _, err := svc.HandleCallback(pinned); err != nil {
		t.Fatalf("empty currency should pin the payment currency: %v", err)
	}
	assertOrderStatus(t, db, order.ID, constants.OrderStatusPaid)
	if got := countClaims(t, db); got != 1 {
		t.Fatalf("claims = %d, want 1", got)
	}
}

func TestClosedOrderSuccessClaimsTradeNumberWithoutFulfillment(t *testing.T) {
	svc, db := setupProviderClaimTest(t)
	channel := createClaimChannel(t, db, "pid-closed", constants.PaymentChannelTypeAlipay)
	closed := createClaimOrder(t, db, "CLAIM-CLOSED", constants.OrderStatusCanceled)
	other := createClaimOrder(t, db, "CLAIM-CLOSED-OTHER", constants.OrderStatusPendingPayment)
	closedPayment := createClaimPayment(t, db, closed, channel, "")
	otherPayment := createClaimPayment(t, db, other, channel, "")

	updated, err := svc.HandleCallback(claimCallback(closedPayment, closed, "TRADE-CLOSED", "10.00", "CNY", constants.PaymentStatusSuccess))
	if err != nil {
		t.Fatalf("closed order callback: %v", err)
	}
	if updated.ExceptionCode != constants.PaymentExceptionClosedOrderSucceeded {
		t.Fatalf("exception = %q", updated.ExceptionCode)
	}
	assertOrderStatus(t, db, closed.ID, constants.OrderStatusCanceled)
	if _, err := svc.HandleCallback(claimCallback(otherPayment, other, "TRADE-CLOSED", "10.00", "CNY", constants.PaymentStatusSuccess)); err != paymentapp.ErrPaymentProviderRefRejected {
		t.Fatalf("closed-order trade number was reused: %v", err)
	}
	assertOrderStatus(t, db, other.ID, constants.OrderStatusPendingPayment)
}

func TestWalletRechargeCannotReuseOrderTradeNumber(t *testing.T) {
	svc, db := setupProviderClaimTest(t)
	channel := createClaimChannel(t, db, "pid-wallet", constants.PaymentChannelTypeAlipay)
	order := createClaimOrder(t, db, "CLAIM-WALLET-ORDER", constants.OrderStatusPendingPayment)
	payment := createClaimPayment(t, db, order, channel, "")
	if _, err := svc.HandleCallback(claimCallback(payment, order, "TRADE-WALLET", "10.00", "CNY", constants.PaymentStatusSuccess)); err != nil {
		t.Fatalf("order callback: %v", err)
	}
	rechargePayment, recharge := createClaimRecharge(t, db, channel)
	input := claimCallback(rechargePayment, &orderdomain.Order{OrderNo: recharge.RechargeNo}, "TRADE-WALLET", "10.00", "CNY", constants.PaymentStatusSuccess)
	input.OrderNo = recharge.RechargeNo
	if _, err := svc.HandleCallback(input); err != paymentapp.ErrPaymentProviderRefRejected {
		t.Fatalf("recharge reused an order trade number: %v", err)
	}
	var stored walletdomain.RechargeOrder
	if err := db.First(&stored, recharge.ID).Error; err != nil {
		t.Fatalf("reload recharge: %v", err)
	}
	if stored.Status != constants.WalletRechargeStatusPending {
		t.Fatalf("recharge status = %s", stored.Status)
	}
	var credits int64
	if err := db.Model(&walletdomain.Transaction{}).Where("user_id = ?", recharge.UserID).Count(&credits).Error; err != nil {
		t.Fatalf("count wallet credits: %v", err)
	}
	if credits != 0 {
		t.Fatalf("rejected recharge created %d wallet transactions", credits)
	}
}

func setupProviderClaimTest(t *testing.T) (*paymentapp.PaymentService, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:provider_claim_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&userdomain.User{},
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&fulfillmentdomain.Fulfillment{},
		&paymentdomain.PaymentChannel{},
		&paymentdomain.Payment{},
		&paymentdomain.ProviderClaim{},
		&walletdomain.Account{},
		&walletdomain.Transaction{},
		&walletdomain.RechargeOrder{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&userdomain.User{
		Email: "claim-user@example.com", PasswordHash: "hash", Status: constants.UserStatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	secret := "test-guest-credential-secret-with-32-bytes"
	walletStore := walletgormstore.New(db)
	return paymentapp.NewPaymentService(paymentapp.PaymentServiceOptions{
		OrderStore:     ordergormstore.New(db, secret),
		ProductRepo:    productgormstore.NewProductStore(db),
		ProductSKURepo: productgormstore.NewSKUStore(db),
		PaymentStore:   paymentgormstore.New(db, secret),
		ChannelStore:   paymentgormstore.NewChannelStore(db),
		WalletRepo:     walletStore,
		WalletService:  walletapp.NewService(walletapp.Options{Repository: walletStore, Transactions: walletStore}),
	}), db
}

func createClaimChannel(t *testing.T, db *gorm.DB, merchantID, channelType string) *paymentdomain.PaymentChannel {
	t.Helper()
	now := time.Now()
	channel := &paymentdomain.PaymentChannel{
		Name: merchantID + "-" + channelType, ProviderType: constants.PaymentProviderEpay, ChannelType: channelType,
		InteractionMode: constants.PaymentInteractionRedirect, FeeRate: money.FromDecimal(decimal.Zero),
		ConfigJSON: jsonmap.JSON{"merchant_id": merchantID, "api_key": "must-not-be-part-of-the-scope"},
		IsActive:   true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func createClaimOrder(t *testing.T, db *gorm.DB, orderNo, status string) *orderdomain.Order {
	t.Helper()
	now := time.Now()
	ten := money.FromDecimal(decimal.NewFromInt(10))
	order := &orderdomain.Order{
		OrderNo: orderNo, UserID: 1, Status: status, Currency: "CNY",
		OriginalAmount: ten, DiscountAmount: money.FromDecimal(decimal.Zero), TotalAmount: ten,
		WalletPaidAmount: money.FromDecimal(decimal.Zero), OnlinePaidAmount: ten,
		RefundedAmount: money.FromDecimal(decimal.Zero), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	return order
}

func createClaimPayment(t *testing.T, db *gorm.DB, order *orderdomain.Order, channel *paymentdomain.PaymentChannel, providerRef string) *paymentdomain.Payment {
	t.Helper()
	now := time.Now()
	payment := &paymentdomain.Payment{
		OrderID: order.ID, ChannelID: channel.ID, ProviderType: channel.ProviderType, ChannelType: channel.ChannelType,
		InteractionMode: channel.InteractionMode, Amount: money.FromDecimal(decimal.NewFromInt(10)),
		FeeRate: money.FromDecimal(decimal.Zero), FeeAmount: money.FromDecimal(decimal.Zero),
		Currency: "CNY", Status: constants.PaymentStatusPending, ProviderRef: providerRef,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(payment).Error; err != nil {
		t.Fatalf("create payment: %v", err)
	}
	return payment
}

func createClaimRecharge(t *testing.T, db *gorm.DB, channel *paymentdomain.PaymentChannel) (*paymentdomain.Payment, *walletdomain.RechargeOrder) {
	t.Helper()
	now := time.Now()
	payment := createClaimPayment(t, db, &orderdomain.Order{ID: 0}, channel, "")
	if payment.OrderID != 0 {
		t.Fatal("recharge payment must not point at an order")
	}
	recharge := &walletdomain.RechargeOrder{
		RechargeNo: fmt.Sprintf("WR%d", now.UnixNano()), UserID: 1, PaymentID: payment.ID, ChannelID: channel.ID,
		ProviderType: channel.ProviderType, ChannelType: channel.ChannelType, InteractionMode: channel.InteractionMode,
		Amount: money.FromDecimal(decimal.NewFromInt(10)), PayableAmount: money.FromDecimal(decimal.NewFromInt(10)),
		FeeRate: money.FromDecimal(decimal.Zero), FeeAmount: money.FromDecimal(decimal.Zero), Currency: "CNY",
		Status: constants.WalletRechargeStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(recharge).Error; err != nil {
		t.Fatalf("create recharge: %v", err)
	}
	return payment, recharge
}

func claimCallback(payment *paymentdomain.Payment, order *orderdomain.Order, ref, amount, currency, status string) paymentapp.PaymentCallbackInput {
	parsed := decimal.RequireFromString(amount)
	return paymentapp.PaymentCallbackInput{
		PaymentID: payment.ID, OrderNo: order.OrderNo, ChannelID: payment.ChannelID, Status: status,
		ProviderRef: ref, Amount: money.Amount{Decimal: parsed}, Currency: currency,
		PaidAt: ptrTime(time.Now()),
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func countClaims(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&paymentdomain.ProviderClaim{}).Count(&count).Error; err != nil {
		t.Fatalf("count claims: %v", err)
	}
	return count
}

func assertOrderStatus(t *testing.T, db *gorm.DB, orderID uint, status string) {
	t.Helper()
	var order orderdomain.Order
	if err := db.First(&order, orderID).Error; err != nil {
		t.Fatalf("reload order %d: %v", orderID, err)
	}
	if order.Status != status {
		t.Fatalf("order %d status = %s, want %s", orderID, order.Status, status)
	}
}
