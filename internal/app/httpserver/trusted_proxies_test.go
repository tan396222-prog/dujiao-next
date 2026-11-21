package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/app/httpserver/middleware"
	orderriskhttp "github.com/dujiao-next/internal/modules/orderrisk/transport/http"

	"github.com/gin-gonic/gin"
)

func TestConfigureTrustedProxiesRejectsSpoofedForwardedIPFromUntrustedPeer(t *testing.T) {
	engine := gin.New()
	if err := configureTrustedProxies(engine, nil); err != nil {
		t.Fatal(err)
	}
	engine.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	if resp.Body.String() != "203.0.113.9" {
		t.Fatalf("untrusted peer spoofed client IP: %q", resp.Body.String())
	}
}

func TestConfigureTrustedProxiesAcceptsForwardedIPFromConfiguredProxy(t *testing.T) {
	engine := gin.New()
	if err := configureTrustedProxies(engine, []string{"127.0.0.1/32"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	if resp.Body.String() != "1.2.3.4" {
		t.Fatalf("trusted proxy client IP=%q, want 1.2.3.4", resp.Body.String())
	}
}

func TestConfigureTrustedProxiesRejectsInvalidCIDR(t *testing.T) {
	if err := configureTrustedProxies(gin.New(), []string{"not-a-cidr"}); err == nil {
		t.Fatal("expected invalid trusted proxy configuration to fail")
	}
}

func TestConfigureTrustedProxiesIgnoresClientSuppliedRealIPAndCloudflareHeader(t *testing.T) {
	engine := gin.New()
	if err := configureTrustedProxies(engine, []string{"127.0.0.1/32", "::1/128"}); err != nil {
		t.Fatal(err)
	}
	engine.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	cases := []struct {
		name   string
		remote string
		header string
		value  string
		want   string
	}{
		{name: "x-real-ip", remote: "127.0.0.1:1234", header: "X-Real-IP", value: "198.51.100.8", want: "127.0.0.1"},
		{name: "cf-connecting-ip", remote: "127.0.0.1:1234", header: "CF-Connecting-IP", value: "198.51.100.9", want: "127.0.0.1"},
		{name: "forwarded chain", remote: "127.0.0.1:1234", header: "X-Forwarded-For", value: "198.51.100.9, 203.0.113.10", want: "203.0.113.10"},
		{name: "ipv4-mapped", remote: "127.0.0.1:1234", header: "X-Forwarded-For", value: "::ffff:203.0.113.11", want: "::ffff:203.0.113.11"},
		{name: "ipv6", remote: "[::1]:1234", header: "X-Forwarded-For", value: "2001:db8:abcd:12::10", want: "2001:db8:abcd:12::10"},
		{name: "untrusted cf", remote: "203.0.113.9:1234", header: "CF-Connecting-IP", value: "198.51.100.9", want: "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set(tc.header, tc.value)
			resp := httptest.NewRecorder()
			engine.ServeHTTP(resp, req)
			if resp.Body.String() != tc.want {
				t.Fatalf("client IP=%q, want %q", resp.Body.String(), tc.want)
			}
		})
	}
}

func TestOrderRiskUnbanIsNotPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	engine := gin.New()
	admin := engine.Group("/api/v1/admin")
	admin.Use(middleware.JWTAuthMiddleware("test-secret", nil))
	admin.POST("/order-risk/attempts/unban", func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/order-risk/attempts/unban", strings.NewReader(`{"ip":"203.0.113.8"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.8:1234"
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	if called {
		t.Fatal("unauthenticated unban reached the handler")
	}
	var body struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status_code=%d body=%s", body.StatusCode, resp.Body.String())
	}
}

func TestOrderRiskClientIdentityIgnoresQueryIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := configureTrustedProxies(engine, []string{"127.0.0.1/32"}); err != nil {
		t.Fatal(err)
	}
	orderriskhttp.RegisterAdminRoutes(engine, orderriskhttp.NewAdminHandler(nil))
	req := httptest.NewRequest(http.MethodGet, "/order-risk/client-ip?ip=198.51.100.77", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.15")
	req.Header.Set("CF-Connecting-IP", "198.51.100.77")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		Data struct {
			IP     string `json:"ip"`
			RiskIP string `json:"risk_ip"`
			Source string `json:"source"`
			Peer   string `json:"peer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.IP != "203.0.113.15" || body.Data.RiskIP != "203.0.113.15" || body.Data.Source != "forwarded" || body.Data.Peer != "127.0.0.1" {
		t.Fatalf("identity=%+v body=%s", body.Data, raw)
	}
}

func TestConfigureTrustedProxiesRejectsTrustAllNetworks(t *testing.T) {
	for _, network := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(network, func(t *testing.T) {
			if err := configureTrustedProxies(gin.New(), []string{network}); err == nil {
				t.Fatalf("expected %s to be rejected", network)
			}
		})
	}
}
