# Dashboard development

The OLED dashboard is a read-only TanStack Start SPA. Go owns all financial calculations. In bearer-mode development, the access token is entered at runtime and kept only in React memory; do not put it in Vite environment variables, URLs, or browser storage.

1. In `web/`, run `npm ci`.
2. Run `npm run dev` while Go listens at `127.0.0.1:8080`; Vite proxies API routes; use the Go-served built UI to verify the complete authentication flow.
3. Run `npm run build`, then `npm run typecheck` (build generates the route tree).
4. Serve `dist/client` with Go, falling back to `index.html` for UI routes only. Never rewrite API/MCP failures to the SPA.
5. Check locked, rejected-token, empty, populated, refreshed and re-locked states at laptop and phone widths. Locking clears the query cache and token; reload also locks.

The server and dashboard must share one origin in production. Static output contains only the generic shell. Bearer mode uses an Authorization header; Tailscale mode discovers identity at /auth/session and does not prompt for a token. Verify the chosen mode.

## API contract

Generate `openapi.json` from the Go application's `openapi` command at repository root, then run `npm run generate:api` here. Commit both the schema and generated `src/lib/api.gen.ts`. The dashboard uses `openapi-fetch` with this generated contract. Regenerate and typecheck after reporting API changes; CI should fail if generation produces an uncommitted diff.

## Visual direction

True black (`#000000`) canvas for OLED, graphite (`#151719`) surfaces, ivory (`#eeeae2`) text, muted gray (`#959b9f`) secondary text and pale blue (`#d8e6fc`) controls. Georgia headings soften the ledger's tabular monospace amounts; Avenir/system text keeps the interface light. Account cards preserve separate currency balances; expandable ledger entries reveal category/project allocations without cluttering the phone layout. No external fonts, illustrations or analytics requests.

## Container prerendering

Vite preview binds to `127.0.0.1` explicitly. TanStack Start uses its preview server to prerender the static SPA shell; an ambiguous localhost IPv4/IPv6 resolution caused Linux container builds to fetch a different loopback address and fail. Keep static prerendering enabled. Verify packaging from the repository root with `docker build -t pa-mcp:poc .`.

Dashboard queries become stale after 15 seconds and refetch when the window regains focus. Explicit Refresh always requests current data.
