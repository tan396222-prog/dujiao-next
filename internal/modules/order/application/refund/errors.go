package refund

import (
	"errors"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
)

var (
	ErrOrderFetchFailed         = orderapp.ErrOrderFetchFailed
	ErrOrderNotFound            = orderapp.ErrOrderNotFound
	ErrOrderRefundExpired       = orderapp.ErrOrderRefundExpired
	ErrOrderStatusInvalid       = orderapp.ErrOrderStatusInvalid
	ErrOrderUpdateFailed        = orderapp.ErrOrderUpdateFailed
	ErrRefundRecordCreateFailed = orderapp.ErrRefundRecordCreateFailed
	// ErrCouponSaleBenefitConsumed 表示退款会把已使用的券款退回，或没有对应的整张未使用券。
	ErrCouponSaleBenefitConsumed = errors.New("coupon sale refund would keep a consumed benefit")
)
