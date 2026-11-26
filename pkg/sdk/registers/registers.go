// Package registers stores accumulation register movements in PostgreSQL.
package registers

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
)

// AccumulationStore keeps the movements and balances of declarative
// accumulation registers in PostgreSQL. The registry binds registered
// registers to it; a test that posts without the server binds them itself.
type AccumulationStore = core.AccumulationStore

// NewAccumulationStore creates the store of declarative registers.
func NewAccumulationStore() *AccumulationStore { return core.NewAccumulationStore() }

// InformationStore keeps the records of declarative information registers
// in PostgreSQL; the registry binds registered registers to it.
type InformationStore = core.InformationStore

// NewInformationStore creates the store of information registers.
func NewInformationStore() *InformationStore { return core.NewInformationStore() }

type (
	// BaseAccumulationRepo writes a register's movements (COPY inside the
	// posting transaction) and deletes a document's earlier versions. Embed
	// it in the register's repository and add its own queries.
	BaseAccumulationRepo[T any] = core.BaseAccumulationRepo[T]
	// MovementRowMapper flattens a movement into its columns' values.
	MovementRowMapper[T any] = core.MovementRowMapper[T]
)

// NewBaseAccumulationRepo returns the repository of the movements table with
// columns, in the order mapper returns their values.
func NewBaseAccumulationRepo[T any](table string, columns []string, mapper MovementRowMapper[T]) BaseAccumulationRepo[T] {
	return core.NewBaseAccumulationRepo(table, columns, mapper)
}

type (
	ExchangeRateRepo = core.ExchangeRateRepo
)

func NewExchangeRateRepo() *core.ExchangeRateRepo { return core.NewExchangeRateRepo() }

func RateSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return core.RateSnapshot(ctx, fn)
}
