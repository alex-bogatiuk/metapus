package tree_probe

import (
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

type Repository interface {
	domain.CatalogRepository[*TreeProbe]
}

type Repo struct {
	*catalog_repo.BaseCatalogRepo[*TreeProbe]
}

func NewRepo() *Repo {
	return &Repo{catalog_repo.NewCatalogRepoForDefinition(Definition, postgres.ExtractDBColumns[TreeProbe](), func() *TreeProbe { return &TreeProbe{} })}
}

var _ Repository = (*Repo)(nil)
