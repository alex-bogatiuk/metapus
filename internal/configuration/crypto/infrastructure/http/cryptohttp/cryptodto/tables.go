package cryptodto

// Crypto catalog tables for postgres.ReferenceResolver / ResolvedRefs. The
// platform tables live in dto (TableOrganizations, TableCurrencies, ...).
const (
	TableMerchants          = "cat_merchants"
	TableBlockchainNetworks = "cat_blockchain_networks"
	TableTokens             = "cat_tokens"
	TableWallets            = "cat_wallets"
)
