package a

import (
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

type Repository interface{ domain.CatalogRepository[*A] }

type Repo struct {
	*catalog_repo.BaseCatalogRepo[*A]
}

func NewRepo() *Repo {
	return &Repo{catalog_repo.NewCatalogRepoForDefinition(Definition, postgres.ExtractDBColumns[A](), func() *A { return &A{} })}
}

var _ Repository = (*Repo)(nil)
