package middleware

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
)

// DirectEventWriter is the HTTP-infrastructure port for events emitted before
// TenantDB has installed a TxManager in the request context.
type DirectEventWriter interface {
	WriteDirect(ctx context.Context, pool *pgxpool.Pool, event eventlog.Event) error
}
