package outboundapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
)

type replaySigningStore struct {
	signingStoreStub
	existing       SigningAuthorizationRecord
	racing         bool
	reads, inserts int
}

func (s *replaySigningStore) GetSigningAttempt(context.Context, string, id.ID, crypto.SigningPurpose, int, int) (SigningAuthorizationRecord, bool, error) {
	s.reads++
	return s.existing, !s.racing || s.reads > 1, nil
}
func (s *replaySigningStore) InsertSigningAuthorization(context.Context, crypto.SigningAuthorization, string, crypto.UnsignedTxEnvelope, ...crypto.ApprovalBundle) (id.ID, error) {
	s.inserts++
	return id.Nil(), ErrSigningAuthorizationExists
}

type replaySigningCustody struct{ signingCustodyStub }

func (*replaySigningCustody) PrepareTransferCustody(_ context.Context, _ crypto.TransferIntent, merchant *id.ID) (*id.ID, error) {
	return merchant, nil
}

func signingReplayFixture(t *testing.T) (SigningAttemptInput, SigningAuthorizationRecord, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	network, asset, source, merchant := id.New(), id.New(), id.New(), id.New()
	input := SigningAttemptInput{Intent: crypto.TransferIntent{BasisType: "WithdrawalRequest", BasisID: id.New(), Purpose: crypto.SigningPurposeWithdrawal, Attempt: 1, Generation: 1,
		Network: crypto.NetworkRef{ID: network, Code: "TEST", Family: crypto.ChainFamilyEVM, NetworkIdentifier: "1", NativeTokenID: asset, NativeTokenSymbol: "ETH", NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: crypto.FinalityByConfirmations},
		Asset:   crypto.AssetRef{TokenID: asset, Standard: token.TokenStandardNative, Decimals: 18}, SourceWalletID: source, SourceAddress: "source", DestinationAddress: "destination", Amount: types.MustCryptoAmount("70")},
		Capabilities: crypto.ChainFamilyCapabilities{Family: crypto.ChainFamilyEVM, SequenceModel: crypto.SequenceAccount, WalletProvisioning: crypto.WalletProvisioningDerived, ExternalResourceMode: crypto.ExternalResourceNone, CheckpointTimeUnit: crypto.CheckpointUnixSeconds, TokenAccountModel: crypto.TokenAccountSameAddress, RequiredRPCRoles: []string{"fullnode"}}, MerchantID: &merchant, VaultKeyID: "key", FeePayerAddress: "source", MaxNetworkFee: types.MustCryptoAmount("10"), ApprovalBundle: crypto.ApprovalBundle{IntentJWS: "intent"}}
	payload := []byte(`{"nonce":"0","amount":"70"}`)
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(crypto.ChainFamilyEVM, 1, payload)
	require.NoError(t, err)
	input.Unsigned = crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: fingerprint, Payload: payload}
	digest, err := crypto.DigestJSON(input.ApprovalBundle)
	require.NoError(t, err)
	auth := crypto.SigningAuthorization{SchemaVersion: 2, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &merchant, Purpose: input.Intent.Purpose, BasisType: input.Intent.BasisType, BasisID: input.Intent.BasisID, Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: now.Add(time.Minute),
		NetworkID: network, NetworkCode: "TEST", ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: "1", SourceAddress: "source", FeePayerAddress: "source", VaultKeyID: "key", DestinationAddress: "destination", AssetStandard: "native", AmountMinorUnits: input.Intent.Amount, MaxNetworkFeeMinor: input.MaxNetworkFee, UnsignedFingerprint: fingerprint, ApprovalBundleDigest: digest, PolicyVersion: "evm-v1"}
	record := SigningAuthorizationRecord{ID: id.New(), Authorization: auth, ApprovalBundle: input.ApprovalBundle, UnsignedTx: input.Unsigned, JWS: "saved-jws", SourceWallet: HotWallet{ID: source, Address: "source", VaultKeyID: "key"}}
	return input, record, now
}

func TestSigningCreateReplayChecksBoundIdentityBeforeDispatch(t *testing.T) {
	for _, racing := range []bool{false, true} {
		for _, change := range []string{"identical", "merchant", "destination", "amount", "fee_cap", "key", "approval", "tenant", "source_wallet", "stored_payload"} {
			t.Run(change+map[bool]string{true: "/insert_race", false: "/existing"}[racing], func(t *testing.T) {
				input, saved, now := signingReplayFixture(t)
				tenant := saved.Authorization.TenantID
				switch change {
				case "merchant":
					other := id.New()
					input.MerchantID = &other
				case "destination":
					input.Intent.DestinationAddress = "other-destination"
				case "amount":
					input.Intent.Amount = types.MustCryptoAmount("71")
				case "fee_cap":
					input.MaxNetworkFee = types.MustCryptoAmount("11")
				case "key":
					input.VaultKeyID = "other-key"
				case "approval":
					input.ApprovalBundle.IntentJWS = "other-intent"
				case "tenant":
					tenant = id.New().String()
				case "source_wallet":
					input.Intent.SourceWalletID = id.New()
				case "stored_payload":
					saved.UnsignedTx.Payload = []byte(`{"nonce":"0","amount":"71"}`)
				}
				store := &replaySigningStore{existing: saved, racing: racing}
				custody, signer := &replaySigningCustody{}, &controlSignerStub{}
				service, err := NewSigningAuthorizationService(store, signer, custody, tenant, func() time.Time { return now })
				require.NoError(t, err)
				got, err := service.Create(t.Context(), input)
				if change == "identical" {
					require.NoError(t, err)
					require.Equal(t, saved.ID, got.ID)
					require.Equal(t, saved.Authorization.AuthorizationID, got.Authorization.AuthorizationID)
					require.Equal(t, saved.Authorization.ExpiresAt, got.Authorization.ExpiresAt, "retry cannot extend signing authority")
				} else {
					require.ErrorIs(t, err, ErrSigningAuthorizationExists)
				}
				require.Empty(t, signer.requests)
				require.Empty(t, custody.stages, "conflicting intent must not create a dispatch permit")
				require.Equal(t, map[bool]int{true: 1, false: 0}[racing], store.inserts)
			})
		}
	}
}

func TestSigningCreateReplayCannotRenewExpiredUnsignedAuthorization(t *testing.T) {
	input, saved, now := signingReplayFixture(t)
	saved.Authorization.ExpiresAt = now.Add(-time.Minute)
	saved.JWS = ""
	store := &replaySigningStore{existing: saved}
	custody, signer := &replaySigningCustody{}, &controlSignerStub{}
	service, err := NewSigningAuthorizationService(store, signer, custody, saved.Authorization.TenantID, func() time.Time { return now })
	require.NoError(t, err)
	_, err = service.Create(t.Context(), input)
	require.ErrorContains(t, err, "expired")
	require.Empty(t, signer.requests)
	require.Empty(t, custody.stages)
	require.Zero(t, store.inserts)
	require.Empty(t, store.jws)
}
