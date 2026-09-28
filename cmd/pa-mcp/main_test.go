package main

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/transport"
	"testing"
)

func TestTailscaleListenBoundary(t *testing.T) {
	for _, tc := range []struct {
		addr, proxy string
		valid       bool
	}{
		{"127.0.0.1:8080", "127.0.0.1", true},
		{"[::1]:8080", "::1", true},
		{":8080", "127.0.0.1", false},
		{"0.0.0.0:8080", "127.0.0.1", false},
		{"localhost:8080", "127.0.0.1", false},
		{"127.0.0.1:8080", "172.18.0.1", false},
	} {
		err := validateListenAddress(transport.AuthConfig{Mode: "tailscale", TrustedProxy: tc.proxy}, tc.addr)
		if (err == nil) != tc.valid {
			t.Errorf("%s/%s: %v", tc.addr, tc.proxy, err)
		}
	}
	if err := validateListenAddress(transport.AuthConfig{Mode: "bearer"}, ":8080"); err != nil {
		t.Fatal(err)
	}
}
