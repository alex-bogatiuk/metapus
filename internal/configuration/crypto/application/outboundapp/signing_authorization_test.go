package outboundapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type signingStoreStub struct {
	SigningAuthorizationRepository
	jws    string
	signed *crypto.SignedTxEnvelope
}

func (s *signingStoreStub) ApplyExactAuthorizationRenewal(_ context.Context, record SigningAuthorizationRecord) (SigningAuthorizationRecord, error) {
	return record, nil
}
func (s *signingStoreStub) SetSigningAuthorizationJWS(_ context.Context, _ string, jws string) error {
	s.jws = jws
	return nil
}
func (s *signingStoreStub) MarkSigningAuthorizationSigned(_ context.Context, _ string, signed crypto.SignedTxEnvelope) error {
	s.signed = &signed
	return nil
}

type signingCustodyStub struct {
	SigningCustody
	calls  int
	err    error
	stages []string
}

func (s *signingCustodyStub) CheckCustody(context.Context, crypto.SigningAuthorization) error {
	s.calls++
	return s.err
}
func (s *signingCustodyStub) CheckCustodySigner(context.Context, crypto.NetworkRef, crypto.PolicyAwareTransactionSigner) error {
	return s.err
}
func (s *signingCustodyStub) BeginDispatch(_ context.Context, _ string, stage string) error {
	s.stages = append(s.stages, stage)
	return nil
}

type controlSignerStub struct {
	crypto.SigningAuthorizationSigner
	failure  error
	requests []crypto.SigningAuthorization
}

func (s *controlSignerStub) SignAuthorization(_ context.Context, _ string, auth crypto.SigningAuthorization) (string, error) {
	s.requests = append(s.requests, auth)
	if s.failure != nil {
		err := s.failure
		s.failure = nil
		return "", err
	}
	return "immutable-jws", nil
}

type familySignerStub struct {
	crypto.PolicyAwareTransactionSigner
	store  *signingStoreStub
	result crypto.SignedTxEnvelope
	calls  int
}

func (f *familySignerStub) Family() crypto.ChainFamily { return crypto.ChainFamilyEVM }
func (f *familySignerStub) Sign(context.Context, crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	if f.store.jws == "" {
		return crypto.SignedTxEnvelope{}, errors.New("family signing before durable control JWS")
	}
	f.calls++
	return f.result, nil
}

func TestSigningRetriesControlResponseAndReusesSavedBytes(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	auth := crypto.SigningAuthorization{SchemaVersion: crypto.SigningAuthorizationSchemaVersion, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeSweep, BasisType: "CryptoSweepAttempt", BasisID: id.New(), Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: now.Add(time.Hour), NetworkID: id.New(), NetworkCode: "TEST", ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: "11155111", SourceAddress: "source", FeePayerAddress: "source", VaultKeyID: "key", DestinationAddress: "destination", AssetStandard: "native", AmountMinorUnits: types.NewRawAmountFromInt64(1), MaxNetworkFeeMinor: types.NewRawAmountFromInt64(1), UnsignedFingerprint: "fixed-fingerprint", PolicyVersion: crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyEVM)}
	unsigned := crypto.UnsignedTxEnvelope{Family: auth.ChainFamily, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint}
	signed := crypto.SignedTxEnvelope{Family: auth.ChainFamily, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: "saved-hash", Payload: []byte("exact saved bytes")}
	record := SigningAuthorizationRecord{Authorization: auth, UnsignedTx: unsigned}
	store, custody := &signingStoreStub{}, &signingCustodyStub{}
	lost := errors.New("lost control signer response")
	control := &controlSignerStub{failure: lost}
	family := &familySignerStub{store: store, result: signed}
	service, err := NewSigningAuthorizationService(store, control, custody, auth.TenantID, func() time.Time { return now })
	require.NoError(t, err)
	_, err = service.SignOrReuse(t.Context(), family, record)
	require.ErrorIs(t, err, lost)
	require.Empty(t, store.jws)
	require.Nil(t, store.signed)
	require.Zero(t, family.calls)
	result, err := service.SignOrReuse(t.Context(), family, record)
	require.NoError(t, err)
	require.Equal(t, signed, result)
	require.Len(t, control.requests, 2)
	require.Equal(t, control.requests[0], control.requests[1], "retry must preserve immutable authorization identity and expiry")
	require.Equal(t, 1, family.calls)
	require.NotNil(t, store.signed)
	record.SignedTx = store.signed
	custody.err = errors.New("current custody policy prohibits another send")
	before := custody.calls
	recovered, err := service.SignOrReuse(t.Context(), nil, record)
	require.NoError(t, err)
	require.Equal(t, signed, recovered)
	require.Equal(t, before, custody.calls, "reading saved bytes must not create another dispatch")
	require.Equal(t, 1, family.calls)
	require.Equal(t, []string{"authorize", "authorize", "sign"}, custody.stages)
}
