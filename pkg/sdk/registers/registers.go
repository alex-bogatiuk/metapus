// Package registers stores accumulation register movements in PostgreSQL.
package registers

import (
	core "metapus/internal/infrastructure/storage/postgres/register_repo"
)

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
