package providerpolicy

import "context"

type bulkKey struct{}

// WithBulk marks a request context as bulk work: history, full blocks, logs
// and receipts of whole blocks. Admission layers below the chain adapter,
// such as an integration pool, use the mark to keep urgent reads ahead.
func WithBulk(ctx context.Context, bulk bool) context.Context {
	if !bulk {
		return ctx
	}
	return context.WithValue(ctx, bulkKey{}, true)
}

// IsBulk reports whether ctx carries the bulk mark of WithBulk.
func IsBulk(ctx context.Context) bool {
	bulk, _ := ctx.Value(bulkKey{}).(bool)
	return bulk
}
