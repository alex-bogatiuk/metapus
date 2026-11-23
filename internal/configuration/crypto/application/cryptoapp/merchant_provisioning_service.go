package cryptoapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type MerchantWalletProvisioningService struct {
	repo  crypto.MerchantWalletProvisionRepository
	vault crypto.MerchantWalletProvisioner
}

func NewMerchantWalletProvisioningService(repo crypto.MerchantWalletProvisionRepository, vault crypto.MerchantWalletProvisioner) *MerchantWalletProvisioningService {
	return &MerchantWalletProvisioningService{repo: repo, vault: vault}
}

func (s *MerchantWalletProvisioningService) Run(ctx context.Context, request crypto.MerchantWalletRequest, worker string) (id.ID, error) {
	if err := request.Validate(); err != nil {
		return id.Nil(), err
	}
	if strings.TrimSpace(worker) == "" {
		return id.Nil(), apperror.NewValidation("provisioning worker identity required")
	}
	plan, err := s.repo.ClaimMerchantWalletProvision(ctx, request, worker, 2*time.Minute)
	if err != nil {
		return id.Nil(), err
	}
	return s.runPlan(ctx, plan)
}

// RunNext claims a queued policy intent and commits its lease before Vault I/O.
func (s *MerchantWalletProvisioningService) RunNext(ctx context.Context, worker string) (bool, id.ID, error) {
	queue, ok := s.repo.(crypto.MerchantPoolProvisionQueue)
	if !ok || strings.TrimSpace(worker) == "" {
		return false, id.Nil(), apperror.NewValidation("merchant pool queue and worker identity required")
	}
	plan, claimed, err := queue.ClaimNextMerchantPoolProvision(ctx, worker, 2*time.Minute)
	if err != nil || !claimed {
		return claimed, id.Nil(), err
	}
	walletID, err := s.runPlan(ctx, plan)
	return true, walletID, err
}

func (s *MerchantWalletProvisioningService) runPlan(ctx context.Context, plan crypto.MerchantWalletProvision) (id.ID, error) {
	request := plan.Request
	if plan.Job.WalletID != nil {
		return *plan.Job.WalletID, nil
	}
	expectedNetwork, err := crypto.MerchantCustodyNetwork(plan.Network)
	if err != nil {
		return id.Nil(), errors.Join(err, s.repo.RetryMerchantWalletProvision(ctx, plan))
	}
	derived, enrollment, err := s.vault.DeriveAndEnroll(ctx, plan.VaultNamespace, plan.Network, request.MerchantID, request.Role, plan.Job.DerivationPath)
	if err == nil {
		err = crypto.ValidateWalletDerivation(plan.Job, derived)
	}
	if err == nil && (enrollment.SchemaVersion != "merchant_custody.v1" || enrollment.Network != expectedNetwork || enrollment.MerchantID != request.MerchantID.String() || enrollment.Role != request.Role ||
		(plan.Network.Family == crypto.ChainFamilyEVM && !strings.EqualFold(enrollment.Address, derived.Address)) || (plan.Network.Family != crypto.ChainFamilyEVM && enrollment.Address != derived.Address)) {
		err = apperror.NewConflict("Vault merchant enrollment identity mismatch")
	}
	if err != nil {
		return id.Nil(), errors.Join(err, s.repo.RetryMerchantWalletProvision(ctx, plan))
	}
	walletID, err := s.repo.CompleteMerchantWalletProvision(ctx, plan, derived, enrollment)
	if err != nil {
		return id.Nil(), errors.Join(err, s.repo.RetryMerchantWalletProvision(ctx, plan))
	}
	return walletID, nil
}
