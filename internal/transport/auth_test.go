package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestServeIdentityBoundary(t *testing.T) {
	c := AuthConfig{Mode: "tailscale", AllowedLogin: "alex@example.com", TrustedProxy: "127.0.0.1", PublicOrigin: "https://ledger.example.ts.net", Token: testToken}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("private shell"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := HandlerWithAuth(ledger.New(nil), c, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*http.Request)
		want   int
	}{
		{"valid", func(r *http.Request) {}, 200},
		{"missing", func(r *http.Request) { r.Header.Del("Tailscale-User-Login") }, 403},
		{"other user", func(r *http.Request) { r.Header.Set("Tailscale-User-Login", "other@example.com") }, 403},
		{"duplicate", func(r *http.Request) { r.Header.Add("Tailscale-User-Login", c.AllowedLogin) }, 403},
		{"combined", func(r *http.Request) { r.Header.Set("Tailscale-User-Login", c.AllowedLogin+", "+c.AllowedLogin) }, 403},
		{"untrusted peer", func(r *http.Request) { r.RemoteAddr = "172.18.0.5:54321" }, 403},
		{"invalid peer", func(r *http.Request) { r.RemoteAddr = "127.0.0.1" }, 403},
		{"forwarded spoof", func(r *http.Request) { r.RemoteAddr = "172.18.0.5:54321"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }, 403},
		{"host", func(r *http.Request) { r.Host = "evil.example" }, 403},
		{"forwarded host", func(r *http.Request) {
			r.Host = "evil.example"
			r.Header.Set("X-Forwarded-Host", "ledger.example.ts.net")
		}, 403},
		{"same origin", func(r *http.Request) { r.Header.Set("Origin", c.PublicOrigin) }, 200},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"spoofed fetch metadata", func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Origin", "https://evil.example")
		}, 403},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"empty origin", func(r *http.Request) { r.Header["Origin"] = []string{""} }, 403},
		{"duplicate origin", func(r *http.Request) { r.Header["Origin"] = []string{c.PublicOrigin, c.PublicOrigin} }, 403},
		{"no bearer fallback", func(r *http.Request) {
			r.Header.Del("Tailscale-User-Login")
			r.Header.Set("Authorization", "Bearer "+testToken)
		}, 403},
	}
	for _, path := range []string{"/", "/auth/session", "/openapi.json"} {
		for _, tc := range cases {
			t.Run(path+tc.name, func(t *testing.T) {
				r := httptest.NewRequestWithContext(t.Context(), "GET", c.PublicOrigin+path, nil)
				r.RemoteAddr = "127.0.0.1:54321"
				r.Header.Set("Tailscale-User-Login", c.AllowedLogin)
				tc.change(r)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != tc.want {
					t.Fatalf("status %d: %s", w.Code, w.Body.String())
				}
			})
		}
	}
	for _, path := range []string{"/api/dashboard", "/mcp", "/assets/app.js"} {
		r := httptest.NewRequestWithContext(t.Context(), "POST", c.PublicOrigin+path, nil)
		r.RemoteAddr = "127.0.0.1:54321"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "http://127.0.0.1/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("health unavailable")
	}
}

func TestAuthConfiguration(t *testing.T) {
	base := AuthConfig{Mode: "tailscale", AllowedLogin: "alex@example.com", TrustedProxy: "127.0.0.1", PublicOrigin: "https://ledger.example.ts.net"}
	for _, change := range []func(*AuthConfig){func(c *AuthConfig) { c.Mode = "unknown" }, func(c *AuthConfig) { c.AllowedLogin = "" }, func(c *AuthConfig) { c.AllowedLogin = "alex,other" }, func(c *AuthConfig) { c.TrustedProxy = "127.0.0.0/8" }, func(c *AuthConfig) { c.PublicOrigin = "http://ledger.example.ts.net" }, func(c *AuthConfig) { c.PublicOrigin += "/" }, func(c *AuthConfig) { c.PublicOrigin += "?" }, func(c *AuthConfig) { c.PublicOrigin = "https://user@ledger.example.ts.net" }} {
		c := base
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
	}
	h, err := Handler(ledger.New(nil), testToken, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/auth/session", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"bearer"`) {
		t.Fatal("bearer discovery failed")
	}
}

func TestServeIdentityMCP(t *testing.T) {
	pool := integrationPool(t)
	cfg := AuthConfig{Mode: "tailscale", AllowedLogin: "alex@example.com", TrustedProxy: "127.0.0.1", PublicOrigin: "https://ledger.example.ts.net"}
	handler, err := HandlerWithAuth(ledger.New(pool), cfg, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate Serve replacing identity headers after authenticating a tailnet peer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "ledger.example.ts.net"
		r.Header.Set("Tailscale-User-Login", cfg.AllowedLogin)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "serve-auth-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(t.Context(), nil)
	if err != nil || len(result.Tools) == 0 {
		t.Fatalf("authenticated tools unavailable: %v", err)
	}
}
