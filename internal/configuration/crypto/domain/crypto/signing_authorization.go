package crypto

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const (
	SigningAuthorizationSequenceLessSchemaVersion = 1
	SigningAuthorizationSchemaVersion             = 2
	signingAuthorizationPolicyRevision            = "v1"
)

// SigningAuthorizationSchemaVersionFor keeps families without an account
// sequence on the stable v1 wire contract. Account-sequence families opt into
// nonce/replacement fields introduced by v2. The database stores generation
// separately for both.
func SigningAuthorizationSchemaVersionFor(sequence SequenceModel) int {
	switch sequence {
	case SequenceAccount:
		return SigningAuthorizationSchemaVersion
	case SequenceNone:
		return SigningAuthorizationSequenceLessSchemaVersion
	default:
		return 0
	}
}

// SigningAuthorization is the complete policy input signed by the control
// plane. A family plugin must independently decode the opaque transaction and
// prove that every relevant field matches this authorization.
type SigningAuthorization struct {
	SchemaVersion        int             `json:"schema_version"`
	AuthorizationID      string          `json:"authorization_id"`
	TenantID             string          `json:"tenant_id"`
	MerchantID           *id.ID          `json:"merchant_id,omitempty"`
	Purpose              SigningPurpose  `json:"purpose"`
	BasisType            string          `json:"basis_type"`
	BasisID              id.ID           `json:"basis_id"`
	Attempt              int             `json:"attempt"`
	Generation           int             `json:"generation"`
	NetworkSequence      string          `json:"network_sequence,omitempty"`
	ExpiresAt            time.Time       `json:"expires_at"`
	NetworkID            id.ID           `json:"network_id"`
	NetworkCode          string          `json:"network_code"`
	ChainFamily          ChainFamily     `json:"chain_family"`
	NetworkIdentifier    string          `json:"network_identifier"`
	SourceAddress        string          `json:"source_address"`
	FeePayerAddress      string          `json:"fee_payer_address"`
	VaultKeyID           string          `json:"vault_key_id"`
	DestinationAddress   string          `json:"destination_address"`
	AssetStandard        string          `json:"asset_standard"`
	ContractAddress      string          `json:"contract_address"`
	AmountMinorUnits     types.RawAmount `json:"amount_minor_units"`
	MaxNetworkFeeMinor   types.RawAmount `json:"max_network_fee_minor"`
	UnsignedFingerprint  string          `json:"unsigned_fingerprint"`
	ApprovalBundleDigest string          `json:"approval_bundle_digest"`
	PolicyVersion        string          `json:"policy_version"`
}

// SigningAuthorizationPolicyVersion returns the policy contract understood by
// the signer for a chain family. An empty result means the family is invalid.
func SigningAuthorizationPolicyVersion(family ChainFamily) string {
	if !family.Valid() {
		return ""
	}
	return string(family) + "-" + signingAuthorizationPolicyRevision
}

func (a SigningAuthorization) Validate(now time.Time) error {
	if a.SchemaVersion != SigningAuthorizationSequenceLessSchemaVersion && a.SchemaVersion != SigningAuthorizationSchemaVersion {
		return fmt.Errorf("unsupported signing authorization schema: %d", a.SchemaVersion)
	}
	if a.AuthorizationID == "" || a.TenantID == "" || a.BasisType == "" {
		return errors.New("authorization, tenant, and basis are required")
	}
	if !a.Purpose.Valid() {
		return fmt.Errorf("unsupported signing purpose: %s", a.Purpose)
	}
	if id.IsNil(a.BasisID) || id.IsNil(a.NetworkID) || a.Attempt <= 0 {
		return errors.New("basis ID, network ID, and positive attempt are required")
	}
	if a.SchemaVersion == SigningAuthorizationSchemaVersion && a.Generation <= 0 {
		return errors.New("signing authorization v2 requires a positive generation")
	}
	if a.SchemaVersion == SigningAuthorizationSequenceLessSchemaVersion && a.Generation < 0 {
		return errors.New("sequence-less signing authorization generation cannot be negative")
	}
	if a.ExpiresAt.IsZero() || !now.Before(a.ExpiresAt) {
		return errors.New("signing authorization is expired")
	}
	if !a.ChainFamily.Valid() || a.NetworkCode == "" || a.NetworkIdentifier == "" {
		return errors.New("network identity is incomplete")
	}
	expectedPolicyVersion := SigningAuthorizationPolicyVersion(a.ChainFamily)
	if a.PolicyVersion != expectedPolicyVersion {
		return fmt.Errorf("unsupported signing policy version: %s", a.PolicyVersion)
	}
	if a.SourceAddress == "" || a.FeePayerAddress == "" || a.DestinationAddress == "" || a.VaultKeyID == "" {
		return errors.New("source, fee payer, destination, and vault key are required")
	}
	if a.Purpose == SigningPurposeWithdrawal && (a.MerchantID == nil || id.IsNil(*a.MerchantID)) {
		return errors.New("withdrawal authorization requires merchant ID")
	}
	if a.Purpose.RequiresApprovalBundle() && a.ApprovalBundleDigest == "" {
		return errors.New("money movement authorization requires approval bundle digest")
	}
	if !a.Purpose.RequiresApprovalBundle() && a.ApprovalBundleDigest != "" {
		return errors.New("non-money-movement authorization cannot include approval bundle digest")
	}
	validAmount := a.AmountMinorUnits.IsPositive() || (a.Purpose == SigningPurposeActivation && a.AmountMinorUnits.IsZero())
	if a.AssetStandard == "" || !validAmount || !a.MaxNetworkFeeMinor.IsPositive() || a.UnsignedFingerprint == "" {
		return errors.New("asset, positive amount, max fee, and unsigned fingerprint are required")
	}
	if a.SchemaVersion == SigningAuthorizationSchemaVersion && a.NetworkSequence == "" {
		return errors.New("signing authorization v2 requires a network sequence")
	}
	return nil
}

// SigningAuthorizationKeyIDProvider exposes only the active public key handle
// needed to cross-check a family plugin keyring before transaction signing.
type SigningAuthorizationKeyIDProvider interface {
	AuthorizationKeyID(ctx context.Context, tenantID string) (string, error)
}

// SigningAuthorizationSigner signs the immutable control-plane authorization.
// Production implementations keep the Ed25519 private key behind Vault
// Transit; callers receive only the compact JWS.
type SigningAuthorizationSigner interface {
	SignAuthorization(ctx context.Context, tenantID string, auth SigningAuthorization) (string, error)
	CheckReady(ctx context.Context, tenantID string) error
}
