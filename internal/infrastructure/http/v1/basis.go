package v1

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// BasisRule fills a new document from another one (ввод на основании): a
// goods issue from a customer order, say. The configuration declares it with
// Basis and adds it with FactoryRegistry.RegisterBasis; the platform then
// serves the draft at GET /document/<target>/based-on/<source key>/:id and
// offers it on the source's form («Создать на основании»).
type BasisRule interface {
	// SourceKey and TargetKey are the documents' entity keys.
	SourceKey() string
	TargetKey() string
	// Label names the action for people ("Расходная накладная").
	Label() string
	source() entity.Definition
	target() entity.Definition
	fill(ctx context.Context, source, target any) error
}

type basisRule[S, T entity.DefinitionProvider, PS documentModel[S], PT documentModel[T]] struct {
	label  string
	filler func(context.Context, PS, PT) error
}

// Basis declares that a document T is filled from a document S. fill copies
// what the new document takes from the source; it may read registers (what
// an order still awaits, say) but writes nothing. The platform has already
// set the target's basis (BasisType, BasisID) and date, and the source is
// read as its GET reads it: with the user's record and field policies.
func Basis[S, T entity.DefinitionProvider, PS documentModel[S], PT documentModel[T]](label string, fill func(ctx context.Context, source PS, target PT) error) BasisRule {
	return basisRule[S, T, PS, PT]{label: label, filler: fill}
}

func (b basisRule[S, T, PS, PT]) SourceKey() string { return b.source().Key }
func (b basisRule[S, T, PS, PT]) TargetKey() string { return b.target().Key }
func (b basisRule[S, T, PS, PT]) Label() string     { return b.label }

func (b basisRule[S, T, PS, PT]) source() entity.Definition {
	var s S
	return s.EntityDefinition()
}

func (b basisRule[S, T, PS, PT]) target() entity.Definition {
	var t T
	return t.EntityDefinition()
}

func (b basisRule[S, T, PS, PT]) fill(ctx context.Context, source, target any) error {
	s, ok := source.(PS)
	if !ok {
		return fmt.Errorf("basis %s: source has type %T", b.SourceKey(), source)
	}
	t, ok := target.(PT)
	if !ok {
		return fmt.Errorf("basis %s: target has type %T", b.TargetKey(), target)
	}
	return b.filler(ctx, s, t)
}

// RegisterBasis adds a rule that fills a new document from another one. Both
// documents are declared registrations (registry.Document); a source and
// target pair is declared once.
func (r *FactoryRegistry) RegisterBasis(rule BasisRule) {
	for _, existing := range r.basis {
		if existing.SourceKey() == rule.SourceKey() && existing.TargetKey() == rule.TargetKey() {
			panic(fmt.Sprintf("duplicate basis rule %s → %s", rule.SourceKey(), rule.TargetKey()))
		}
	}
	if rule.SourceKey() == rule.TargetKey() {
		panic("a basis rule fills a document of another type; copy fills the same: " + rule.SourceKey())
	}
	r.basis = append(r.basis, rule)
}

// BasisRules returns the declared basis rules.
func (r *FactoryRegistry) BasisRules() []BasisRule { return r.basis }

func (r *FactoryRegistry) basisRulesTo(target string) []BasisRule {
	var rules []BasisRule
	for _, rule := range r.basis {
		if rule.TargetKey() == target {
			rules = append(rules, rule)
		}
	}
	return rules
}

// basisSource is a document registration that reads its documents for a
// basis rule; AutoDocument is one once built.
type basisSource interface {
	readForBasis(ctx context.Context, docID id.ID) (any, error)
}

// readBasis reads the source document of a rule.
func (r *FactoryRegistry) readBasis(ctx context.Context, key string, docID id.ID) (any, error) {
	for _, doc := range r.documents {
		if doc.EntityDefinition().Key != key {
			continue
		}
		source, ok := doc.(basisSource)
		if !ok {
			break
		}
		return source.readForBasis(ctx, docID)
	}
	return nil, apperror.NewInternal(fmt.Errorf("document %s cannot be a basis: it is not a declared registration", key))
}

// checkBasisRules makes sure every rule joins two declared documents; a
// mistake is found when the router is built.
func (r *FactoryRegistry) checkBasisRules() {
	declared := map[string]bool{}
	for _, doc := range r.documents {
		if _, ok := doc.(basisSource); ok {
			declared[doc.EntityDefinition().Key] = true
		}
	}
	for _, rule := range r.basis {
		if !declared[rule.SourceKey()] || !declared[rule.TargetKey()] {
			panic(fmt.Sprintf("basis rule %s → %s joins a document that is not a declared registration", rule.SourceKey(), rule.TargetKey()))
		}
	}
}

// basedOnMetadata is what the source's form offers: one entry per rule.
func (r *FactoryRegistry) basedOnMetadata(source string) []metadata.CreateBasedOnDef {
	var result []metadata.CreateBasedOnDef
	for _, rule := range r.basis {
		if rule.SourceKey() != source {
			continue
		}
		target := rule.target()
		result = append(result, metadata.CreateBasedOnDef{
			Target: target.Key, Label: rule.Label(), RoutePrefix: target.Route, Permission: target.Permission() + ":create",
		})
	}
	return result
}

// BasisRoute is the draft route of one basis rule on its target document.
type BasisRoute struct {
	SourceKey        string
	SourcePermission string
	Handler          gin.HandlerFunc
}

// DocumentBasisHandler is a document handler that serves drafts filled from
// other documents.
type DocumentBasisHandler interface {
	BasisRoutes() []BasisRoute
}

// autoDocumentHandler adds to a declared document's handler its basis
// routes and the status history of its status model; the embedded handler
// keeps its optional interfaces.
type autoDocumentHandler[PT, C, U any] struct {
	*handlers.BaseDocumentHandler[PT, C, U]
	routes        []BasisRoute
	statusHistory gin.HandlerFunc
}

func (h autoDocumentHandler[PT, C, U]) BasisRoutes() []BasisRoute { return h.routes }

func (h autoDocumentHandler[PT, C, U]) StatusHistory() gin.HandlerFunc { return h.statusHistory }

// basisReader is how a built AutoDocument reads its documents for a rule.
type basisReader struct {
	read func(ctx context.Context, docID id.ID) (any, error)
}

func (r *AutoDocument[T, PT]) readForBasis(ctx context.Context, docID id.ID) (any, error) {
	reader := r.reader.Load()
	if reader == nil {
		return nil, apperror.NewInternal(fmt.Errorf("document %s is not built", r.EntityDefinition().Key))
	}
	return reader.read(ctx, docID)
}

// storeBasisReader keeps the built service's read for rules whose source is
// this document: the document as GET reads it, record policies applied by
// the service and the read field policy masked.
func (r *AutoDocument[T, PT]) storeBasisReader(get func(context.Context, id.ID) (PT, error)) {
	key := r.EntityDefinition().Key
	r.reader.Store(&basisReader{read: func(ctx context.Context, docID id.ID) (any, error) {
		doc, err := get(ctx, docID)
		if err != nil {
			return nil, err
		}
		if policy := security.GetFieldPolicy(ctx, key, "read"); policy != nil {
			security.MaskForRead(doc, policy)
		}
		return doc, nil
	}})
}

// basisRoutes are the draft routes of the rules whose target is this
// document. Each reads the source, fills a new draft and answers it as the
// document's GET would, without storing it.
func (r *AutoDocument[T, PT]) basisRoutes(h *handlers.BaseDocumentHandler[PT, AutoCreate[T], AutoUpdate[T]], shape *autoShape) []BasisRoute {
	if r.registry == nil {
		return nil
	}
	var routes []BasisRoute
	for _, rule := range r.registry.basisRulesTo(r.EntityDefinition().Key) {
		source := rule.source()
		routes = append(routes, BasisRoute{
			SourceKey:        source.Key,
			SourcePermission: source.Permission(),
			Handler: func(c *gin.Context) {
				ctx := c.Request.Context()
				sourceID, err := id.Parse(c.Param("id"))
				if err != nil {
					h.Error(c, apperror.NewValidation("invalid id format"))
					return
				}
				doc, err := r.registry.readBasis(ctx, source.Key, sourceID)
				if err != nil {
					h.Error(c, err)
					return
				}
				draft := PT(newAutoModel[T](reflect.Value{}, entity.DocumentKind))
				v := reflect.ValueOf(draft).Elem()
				v.FieldByName("BasisType").SetString(source.EntityName())
				v.FieldByName("BasisID").Set(reflect.ValueOf(&sourceID))
				v.FieldByName("Date").Set(reflect.ValueOf(time.Now().UTC()))
				if err := rule.fill(ctx, doc, draft); err != nil {
					h.Error(c, err)
					return
				}
				if err := shape.computeAutoCalcs(draft); err != nil {
					h.Error(c, err)
					return
				}
				refs, err := h.ResolveRefs(ctx, draft)
				if err != nil {
					h.Error(c, err)
					return
				}
				h.ApplyFLSRead(c, draft)
				c.JSON(http.StatusOK, h.ToDTO(draft, refs))
			},
		})
	}
	return routes
}
