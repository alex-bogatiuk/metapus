package outboundapp

import (
	"context"
	"errors"
	"fmt"

	"metapus/internal/configuration/crypto/domain/crypto"
)

type NativeFundingConfirmationService struct {
	repository NativeFundingConfirmationRepository
	funding    FundingConfirmer
	drivers    *crypto.ChainDriverRegistry
}

func NewNativeFundingConfirmationService(repository NativeFundingConfirmationRepository, funding FundingConfirmer, drivers *crypto.ChainDriverRegistry) *NativeFundingConfirmationService {
	return &NativeFundingConfirmationService{repository, funding, drivers}
}

func (p *NativeFundingConfirmationService) RunBatch(ctx context.Context, owner string, limit int) error {
	if p == nil || p.repository == nil || p.funding == nil || p.drivers == nil {
		return fmt.Errorf("native funding confirmation service is not configured")
	}
	fundings, err := p.repository.ClaimNativeFundingConfirmations(ctx, owner, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, funding := range fundings {
		// Bound each read/settlement; no RPC occurs in a PostgreSQL transaction.
		callCtx, cancel := context.WithTimeout(ctx, FundingConfirmationTimeout)
		driver, fundingErr := p.drivers.Resolve(funding.NetworkID)
		if fundingErr == nil {
			_, fundingErr = p.funding.Confirm(callCtx, driver, funding)
		}
		cancel()
		if releaseErr := p.repository.ReleaseNativeFundingConfirmation(ctx, funding.ID, owner); releaseErr != nil && firstErr == nil {
			firstErr = releaseErr
		}
		if fundingErr != nil && !errors.Is(fundingErr, ErrNativeFundingPending) && firstErr == nil {
			firstErr = fmt.Errorf("settle native funding %s: %w", funding.ID, fundingErr)
		}
	}
	return firstErr
}
