package v1

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/metadata"
)

// Declarative registration: an applied module declares a model and registers
// it in one line; storage, service, CRUD routes, wire contracts, metadata and
// permissions are derived from the model and its Definition.
//
//	type Vehicle struct {
//		entity.Catalog
//		Plate string `db:"plate" json:"plate" meta:"label:Госномер" binding:"required"`
//	}
//	func (Vehicle) EntityDefinition() entity.Definition { return Definition }
//
//	reg.RegisterCatalog(v1.Catalog[Vehicle](metadata.Presentation{Singular: "ТС", Plural: "Транспорт"}))
//
// The wire projection follows one rule (see autoShape); business behaviour
// stays on the model (Validate, CanPost, movement sources) or in hooks added
// with Configure. An entity that needs its own DTOs, references in responses
// or custom routes keeps an explicit registration (CatalogRegistrationFor).

// catalogModel is satisfied by *T for a catalog model T.
type catalogModel[T any] interface {
	*T
	entity.CatalogEntity
}

// documentModel is satisfied by *T for a header-only document model T.
type documentModel[T any] interface {
	*T
	domain.HeaderDocumentEntity
}

// AutoCatalog is a catalog registration derived from its model.
type AutoCatalog[T entity.DefinitionProvider, PT catalogModel[T]] struct {
	EntityRegistration[T]
	presentation metadata.Presentation
	codePrefix   string
	configure    []func(*domain.CatalogService[PT])
}

// Catalog declares a catalog from its model T, which embeds entity.Catalog
// and returns its Definition from EntityDefinition.
func Catalog[T entity.DefinitionProvider, PT catalogModel[T]](presentation metadata.Presentation) *AutoCatalog[T, PT] {
	r := &AutoCatalog[T, PT]{presentation: presentation}
	autoShapeFor[T]() // fail at registration, not on the first request
	return r
}

// CodePrefix sets the numerator prefix of generated codes; by default it is
// derived from the entity key.
func (r *AutoCatalog[T, PT]) CodePrefix(prefix string) *AutoCatalog[T, PT] {
	r.codePrefix = prefix
	return r
}

// Configure adds hooks or settings to the catalog service when it is built.
func (r *AutoCatalog[T, PT]) Configure(fn func(*domain.CatalogService[PT])) *AutoCatalog[T, PT] {
	r.configure = append(r.configure, fn)
	return r
}

func (r *AutoCatalog[T, PT]) EntityPresentation() metadata.Presentation { return r.presentation }
func (r *AutoCatalog[T, PT]) ReferenceTypes() []string                  { return []string{r.EntityDefinition().Key} }

func (r *AutoCatalog[T, PT]) HTTPContracts() spec.Bundle {
	shape := autoShapeFor[T]()
	return spec.EntityOf(r.EntityDefinition(), metadata.DocumentCapabilities{}, shape.create, shape.update, shape.read)
}

func (r *AutoCatalog[T, PT]) Build(deps CatalogDeps) CatalogRouteHandler {
	def := r.EntityDefinition()
	shape := autoShapeFor[T]()
	repo := catalog_repo.NewCatalogRepoForDefinition(def, postgres.ExtractDBColumns[T](), func() PT { return new(T) })
	service := domain.NewCatalogService(domain.CatalogServiceConfig[PT]{Definition: &def, Repo: repo, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine})
	prefix := r.codePrefix
	if prefix == "" {
		prefix = autoCodePrefix(def.Key)
	}
	service.Hooks().OnBeforeCreate(func(ctx context.Context, model PT) error {
		code := reflect.ValueOf(model).Elem().FieldByName("Code")
		if code.String() != "" || deps.Numerator == nil {
			return nil
		}
		next, err := deps.Numerator.GetNextNumber(ctx, numerator.DefaultConfig(prefix), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		code.SetString(next)
		return nil
	})
	for _, fn := range r.configure {
		fn(service)
	}
	domain.NewEventLogCatalogService(service, def.Key, deps.EventWriter)
	return handlers.NewCatalogHandler(deps.BaseHandler, handlers.CatalogHandlerConfig[PT, AutoCreate[T], AutoUpdate[T]]{
		Service:    service,
		EntityName: def.Key,
		MapCreateDTO: func(req AutoCreate[T]) PT {
			model := newAutoModel[T](req.value, entity.CatalogKind)
			shape.applyAutoRequest(req.value, model)
			return model
		},
		MapUpdateDTO: func(req AutoUpdate[T], existing PT) PT {
			shape.applyAutoRequest(req.value, existing)
			reflect.ValueOf(existing).Elem().FieldByName("Version").SetInt(req.value.FieldByName("Version").Int())
			return existing
		},
		MapToDTO: func(model PT) any { return shape.response(model) },
	})
}

// autoDocumentCapabilities is the generic surface of a declared document.
var autoDocumentCapabilities = metadata.DocumentCapabilities{
	List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true,
	Post: true, Unpost: true, Repost: true, ExportList: true, Movements: true, BatchAction: true, BatchActionByFilter: true,
}

// AutoDocument is a header-only document registration derived from its model.
type AutoDocument[T entity.DefinitionProvider, PT documentModel[T]] struct {
	EntityRegistration[T]
	presentation metadata.Presentation
	numberPrefix string
	configure    []func(*domain.BaseHeaderDocumentService[PT])
}

// Document declares a header-only document from its model T, which embeds
// entity.Document and returns its Definition from EntityDefinition.
func Document[T entity.DefinitionProvider, PT documentModel[T]](presentation metadata.Presentation) *AutoDocument[T, PT] {
	r := &AutoDocument[T, PT]{presentation: presentation}
	autoShapeFor[T]()
	return r
}

// NumberPrefix sets the numerator prefix of document numbers; by default it
// is derived from the entity key.
func (r *AutoDocument[T, PT]) NumberPrefix(prefix string) *AutoDocument[T, PT] {
	r.numberPrefix = prefix
	return r
}

// Configure adds hooks or settings to the document service when it is built.
func (r *AutoDocument[T, PT]) Configure(fn func(*domain.BaseHeaderDocumentService[PT])) *AutoDocument[T, PT] {
	r.configure = append(r.configure, fn)
	return r
}

func (r *AutoDocument[T, PT]) EntityPresentation() metadata.Presentation { return r.presentation }
func (r *AutoDocument[T, PT]) DocumentCapabilities() metadata.DocumentCapabilities {
	return autoDocumentCapabilities
}

func (r *AutoDocument[T, PT]) HTTPContracts() spec.Bundle {
	shape := autoShapeFor[T]()
	return spec.EntityOf(r.EntityDefinition(), r.DocumentCapabilities(), shape.create, shape.update, shape.read)
}

func (r *AutoDocument[T, PT]) Build(deps DocumentDeps) DocumentRouteHandler {
	def := r.EntityDefinition()
	shape := autoShapeFor[T]()
	prefix := r.numberPrefix
	if prefix == "" {
		prefix = autoCodePrefix(def.Key)
	}
	repo := document_repo.NewDocumentRepoForDefinition(def, postgres.ExtractDBColumns[T](), func() PT { return new(T) })
	service := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[PT]{
		Definition: &def, Repo: repo, PostingEngine: deps.PostingEngine, Numerator: deps.Numerator, PolicyEngine: deps.PolicyEngine,
		NumeratorPrefix: prefix, NumeratorStrategy: numerator.StrategyStrict, EntityName: def.Key,
	})
	for _, fn := range r.configure {
		fn(service)
	}
	decorated := domain.WithEventLog[PT](def.Key, deps.EventWriter)(service)
	return handlers.NewBaseDocumentHandler(deps.BaseHandler, handlers.BaseDocumentHandlerConfig[PT, AutoCreate[T], AutoUpdate[T]]{
		Service:    decorated,
		EntityName: def.Key,
		MapCreateDTO: func(req AutoCreate[T]) (PT, error) {
			model := newAutoModel[T](req.value, entity.DocumentKind)
			shape.applyAutoRequest(req.value, model)
			return model, nil
		},
		MapUpdateDTO: func(req AutoUpdate[T], existing PT) (PT, error) {
			shape.applyAutoRequest(req.value, existing)
			reflect.ValueOf(existing).Elem().FieldByName("Version").SetInt(req.value.FieldByName("Version").Int())
			return existing, nil
		},
		MapToDTO:            func(model PT) any { return shape.response(model) },
		IsPostImmediately:   func(req AutoCreate[T]) bool { return req.value.FieldByName("PostImmediately").Bool() },
		MovementProviders:   deps.MovementProviders,
		MovementRefResolver: deps.MovementRefResolver,
		SettingsRepo:        deps.SettingsRepo,
	})
}

// autoCodePrefix derives a numerator prefix from an entity key: the first
// letters of its words, e.g. "fuel_card" → "FC", "vehicle" → "VE".
func autoCodePrefix(key string) string {
	words := strings.Split(key, "_")
	if len(words) == 1 {
		return strings.ToUpper(key[:min(2, len(key))])
	}
	var prefix strings.Builder
	for _, w := range words[:min(3, len(words))] {
		if w != "" {
			prefix.WriteByte(w[0])
		}
	}
	return strings.ToUpper(prefix.String())
}
