package contracts

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/jsoncodec"
)

func TestRouteContractsResolveConcreteDirectionsAndStayCurrent(t *testing.T) {
	generated, err := GenerateRoutes(Registry(configurationBundles()...), Routes(configurationBundles()...))
	require.NoError(t, err)
	committed, err := os.ReadFile("../../../../frontend/lib/generated/http-contract-routes.ts")
	require.NoError(t, err)
	require.Equal(t, string(generated), string(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n"))))
	for _, routes := range [][]RouteContract{
		{{Scope: "api", Method: "POST", Path: "/test", Request: "PortalSummaryResponse"}},
		{{Scope: "api", Method: "GET", Path: "/test", Response: "Missing"}},
		{Routes(configurationBundles()...)[0], Routes(configurationBundles()...)[0]},
		{{Scope: "api", Method: "POST", Path: "/test", ResponseVariants: []string{"LoginResponse"}}},
		{{Scope: "api", Method: "POST", Path: "/test", ResponseVariants: []string{"LoginResponse", "Missing"}}},
		{{Scope: "api", Method: "POST", Path: "/test", Response: "LoginResponse", ResponseVariants: []string{"LoginResponse", "MFAChallengeResponse"}}},
	} {
		_, err := GenerateRoutes(Registry(configurationBundles()...), routes)
		require.Error(t, err)
	}
}

func TestWebhookTestResultPreservesOmittedNullAndZero(t *testing.T) {
	var status *int
	zero := 0
	for _, tc := range []struct {
		value    cryptodto.WebhookTestResponse
		expected string
	}{
		{cryptodto.WebhookTestResponse{EventID: "event"}, `{"eventId":"event","success":false}`},
		{cryptodto.WebhookTestResponse{EventID: "event", StatusCode: &status}, `{"eventId":"event","success":false,"statusCode":null}`},
		{cryptodto.WebhookTestResponse{EventID: "event", DurationMs: ptr(&zero)}, `{"eventId":"event","success":false,"durationMs":0}`},
	} {
		encoded, err := jsoncodec.Marshal(tc.value)
		require.NoError(t, err)
		require.JSONEq(t, tc.expected, string(encoded))
	}
}

func ptr[T any](value T) *T { return &value }

func TestCustodyAdjustmentResponseUsesSameExactWireShapeForDetailAndActions(t *testing.T) {
	amount, err := types.NewCryptoAmountFromString("9007199254740993")
	require.NoError(t, err)
	response := cryptodto.FromCustodyAdjustment(&crypto.CryptoCustodyAdjustment{Amount: amount, Status: "posted", PostedVersion: 2})
	encoded, err := jsoncodec.Marshal(response)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &wire))
	require.Equal(t, `"9007199254740993"`, string(wire["amount"]))
	require.Equal(t, `"posted"`, string(wire["status"]))
	require.Equal(t, `2`, string(wire["postedVersion"]))
	require.NotContains(t, wire, "Amount")
	require.NotContains(t, wire, "decisionBy")
	require.Nil(t, cryptodto.FromCustodyAdjustment(nil))
}
