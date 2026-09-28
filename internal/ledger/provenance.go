package ledger

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode"
)

type auditContextKey struct{}
type auditProvenance struct{ source, operationKey, requestID, clientName, clientVersion string }

// WithMCPClient marks a server-dispatched MCP operation. Client strings are
// self-reported protocol metadata, not authenticated user identities.
func WithMCPClient(ctx context.Context, name, version string) context.Context {
	return context.WithValue(ctx, auditContextKey{}, auditProvenance{source: "mcp", clientName: boundedClientLabel(name, 200), clientVersion: boundedClientLabel(version, 100)})
}
func boundedClientLabel(value string, max int) string {
	value = strings.ToValidUTF8(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}

// Metadata follows the transaction rather than arbitrary request fields. Nested
// savepoints retain it, so every audit event from a bulk operation correlates.
type auditedTx struct {
	pgx.Tx
	provenance auditProvenance
}

func (t *auditedTx) Begin(ctx context.Context) (pgx.Tx, error) {
	nested, e := t.Tx.Begin(ctx)
	if e != nil {
		return nil, e
	}
	return &auditedTx{Tx: nested, provenance: t.provenance}, nil
}
