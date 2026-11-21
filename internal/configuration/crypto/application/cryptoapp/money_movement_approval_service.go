package cryptoapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

// MoneyMovementApprovalRepository is the persistence port for immutable
// movement intents and signed approvals.
type MoneyMovementApprovalRepository interface {
	SaveArtifacts(context.Context, string, id.ID, crypto.MoneyMovementApprovalArtifacts) error
	SaveApproval(context.Context, string, id.ID, string, crypto.MoneyMovementApproval, string) error
	LoadBundle(context.Context, string, id.ID) (crypto.ApprovalBundle, bool, error)
}

// FreshStepUpChecker is the application boundary for object-bound step-up.
type FreshStepUpChecker interface {
	HasFreshStepUp(context.Context, id.ID, id.ID, string, string, time.Time) (bool, error)
}

type PreparedMoneyMovementApproval struct {
	Intent       crypto.MoneyMovementIntent
	IntentDigest string
	Signed       crypto.MoneyMovementSignedApproval
}

// MoneyMovementApprovalService centralizes immutable-intent verification and
// maker-checker proof construction. HTTP handlers never sign or persist
// financial authorization artifacts directly.
type MoneyMovementApprovalService struct {
	repo    MoneyMovementApprovalRepository
	signers crypto.MoneyMovementApprovalSignerProvider
	stepUp  FreshStepUpChecker
	now     func() time.Time
}

func NewMoneyMovementApprovalService(
	repo MoneyMovementApprovalRepository,
	signers crypto.MoneyMovementApprovalSignerProvider,
	stepUp FreshStepUpChecker,
) *MoneyMovementApprovalService {
	if repo == nil || signers == nil {
		panic("cryptoapp.NewMoneyMovementApprovalService: repository and signer provider are required")
	}
	return &MoneyMovementApprovalService{repo: repo, signers: signers, stepUp: stepUp, now: time.Now}
}

func (s *MoneyMovementApprovalService) BuildArtifacts(ctx context.Context, intent crypto.MoneyMovementIntent) (crypto.MoneyMovementApprovalArtifacts, error) {
	tenantID, err := requiredTenantID(ctx)
	if err != nil {
		return crypto.MoneyMovementApprovalArtifacts{}, err
	}
	intent.TenantID = tenantID
	signer, err := s.signers.SignerForTenant(tenantID)
	if err != nil {
		return crypto.MoneyMovementApprovalArtifacts{}, apperror.NewInternal(fmt.Errorf("resolve money movement signer: %w", err))
	}
	return signer.BuildArtifacts(ctx, intent, nil, "")
}

func (s *MoneyMovementApprovalService) SaveArtifacts(ctx context.Context, artifacts crypto.MoneyMovementApprovalArtifacts) error {
	return s.repo.SaveArtifacts(ctx, artifacts.Intent.BasisType, artifacts.Intent.BasisID, artifacts)
}

func (s *MoneyMovementApprovalService) RequiresAdmin(artifacts crypto.MoneyMovementApprovalArtifacts) bool {
	return slices.Contains(artifacts.RequiredPrincipals, crypto.ApprovalPrincipalAdmin)
}

func (s *MoneyMovementApprovalService) PrepareAdminApproval(
	ctx context.Context,
	basisType string,
	basisID id.ID,
	current crypto.MoneyMovementIntent,
	actorID id.ID,
) (PreparedMoneyMovementApproval, error) {
	intent, bundle, digest, err := s.loadAndVerify(ctx, basisType, basisID)
	if err != nil || !MoneyMovementIntentMatches(intent, current) {
		if err == nil {
			err = fmt.Errorf("stored money movement intent does not match current financial command")
		}
		return PreparedMoneyMovementApproval{}, err
	}
	actionCode := MoneyMovementApprovalActionCode(digest)
	stepUpActorID, sessionID, tenantID, err := s.RequireStepUp(ctx, actionCode)
	if err != nil {
		return PreparedMoneyMovementApproval{}, err
	}
	if stepUpActorID != actorID {
		return PreparedMoneyMovementApproval{}, apperror.NewForbidden("approval actor does not match the authenticated step-up principal")
	}
	signer, err := s.signers.SignerForTenant(tenantID)
	if err != nil {
		return PreparedMoneyMovementApproval{}, apperror.NewInternal(fmt.Errorf("resolve money movement signer: %w", err))
	}
	proofDigest := stepUpProofDigest(tenantID, actorID.String(), sessionID.String(), actionCode)
	signed, err := signer.BuildAdminApprovalForBundle(ctx, bundle, actorID, proofDigest)
	if err != nil {
		return PreparedMoneyMovementApproval{}, apperror.NewInternal(err)
	}
	return PreparedMoneyMovementApproval{Intent: intent, IntentDigest: digest, Signed: signed}, nil
}

// VerifyLockedIntent must be called again after the business row is locked.
func (s *MoneyMovementApprovalService) VerifyLockedIntent(
	ctx context.Context,
	basisType string,
	basisID id.ID,
	current crypto.MoneyMovementIntent,
	expectedDigest string,
) error {
	intent, _, digest, err := s.loadAndVerify(ctx, basisType, basisID)
	if err != nil {
		return err
	}
	if digest != expectedDigest || !MoneyMovementIntentMatches(intent, current) {
		return fmt.Errorf("stored money movement intent does not match locked financial command")
	}
	return nil
}

func (s *MoneyMovementApprovalService) SavePreparedApproval(ctx context.Context, basisType string, basisID id.ID, prepared PreparedMoneyMovementApproval) error {
	return s.repo.SaveApproval(ctx, basisType, basisID, prepared.Intent.PolicyDigest, prepared.Signed.Approval, prepared.Signed.Bundle.JWS)
}

func (s *MoneyMovementApprovalService) RequireStepUp(ctx context.Context, actionCode string) (id.ID, id.ID, string, error) {
	return requireFreshStepUp(ctx, s.stepUp, actionCode, s.now().UTC())
}

func requireFreshStepUp(ctx context.Context, checker FreshStepUpChecker, actionCode string, now time.Time) (id.ID, id.ID, string, error) {
	if checker == nil {
		return id.Nil(), id.Nil(), "", apperror.NewInternal(fmt.Errorf("step-up checker is not configured"))
	}
	user := appctx.GetUser(ctx)
	if user == nil || user.UserID == "" {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("user context is required")
	}
	userID, err := id.Parse(user.UserID)
	if err != nil || id.IsNil(userID) {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}
	sessionID, err := id.Parse(user.SessionID)
	if err != nil || id.IsNil(sessionID) {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("invalid session")
	}
	tenantID, err := requiredTenantID(ctx)
	if err != nil {
		return id.Nil(), id.Nil(), "", err
	}
	ok, err := checker.HasFreshStepUp(ctx, sessionID, userID, tenantID, actionCode, now)
	if err != nil {
		return id.Nil(), id.Nil(), "", apperror.NewInternal(err)
	}
	if !ok {
		return id.Nil(), id.Nil(), "", apperror.NewStepUpRequired(actionCode)
	}
	return userID, sessionID, tenantID, nil
}

func (s *MoneyMovementApprovalService) loadAndVerify(ctx context.Context, basisType string, basisID id.ID) (crypto.MoneyMovementIntent, crypto.ApprovalBundle, string, error) {
	var intent crypto.MoneyMovementIntent
	bundle, ok, err := s.repo.LoadBundle(ctx, basisType, basisID)
	if err != nil {
		return intent, bundle, "", err
	}
	if !ok {
		// The intent is signed asynchronously after creation (pending_signature).
		// Approving before that is a retryable state conflict, not a server error.
		return intent, bundle, "", apperror.NewConflict("money movement is not ready for approval: its intent is still being signed").
			WithDetail("basisType", basisType).WithDetail("basisId", basisID.String())
	}
	tenantID, err := requiredTenantID(ctx)
	if err != nil {
		return intent, bundle, "", err
	}
	keys, err := s.signers.PublicKeysForTenant(tenantID)
	if err != nil {
		return intent, bundle, "", fmt.Errorf("resolve money movement verifier: %w", err)
	}
	if _, err := crypto.VerifyEdDSAJWS(bundle.IntentJWS, keys.IntentPublicKey, &intent); err != nil {
		return intent, bundle, "", fmt.Errorf("verify stored money movement intent: %w", err)
	}
	if intent.TenantID != tenantID {
		return intent, bundle, "", fmt.Errorf("stored money movement intent tenant mismatch")
	}
	digest, err := crypto.JWSPayloadDigest(bundle.IntentJWS)
	return intent, bundle, digest, err
}

func requiredTenantID(ctx context.Context) (string, error) {
	tenantID := tenant.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = appctx.GetTenantID(ctx)
	}
	if tenantID == "" {
		return "", apperror.NewValidation("tenant context is required for money movement approval")
	}
	return tenantID, nil
}

func MoneyMovementIntentMatches(frozen, current crypto.MoneyMovementIntent) bool {
	if frozen.TenantID != current.TenantID || frozen.Purpose != current.Purpose ||
		frozen.BasisType != current.BasisType || frozen.BasisID != current.BasisID ||
		frozen.NetworkCode != current.NetworkCode || frozen.TokenID != current.TokenID ||
		frozen.TokenStandard != current.TokenStandard || frozen.ContractAddress != current.ContractAddress ||
		!frozen.AmountMinorUnits.Equal(current.AmountMinorUnits) ||
		frozen.DestinationAddress != current.DestinationAddress || frozen.AddressID != current.AddressID ||
		frozen.SourceAddress != current.SourceAddress || frozen.SourceVaultKeyID != current.SourceVaultKeyID {
		return false
	}
	if frozen.MerchantID == nil || current.MerchantID == nil {
		return frozen.MerchantID == nil && current.MerchantID == nil
	}
	return *frozen.MerchantID == *current.MerchantID
}

func MoneyMovementApprovalActionCode(intentDigest string) string {
	return "money_movement.approve:" + intentDigest
}

func stepUpProofDigest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
