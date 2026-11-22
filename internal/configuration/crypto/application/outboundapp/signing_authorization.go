package outboundapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
)

const SigningAuthorizationTTL = 10 * time.Minute

type SigningAuthorizationRepository interface {
	GetSigningAttempt(context.Context, string, id.ID, crypto.SigningPurpose, int, int) (SigningAuthorizationRecord, bool, error)
	InsertSigningAuthorization(context.Context, crypto.SigningAuthorization, string, crypto.UnsignedTxEnvelope, ...crypto.ApprovalBundle) (id.ID, error)
	ApplyExactAuthorizationRenewal(context.Context, SigningAuthorizationRecord) (SigningAuthorizationRecord, error)
	SetRenewedAuthorizationJWS(context.Context, id.ID, string) error
	SetSigningAuthorizationJWS(context.Context, string, string) error
	MarkSigningAuthorizationSigned(context.Context, string, crypto.SignedTxEnvelope) error
}
type SigningCustody interface {
	PrepareTransferCustody(context.Context, crypto.TransferIntent, *id.ID) (*id.ID, error)
	CheckCustody(context.Context, crypto.SigningAuthorization) error
	CheckCustodySigner(context.Context, crypto.NetworkRef, crypto.PolicyAwareTransactionSigner) error
	BeginDispatch(context.Context, string, string) error
}
type SigningAuthorizationService struct {
	repo     SigningAuthorizationRepository
	signer   crypto.SigningAuthorizationSigner
	custody  SigningCustody
	tenantID string
	now      func() time.Time
}

func NewSigningAuthorizationService(repo SigningAuthorizationRepository, signer crypto.SigningAuthorizationSigner, custody SigningCustody, tenantID string, now func() time.Time) (*SigningAuthorizationService, error) {
	if repo == nil || signer == nil || custody == nil {
		return nil, fmt.Errorf("signing authorization repository, custody policy and signer are required")
	}
	if _, err := id.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("signing authorization tenant_id must be a UUID: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	return &SigningAuthorizationService{repo, signer, custody, tenantID, now}, nil
}
func (s *SigningAuthorizationService) CheckReady(ctx context.Context) (string, error) {
	if err := s.signer.CheckReady(ctx, s.tenantID); err != nil {
		return "", fmt.Errorf("authorization signer is not ready: %w", err)
	}
	provider, ok := s.signer.(crypto.SigningAuthorizationKeyIDProvider)
	if !ok {
		return "", fmt.Errorf("authorization signer does not expose an active kid")
	}
	kid, err := provider.AuthorizationKeyID(ctx, s.tenantID)
	if err != nil {
		return "", fmt.Errorf("load active authorization kid: %w", err)
	}
	return kid, nil
}

func (s *SigningAuthorizationService) Create(ctx context.Context, input SigningAttemptInput) (SigningAuthorizationRecord, error) {
	return s.create(ctx, input, nil)
}
func (s *SigningAuthorizationService) CreateGuarded(ctx context.Context, input SigningAttemptInput, guard SigningExecutionGuard) (SigningAuthorizationRecord, error) {
	return s.create(ctx, input, guard)
}
func (s *SigningAuthorizationService) create(ctx context.Context, input SigningAttemptInput, guard SigningExecutionGuard) (SigningAuthorizationRecord, error) {
	if err := requireActivationSigningGuard(input.Intent.Purpose, guard); err != nil {
		return SigningAuthorizationRecord{}, err
	}
	if err := runSigningCommand(ctx, guard, func(context.Context) error { return nil }); err != nil {
		return SigningAuthorizationRecord{}, err
	}
	if input.Unsigned.Generation <= 0 {
		input.Unsigned.Generation = input.Intent.EffectiveGeneration()
	}
	if err := validateSigningAttemptInput(input); err != nil {
		return SigningAuthorizationRecord{}, err
	}
	merchantID, err := s.custody.PrepareTransferCustody(ctx, input.Intent, input.MerchantID)
	if err != nil {
		return SigningAuthorizationRecord{}, HoldExecution(err)
	}
	input.MerchantID = merchantID

	digest := ""
	isMoneyMovement := input.Intent.Purpose.RequiresApprovalBundle()
	if isMoneyMovement {
		digest, err = crypto.DigestJSON(input.ApprovalBundle)
		if err != nil {
			return SigningAuthorizationRecord{}, fmt.Errorf("digest approval bundle: %w", err)
		}
	}
	auth := crypto.SigningAuthorization{
		SchemaVersion:        crypto.SigningAuthorizationSchemaVersionFor(input.Capabilities.SequenceModel),
		AuthorizationID:      id.New().String(),
		TenantID:             s.tenantID,
		MerchantID:           input.MerchantID,
		Purpose:              input.Intent.Purpose,
		BasisType:            input.Intent.BasisType,
		BasisID:              input.Intent.BasisID,
		Attempt:              input.Intent.Attempt,
		Generation:           input.Unsigned.Generation,
		NetworkSequence:      input.Unsigned.NetworkSequence,
		ExpiresAt:            s.now().UTC().Add(SigningAuthorizationTTL),
		NetworkID:            input.Intent.Network.ID,
		NetworkCode:          input.Intent.Network.Code,
		ChainFamily:          input.Intent.Network.Family,
		NetworkIdentifier:    input.Intent.Network.NetworkIdentifier,
		SourceAddress:        input.Intent.SourceAddress,
		FeePayerAddress:      input.FeePayerAddress,
		VaultKeyID:           input.VaultKeyID,
		DestinationAddress:   input.Intent.DestinationAddress,
		AssetStandard:        string(input.Intent.Asset.Standard),
		ContractAddress:      input.Intent.Asset.ContractAddress,
		AmountMinorUnits:     input.Intent.Amount,
		MaxNetworkFeeMinor:   input.MaxNetworkFee,
		UnsignedFingerprint:  input.Unsigned.UnsignedFingerprint,
		ApprovalBundleDigest: digest,
		PolicyVersion:        crypto.SigningAuthorizationPolicyVersion(input.Intent.Network.Family),
	}
	if err := auth.Validate(s.now().UTC()); err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("build signing authorization: %w", err)
	}

	existing, ok, err := s.repo.GetSigningAttempt(ctx, input.Intent.BasisType, input.Intent.BasisID, input.Intent.Purpose, input.Intent.Attempt, input.Unsigned.Generation)
	if err != nil {
		return SigningAuthorizationRecord{}, err
	}
	if ok {
		existing, err = matchExistingSigningAttempt(existing, input, auth)
		if err != nil {
			return SigningAuthorizationRecord{}, err
		}
		return s.ensureControlAuthorizationSignature(ctx, existing, guard)
	}

	var recordID id.ID
	err = runSigningCommand(ctx, guard, func(ctx context.Context) error {
		var err error
		recordID, err = s.repo.InsertSigningAuthorization(ctx, auth, "", input.Unsigned, input.ApprovalBundle)
		return err
	})
	if err != nil {
		if !errors.Is(err, ErrSigningAuthorizationExists) {
			return SigningAuthorizationRecord{}, err
		}
		existing, ok, loadErr := s.repo.GetSigningAttempt(ctx, auth.BasisType, auth.BasisID, auth.Purpose, auth.Attempt, auth.Generation)
		if loadErr != nil {
			return SigningAuthorizationRecord{}, loadErr
		}
		if !ok {
			return SigningAuthorizationRecord{}, err
		}
		existing, matchErr := matchExistingSigningAttempt(existing, input, auth)
		if matchErr != nil {
			return SigningAuthorizationRecord{}, matchErr
		}
		return s.ensureControlAuthorizationSignature(ctx, existing, guard)
	}
	record := SigningAuthorizationRecord{
		ID:             recordID,
		Authorization:  auth,
		ApprovalBundle: input.ApprovalBundle,
		UnsignedTx:     input.Unsigned,
		SourceWallet:   HotWallet{ID: input.Intent.SourceWalletID, Address: input.Intent.SourceAddress, VaultKeyID: input.VaultKeyID},
		Status:         "created",
	}
	return s.ensureControlAuthorizationSignature(ctx, record, guard)
}

func (s *SigningAuthorizationService) ensureControlAuthorizationSignature(ctx context.Context, record SigningAuthorizationRecord, guard SigningExecutionGuard) (SigningAuthorizationRecord, error) {
	if err := s.custody.CheckCustody(ctx, record.Authorization); err != nil {
		return SigningAuthorizationRecord{}, HoldExecution(err)
	}
	if strings.TrimSpace(record.JWS) != "" {
		return record, nil
	}
	if err := record.Authorization.Validate(s.now().UTC()); err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("persisted signing authorization is not signable: %w", err)
	}
	if err := runSigningDispatch(ctx, guard, record.Authorization, "authorize", func(ctx context.Context) error {
		return s.custody.BeginDispatch(ctx, record.Authorization.AuthorizationID, "authorize")
	}); err != nil {
		return SigningAuthorizationRecord{}, HoldExecution(err)
	}
	jws, err := s.signer.SignAuthorization(ctx, s.tenantID, record.Authorization)
	if err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("sign %s authorization: %w", record.Authorization.Purpose, err)
	}
	saveJWS := func() error {
		if record.RenewalID != nil {
			return s.repo.SetRenewedAuthorizationJWS(ctx, *record.RenewalID, jws)
		}
		return s.repo.SetSigningAuthorizationJWS(ctx, record.Authorization.AuthorizationID, jws)
	}
	if err := saveJWS(); err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("persist control authorization signature: %w", err)
	}
	record.JWS = jws
	return record, nil
}

func (s *SigningAuthorizationService) SignOrReuse(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord) (crypto.SignedTxEnvelope, error) {
	return s.signOrReuse(ctx, signer, record, nil)
}
func (s *SigningAuthorizationService) SignOrReuseGuarded(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord, guard SigningExecutionGuard) (crypto.SignedTxEnvelope, error) {
	return s.signOrReuse(ctx, signer, record, guard)
}
func (s *SigningAuthorizationService) signOrReuse(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord, guard SigningExecutionGuard) (crypto.SignedTxEnvelope, error) {
	// Saved bytes are facts; reading them never authorizes another dispatch.
	if record.Authorization.Purpose == crypto.SigningPurposeActivation && record.SignedTx != nil && guard == nil {
		if err := ValidateSignedEnvelope(record.Authorization, record.UnsignedTx, *record.SignedTx); err != nil {
			return crypto.SignedTxEnvelope{}, err
		}
		return *record.SignedTx, nil
	}
	if err := requireActivationSigningGuard(record.Authorization.Purpose, guard); err != nil {
		return crypto.SignedTxEnvelope{}, err
	}
	renewalErr := runSigningCommand(ctx, guard, func(ctx context.Context) error {
		var err error
		record, err = s.repo.ApplyExactAuthorizationRenewal(ctx, record)
		return err
	})
	if renewalErr != nil {
		return crypto.SignedTxEnvelope{}, renewalErr
	}

	// Reading already saved bytes is recovery, not another signing dispatch.
	if record.SignedTx != nil {
		if err := ValidateSignedEnvelope(record.Authorization, record.UnsignedTx, *record.SignedTx); err != nil {
			return crypto.SignedTxEnvelope{}, err
		}
		return *record.SignedTx, nil
	}
	if err := s.custody.CheckCustody(ctx, record.Authorization); err != nil {
		return crypto.SignedTxEnvelope{}, HoldExecution(err)
	}
	if strings.TrimSpace(record.JWS) == "" {
		var err error
		record, err = s.ensureControlAuthorizationSignature(ctx, record, guard)
		if err != nil {
			return crypto.SignedTxEnvelope{}, err
		}
	}
	if record.SignedTx != nil {
		if err := ValidateSignedEnvelope(record.Authorization, record.UnsignedTx, *record.SignedTx); err != nil {
			return crypto.SignedTxEnvelope{}, err
		}
		return *record.SignedTx, nil
	}
	if signer == nil || signer.Family() != record.Authorization.ChainFamily {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("policy-aware signer does not match authorization family")
	}
	network := crypto.NetworkRef{ID: record.Authorization.NetworkID, Family: record.Authorization.ChainFamily, NetworkIdentifier: record.Authorization.NetworkIdentifier}
	if err := s.custody.CheckCustodySigner(ctx, network, signer); err != nil {
		return crypto.SignedTxEnvelope{}, HoldExecution(err)
	}
	if err := runSigningDispatch(ctx, guard, record.Authorization, "sign", func(ctx context.Context) error {
		return s.custody.BeginDispatch(ctx, record.Authorization.AuthorizationID, "sign")
	}); err != nil {
		return crypto.SignedTxEnvelope{}, HoldExecution(err)
	}
	signed, err := signer.Sign(ctx, crypto.PolicySigningRequest{
		AuthorizationJWS: record.JWS,
		ApprovalBundle:   record.ApprovalBundle,
		UnsignedEnvelope: record.UnsignedTx,
	})
	if err != nil {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("family signer rejected transaction: %w", err)
	}
	if err := ValidateSignedEnvelope(record.Authorization, record.UnsignedTx, signed); err != nil {
		return crypto.SignedTxEnvelope{}, err
	}
	if err := s.repo.MarkSigningAuthorizationSigned(ctx, record.Authorization.AuthorizationID, signed); err != nil {
		return crypto.SignedTxEnvelope{}, fmt.Errorf("persist signed envelope: %w", err)
	}
	return signed, nil
}

func validateSigningAttemptInput(input SigningAttemptInput) error {
	intent := input.Intent
	if strings.TrimSpace(intent.BasisType) == "" || id.IsNil(intent.BasisID) || intent.Attempt <= 0 {
		return fmt.Errorf("signing intent basis and positive attempt are required")
	}
	if err := intent.Network.Validate(); err != nil {
		return fmt.Errorf("invalid signing intent network: %w", err)
	}
	if id.IsNil(intent.Asset.TokenID) || id.IsNil(intent.SourceWalletID) || strings.TrimSpace(intent.SourceAddress) == "" || strings.TrimSpace(intent.DestinationAddress) == "" {
		return fmt.Errorf("signing intent asset, source wallet, source, and destination are required")
	}
	validAmount := intent.Amount.IsPositive() || (intent.Purpose == crypto.SigningPurposeActivation && intent.Amount.IsZero())
	if !validAmount || strings.TrimSpace(input.VaultKeyID) == "" || strings.TrimSpace(input.FeePayerAddress) == "" {
		return fmt.Errorf("signing intent amount, Vault key, fee payer, and max fee are invalid")
	}
	if input.Unsigned.Family != intent.Network.Family || input.Unsigned.UnsignedFingerprint == "" {
		return fmt.Errorf("unsigned envelope family or fingerprint does not match intent")
	}
	if input.Unsigned.Generation != intent.EffectiveGeneration() {
		return fmt.Errorf("unsigned envelope generation does not match intent")
	}
	if err := input.Capabilities.Validate(); err != nil {
		return fmt.Errorf("invalid signing capabilities: %w", err)
	}
	if input.Capabilities.Family != intent.Network.Family {
		return fmt.Errorf("signing capabilities family does not match intent")
	}
	if input.Capabilities.RequiresNetworkSequence() && strings.TrimSpace(input.Unsigned.NetworkSequence) == "" {
		return fmt.Errorf("unsigned envelope network sequence is required")
	}
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(input.Unsigned.Family, input.Unsigned.SchemaVersion, input.Unsigned.Payload)
	if err != nil || fingerprint != input.Unsigned.UnsignedFingerprint {
		return fmt.Errorf("unsigned envelope fingerprint verification failed")
	}
	isMoneyMovement := intent.Purpose.RequiresApprovalBundle()
	if isMoneyMovement && input.ApprovalBundle.IntentJWS == "" {
		return fmt.Errorf("approval bundle is required for %s", intent.Purpose)
	}
	if !isMoneyMovement && input.ApprovalBundle.IntentJWS != "" {
		return fmt.Errorf("approval bundle is not allowed for %s", intent.Purpose)
	}
	return nil
}

func matchExistingSigningAttempt(existing SigningAuthorizationRecord, input SigningAttemptInput, expected crypto.SigningAuthorization) (SigningAuthorizationRecord, error) {
	requested := input.Unsigned
	conflict := func() (SigningAuthorizationRecord, error) {
		return SigningAuthorizationRecord{}, fmt.Errorf("%w with different bound authorization or unsigned envelope", ErrSigningAuthorizationExists)
	}
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(existing.UnsignedTx.Family, existing.UnsignedTx.SchemaVersion, existing.UnsignedTx.Payload)
	if err != nil || fingerprint != requested.UnsignedFingerprint ||
		existing.SourceWallet.ID != input.Intent.SourceWalletID ||
		existing.UnsignedTx.UnsignedFingerprint != requested.UnsignedFingerprint ||
		existing.UnsignedTx.Family != requested.Family || existing.UnsignedTx.SchemaVersion != requested.SchemaVersion ||
		existing.UnsignedTx.Generation != requested.Generation || existing.UnsignedTx.NetworkSequence != requested.NetworkSequence {
		return conflict()
	}
	// These two values are generated for each create attempt. Reuse keeps
	// the original ID and deadline; a retry cannot extend signing authority.
	saved := existing.Authorization
	saved.AuthorizationID, expected.AuthorizationID = "", ""
	saved.ExpiresAt, expected.ExpiresAt = time.Time{}, time.Time{}
	savedJSON, err := json.MarshalDeterministic(saved)
	if err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("encode saved authorization identity: %w", err)
	}
	requestedJSON, err := json.MarshalDeterministic(expected)
	if err != nil {
		return SigningAuthorizationRecord{}, fmt.Errorf("encode requested authorization identity: %w", err)
	}
	if !bytes.Equal(savedJSON, requestedJSON) {
		return conflict()
	}
	return existing, nil
}
