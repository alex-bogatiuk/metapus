package doc_probe

import (
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

type Repository interface {
	domain.HeaderDocumentRepository[*DocProbe]
}

type Repo struct {
	*document_repo.BaseDocumentRepo[*DocProbe]
}

func NewRepo() *Repo {
	base := document_repo.NewDocumentRepoForDefinition(Definition, postgres.ExtractDBColumns[DocProbe](), func() *DocProbe { return &DocProbe{} })
	return &Repo{base}

}

var _ Repository = (*Repo)(nil)
