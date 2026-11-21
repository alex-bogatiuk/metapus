package crypto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type transactionSignerStub struct {
	family ChainFamily
}

func (s transactionSignerStub) Family() ChainFamily { return s.family }
func (s transactionSignerStub) CheckReady(context.Context, NetworkRef, string) error {
	return nil
}
func (s transactionSignerStub) Sign(context.Context, PolicySigningRequest) (SignedTxEnvelope, error) {
	return SignedTxEnvelope{}, nil
}

func TestFamilySignerRegistry(t *testing.T) {
	registry := NewFamilySignerRegistry()
	signer := transactionSignerStub{family: ChainFamilyTRON}

	require.NoError(t, registry.Register(signer))
	resolved, err := registry.Resolve(ChainFamilyTRON)
	require.NoError(t, err)
	require.Equal(t, signer, resolved)

	require.ErrorContains(t, registry.Register(signer), "already registered")
	_, err = registry.Resolve(ChainFamilyEVM)
	require.ErrorIs(t, err, ErrFamilySignerNotFound)
	require.Error(t, registry.Register(nil))
	require.Error(t, registry.Register(transactionSignerStub{family: ChainFamily("unknown")}))
}
