package transport

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// AuthConfig defines a single authentication authority, never a fallback chain.
// In tailscale mode the immediate proxy is trusted to remove and replace identity
// headers. Access to that proxy's backend listener must be restricted externally.
type AuthConfig struct {
	Mode         string
	Token        string
	AllowedLogin string
	TrustedProxy string
	PublicOrigin string
}

func (c AuthConfig) Validate() error {
	switch c.Mode {
	case "bearer":
		if strings.TrimSpace(c.Token) == "" {
			return fmt.Errorf("PA_MCP_TOKEN is required in bearer mode")
		}
	case "tailscale":
		if c.AllowedLogin == "" || strings.TrimSpace(c.AllowedLogin) != c.AllowedLogin || strings.ContainsAny(c.AllowedLogin, ",\r\n\t ") {
			return fmt.Errorf("PA_MCP_ALLOWED_LOGIN must be one exact login")
		}
		if _, err := netip.ParseAddr(c.TrustedProxy); err != nil {
			return fmt.Errorf("PA_MCP_TRUSTED_PROXY must be one IP address")
		}
		origin, err := url.Parse(c.PublicOrigin)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.Opaque != "" || origin.String() != c.PublicOrigin {
			return fmt.Errorf("PA_MCP_PUBLIC_ORIGIN must be an HTTPS origin without path, credentials, query or fragment")
		}
	default:
		return fmt.Errorf("PA_MCP_AUTH_MODE must be bearer or tailscale")
	}
	return nil
}

func (c AuthConfig) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if c.Mode == "bearer" {
			expected := sha256.Sum256([]byte("Bearer " + c.Token))
			actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
			if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			peer, parseErr := netip.ParseAddr(host)
			expected, _ := netip.ParseAddr(c.TrustedProxy)
			origin, _ := url.Parse(c.PublicOrigin)
			logins := r.Header.Values("Tailscale-User-Login")
			origins := r.Header.Values("Origin")
			if err != nil || parseErr != nil || peer != expected || r.Host != origin.Host || len(logins) != 1 || logins[0] != c.AllowedLogin || len(origins) > 1 || (len(origins) == 1 && origins[0] != c.PublicOrigin) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
