// poc-example records the documented sample through MCP in a disposable database.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	token := os.Getenv("PA_MCP_TOKEN")
	if token == "" {
		return fmt.Errorf("PA_MCP_TOKEN required")
	}
	endpoint := os.Getenv("PA_MCP_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080/mcp"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "poc-example", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearer{token}}}, nil)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	call := func(name string, args map[string]any) (map[string]any, error) {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			return nil, e
		}
		if r.IsError {
			return nil, fmt.Errorf("%s failed: %v", name, r.Content)
		}
		raw, e := json.Marshal(r.StructuredContent)
		if e != nil {
			return nil, e
		}
		var out map[string]any
		e = json.Unmarshal(raw, &out)
		return out, e
	}
	account, e := call("create_account", map[string]any{"operation_key": "poc-example-account-v1", "name": "POC · RON", "currency": "RON", "opening_amount": "5000.00", "opening_date": "2026-09-01"})
	if e != nil {
		return e
	}
	suspension, e := call("create_category", map[string]any{"operation_key": "poc-example-suspension-v1", "name": "Suspension"})
	if e != nil {
		return e
	}
	misc, e := call("create_category", map[string]any{"operation_key": "poc-example-misc-v1", "name": "Miscellaneous"})
	if e != nil {
		return e
	}
	project, e := call("create_project", map[string]any{"operation_key": "poc-example-e30-v1", "name": "E30"})
	if e != nil {
		return e
	}
	_, e = call("create_expense", map[string]any{"operation_key": "poc-example-expense-v1", "account_id": account["id"], "currency": "RON", "amount": "430.00", "date": "2026-09-27", "description": "E30 parts", "allocations": []map[string]any{{"amount": "250.00", "category_id": suspension["id"], "project_id": project["id"]}, {"amount": "180.00", "category_id": misc["id"], "project_id": project["id"]}}})
	if e != nil {
		return e
	}
	dashboard, e := call("get_dashboard", map[string]any{})
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(dashboard)
}
