package tree_probe

import (
	"context"
	"metapus/internal/core/entity"
)

// TreeProbe stores the business data. Add typed fields and pure validation here.
type TreeProbe struct {
	entity.Catalog
}

func NewTreeProbe(code, name string) *TreeProbe {
	return &TreeProbe{Catalog: entity.NewCatalog(code, name)}
}

func (e *TreeProbe) Validate(ctx context.Context) error { return e.Catalog.Validate(ctx) }

func (TreeProbe) EntityDefinition() entity.Definition { return Definition }
