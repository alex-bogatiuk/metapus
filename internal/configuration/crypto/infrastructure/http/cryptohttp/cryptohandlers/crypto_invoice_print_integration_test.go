//go:build integration

package cryptohandlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/readaccess"
	"metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"metapus/internal/metadata"
)

// The unit tests fix the projection field names and Go types the builder
// expects; this test proves the production authorized reader supplies them.
func TestCryptoInvoicePrintReadsReferencesThroughAuthorizedReader(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	reg := metadata.NewRegistry()
	for _, entry := range []struct {
		model any
		name  string
		key   string
		table string
	}{
		{merchant.Merchant{}, "Merchant", "merchant", "cat_merchants"},
		{token.Token{}, "Token", "token", "cat_tokens"},
		{blockchain_network.BlockchainNetwork{}, "BlockchainNetwork", "blockchain_network", "cat_blockchain_networks"},
		{wallet.Wallet{}, "Wallet", "wallet", "cat_wallets"},
	} {
		def := metadata.Inspect(entry.model, entry.name, metadata.TypeCatalog)
		def.Key, def.TableName, def.ReadPermission = entry.key, entry.table, entry.key+":read"
		reg.Register(def)
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()

	f := newPrintFixture()
	network := id.New()
	ctx := context.Background()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO cat_merchants(id,code,name) VALUES($1,'PRINT','Print merchant')`, []any{f.doc.MerchantID}},
		{`INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed) VALUES($1,'PRINT-NET','Print network','tron','print-net','TRX',6,19)`, []any{network}},
		{`INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,'PRINT-USDT','Print USDT',$2,'USDT',6,'trc20')`, []any{f.doc.TokenID, network}},
		{`INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,vault_key_id) VALUES($1,'PRINT-W','Print wallet',$2,'TPrintAddress','TPrintAddress','test-key-ref')`, []any{*f.doc.WalletID, network}},
	} {
		_, err := tenant.Pool.Exec(ctx, stmt.sql, stmt.args...)
		require.NoError(t, err)
	}

	principal := appctx.UserContext{UserID: id.New().String(), Permissions: []string{"merchant:read", "token:read", "blockchain_network:read", "wallet:read"}}
	readCtx := readaccess.WithService(tenant.Context(principal), postgres.NewAuthorizedReadService(reg, engine))

	data, err := buildCryptoInvoicePrintData(readCtx, f.doc, nil)
	require.NoError(t, err)
	for label, want := range map[string]string{"Мерчант": "Print merchant", "Сеть": "Print network", "Токен": "USDT", "Адрес для оплаты": "TPrintAddress"} {
		got, _ := headerValue(data, label)
		require.Equal(t, want, got, label)
	}
	require.Equal(t, "100,50 USDT", data.Table.Totals[0].Value)
}
