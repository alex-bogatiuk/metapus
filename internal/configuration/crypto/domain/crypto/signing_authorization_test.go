package crypto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestSigningAuthorizationPolicyVersion(t *testing.T) {
	assert.Equal(t, "tron-v1", SigningAuthorizationPolicyVersion(ChainFamilyTRON))
	assert.Equal(t, "evm-v1", SigningAuthorizationPolicyVersion(ChainFamilyEVM))
	assert.Empty(t, SigningAuthorizationPolicyVersion(ChainFamily("unknown")))
}

func TestSigningAuthorizationSchemaVersionFollowsSequenceModel(t *testing.T) {
	require.Equal(t, SigningAuthorizationSequenceLessSchemaVersion, SigningAuthorizationSchemaVersionFor(SequenceNone))
	require.Equal(t, SigningAuthorizationSchemaVersion, SigningAuthorizationSchemaVersionFor(SequenceAccount))
	require.Zero(t, SigningAuthorizationSchemaVersionFor(SequenceModel("unknown")))
}

func TestSigningAuthorizationValidate(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	auth := validSigningAuthorization(now)
	require.NoError(t, auth.Validate(now))

	t.Run("schema mismatch", func(t *testing.T) {
		invalid := auth
		invalid.SchemaVersion = 99
		require.ErrorContains(t, invalid.Validate(now), "unsupported signing authorization schema")
	})

	t.Run("v2 requires network sequence", func(t *testing.T) {
		invalid := auth
		invalid.SchemaVersion = SigningAuthorizationSchemaVersion
		invalid.NetworkSequence = ""
		require.ErrorContains(t, invalid.Validate(now), "network sequence")
	})

	t.Run("v2 with network sequence", func(t *testing.T) {
		valid := auth
		valid.SchemaVersion = SigningAuthorizationSchemaVersion
		valid.NetworkSequence = "7"
		require.NoError(t, valid.Validate(now))
	})

	t.Run("policy mismatch", func(t *testing.T) {
		invalid := auth
		invalid.PolicyVersion = "tron-unsupported"
		require.ErrorContains(t, invalid.Validate(now), "unsupported signing policy version")
	})

	t.Run("expired", func(t *testing.T) {
		invalid := auth
		invalid.ExpiresAt = now
		require.ErrorContains(t, invalid.Validate(now), "expired")
	})

	t.Run("unsupported purpose", func(t *testing.T) {
		invalid := auth
		invalid.Purpose = SigningPurpose("unknown")
		require.ErrorContains(t, invalid.Validate(now), "unsupported signing purpose")
	})

	t.Run("money movement without approval digest", func(t *testing.T) {
		invalid := auth
		merchantID := id.MustParse("00000000-0000-0000-0000-000000000004")
		invalid.Purpose = SigningPurposeWithdrawal
		invalid.MerchantID = &merchantID
		require.ErrorContains(t, invalid.Validate(now), "approval bundle digest")
	})

	t.Run("non-money movement with approval digest", func(t *testing.T) {
		invalid := auth
		invalid.ApprovalBundleDigest = "unexpected"
		require.ErrorContains(t, invalid.Validate(now), "cannot include approval bundle digest")
	})
}

func validSigningAuthorization(now time.Time) SigningAuthorization {
	return SigningAuthorization{
		SchemaVersion:       SigningAuthorizationSequenceLessSchemaVersion,
		AuthorizationID:     "auth-test",
		TenantID:            "00000000-0000-0000-0000-000000000001",
		Purpose:             SigningPurposeSweep,
		BasisType:           "CryptoSweep",
		BasisID:             id.MustParse("00000000-0000-0000-0000-000000000002"),
		Attempt:             1,
		Generation:          1,
		ExpiresAt:           now.Add(time.Minute),
		NetworkID:           id.MustParse("00000000-0000-0000-0000-000000000003"),
		NetworkCode:         "TRON-NILE",
		ChainFamily:         ChainFamilyTRON,
		NetworkIdentifier:   "nile",
		SourceAddress:       "source",
		FeePayerAddress:     "source",
		VaultKeyID:          "vault-key",
		DestinationAddress:  "destination",
		AssetStandard:       "native",
		AmountMinorUnits:    types.NewRawAmountFromInt64(1),
		MaxNetworkFeeMinor:  types.NewRawAmountFromInt64(1_000_000),
		UnsignedFingerprint: "9a5ba2e42fc6a3e87fcbe97355576361f80f861d4034f4e8ea09544db7d00258",
		PolicyVersion:       SigningAuthorizationPolicyVersion(ChainFamilyTRON),
	}
}
