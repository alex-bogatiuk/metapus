package cryptodto

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

func TestInvoiceReferenceRequestMetadataPresenceAndStrictShape(t *testing.T) {
	for _, body := range []string{
		`{"orderId":"ORD-1","metadata":null}`,
		`{"orderId":"ORD-1","metadata":[]}`,
		`{"orderId":"ORD-1","metadata":{"sap_doc":null}}`,
		`{"orderId":"ORD-1","metadata":{"sap_doc":1}}`,
		`{"orderId":"ORD-1","metadata":{"sap_doc":"a","sap_doc":"b"}}`,
		`{"orderId":"ORD-1","externalId":null}`,
		`{"externalId":"ERP-1","orderId":null}`,
		`{"externalId":"ERP-1","orderId":""}`,
		`{"externalId":123}`,
		`{"externalId":""}`,
		`{"orderId":"ORD-1","amount":"1"}`,
		`{"orderId":"ORD-1","status":"completed"}`,
	} {
		t.Run(body, func(t *testing.T) {
			var request BindMerchantInvoiceReferenceRequest
			require.Error(t, json.UnmarshalStrict([]byte(body), &request))
		})
	}
	var request BindMerchantInvoiceReferenceRequest
	require.NoError(t, json.UnmarshalStrict([]byte(`{"orderId":"ORD-1"}`), &request))
	require.Nil(t, request.Metadata)
	require.NoError(t, json.UnmarshalStrict([]byte(`{"orderId":"ORD-1","metadata":{}}`), &request))
	require.NotNil(t, request.Metadata)
	require.Empty(t, request.Metadata)
	require.NoError(t, json.UnmarshalStrict([]byte(`{"orderId":"ORD-1","metadata":{"sap_doc":"45001"}}`), &request))
	require.Equal(t, "45001", request.Metadata["sap_doc"])
	var externalOnly BindMerchantInvoiceReferenceRequest
	require.NoError(t, json.UnmarshalStrict([]byte(`{"externalId":"1С-проводка-001"}`), &externalOnly))
	require.Empty(t, externalOnly.OrderID)
	require.Equal(t, "1С-проводка-001", string(externalOnly.ExternalID))
	require.NoError(t, json.UnmarshalStrict([]byte(`{"orderId":"ORD-1","externalId":"ERP-1"}`), &externalOnly))
	require.Equal(t, "ORD-1", string(externalOnly.OrderID))
}
