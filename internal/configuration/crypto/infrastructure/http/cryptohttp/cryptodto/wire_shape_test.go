package cryptodto

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/jsoncodec"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
	"github.com/stretchr/testify/require"
)

func TestWebhookTestResultPreservesOmittedNullAndZero(t *testing.T) {
	var status *int
	zero := 0
	for _, tc := range []struct {
		value    WebhookTestResponse
		expected string
	}{
		{WebhookTestResponse{EventID: "event"}, `{"eventId":"event","success":false}`},
		{WebhookTestResponse{EventID: "event", StatusCode: &status}, `{"eventId":"event","success":false,"statusCode":null}`},
		{WebhookTestResponse{EventID: "event", DurationMs: ptr(&zero)}, `{"eventId":"event","success":false,"durationMs":0}`},
	} {
		encoded, err := jsoncodec.Marshal(tc.value)
		require.NoError(t, err)
		require.JSONEq(t, tc.expected, string(encoded))
	}
}

func ptr[T any](value T) *T { return &value }

func TestCustodyAdjustmentResponseUsesSameExactWireShapeForDetailAndActions(t *testing.T) {
	amount, err := types.NewRawAmountFromString("9007199254740993")
	require.NoError(t, err)
	response := FromCustodyAdjustment(&crypto.CryptoCustodyAdjustment{Amount: amount, Status: "posted", PostedVersion: 2})
	encoded, err := jsoncodec.Marshal(response)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &wire))
	require.Equal(t, `"9007199254740993"`, string(wire["amount"]))
	require.Equal(t, `"posted"`, string(wire["status"]))
	require.Equal(t, `2`, string(wire["postedVersion"]))
	require.NotContains(t, wire, "Amount")
	require.NotContains(t, wire, "decisionBy")
	require.Nil(t, FromCustodyAdjustment(nil))
}
