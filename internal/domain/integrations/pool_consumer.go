package integrations

import (
	"context"

	"metapus/internal/core/id"
)

// PoolConsumer is a catalog that reads accounts through integration pools,
// such as a configuration's RPC endpoints. Pool and account writes consult
// every consumer under the integration configuration lock, so a pool never
// loses an account key, or disappears, while something still reads through it.
type PoolConsumer interface {
	// UsesPool reports whether an active record still reads through the pool.
	UsesPool(ctx context.Context, poolID id.ID) (bool, error)
	// ValidatePool checks a pool's new member list against the consumer.
	ValidatePool(ctx context.Context, pool *Pool) error
	// ValidateAccount checks an account's new credentials against the
	// consumers of every pool that contains it.
	ValidateAccount(ctx context.Context, account *Account) error
}
