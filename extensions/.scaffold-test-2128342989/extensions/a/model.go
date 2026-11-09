package a

import (
	"context"
	"metapus/internal/core/entity"
)

// A stores the business data. Add typed fields and pure validation here.
type A struct {
	entity.Catalog
}

func NewA(code, name string) *A {
	return &A{Catalog: entity.NewCatalog(code, name)}
}

func (e *A) Validate(ctx context.Context) error { return e.Catalog.Validate(ctx) }

func (A) EntityDefinition() entity.Definition { return Definition }
