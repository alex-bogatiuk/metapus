package content

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_merchant_credit"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

func TestCryptoDocumentRLSDimensionsSingleSourceOfTruth(t *testing.T) {
	factories := configuration.Compose(Crypto(Services{}))
	readReg := v1.BuildReadRegistry(factories)

	byKey := map[string]map[string]string{}
	for _, def := range readReg.List() {
		byKey[def.Key] = def.RLSDimensions
		_, hasOrg := def.RLSDimensions["organization"]
		require.False(t, hasOrg, "crypto entity %s must not declare non-existent organization_id RLS dimension", def.Key)
	}

	for _, key := range []string{
		"crypto_invoice",
		"crypto_payment",
		"crypto_withdrawal",
		"crypto_merchant_credit",
		"withdrawal_request",
	} {
		dims, ok := byKey[key]
		require.True(t, ok, "expected metadata registration for %s", key)
		require.Equal(t, map[string]string{"merchant": "merchant_id"}, dims, "merchant RLS mismatch on %s", key)
	}

	merchantID := id.New()
	credit := &crypto_merchant_credit.CryptoMerchantCredit{MerchantID: merchantID}
	require.Equal(t, map[string]string{"merchant": merchantID.String()}, credit.GetRLSDimensions())
}
