package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/alexcatdad/personal-ledger-mcp/internal/transport"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 4 * time.Second}
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:8080/healthz", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return errors.New("health endpoint unavailable")
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return errors.New("health endpoint unhealthy")
		}
		return nil
	}
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		api := transport.API(http.NewServeMux(), nil)
		return json.NewEncoder(os.Stdout).Encode(api.OpenAPI())
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		db, err := sql.Open("pgx", url)
		if err != nil {
			return errors.New("invalid database configuration")
		}
		defer func() { _ = db.Close() }()
		if err = goose.SetDialect("postgres"); err != nil {
			return err
		}
		return goose.Up(db, "migrations")
	}
	authConfig := transport.AuthConfig{Mode: os.Getenv("PA_MCP_AUTH_MODE"), Token: os.Getenv("PA_MCP_TOKEN"), AllowedLogin: os.Getenv("PA_MCP_ALLOWED_LOGIN"), TrustedProxy: os.Getenv("PA_MCP_TRUSTED_PROXY"), PublicOrigin: os.Getenv("PA_MCP_PUBLIC_ORIGIN")}
	if authConfig.Mode == "" {
		authConfig.Mode = "bearer"
	}
	if err := authConfig.Validate(); err != nil {
		return err
	}
	addr := os.Getenv("PA_MCP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if err := validateListenAddress(authConfig, addr); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	config.MaxConns = 5
	config.MinConns = 0
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("cannot create database pool")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("database unavailable")
	}
	dir := os.Getenv("PA_MCP_WEB_DIR")
	if dir == "" {
		dir = "web/dist/client"
	}
	handler, err := transport.HandlerWithAuth(ledger.New(pool), authConfig, dir, pool.Ping)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { slog.Info("listening", "address", addr); done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP server: %w", err)
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func validateListenAddress(c transport.AuthConfig, addr string) error {
	if c.Mode != "tailscale" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	ip, parseErr := netip.ParseAddr(host)
	proxy, proxyErr := netip.ParseAddr(c.TrustedProxy)
	if err != nil || parseErr != nil || !ip.IsLoopback() || proxyErr != nil || !proxy.IsLoopback() {
		return errors.New("tailscale mode requires loopback listener and trusted proxy")
	}
	return nil
}
