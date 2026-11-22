package crypto_resource_rental

import (
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/numerator"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/tx"
)

type Service struct {
	*domain.BaseHeaderDocumentService[*CryptoResourceRental]
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager) *Service {
	return &Service{BaseHeaderDocumentService: domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*CryptoResourceRental]{
		Repo: repo, PostingEngine: engine, Numerator: num, TxManager: txm,
		Definition: &Definition,
	})}
}

func (s *Service) Hooks() *domain.HookRegistry[*CryptoResourceRental] { return s.GetHooks() }
