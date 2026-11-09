package crypto_resource_rental

import (
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
)

type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoResourceRental]
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager) *Service {
	return &Service{BaseHeaderDocumentService: domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoResourceRental]{
		Repo: repo, PostingEngine: engine, Numerator: num, TxManager: txm,
		NumeratorPrefix: "RR", Definition: &Definition,
	})}
}

func (s *Service) Hooks() *domain.HookRegistry[*CryptoResourceRental] { return s.GetHooks() }
