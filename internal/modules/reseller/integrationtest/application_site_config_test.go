package integrationtest

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	resellermodule "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/storeimage"
)

func TestResellerSiteConfigServiceUserUpdateRequiresActiveProfile(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-pending@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusPendingReview, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	_, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{SiteName: "Pending Store"})
	if !errors.Is(err, ErrResellerProfileInactive) {
		t.Fatalf("expected inactive profile error, got %v", err)
	}
}

func TestResellerSiteConfigServiceNormalizesAndStoresSafeFields(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-active@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	row, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{
		SiteName: "  Alice Store  ",
		Logo:     "/uploads/reseller/logo.png",
		Favicon:  "https://cdn.example.test/favicon.png",
		Announcement: ResellerAnnouncementInput{
			Enabled: true,
			Type:    "info",
			Title:   LocalizedTextInput{"zh-CN": "公告", "fr-FR": "drop me"},
			Content: LocalizedTextInput{"zh-CN": "欢迎"},
		},
		Support: ResellerSupportInput{
			Telegram:   "https://t.me/alice",
			Email:      "support@example.test",
			SupportURL: "mailto:support@example.test",
		},
		SEO: ResellerSEOInput{
			Title:       LocalizedTextInput{"zh-CN": "爱丽丝商店", "zh-TW": "愛麗絲商店", "en-US": "Alice Store"},
			Description: LocalizedTextInput{"zh-CN": "精选商品", "en-US": "Curated products"},
		},
	})
	if err != nil {
		t.Fatalf("update site config failed: %v", err)
	}
	if row.SiteName != "Alice Store" || row.Logo != "/uploads/reseller/logo.png" || row.Favicon != "https://cdn.example.test/favicon.png" {
		t.Fatalf("unexpected normalized row: %+v", row)
	}
	if row.SupportJSON["telegram"] != "https://t.me/alice" || row.SupportJSON["email"] != "support@example.test" {
		t.Fatalf("unexpected support json: %+v", row.SupportJSON)
	}
	announcementTitle := row.AnnouncementJSON["title"].(jsonmap.JSON)
	if _, exists := announcementTitle["fr-FR"]; exists {
		t.Fatalf("unexpected unsupported locale retained: %+v", announcementTitle)
	}
	if len(row.ThemeJSON) != 0 {
		t.Fatalf("expected theme config to be ignored, got: %+v", row.ThemeJSON)
	}
}

func TestResellerSiteConfigServiceRejectsUnsafeURLs(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-unsafe@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	_, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{
		SiteName: "Unsafe Store",
		Support:  ResellerSupportInput{SupportURL: "javascript:alert(1)"},
	})
	if !errors.Is(err, ErrResellerSiteConfigInvalid) {
		t.Fatalf("expected invalid site config error, got %v", err)
	}
}

func TestResellerSiteConfigServiceReturnsFieldErrorForInvalidSupport(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-field@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	_, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{
		SiteName: "Field Store",
		Support:  ResellerSupportInput{WhatsApp: "https://w.me/123"},
	})
	if !errors.Is(err, ErrResellerSiteConfigInvalid) {
		t.Fatalf("expected invalid site config error, got %v", err)
	}
	var fieldErr *ResellerSiteConfigFieldError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("expected field error, got %v", err)
	}
	if fieldErr.Field != "support_whatsapp" {
		t.Fatalf("expected support_whatsapp field, got %q", fieldErr.Field)
	}
}

func TestNormalizeResellerSupportAcceptsTelegramMe(t *testing.T) {
	support, err := resellermodule.NormalizeResellerSupport(ResellerSupportInput{Telegram: "https://telegram.me/alice"})
	if err != nil {
		t.Fatalf("expected telegram.me link to be accepted, got %v", err)
	}
	if support["telegram"] != "https://telegram.me/alice" {
		t.Fatalf("unexpected support json: %+v", support)
	}
}

func TestResellerSiteConfigServiceApplyPublicConfigOverlay(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-overlay@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	if _, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{
		SiteName: "Overlay Store",
		Favicon:  "/uploads/reseller/favicon.png",
		Support:  ResellerSupportInput{Email: "help@shop.example.test", SupportURL: "https://shop.example.test/support"},
		SEO: ResellerSEOInput{
			Title: LocalizedTextInput{"zh-CN": "覆盖标题", "en-US": "Overlay Title"},
		},
	}); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	resellerID := profile.ID
	base := map[string]interface{}{
		"brand": map[string]interface{}{
			"site_name": "Main Store",
			"site_icon": "/dj.svg",
			"site_url":  "https://main.example.test",
		},
		"currency": "CNY",
		"contact": map[string]interface{}{
			"telegram": "https://t.me/main",
			"whatsapp": "https://wa.me/1234567890",
		},
		"seo": map[string]interface{}{
			"title": map[string]interface{}{"zh-CN": "主站标题", "en-US": "Main Title"},
		},
		"announcement": map[string]interface{}{"enabled": true},
		"nav_config":   map[string]interface{}{"builtin": map[string]interface{}{"blog": false}},
		"scripts":      []interface{}{map[string]interface{}{"name": "main-analytics"}},
	}
	tenant := ResellerTenantContext("shop.example.test", resellerID, user.ID, "shop.example.test")
	out, err := svc.ApplyPublicConfigOverlay(context.Background(), tenant, base)
	if err != nil {
		t.Fatalf("apply overlay failed: %v", err)
	}
	brand := out["brand"].(map[string]interface{})
	if brand["site_name"] != "Overlay Store" || brand["site_icon"] != "/uploads/reseller/favicon.png" {
		t.Fatalf("unexpected brand overlay: %+v", brand)
	}
	if brand["site_url"] != "https://shop.example.test" {
		t.Fatalf("reseller brand must not inherit the main site URL: %+v", brand)
	}
	if out["currency"] != "CNY" {
		t.Fatalf("global inherited fields should remain, got currency=%v", out["currency"])
	}
	if scripts, _ := out["scripts"].([]interface{}); len(scripts) != 0 {
		t.Fatalf("main-site custom scripts must not run on reseller sites, got %+v", out["scripts"])
	}
	contact := resellerSiteConfigTestMap(out["contact"])
	if contact["email"] != "help@shop.example.test" || contact["support_url"] != "https://shop.example.test/support" {
		t.Fatalf("reseller contact fields should be emitted, got %+v", contact)
	}
	if _, exists := contact["telegram"]; exists {
		t.Fatalf("empty reseller contact must not fall back to main site, got %+v", contact)
	}
	if _, exists := contact["whatsapp"]; exists {
		t.Fatalf("empty reseller contact must not fall back to main site, got %+v", contact)
	}
	seo := resellerSiteConfigTestMap(out["seo"])
	title := resellerSiteConfigTestMap(seo["title"])
	if title["zh-CN"] != "覆盖标题" || title["en-US"] != "Overlay Title" {
		t.Fatalf("unexpected localized seo overlay: %+v", seo)
	}
	if _, exists := out["announcement"]; exists {
		t.Fatalf("disabled reseller announcement should remove the field, got %+v", out["announcement"])
	}
	if nav := resellerSiteConfigTestMap(out["nav_config"]); nav["builtin"] == nil {
		t.Fatalf("saved reseller config should intentionally own nav defaults, got %+v", nav)
	}
	tenantPayload := out["tenant"].(map[string]interface{})
	if tenantPayload["mode"] != "reseller" || tenantPayload["host"] != "shop.example.test" {
		t.Fatalf("unexpected tenant payload: %+v", tenantPayload)
	}
}

func TestResellerSiteConfigServiceOverlayEmitsActiveAnnouncement(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	user := seedResellerManagementUser(t, db, "site-config-announcement@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile failed: %v", err)
	}
	svc := NewResellerSiteConfigService(repo)
	if _, err := svc.UpdateUserSiteConfig(context.Background(), user.ID, ResellerSiteConfigInput{
		SiteName: "Announcement Store",
		Announcement: ResellerAnnouncementInput{
			Enabled: true,
			Type:    "success",
			Title:   LocalizedTextInput{"zh-CN": "测试"},
			Content: LocalizedTextInput{"zh-CN": "<p>测试测试</p>"},
		},
	}); err != nil {
		t.Fatalf("save config failed: %v", err)
	}
	tenant := ResellerTenantContext("shop.example.test", profile.ID, user.ID, "shop.example.test")
	out, err := svc.ApplyPublicConfigOverlay(context.Background(), tenant, map[string]interface{}{
		"announcement": map[string]interface{}{"type": "info", "version": "main0000"},
	})
	if err != nil {
		t.Fatalf("apply overlay failed: %v", err)
	}
	announcement := resellerSiteConfigTestMap(out["announcement"])
	if announcement == nil {
		t.Fatalf("enabled reseller announcement should be emitted, got %+v", out["announcement"])
	}
	if announcement["type"] != "success" {
		t.Fatalf("unexpected announcement type: %+v", announcement)
	}
	if _, exists := announcement["enabled"]; exists {
		t.Fatalf("public announcement should not expose enabled flag, got %+v", announcement)
	}
	version, _ := announcement["version"].(string)
	if len(version) != 8 || version == "main0000" {
		t.Fatalf("expected reseller-derived version fingerprint, got %q", version)
	}
	content := resellerSiteConfigTestMap(announcement["content"])
	if content["zh-CN"] != "<p>测试测试</p>" {
		t.Fatalf("unexpected announcement content: %+v", content)
	}
}

func TestResellerWallpaperStaysInsideItsOwnStore(t *testing.T) {
	root := t.TempDir()
	prev := storeimage.Root
	storeimage.Root = root
	t.Cleanup(func() { storeimage.Root = prev })

	mainPath := "/uploads/common/2026/11/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.png"
	alicePath := "/uploads/reseller/2026/11/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb.png"
	bobPath := "/uploads/reseller/2026/11/cccccccc-cccc-4ccc-8ccc-cccccccccccc.png"
	writeResellerWallpaperPNG(t, root, mainPath)
	writeResellerWallpaperPNG(t, root, alicePath)
	writeResellerWallpaperPNG(t, root, bobPath)

	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	aliceUser := seedResellerManagementUser(t, db, "wallpaper-alice@example.test")
	bobUser := seedResellerManagementUser(t, db, "wallpaper-bob@example.test")
	alice := resellerdomain.Profile{UserID: aliceUser.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	bob := resellerdomain.Profile{UserID: bobUser.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewResellerSiteConfigService(repo)
	if _, err := svc.UpdateUserSiteConfig(context.Background(), aliceUser.ID, ResellerSiteConfigInput{
		SiteName:  "Alice",
		Wallpaper: alicePath,
	}); err != nil {
		t.Fatalf("save alice wallpaper failed: %v", err)
	}
	if _, err := svc.UpdateUserSiteConfig(context.Background(), bobUser.ID, ResellerSiteConfigInput{
		SiteName:  "Bob",
		Wallpaper: bobPath,
	}); err != nil {
		t.Fatalf("save bob wallpaper failed: %v", err)
	}

	base := map[string]interface{}{
		"brand": map[string]interface{}{
			"site_name": "Main",
			"wallpaper": mainPath,
		},
	}
	aliceOut, err := svc.ApplyPublicConfigOverlay(context.Background(), ResellerTenantContext("alice.example.test", alice.ID, aliceUser.ID, "alice.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	bobOut, err := svc.ApplyPublicConfigOverlay(context.Background(), ResellerTenantContext("bob.example.test", bob.ID, bobUser.ID, "bob.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	mainOut, err := svc.ApplyPublicConfigOverlay(context.Background(), resellercontract.MainTenantContext("shop.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	if aliceOut["brand"].(map[string]interface{})["wallpaper"] != alicePath {
		t.Fatalf("alice store should use her wallpaper, got %+v", aliceOut["brand"])
	}
	if bobOut["brand"].(map[string]interface{})["wallpaper"] != bobPath {
		t.Fatalf("bob store should use his wallpaper, got %+v", bobOut["brand"])
	}
	if mainOut["brand"].(map[string]interface{})["wallpaper"] != mainPath {
		t.Fatalf("main store changed, got %+v", mainOut["brand"])
	}
	if base["brand"].(map[string]interface{})["wallpaper"] != mainPath {
		t.Fatalf("overlay mutated the main config: %+v", base["brand"])
	}

	if _, err := svc.UpdateUserSiteConfig(context.Background(), aliceUser.ID, ResellerSiteConfigInput{
		SiteName:  "Alice",
		Wallpaper: "https://evil.example.test/a.jpg",
	}); err == nil {
		t.Fatal("remote wallpaper was accepted")
	}
	var fieldErr *ResellerSiteConfigFieldError
	_, err = svc.UpdateUserSiteConfig(context.Background(), aliceUser.ID, ResellerSiteConfigInput{
		SiteName:  "Alice",
		Wallpaper: "/uploads/common/2026/11/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.svg",
	})
	if !errors.As(err, &fieldErr) || fieldErr.Field != "wallpaper" {
		t.Fatalf("expected wallpaper rejection, got %v", err)
	}
	_, row, _, err := svc.GetUserSiteConfig(aliceUser.ID)
	if err != nil || row == nil || row.Wallpaper != alicePath {
		t.Fatalf("failed replace lost alice wallpaper: %+v err %v", row, err)
	}
	_, bobRow, _, err := svc.GetUserSiteConfig(bobUser.ID)
	if err != nil || bobRow == nil || bobRow.Wallpaper != bobPath {
		t.Fatalf("alice update changed bob wallpaper: %+v err %v", bobRow, err)
	}

	if _, err := svc.UpdateUserSiteConfig(context.Background(), bobUser.ID, ResellerSiteConfigInput{SiteName: "Bob"}); err != nil {
		t.Fatalf("clear bob wallpaper failed: %v", err)
	}
	inherited, err := svc.ApplyPublicConfigOverlay(context.Background(), ResellerTenantContext("bob.example.test", bob.ID, bobUser.ID, "bob.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	if inherited["brand"].(map[string]interface{})["wallpaper"] != mainPath {
		t.Fatalf("cleared reseller wallpaper should inherit the main site, got %+v", inherited["brand"])
	}
	aliceAgain, err := svc.ApplyPublicConfigOverlay(context.Background(), ResellerTenantContext("alice.example.test", alice.ID, aliceUser.ID, "alice.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	if aliceAgain["brand"].(map[string]interface{})["wallpaper"] != alicePath {
		t.Fatalf("alice wallpaper changed after bob cleared his: %+v", aliceAgain["brand"])
	}
}

func writeResellerWallpaperPNG(t *testing.T, root, publicPath string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(publicPath, "/uploads/")))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
}

func resellerSiteConfigTestMap(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case jsonmap.JSON:
		return map[string]interface{}(typed)
	case map[string]interface{}:
		return typed
	default:
		return nil
	}
}
