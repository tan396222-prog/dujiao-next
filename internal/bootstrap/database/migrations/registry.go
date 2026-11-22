package migrations

import (
	"errors"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
	"github.com/dujiao-next/internal/platform/database/gormdb"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"gorm.io/gorm"

	affiliatedomain "github.com/dujiao-next/internal/modules/affiliate/domain"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	auditlogdomain "github.com/dujiao-next/internal/modules/auditlog/domain"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cartdomain "github.com/dujiao-next/internal/modules/cart/domain"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	channelclientdomain "github.com/dujiao-next/internal/modules/channelclient/domain"
	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"
	contentdomain "github.com/dujiao-next/internal/modules/content/domain"
	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	downstreamcallbackdomain "github.com/dujiao-next/internal/modules/downstreamcallback/domain"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	giftcarddomain "github.com/dujiao-next/internal/modules/giftcard/domain"
	admindomain "github.com/dujiao-next/internal/modules/identity/admin/domain"
	adminapitokendomain "github.com/dujiao-next/internal/modules/identity/adminapitoken/domain"
	emailverificationdomain "github.com/dujiao-next/internal/modules/identity/emailverification/domain"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	invitecodedomain "github.com/dujiao-next/internal/modules/identity/invitecode/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	memberleveldomain "github.com/dujiao-next/internal/modules/memberlevel/domain"
	notificationdomain "github.com/dujiao-next/internal/modules/notification/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	orderriskcontract "github.com/dujiao-next/internal/modules/orderrisk/contract"
	orderriskdomain "github.com/dujiao-next/internal/modules/orderrisk/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"
	reconciliationdomain "github.com/dujiao-next/internal/modules/reconciliation/domain"
	resellerstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	broadcastdomain "github.com/dujiao-next/internal/modules/telegram/broadcast/domain"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
)

// AutoMigrate owns the application-wide schema registry and ordered data migrations.
func AutoMigrate() error {
	db := gormdb.DB
	if err := db.AutoMigrate(
		&admindomain.Admin{},
		&adminapitokendomain.Token{},
		&userdomain.User{},
		&externalidentitydomain.Identity{},
		&invitecodedomain.InviteCode{},
		&affiliatedomain.Profile{},
		&affiliatedomain.Click{},
		&affiliatedomain.Commission{},
		&affiliatedomain.WithdrawRequest{},
		&walletdomain.Account{},
		&walletdomain.Transaction{},
		&walletdomain.RechargeOrder{},
		// daily_checkins 只增加签到记录。回滚代码时保留 wallet_transactions 中 daily-checkin: 流水，避免同一天再次入账。
		&checkindomain.CheckIn{},
		&auditlogdomain.UserLoginLog{},
		&auditlogdomain.AuthzAuditLog{},
		&notificationdomain.NotificationLog{},
		&auditlogdomain.AdminLoginLog{},
		&emailverificationdomain.Code{},
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&orderdomain.OrderRefundRecord{},
		&orderriskdomain.LockKey{},
		&cartdomain.Item{},
		&paymentdomain.PaymentChannel{},
		&paymentdomain.Payment{},
		&paymentdomain.ProviderClaim{},
		&cardsecretdomain.Secret{},
		&cardsecretdomain.Batch{},
		&giftcarddomain.GiftCard{},
		&giftcarddomain.GiftCardBatch{},
		&fulfillmentdomain.Fulfillment{},
		&coupondomain.Coupon{},
		&coupondomain.CouponUsage{},
		&promotiondomain.Promotion{},
		&categorydomain.Category{},
		&productdomain.Product{},
		&productdomain.ProductSKU{},
		&contentdomain.Post{},
		&contentdomain.PostProduct{},
		&contentdomain.PostCategory{},
		&contentdomain.Banner{},
		&settingsstore.SettingRecord{},
		&apicredentialdomain.ApiCredential{},
		&siteconnectiondomain.Connection{},
		&mappingdomain.Mapping{},
		&mappingdomain.SKUMapping{},
		&procurementdomain.Order{},
		&downstreamcallbackdomain.OrderRef{},
		&reconciliationdomain.Job{},
		&reconciliationdomain.Item{},
		&channelclientdomain.Client{},
		&broadcastdomain.Broadcast{},
		&memberleveldomain.MemberLevel{},
		&memberleveldomain.MemberLevelPrice{},
		&memberleveldomain.UserProductPrice{},
		&contentdomain.Media{},
	); err != nil {
		return err
	}

	if err := ensureUserOAuthIdentityUserProviderUniqueIndex(); err != nil {
		return err
	}
	if err := backfillPendingOrderRiskIPs(db); err != nil {
		return err
	}
	if err := resellerstore.Migrate(db); err != nil {
		return err
	}
	if err := migrateCartSKUUniqueIndex(); err != nil {
		return err
	}
	if err := ensureProductSKUMigration(); err != nil {
		return err
	}
	if err := ensureManualStockRemainingMigration(); err != nil {
		return err
	}
	if err := ensureCategoryParentMigration(); err != nil {
		return err
	}
	if err := ensurePaymentProviderBepusdtRenameMigration(); err != nil {
		return err
	}
	if err := ensurePaymentChannelBepusdtConfigMigration(); err != nil {
		return err
	}
	if err := ensurePaymentFeePolicyMigration(); err != nil {
		return err
	}
	if err := ensureOrderRefundPaymentFeeMigration(); err != nil {
		return err
	}
	if err := ensureOrderItemOriginalPriceMigration(); err != nil {
		return err
	}
	if err := ensureCartForeignKeyConstraints(); err != nil {
		return err
	}
	if err := ensureProcurementOrderForeignKeyConstraint(); err != nil {
		return err
	}
	if db.Migrator().HasColumn(&productdomain.Product{}, "price_currency") {
		if err := db.Migrator().DropColumn(&productdomain.Product{}, "price_currency"); err != nil {
			return err
		}
	}
	if err := ensurePaymentProviderClaims(db); err != nil {
		return err
	}
	if err := ensureOrderAttemptLimitFloor(db); err != nil {
		return err
	}
	return nil
}

// backfillPendingOrderRiskIPs 只迁移仍可能占用库存的在途订单，避免启动时扫描全部历史订单。
func backfillPendingOrderRiskIPs(db *gorm.DB) error {
	type orderIPRow struct {
		ID       uint
		ClientIP string
	}
	var rows []orderIPRow
	if err := db.Model(&orderdomain.Order{}).
		Select("id", "client_ip").
		Where("deleted_at IS NULL AND status = ? AND (risk_ip IS NULL OR risk_ip = '') AND client_ip <> ''", constants.OrderStatusPendingPayment).
		Find(&rows).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			riskIP := orderriskcontract.NormalizeRiskIP(row.ClientIP)
			if riskIP == "" {
				continue
			}
			if err := tx.Model(&orderdomain.Order{}).
				Where("id = ? AND (risk_ip IS NULL OR risk_ip = '')", row.ID).
				UpdateColumn("risk_ip", riskIP).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ensurePaymentProviderClaims 为历史成功支付补上外部交易号占用。
// 同一商户下重复的交易号只保留最早的一笔，不删除其余支付，也不因历史重复让启动失败。
func ensurePaymentProviderClaims(db *gorm.DB) error {
	var payments []paymentdomain.Payment
	if err := db.Where("deleted_at IS NULL AND status = ? AND provider_ref <> ''", constants.PaymentStatusSuccess).
		Order("id asc").Find(&payments).Error; err != nil {
		return err
	}
	if len(payments) == 0 {
		return nil
	}
	var channels []paymentdomain.PaymentChannel
	if err := db.Find(&channels).Error; err != nil {
		return err
	}
	channelByID := make(map[uint]paymentdomain.PaymentChannel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	seen := make(map[string]struct{}, len(payments))
	return db.Transaction(func(tx *gorm.DB) error {
		for _, payment := range payments {
			ref := strings.TrimSpace(payment.ProviderRef)
			if ref == "" {
				continue
			}
			channel := channelByID[payment.ChannelID]
			provider := payment.ProviderType
			config := channel.ConfigJSON
			if strings.TrimSpace(channel.ProviderType) != "" {
				provider = channel.ProviderType
			}
			scope := paymentdomain.ProviderClaimScope(provider, config, payment.ChannelID)
			key := scope + "\n" + ref
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			var count int64
			if err := tx.Model(&paymentdomain.ProviderClaim{}).
				Where("scope_key = ? AND provider_ref = ?", scope, ref).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			claim := paymentdomain.ProviderClaim{
				ScopeKey:    scope,
				ProviderRef: ref,
				PaymentID:   payment.ID,
				OrderID:     payment.OrderID,
				ChannelID:   payment.ChannelID,
				CreatedAt:   time.Now(),
			}
			if err := tx.Create(&claim).Error; err != nil {
				if providerClaimConflict(err) {
					continue
				}
				return err
			}
		}
		return nil
	})
}

func providerClaimConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}

// ensureOrderAttemptLimitFloor 只在第一次启动时启用 60 秒 20 次、封禁 24 小时。
// 总风控开关保持原值。管理员之后关闭下单封禁时，不会在下次启动被重新打开。
func ensureOrderAttemptLimitFloor(db *gorm.DB) error {
	var marker settingsstore.SettingRecord
	err := db.Where("key = ?", constants.SettingKeyOrderAttemptLimitFloor).First(&marker).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var record settingsstore.SettingRecord
	err = db.Where("key = ?", constants.SettingKeyOrderRiskControlConfig).First(&record).Error
	if err == nil {
		cfg := settingssecurity.DecodeOrderRiskControlConfig(record.ValueJSON, settingssecurity.DefaultOrderRiskControlConfig())
		cfg.Common.AttemptLimit = settingssecurity.OrderRateLimitConfig{
			Enabled:       true,
			WindowSeconds: 60,
			MaxRequests:   20,
			BlockSeconds:  86400,
		}
		record.ValueJSON = settingssecurity.EncodeOrderRiskControlConfig(cfg)
		if err := db.Save(&record).Error; err != nil {
			return err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&settingsstore.SettingRecord{
		Key:       constants.SettingKeyOrderAttemptLimitFloor,
		ValueJSON: jsonmap.JSON{"applied": true},
	}).Error
}
