# Connecting an MCP client

The Streamable HTTP endpoint is `/mcp`. Configure the URL and credentials in your client's MCP settings.

For local bearer-mode development, use `http://127.0.0.1:8080/mcp` and supply Authorization from a secret environment variable. Never commit a token or put it in a URL. The client must run where it can reach that address.

For a private Tailscale deployment, use your own Serve HTTPS hostname with `/mcp`. An allowed user-owned Tailscale device can authenticate through Serve without a bearer token. A remote cloud client does not automatically inherit your laptop's network or identity. Verify actual connectivity from the client; do not infer it from browser access.

Start with get_dashboard, a read-only operation. Tool availability in a chat, MCP transport connectivity and successful domain mutations are separate checks.
