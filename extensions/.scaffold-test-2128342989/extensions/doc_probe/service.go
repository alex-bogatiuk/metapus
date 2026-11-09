package doc_probe

import (
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

type Service struct {
	*domain.BaseHeaderDocumentService[*DocProbe]
}

func NewService(repo Repository, num numerator.Generator, engine *posting.Engine) *Service {
	base := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*DocProbe]{
		Repo: repo, Numerator: num, PostingEngine: engine, Definition: &Definition, NumeratorPrefix: "DO", NumeratorStrategy: numerator.StrategyStrict,
	})

	return &Service{base}
}
