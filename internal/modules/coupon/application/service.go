package application

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	couponcontract "github.com/dujiao-next/internal/modules/coupon/contract"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// CouponService 优惠券服务
type Service struct {
	couponRepo couponcontract.Repository
	usageRepo  couponcontract.UsageRepository
}

type couponEligibility struct {
	subtotal money.Amount
	quantity int
}

// NewService 创建优惠券服务
func NewService(couponRepo couponcontract.Repository, usageRepo couponcontract.UsageRepository) *Service {
	return &Service{
		couponRepo: couponRepo,
		usageRepo:  usageRepo,
	}
}

// ApplyCoupon 计算优惠券折扣金额
func (s *Service) ApplyCoupon(subtotal money.Amount, code string, userID uint, items []couponcontract.EligibilityItem, isGuest bool, memberLevelID uint) (money.Amount, *coupondomain.Coupon, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return money.Amount{}, nil, couponcontract.ErrInvalid
	}

	coupon, err := s.couponRepo.GetByCode(trimmed)
	if err != nil {
		return money.Amount{}, nil, err
	}
	if coupon == nil {
		return money.Amount{}, nil, couponcontract.ErrNotFound
	}
	if !coupon.IsActive {
		return money.Amount{}, coupon, couponcontract.ErrInactive
	}

	now := time.Now()
	if coupon.StartsAt != nil && now.Before(*coupon.StartsAt) {
		return money.Amount{}, coupon, couponcontract.ErrNotStarted
	}
	if coupon.EndsAt != nil && now.After(*coupon.EndsAt) {
		return money.Amount{}, coupon, couponcontract.ErrExpired
	}

	if coupon.UsageLimit > 0 && coupon.UsedCount >= coupon.UsageLimit {
		return money.Amount{}, coupon, couponcontract.ErrUsageLimit
	}
	if roleErr := resolveCouponPaymentRoleError(coupon, isGuest); roleErr != nil {
		return money.Amount{}, coupon, roleErr
	}
	if !matchesCouponMemberLevel(coupon, memberLevelID) {
		return money.Amount{}, coupon, couponcontract.ErrMemberLevelNotAllowed
	}

	if coupon.PerUserLimit > 0 && userID != 0 {
		count, err := s.usageRepo.CountByUser(coupon.ID, userID)
		if err != nil {
			return money.Amount{}, coupon, err
		}
		if int(count) >= coupon.PerUserLimit {
			return money.Amount{}, coupon, couponcontract.ErrPerUserLimit
		}
	}

	eligibility, err := s.resolveCouponEligibility(coupon, items)
	if err != nil {
		return money.Amount{}, coupon, err
	}

	if eligibility.subtotal.Decimal.Cmp(coupon.MinAmount.Decimal) < 0 {
		return money.Amount{}, coupon, couponcontract.ErrMinAmount
	}

	discount, err := s.calculateDiscount(coupon, eligibility)
	if err != nil {
		return money.Amount{}, coupon, err
	}

	if coupon.MaxDiscount.Decimal.GreaterThan(decimal.Zero) && discount.Decimal.GreaterThan(coupon.MaxDiscount.Decimal) {
		discount = money.FromDecimal(coupon.MaxDiscount.Decimal)
	}

	if discount.Decimal.GreaterThan(eligibility.subtotal.Decimal) {
		discount = money.FromDecimal(eligibility.subtotal.Decimal)
	}

	return discount, coupon, nil
}

// matchesCouponRole 判断当前下单角色是否满足优惠券付款角色限制；未配置限制时默认允许。
func matchesCouponRole(coupon *coupondomain.Coupon, isGuest bool) bool {
	if coupon == nil || len(coupon.PaymentRoles) == 0 {
		return true
	}
	targetRole := constants.PaymentRoleMember
	if isGuest {
		targetRole = constants.PaymentRoleGuest
	}
	for _, role := range coupon.PaymentRoles {
		if strings.EqualFold(strings.TrimSpace(role), targetRole) {
			return true
		}
	}
	return false
}

// resolveCouponPaymentRoleError 解析付款角色限制不满足时的业务错误。
// 当限制仅单选一个角色时返回更精确的提示错误；否则返回通用角色不匹配错误。
func resolveCouponPaymentRoleError(coupon *coupondomain.Coupon, isGuest bool) error {
	if matchesCouponRole(coupon, isGuest) {
		return nil
	}
	if coupon == nil || len(coupon.PaymentRoles) == 0 {
		return couponcontract.ErrPaymentRoleNotAllowed
	}

	roles := make(map[string]struct{}, len(coupon.PaymentRoles))
	for _, role := range coupon.PaymentRoles {
		normalized := strings.ToLower(strings.TrimSpace(role))
		if normalized != constants.PaymentRoleGuest && normalized != constants.PaymentRoleMember {
			continue
		}
		roles[normalized] = struct{}{}
	}

	if len(roles) == 1 {
		if _, ok := roles[constants.PaymentRoleGuest]; ok {
			return couponcontract.ErrPaymentRoleGuestOnly
		}
		if _, ok := roles[constants.PaymentRoleMember]; ok {
			return couponcontract.ErrPaymentRoleMemberOnly
		}
	}
	return couponcontract.ErrPaymentRoleNotAllowed
}

// matchesCouponMemberLevel 判断当前会员等级是否满足优惠券会员等级限制；未配置限制时默认允许。
func matchesCouponMemberLevel(coupon *coupondomain.Coupon, memberLevelID uint) bool {
	if coupon == nil || len(coupon.MemberLevels) == 0 {
		return true
	}
	if memberLevelID == 0 {
		return false
	}
	for _, levelID := range coupon.MemberLevels {
		if levelID == memberLevelID {
			return true
		}
	}
	return false
}

// SaleCouponCode 为同一订单项的第 sequence 张券生成稳定优惠码，支付回调重试不会再发一张。
func SaleCouponCode(orderItemID uint, sequence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("dujiao-coupon-sale:%d:%d", orderItemID, sequence)))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return "DJ" + strings.ToUpper(encoded[:10])
}

// EnsureSaleCoupon 发放或取回一张永久有效、仅可使用一次的九折券。
// 九折是在售价上减 10%，百分比券的 value 因此是 10。
func (s *Service) EnsureSaleCoupon(repo couponcontract.Repository, orderItemID uint, sequence int) (string, error) {
	if s == nil {
		return "", couponcontract.ErrInvalid
	}
	if repo == nil {
		repo = s.couponRepo
	}
	if repo == nil || orderItemID == 0 || sequence <= 0 {
		return "", couponcontract.ErrInvalid
	}
	code := SaleCouponCode(orderItemID, sequence)
	existing, err := repo.GetByCode(code)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return existing.Code, nil
	}
	now := time.Now()
	coupon := &coupondomain.Coupon{
		Code:         code,
		Type:         constants.CouponTypePercent,
		Value:        money.FromDecimal(decimal.NewFromInt(10)),
		MinAmount:    money.FromDecimal(decimal.Zero),
		MaxDiscount:  money.FromDecimal(decimal.Zero),
		UsageLimit:   1,
		PerUserLimit: 1,
		ScopeType:    constants.ScopeTypeStore,
		ScopeRefIDs:  "[]",
		ScopeSKUIDs:  "[]",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(coupon); err != nil {
		existing, getErr := repo.GetByCode(code)
		if getErr == nil && existing != nil {
			return existing.Code, nil
		}
		return "", err
	}
	return code, nil
}

func (s *Service) resolveCouponEligibility(coupon *coupondomain.Coupon, items []couponcontract.EligibilityItem) (couponEligibility, error) {
	scope := strings.ToLower(strings.TrimSpace(coupon.ScopeType))
	if scope == constants.ScopeTypeStore {
		return storeCouponEligibility(items)
	}
	if scope != constants.ScopeTypeProduct {
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}

	ids, err := coupondomain.DecodeScopeIDs(coupon.ScopeRefIDs)
	if err != nil {
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}
	if len(ids) == 0 {
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}
	// 规格限制为空时只按商品判断；非空时订单项的规格也必须在范围内。
	skuIDs, err := coupondomain.DecodeScopeIDs(coupon.ScopeSKUIDs)
	if err != nil {
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}

	eligible := decimal.Zero
	eligibleQuantity := 0
	scopeMatched := 0
	wholesaleExcluded := 0
	for _, item := range items {
		if _, ok := ids[item.ProductID]; !ok {
			continue
		}
		if len(skuIDs) > 0 {
			if _, ok := skuIDs[item.SKUID]; !ok {
				continue
			}
		}
		scopeMatched++
		if coupon.DisabledWholesalePrice && item.WholesaleDiscount.Decimal.GreaterThan(decimal.Zero) {
			wholesaleExcluded++
			continue
		}
		if item.CouponExcluded {
			continue
		}
		eligible = eligible.Add(item.TotalPrice.Decimal)
		if item.Quantity > 0 {
			eligibleQuantity += item.Quantity
		}
	}

	if eligible.IsZero() {
		if scopeMatched > 0 && wholesaleExcluded == scopeMatched {
			return couponEligibility{}, couponcontract.ErrWholesaleDisabled
		}
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}
	return couponEligibility{
		subtotal: money.FromDecimal(eligible),
		quantity: eligibleQuantity,
	}, nil
}

func storeCouponEligibility(items []couponcontract.EligibilityItem) (couponEligibility, error) {
	eligible := decimal.Zero
	eligibleQuantity := 0
	for _, item := range items {
		if item.CouponExcluded {
			continue
		}
		eligible = eligible.Add(item.TotalPrice.Decimal)
		if item.Quantity > 0 {
			eligibleQuantity += item.Quantity
		}
	}
	if eligible.LessThanOrEqual(decimal.Zero) {
		return couponEligibility{}, couponcontract.ErrScopeInvalid
	}
	return couponEligibility{
		subtotal: money.FromDecimal(eligible),
		quantity: eligibleQuantity,
	}, nil
}

func (s *Service) calculateDiscount(coupon *coupondomain.Coupon, eligibility couponEligibility) (money.Amount, error) {
	switch strings.ToLower(strings.TrimSpace(coupon.Type)) {
	case constants.CouponTypeFixed:
		if coupon.Value.Decimal.LessThanOrEqual(decimal.Zero) {
			return money.Amount{}, couponcontract.ErrInvalid
		}
		if coupon.PerItemDiscount {
			if eligibility.quantity <= 0 {
				return money.Amount{}, couponcontract.ErrScopeInvalid
			}
			discount := coupon.Value.Decimal.Mul(decimal.NewFromInt(int64(eligibility.quantity)))
			return money.FromDecimal(discount), nil
		}
		return money.FromDecimal(coupon.Value.Decimal), nil
	case constants.CouponTypePercent:
		if coupon.Value.Decimal.LessThanOrEqual(decimal.Zero) {
			return money.Amount{}, couponcontract.ErrInvalid
		}
		percent := coupon.Value.Decimal.Div(decimal.NewFromInt(100))
		discount := eligibility.subtotal.Decimal.Mul(percent)
		return money.FromDecimal(discount), nil
	default:
		return money.Amount{}, couponcontract.ErrInvalid
	}
}
