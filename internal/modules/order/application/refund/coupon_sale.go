package refund

import (
	"strings"
	"time"

	couponapp "github.com/dujiao-next/internal/modules/coupon/application"
	couponcontract "github.com/dujiao-next/internal/modules/coupon/contract"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/shopspring/decimal"
)

type saleCouponSlot struct {
	orderID  uint
	itemID   uint
	sequence int
	code     string
	price    decimal.Decimal
	coupon   *coupondomain.Coupon
}

// enforceCouponSaleRefund 在现有退款事务里处理售券订单。
// 未使用的券按整张面额失效；已使用的券款不能退回。券行锁和核销使用同一行。
func enforceCouponSaleRefund(tx ordercontract.Transaction, order *orderdomain.Order, amount decimal.Decimal) error {
	if tx == nil || order == nil {
		return nil
	}
	slots := saleCouponSlots(order)
	if len(slots) == 0 {
		return nil
	}
	repo := tx.Coupons()
	if repo == nil {
		return ErrOrderUpdateFailed
	}
	for index := range slots {
		existing, err := repo.GetByCode(slots[index].code)
		if err != nil {
			return err
		}
		if existing == nil {
			continue
		}
		locked, err := repo.GetByIDForUpdate(existing.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return couponcontract.ErrNotFound
		}
		slots[index].coupon = locked
	}

	live := make([]saleCouponSlot, 0, len(slots))
	couponSum := decimal.Zero
	liveSum := decimal.Zero
	for _, slot := range slots {
		couponSum = couponSum.Add(slot.price).Round(2)
		if slot.coupon != nil && (slot.coupon.UsedCount > 0 || !slot.coupon.IsActive) {
			continue
		}
		live = append(live, slot)
		liveSum = liveSum.Add(slot.price).Round(2)
	}
	other := order.TotalAmount.Decimal.Round(2).Sub(couponSum).Round(2)
	if other.IsNegative() {
		other = decimal.Zero
	}
	refunded := order.RefundedAmount.Decimal.Round(2)
	otherRemaining := other.Sub(refunded).Round(2)
	if otherRemaining.IsNegative() {
		otherRemaining = decimal.Zero
	}
	couponRequested := amount.Round(2).Sub(otherRemaining).Round(2)
	if couponRequested.IsNegative() {
		couponRequested = decimal.Zero
	}
	if couponRequested.GreaterThan(liveSum) {
		return ErrCouponSaleBenefitConsumed
	}
	selected, ok := takeCouponPrefix(live, couponRequested)
	if !ok {
		return ErrCouponSaleBenefitConsumed
	}
	for _, slot := range selected {
		if err := revokeSaleCoupon(repo, slot); err != nil {
			return err
		}
	}
	return refreshCouponSaleFulfillment(tx, order, slots)
}

func saleCouponSlots(order *orderdomain.Order) []saleCouponSlot {
	if order == nil {
		return nil
	}
	sources := []orderdomain.Order{*order}
	if len(order.Children) > 0 {
		sources = order.Children
	}
	slots := make([]saleCouponSlot, 0)
	for _, source := range sources {
		for _, item := range source.Items {
			if !item.IsCouponSale() || item.ID == 0 {
				continue
			}
			quantity := item.Quantity
			if quantity < 1 {
				quantity = 1
			}
			prices := saleCouponPrices(item.TotalPrice.Decimal, quantity)
			for sequence := 1; sequence <= quantity; sequence++ {
				slots = append(slots, saleCouponSlot{
					orderID:  source.ID,
					itemID:   item.ID,
					sequence: sequence,
					code:     couponapp.SaleCouponCode(item.ID, sequence),
					price:    prices[sequence-1],
				})
			}
		}
	}
	return slots
}

func saleCouponPrices(total decimal.Decimal, quantity int) []decimal.Decimal {
	total = total.Round(2)
	if quantity < 1 {
		quantity = 1
	}
	unit := total.Div(decimal.NewFromInt(int64(quantity))).Round(2)
	prices := make([]decimal.Decimal, quantity)
	allocated := decimal.Zero
	for index := 0; index < quantity-1; index++ {
		prices[index] = unit
		allocated = allocated.Add(unit)
	}
	prices[quantity-1] = total.Sub(allocated).Round(2)
	return prices
}

func takeCouponPrefix(slots []saleCouponSlot, amount decimal.Decimal) ([]saleCouponSlot, bool) {
	amount = amount.Round(2)
	if amount.IsZero() {
		return nil, true
	}
	sum := decimal.Zero
	selected := make([]saleCouponSlot, 0)
	for _, slot := range slots {
		if sum.Equal(amount) {
			break
		}
		next := sum.Add(slot.price).Round(2)
		if next.GreaterThan(amount) {
			return nil, false
		}
		selected = append(selected, slot)
		sum = next
	}
	return selected, sum.Equal(amount)
}

func revokeSaleCoupon(repo couponcontract.Repository, slot saleCouponSlot) error {
	coupon := slot.coupon
	if coupon == nil {
		issuer := couponapp.NewService(nil, nil)
		if _, err := issuer.EnsureSaleCoupon(repo, slot.itemID, slot.sequence); err != nil {
			return err
		}
		created, err := repo.GetByCode(slot.code)
		if err != nil {
			return err
		}
		if created == nil {
			return couponcontract.ErrNotFound
		}
		locked, err := repo.GetByIDForUpdate(created.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return couponcontract.ErrNotFound
		}
		coupon = locked
	}
	if coupon.UsedCount > 0 {
		return ErrCouponSaleBenefitConsumed
	}
	if !coupon.IsActive {
		return nil
	}
	coupon.IsActive = false
	coupon.UpdatedAt = time.Now()
	return repo.Update(coupon)
}

func refreshCouponSaleFulfillment(tx ordercontract.Transaction, order *orderdomain.Order, slots []saleCouponSlot) error {
	if tx.Fulfillments() == nil {
		return nil
	}
	ids := []uint{order.ID}
	for _, child := range order.Children {
		if child.ID != 0 {
			ids = append(ids, child.ID)
		}
	}
	repo := tx.Coupons()
	for _, orderID := range ids {
		record, found, err := tx.Fulfillments().FindByOrderIDForUpdate(orderID)
		if err != nil {
			return err
		}
		if !found || record == nil {
			continue
		}
		codes := make([]string, 0)
		for _, slot := range slots {
			if slot.orderID != orderID && slot.orderID != 0 {
				continue
			}
			coupon, err := repo.GetByCode(slot.code)
			if err != nil {
				return err
			}
			if coupon != nil && coupon.IsActive {
				codes = append(codes, coupon.Code)
			}
		}
		record.Payload = strings.Join(codes, "\n")
		record.UpdatedAt = time.Now()
		if err := tx.Fulfillments().Update(record); err != nil {
			return err
		}
	}
	return nil
}
