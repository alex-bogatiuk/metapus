package emulator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

func fixture() (aml.Job, aml.RuntimeConfig) {
	r := aml.CheckRequest{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New(), Kind: aml.TransferSubject, Address: "recipient", TxHash: "tx", EventKind: "contract_log", BlockHash: "block", EconomicsHash: "economics", MatchingEffects: 1, ContractAddress: "contract"}
	j := aml.Job{ID: id.New(), Generation: 1, Provider: aml.TestnetProvider, ProviderAccountID: id.New(), MappingVersion: aml.TestnetContract, Request: r, RequestDigest: r.Digest(aml.TestnetContract), SubmitRequestID: "submit", Status: aml.JobSubmitting}
	c := aml.RuntimeConfig{AccountID: j.ProviderAccountID, Provider: j.Provider, NetworkID: r.NetworkID, TokenID: r.TokenID, NetworkFamily: "tron", NetworkIdentifier: "nile", MappingVersion: j.MappingVersion, ContractVersion: aml.TestnetContract, ContractAddress: r.ContractAddress, Emulator: &aml.EmulatorConfig{Seed: "test-seed", MaxDelaySeconds: 10, UnknownRateBP: 1000}}
	return j, c
}

func TestEmulatorTicketSurvivesRestartAndPollingIsStable(t *testing.T) {
	j, c := fixture()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	p, err := New(j, c)
	require.NoError(t, err)
	p.now = func() time.Time { return now }
	ticket, err := p.Submit(t.Context(), j.Request, j.SubmitRequestID)
	require.NoError(t, err)
	raw, err := json.Marshal(ticket)
	require.NoError(t, err)
	var restored aml.Ticket
	require.NoError(t, json.Unmarshal(raw, &restored))
	j.Status, j.PollRequestID, j.Ticket = aml.JobPolling, "poll", &restored
	p, err = New(j, c)
	require.NoError(t, err)
	p.now = func() time.Time { return now.Add(time.Minute) }
	first, err := p.GetResult(t.Context(), restored, "poll")
	require.NoError(t, err)
	require.Equal(t, aml.CheckSucceeded, first.State)
	require.EqualValues(t, 7, first.Report.ID.Version())
	second, err := p.GetResult(t.Context(), restored, "poll")
	require.NoError(t, err)
	require.Equal(t, first, second)
	var payload struct {
		Emulated bool   `json:"emulated"`
		Provider string `json:"provider"`
	}
	require.NoError(t, json.Unmarshal(first.Report.RawResponse, &payload))
	require.True(t, payload.Emulated)
	require.Equal(t, aml.TestnetProvider, payload.Provider)
	bad := restored
	bad.Request.TokenID = id.New()
	_, err = p.GetResult(t.Context(), bad, "poll")
	require.Error(t, err)
	_, err = p.GetResult(t.Context(), restored, "other")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = p.GetResult(ctx, restored, "poll")
	require.ErrorIs(t, err, context.Canceled)
}

func TestEmulatorVariesByJobAndGenerationWithoutInvalidScores(t *testing.T) {
	j, c := fixture()
	j.ProviderAccountID = id.MustParse("22222222-2222-4222-8222-222222222222")
	j.Request.MerchantID = id.MustParse("33333333-3333-4333-8333-333333333333")
	j.Request.NetworkID = id.MustParse("44444444-4444-4444-8444-444444444444")
	j.Request.TokenID = id.MustParse("55555555-5555-4555-8555-555555555555")
	j.RequestDigest = j.Request.Digest(j.MappingVersion)
	c.AccountID, c.NetworkID, c.TokenID = j.ProviderAccountID, j.Request.NetworkID, j.Request.TokenID
	c.Emulator.MaxDelaySeconds = 0
	policy := aml.Policy{Version: "test", LowMax: 3000, MediumMax: 7000, MaxReportAge: time.Hour, DecisionTTL: time.Minute, AcceptedSignals: []string{"wallet", "sanctions"}, CriticalSignals: []string{"sanctions"}}
	classes := map[aml.Class]bool{}
	for i := 1; i <= 256; i++ {
		j.ID = id.MustParse("11111111-1111-4111-8111-111111111111")
		j.Generation = int64(i)
		j.SubmitRequestID = fmt.Sprint(i)
		j.Status = aml.JobSubmitting
		p, err := New(j, c)
		require.NoError(t, err)
		ticket, err := p.Submit(t.Context(), j.Request, j.SubmitRequestID)
		require.NoError(t, err)
		j.Status, j.PollRequestID, j.Ticket = aml.JobPolling, "poll", &ticket
		p, err = New(j, c)
		require.NoError(t, err)
		result, err := p.GetResult(t.Context(), ticket, "poll")
		require.NoError(t, err)
		require.True(t, result.Report.Score.Valid())
		assessment, err := policy.Assess(*result.Report, time.Now())
		require.NoError(t, err)
		classes[assessment.Class] = true
	}
	require.Len(t, classes, 4)
}

func TestEmulatorRefusesMainnetEvenWhenCodeLooksLikeTestnet(t *testing.T) {
	j, c := fixture()
	for _, network := range []struct{ family, identifier string }{{"tron", "mainnet"}, {"evm", "1"}, {"evm", "5"}, {"tron", "11155111"}, {"evm", "nile"}} {
		c.NetworkFamily, c.NetworkIdentifier = network.family, network.identifier
		_, err := New(j, c)
		require.Error(t, err)
	}
	c.NetworkFamily, c.NetworkIdentifier = "evm", "11155111"
	_, err := New(j, c)
	require.NoError(t, err)
}
