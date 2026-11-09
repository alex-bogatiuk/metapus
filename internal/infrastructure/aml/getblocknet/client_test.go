package getblocknet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/aml"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, transport roundTripFunc) (*Client, aml.CheckRequest) {
	t.Helper()
	request := aml.CheckRequest{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New(), Kind: aml.TransferSubject,
		ContractAddress: "test-contract", Address: "test-receiver", TxHash: "test-tx", EventKind: "contract_log", EventIndex: 0,
		BlockHash: "test-block", EconomicsHash: "verified-economics", MatchingEffects: 1}
	client, err := New(Config{AccountID: id.New(), BearerToken: "test-secret", ContractVersion: "synthetic-test-contract-v1",
		Mappings: []AssetMapping{{NetworkID: request.NetworkID, TokenID: request.TokenID, ContractAddress: request.ContractAddress,
			Currency: "TRX", ProviderTokenID: "123", Version: "synthetic-test-mapping-v1", CapabilityAccepted: true}},
		PendingStatuses: []string{"TEST_PENDING"}, FailureStatuses: []string{"TEST_FAILED"}}, &http.Client{Transport: transport})
	require.NoError(t, err)
	client.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return client, request
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func successBody(score string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":"poll-1","result":{"check":{"hash":"check-hash","currency":"TRX","address":"test-receiver","transaction":"test-tx","status":"SUCCESS","resultDate":"2026-10-25T12:00:00Z","report":{"amount":null,"riskscore":%s,"signals":{"wallet":1,"sanctions":0}}}}}`, score)
}

func ticketFor(client *Client, request aml.CheckRequest) aml.Ticket {
	mapping := client.config.Mappings[0]
	return aml.Ticket{Provider: ProviderName, ProviderAccountID: client.config.AccountID, CheckHash: "check-hash",
		SubmitRequestID: "submit-1", RequestDigest: request.Digest(mapping.Version), MappingVersion: mapping.Version,
		Request: request, SubmittedAt: client.now()}
}

func TestSubmitPinsExactAssetRequestAndAccountWithoutPolling(t *testing.T) {
	calls := 0
	client, request := testClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, _endpoint, r.URL.String())
		require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		var payload rpcRequest[checkParams]
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "submit-1", payload.ID)
		require.Equal(t, "checkup.checktx", payload.Method)
		require.Equal(t, "TRX", payload.Params.Currency)
		require.Equal(t, "123", payload.Params.TokenID)
		require.Equal(t, "test-receiver", payload.Params.Address)
		return response(`{"jsonrpc":"2.0","id":"submit-1","result":{"check":{"hash":"check-hash"}}}`), nil
	})
	ticket, err := client.Submit(t.Context(), request, "submit-1")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, client.config.AccountID, ticket.ProviderAccountID)
	require.Equal(t, request.Digest(client.config.Mappings[0].Version), ticket.RequestDigest)
	require.Equal(t, "check-hash", ticket.CheckHash)
}

func TestSubmitUnknownOutcomeDoesNotRetryOrExposeSecrets(t *testing.T) {
	calls := 0
	client, request := testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("test-secret must not appear in diagnostics")
	})
	_, err := client.Submit(t.Context(), request, "submit-1")
	var protocol *ProtocolError
	require.ErrorAs(t, err, &protocol)
	require.True(t, protocol.SubmissionMayExist)
	require.Equal(t, 1, calls)
	require.NotContains(t, err.Error(), "test-secret")
}

func TestPollNormalizesOnlyFractionScaleAndRoundsUp(t *testing.T) {
	tests := []struct {
		give string
		want aml.ScoreBP
	}{{"0", 0}, {"0.056", 560}, {"0.30", 3000}, {"0.300000001", 3001}, {"0.70", 7000}, {"1", 10000}}
	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			client, request := testClient(t, func(*http.Request) (*http.Response, error) { return response(successBody(tt.give)), nil })
			result, err := client.GetResult(t.Context(), ticketFor(client, request), "poll-1")
			require.NoError(t, err)
			require.Equal(t, aml.CheckSucceeded, result.State)
			require.Equal(t, tt.want, *result.Report.Score)
			require.True(t, result.Report.CoverageAccepted)
			require.Nil(t, result.Report.DatasetAsOf)
			require.Equal(t, client.now(), result.Report.CompletedAt)
		})
	}
}

func TestInvalidReportCannotBecomeZeroRisk(t *testing.T) {
	tests := []struct{ name, body string }{
		{"missing score", strings.Replace(successBody("0"), `"riskscore":0,`, "", 1)},
		{"history scale", successBody("50")},
		{"null score", successBody("null")},
		{"string score", successBody(`"0.1"`)},
		{"missing report", strings.Replace(successBody("0"), `"report":`, `"unused":`, 1)},
		{"bad id", strings.Replace(successBody("0"), "poll-1", "wrong-id", 1)},
		{"bad hash", strings.Replace(successBody("0"), "check-hash", "wrong-hash", 1)},
		{"wrong address", strings.Replace(successBody("0"), "test-receiver", "wrong-address", 1)},
		{"wrong transaction", strings.Replace(successBody("0"), "test-tx", "wrong-tx", 1)},
		{"wrong currency", strings.Replace(successBody("0"), `"TRX"`, `"ETH"`, 1)},
		{"unknown status", strings.Replace(successBody("0"), "SUCCESS", "NEW_VENDOR_STATUS", 1)},
		{"RPC error HTTP 200", `{"jsonrpc":"2.0","id":"poll-1","error":{"code":-40006,"message":"test-secret"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, request := testClient(t, func(*http.Request) (*http.Response, error) { return response(tt.body), nil })
			result, err := client.GetResult(t.Context(), ticketFor(client, request), "poll-1")
			require.Error(t, err)
			require.Nil(t, result.Report)
			require.NotContains(t, err.Error(), "test-secret")
		})
	}
}

func TestTicketCannotMoveAccountAssetMappingOrMerchant(t *testing.T) {
	calls := 0
	client, request := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return response(successBody("0")), nil })
	tests := []struct {
		name string
		give func(*aml.Ticket)
	}{
		{"account", func(ticket *aml.Ticket) { ticket.ProviderAccountID = id.New() }},
		{"mapping", func(ticket *aml.Ticket) { ticket.MappingVersion = "other-version" }},
		{"merchant", func(ticket *aml.Ticket) { ticket.Request.MerchantID = id.New() }},
		{"contract", func(ticket *aml.Ticket) { ticket.Request.ContractAddress = "wrong-contract" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket := ticketFor(client, request)
			tt.give(&ticket)
			_, err := client.GetResult(t.Context(), ticket, "poll-1")
			require.Error(t, err)
		})
	}
	require.Zero(t, calls)
}

func TestPendingAndAmbiguousEffectsDoNotGiveAllow(t *testing.T) {
	client, request := testClient(t, func(*http.Request) (*http.Response, error) {
		return response(`{"jsonrpc":"2.0","id":"poll-1","result":{"check":{"hash":"check-hash","status":"TEST_PENDING"}}}`), nil
	})
	result, err := client.GetResult(context.Background(), ticketFor(client, request), "poll-1")
	require.NoError(t, err)
	require.Equal(t, aml.CheckPending, result.State)
	require.Nil(t, result.Report)
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(successBody("0")), nil })
	request.MatchingEffects = 2
	result, err = client.GetResult(t.Context(), ticketFor(client, request), "poll-1")
	require.NoError(t, err)
	require.False(t, result.Report.CoverageAccepted)
}

func TestUnacceptedCapabilityAndOversizedRequestIDDoNotSubmit(t *testing.T) {
	calls := 0
	client, request := testClient(t, func(*http.Request) (*http.Response, error) { calls++; return nil, nil })
	client.config.Mappings[0].CapabilityAccepted = false
	_, err := client.Submit(t.Context(), request, "submit-1")
	require.Error(t, err)
	client.config.Mappings[0].CapabilityAccepted = true
	_, err = client.Submit(t.Context(), request, strings.Repeat("a", 33))
	require.Error(t, err)
	require.Zero(t, calls)
}
