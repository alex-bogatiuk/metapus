// Package registry declares a configuration's catalogs and documents. A
// model declared with Catalog or Document gets its storage, HTTP routes, wire
// contracts, metadata, permissions and, for a document, posting. An add-on
// declares its own fields for a catalog of its base or of the platform with
// Extend.
package registry

import (
	"context"

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
	// BasisRule fills a new document from another (ввод на основании); add
	// it with Registry.RegisterBasis.
	BasisRule = v1.BasisRule
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

// Basis declares that a document of type T is filled from a document of
// type S: fill copies what the new document takes from the source and may
// read registers, but writes nothing. The platform sets the new document's
// basis and date, serves the draft at
// GET /document/<target route>/based-on/<source key>/:id and offers it on the
// source's form. Both documents are declared with Document.
func Basis[S, T core.DefinitionProvider, PS DocumentModel[S], PT DocumentModel[T]](label string, fill func(ctx context.Context, source PS, target PT) error) BasisRule {
	return v1.Basis[S, T, PS, PT](label, fill)
}

type (
	// Extension is an add-on's extension of a catalog, declared with Extend.
	Extension[T core.DefinitionProvider, E any, PT CatalogModel[T]] = v1.Extension[T, E, PT]
	// ExtensionRegistration is an extension to add with
	// Registry.RegisterExtension.
	ExtensionRegistration = v1.ExtensionRegistration
)

// Extend declares the fields E that the add-on addOn (its Name) adds to the
// catalog of model T, of its base or of the platform: the exported fields of
// E with a db column (the json name in snake case) and a json name, with
// meta labels, readOnly and binding rules as on a model. They are stored in
// the add-on's table <catalog table>__<addOn>, one row per item, whose id is
// the item's id (PRIMARY KEY REFERENCES the catalog's table ON DELETE
// CASCADE), created by the add-on's migration. The platform reads and writes
// them with the item in its transaction and serves them beside the catalog's
// fields: in its HTTP bodies, list and filters, in its form in the group
// named group, in its file import and under its field policies. Compute the
// extension's read-only fields in OnWrite; make the list search find items
// by a column with Search. Add it with Registry.RegisterExtension.
func Extend[T core.DefinitionProvider, E any, PT CatalogModel[T]](addOn, group string) *Extension[T, E, PT] {
	return v1.Extend[T, E, PT](addOn, group)
}
