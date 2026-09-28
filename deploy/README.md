# Private deployment example

The Compose example targets a dedicated Linux host with Docker host networking. It does not provision infrastructure or expose a public endpoint.

Set PA_MCP_IMAGE in a private Compose .env file to an immutable tested image. Create .env.runtime outside Git with DATABASE_URL for a restricted runtime role and:

```dotenv
PA_MCP_AUTH_MODE=tailscale
PA_MCP_ALLOWED_LOGIN=your-login@example.com
PA_MCP_TRUSTED_PROXY=127.0.0.1
PA_MCP_PUBLIC_ORIGIN=https://your-server.your-tailnet.ts.net
```

Configure Tailscale Serve to proxy HTTPS to http://127.0.0.1:8080. Do not enable Funnel. The app verifies the immediate proxy, exact login and canonical Host/Origin. Do not add bearer fallback or expose the backend listener. Local processes and host-network containers are trusted in this arrangement; keep untrusted workloads off the host. Tagged machine clients need a separately designed policy.

Apply migrations explicitly with a separate owner identity and then scripts/grant-runtime.sql with the chosen runtime_role. Never provide owner credentials to the running service. Protect env files with restrictive filesystem permissions.

Run docker compose up -d --wait. Verify health and authenticated UI/API/MCP access; verify missing identity, spoofed headers, wrong origins and direct network access are rejected. Follow RUNBOOK.md for backup and rollback. Publication of this source does not make any deployment public.
