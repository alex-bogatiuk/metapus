package outboundapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
)

type resourceStrategyStub struct {
	result ResourceReservationResult
	err    error
}

func (s resourceStrategyStub) PrepareResource(context.Context, ResourceRequest) (ResourceReservationResult, error) {
	return s.result, s.err
}
func (resourceStrategyStub) MarkResourceBroadcast(context.Context, ResourceBroadcastCommand) error {
	return nil
}
func (resourceStrategyStub) SettleResource(context.Context, string, crypto.ChainReceipt) error {
	return nil
}
func (resourceStrategyStub) ReleaseResource(context.Context, id.ID, string) error { return nil }
func (resourceStrategyStub) MaintainResources(context.Context, string) error      { return nil }

func TestPreferRentalFallbackIsFailClosedOnAmbiguity(t *testing.T) {
	policy := crypto.NetworkPolicy{ExternalResources: &crypto.ExternalResourcePolicy{Strategy: crypto.ResourcePreferRental, ProviderFailureThreshold: 1}}
	_, _, err := PrepareResourceByPolicy(context.Background(), resourceStrategyStub{err: crypto.ErrResourceOrderAmbiguous}, policy, true, ResourceRequest{})
	require.ErrorIs(t, err, crypto.ErrResourceOrderAmbiguous)
	_, strategy, err := PrepareResourceByPolicy(context.Background(), resourceStrategyStub{err: crypto.ErrResourceProviderTransient}, policy, true, ResourceRequest{})
	require.NoError(t, err)
	require.Equal(t, crypto.FeeStrategyNativeBurn, strategy)
	_, _, err = PrepareResourceByPolicy(context.Background(), resourceStrategyStub{err: crypto.ErrResourceProviderInvalidReply}, policy, true, ResourceRequest{})
	require.Error(t, err)
	_, _, err = PrepareResourceByPolicy(context.Background(), resourceStrategyStub{err: errors.New("unexpected")}, policy, true, ResourceRequest{})
	require.Error(t, err)
}
