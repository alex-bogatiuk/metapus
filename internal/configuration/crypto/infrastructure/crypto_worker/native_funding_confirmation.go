package crypto_worker

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
)

type nativeFundingConfirmationStore = outbound_repo.NativeFundingConfirmationStore

func (p *CryptoProcessor) nativeFundingConfirmationApplication() *outboundapp.NativeFundingConfirmationService {
	return outboundapp.NewNativeFundingConfirmationService(nativeFundingConfirmationStore{}, NewNativeFundingService(p.outboundRepo, p.feeBudget, nil), p.drivers)
}
