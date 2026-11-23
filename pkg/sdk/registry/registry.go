// Package registry declares a configuration's catalogs and documents. A
// model declared with Catalog or Document gets its storage, HTTP routes, wire
// contracts, metadata, permissions and, for a document, posting.
package registry

import (
	core "github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platform"
)

type (
	// Registry collects the application's registrations
	// (configuration.Configuration.Register adds to it).
	Registry = v1.FactoryRegistry
	// Presentation names an entity in the user interface.
	Presentation = metadata.Presentation
	// MovementSourceProvider declares which fields of a document disclose
	// each column of its register movements (field policies).
	MovementSourceProvider = platform.MovementSourceProvider
)

// CatalogModel is satisfied by *T for a catalog model T.
type CatalogModel[T any] interface {
	*T
	core.CatalogEntity
}

// DocumentModel is satisfied by *T for a document model T.
type DocumentModel[T any] interface {
	*T
	domain.HeaderDocumentEntity
}

type (
	// AutoCatalog is a catalog declared from its model.
	AutoCatalog[T core.DefinitionProvider, PT CatalogModel[T]] = v1.AutoCatalog[T, PT]
	// AutoDocument is a document declared from its model.
	AutoDocument[T core.DefinitionProvider, PT DocumentModel[T]] = v1.AutoDocument[T, PT]
)

// Catalog declares a catalog from its model T, which embeds entity.Catalog
// and returns its Definition from EntityDefinition. Add it with
// Registry.RegisterCatalog.
func Catalog[T core.DefinitionProvider, PT CatalogModel[T]](presentation Presentation) *AutoCatalog[T, PT] {
	return v1.Catalog[T, PT](presentation)
}

// Document declares a document from its model T, which embeds
// entity.Document; each table part of its Definition is a []Line field.
// Add it with Registry.RegisterDocument.
func Document[T core.DefinitionProvider, PT DocumentModel[T]](presentation Presentation) *AutoDocument[T, PT] {
	return v1.Document[T, PT](presentation)
}
