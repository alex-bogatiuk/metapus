// Package document_repo stores documents.
package document_repo

import (
	"metapus/internal/core/entity"
	core "metapus/internal/infrastructure/storage/postgres/document_repo"
)

type (
	// BaseDocumentRepo provides common CRUD operations for document entities.
	// In Database-per-Tenant architecture:
	// - TxManager is obtained from context per-request
	// - No tenant filtering in queries (isolation is physical)
	BaseDocumentRepo[T any] = core.BaseDocumentRepo[T]
)

func NewDocumentRepoForDefinition[T any](def entity.Definition, selectCols []string, newFn func() T) *core.BaseDocumentRepo[T] {
	return core.NewDocumentRepoForDefinition[T](def, selectCols, newFn)
}
