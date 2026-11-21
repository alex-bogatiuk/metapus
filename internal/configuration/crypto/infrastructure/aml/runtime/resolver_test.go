package runtime

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
)

type runtimeStoreStub struct {
	cfg        aml.RuntimeConfig
	admissions int
	failure    error
}

func (s *runtimeStoreStub) LoadAMLRuntimeConfig(context.Context, aml.Job) (aml.RuntimeConfig, error) {
	return s.cfg, nil
}
func (s *runtimeStoreStub) AdmitAMLSubmit(context.Context, aml.Job) error {
	s.admissions++
	return s.failure
}

type runtimeTransport struct{ calls int }

func (t *runtimeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected wire call")
}

func TestResolverRejectsChangedPinnedContextBeforePaidHTTP(t *testing.T) {
	account, network, token := id.New(), id.New(), id.New()
	request := aml.CheckRequest{MerchantID: id.New(), NetworkID: network, TokenID: token, Kind: aml.AddressSubject, Address: "receiver", ContractAddress: "contract"}
	job := aml.Job{Provider: "getblock_net", ProviderAccountID: account, MappingVersion: "v1", Request: request, RequestDigest: request.Digest("v1"), Status: aml.JobSubmitting, SubmitRequestID: "persisted-id"}
	cfg := aml.RuntimeConfig{AccountID: account, NetworkID: network, TokenID: token, MappingVersion: "v1", ContractVersion: "synthetic", ContractAddress: "contract", Currency: "TRX", ProviderTokenID: "synthetic-token", BearerToken: "synthetic-secret"}
	for _, tc := range []struct {
		name   string
		change func(*aml.RuntimeConfig)
	}{
		{"account", func(c *aml.RuntimeConfig) { c.AccountID = id.New() }},
		{"mapping", func(c *aml.RuntimeConfig) { c.MappingVersion = "v2" }},
		{"asset", func(c *aml.RuntimeConfig) { c.TokenID = id.New() }},
		{"contract", func(c *aml.RuntimeConfig) { c.ContractAddress = "different" }},
		{"synthetic mapping on live provider", func(c *aml.RuntimeConfig) { c.ContractVersion = aml.TestnetContract }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &runtimeStoreStub{cfg: cfg}
			tc.change(&store.cfg)
			wire := &runtimeTransport{}
			_, err := NewResolver(store, &http.Client{Transport: wire}).ResolveAMLProvider(t.Context(), job)
			require.Error(t, err)
			require.Zero(t, store.admissions)
			require.Zero(t, wire.calls)
		})
	}
	store := &runtimeStoreStub{cfg: cfg, failure: errors.New("budget exhausted")}
	wire := &runtimeTransport{}
	provider, err := NewResolver(store, &http.Client{Transport: wire}).ResolveAMLProvider(t.Context(), job)
	require.NoError(t, err)
	_, err = provider.Submit(t.Context(), request, job.SubmitRequestID)
	require.ErrorContains(t, err, "budget exhausted")
	require.Equal(t, 1, store.admissions)
	require.Zero(t, wire.calls)
	_, err = provider.Submit(t.Context(), request, "different-id")
	require.Error(t, err)
	require.Equal(t, 1, store.admissions)
}

func TestTestnetResolverNeverCallsPaidAdmissionOrHTTP(t *testing.T) {
	account, network, token := id.New(), id.New(), id.New()
	request := aml.CheckRequest{MerchantID: id.New(), NetworkID: network, TokenID: token, Kind: aml.AddressSubject, Address: "receiver"}
	job := aml.Job{ID: id.New(), Generation: 1, Provider: aml.TestnetProvider, ProviderAccountID: account, MappingVersion: aml.TestnetContract, Request: request, RequestDigest: request.Digest(aml.TestnetContract), Status: aml.JobSubmitting, SubmitRequestID: "submit"}
	store := &runtimeStoreStub{cfg: aml.RuntimeConfig{Provider: aml.TestnetProvider, AccountID: account, NetworkID: network, TokenID: token, NetworkFamily: "tron", NetworkIdentifier: "nile", Currency: "TRX", MappingVersion: job.MappingVersion, ContractVersion: aml.TestnetContract, Emulator: &aml.EmulatorConfig{Seed: "seed", MaxDelaySeconds: 0}}, failure: errors.New("paid calls forbidden")}
	wire := &runtimeTransport{}
	resolver := NewResolver(store, &http.Client{Transport: wire})
	provider, err := resolver.ResolveAMLProvider(t.Context(), job)
	require.NoError(t, err)
	ticket, err := provider.Submit(t.Context(), request, "submit")
	require.NoError(t, err)
	job.Status, job.PollRequestID, job.Ticket = aml.JobPolling, "poll", &ticket
	provider, err = resolver.ResolveAMLProvider(t.Context(), job)
	require.NoError(t, err)
	result, err := provider.GetResult(t.Context(), ticket, "poll")
	require.NoError(t, err)
	require.Equal(t, aml.CheckSucceeded, result.State)
	require.Zero(t, store.admissions)
	require.Zero(t, wire.calls)
	store.cfg.NetworkIdentifier = "mainnet"
	_, err = resolver.ResolveAMLProvider(t.Context(), job)
	require.Error(t, err)
	require.Zero(t, store.admissions)
	require.Zero(t, wire.calls)
}
