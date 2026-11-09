package crypto_worker

import (
	"metapus/internal/application/outboundapp"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
)

type nativeFundingConfirmationStore = outbound_repo.NativeFundingConfirmationStore

func (p *CryptoProcessor) nativeFundingConfirmationApplication() *outboundapp.NativeFundingConfirmationService {
	return outboundapp.NewNativeFundingConfirmationService(nativeFundingConfirmationStore{}, NewNativeFundingService(p.outboundRepo, p.feeBudget, nil), p.drivers)
}
