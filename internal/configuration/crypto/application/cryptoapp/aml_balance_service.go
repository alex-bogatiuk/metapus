package cryptoapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

type AMLBalanceService struct{ repo aml.MerchantBalanceRepository }

func NewAMLBalanceService(repo aml.MerchantBalanceRepository) *AMLBalanceService {
	return &AMLBalanceService{repo: repo}
}

func (s *AMLBalanceService) Get(ctx context.Context, merchantID id.ID) (aml.MerchantBalanceSnapshot, error) {
	if id.IsNil(merchantID) {
		return aml.MerchantBalanceSnapshot{}, apperror.NewValidation("merchant is required for risk balance")
	}
	now, evidence, err := s.repo.ReadMerchantRiskBalance(ctx, merchantID)
	if err != nil {
		return aml.MerchantBalanceSnapshot{}, apperror.NewInternal(err)
	}
	result := aml.MerchantBalanceSnapshot{AsOf: now, FreshUntil: now.Add(30 * time.Second), Items: make([]aml.MerchantRiskBalance, 0, len(evidence))}
	for _, entry := range evidence {
		if entry.Scope.MerchantID != merchantID {
			return result, apperror.NewInternal(fmt.Errorf("risk balance merchant scope mismatch"))
		}
		item, err := aml.BuildMerchantRiskBalance(entry, now)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		for _, source := range entry.Sources {
			if source.ExpiresAt != nil && now.Before(*source.ExpiresAt) && source.ExpiresAt.Before(result.FreshUntil) {
				result.FreshUntil = *source.ExpiresAt
			}
		}
	}
	// Include source revisions and raw inputs as well as effective classes. The
	// digest is a view identity only; outbound must acquire its own locks/permit.
	raw, err := json.MarshalDeterministic(struct {
		Merchant id.ID
		Evidence []aml.MerchantBalanceEvidence
		Items    []aml.MerchantRiskBalance
	}{merchantID, evidence, result.Items})
	if err != nil {
		return result, apperror.NewInternal(err)
	}
	digest := sha256.Sum256(raw)
	result.SnapshotID = hex.EncodeToString(digest[:])
	return result, nil
}
