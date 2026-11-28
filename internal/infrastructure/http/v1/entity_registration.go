package v1

import (
	"maps"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// EntityRegistration derives transport identity from the domain model. Its zero
// value is ready to use, so registrations cannot forget to initialize identity.
type EntityRegistration[T entity.DefinitionProvider] struct{}

func (EntityRegistration[T]) EntityDefinition() entity.Definition {
	var model T
	return model.EntityDefinition()
}
func (r EntityRegistration[T]) EntityName() string  { return r.EntityDefinition().Name }
func (r EntityRegistration[T]) RoutePrefix() string { return r.EntityDefinition().Route }
func (r EntityRegistration[T]) Permission() string  { return r.EntityDefinition().Permission() }
func (r EntityRegistration[T]) TableName() string   { return r.EntityDefinition().Table }
func (EntityRegistration[T]) EntityStruct() any     { var model T; return model }
func (r EntityRegistration[T]) RLSDimensions() map[string]string {
	return maps.Clone(r.EntityDefinition().RLSDimensions)
}

// CatalogRegistrationFor shares standard typed DTOs and routes between built-in
// entities and extensions. Build remains the entity's explicit composition root.
type CatalogRegistrationFor[T entity.DefinitionProvider, Create, Update, Read any] struct{ EntityRegistration[T] }

func (r CatalogRegistrationFor[T, Create, Update, Read]) HTTPContracts() spec.Bundle {
	return spec.Entity[Create, Update, Read](r.EntityDefinition(), metadata.DocumentCapabilities{})
}
