package crypto_worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

type readinessTestSigner struct{ err error }

func (s readinessTestSigner) Family() crypto.ChainFamily { return crypto.ChainFamilyTRON }
func (s readinessTestSigner) CheckReady(context.Context, crypto.NetworkRef, string) error {
	return s.err
}
func (s readinessTestSigner) Sign(context.Context, crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	return crypto.SignedTxEnvelope{}, nil
}

type readinessTestAuthorizationSigner struct{ err error }

func (s readinessTestAuthorizationSigner) CheckReady(context.Context) (string, error) {
	return "transit/metapus-signing-auth-00000000-0000-0000-0000-000000000001/v1", s.err
}

func TestResolveOutboundSignerFailsClosedBeforeResourcePreparation(t *testing.T) {
	network := crypto.NetworkRef{
		ID: id.MustParse("00000000-0000-0000-0000-000000000001"), Code: "TRON-NILE",
		Family: crypto.ChainFamilyTRON, NetworkIdentifier: "nile",
		NativeTokenID: id.MustParse("00000000-0000-0000-0000-000000000002"), NativeDecimals: 6,
	}
	registry := crypto.NewFamilySignerRegistry()
	require.NoError(t, registry.Register(readinessTestSigner{}))
	_, err := resolveOutboundSigner(context.Background(), registry, readinessTestAuthorizationSigner{}, network)
	require.NoError(t, err)

	broken := crypto.NewFamilySignerRegistry()
	require.NoError(t, broken.Register(readinessTestSigner{err: errors.New("mount missing")}))
	_, err = resolveOutboundSigner(context.Background(), broken, readinessTestAuthorizationSigner{}, network)
	require.ErrorContains(t, err, "mount missing")
	_, err = resolveOutboundSigner(context.Background(), nil, readinessTestAuthorizationSigner{}, network)
	require.Error(t, err)
	_, err = resolveOutboundSigner(context.Background(), registry, readinessTestAuthorizationSigner{err: errors.New("transit missing")}, network)
	require.ErrorContains(t, err, "transit missing")
}
