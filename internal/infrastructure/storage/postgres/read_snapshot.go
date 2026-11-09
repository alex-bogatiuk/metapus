package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func ReadSnapshot(ctx context.Context, read func(context.Context) error) error {
	options := DefaultTxOptions()
	options.IsolationLevel = pgx.RepeatableRead
	return MustGetTxManager(ctx).RunInTransactionWithOptions(ctx, options, read)
}
