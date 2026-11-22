package crypto_repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

// ReadAMLConfiguration finishes its read-only snapshot before any Vault calls.
// It deliberately does not claim jobs, enroll owners or refresh projections.
func (r *AMLRepository) ReadAMLConfiguration(ctx context.Context, networkCode string, namespace id.ID) (crypto.AMLConfigurationReport, error) {
	result := crypto.AMLConfigurationReport{ExpectedSchema: cryptoschema.ExpectedVersion, Blockers: []string{}, Networks: []crypto.AMLNetworkConfiguration{}}
	tm := postgres.MustGetTxManager(ctx)
	opts := postgres.DefaultTxOptions()
	opts.AccessMode, opts.IsolationLevel = pgx.ReadOnly, pgx.RepeatableRead
	err := tm.RunInTransactionWithOptions(ctx, opts, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var platformSchema int
		if err := q.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&result.ObservedAt); err != nil {
			return err
		}
		if err := q.QueryRow(ctx, cryptoschema.VersionQuery).Scan(&platformSchema, &result.DatabaseSchema); err != nil {
			return err
		}
		if !cryptoschema.Compatible(platformSchema, result.DatabaseSchema) {
			result.Blockers = append(result.Blockers, "schema_incompatible")
			return nil
		}
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database() AND vault_namespace=$1)`, namespace).Scan(&result.NamespaceEnrolled); err != nil {
			return err
		}
		if !result.NamespaceEnrolled {
			result.Blockers = append(result.Blockers, "vault_namespace_not_enrolled")
		}
		rows, err := q.Query(ctx, `WITH scopes AS (SELECT * FROM sys_crypto_aml_routing_scopes WHERE enabled)
 SELECT n.id,n.code,n.chain_family,n.network_identifier,COALESCE((SELECT t.id FROM cat_tokens t WHERE t.network_id=n.id AND t.token_standard='native'),'00000000-0000-0000-0000-000000000000'::uuid),n.native_token_symbol,n.native_decimals,n.confirmations_needed,n.finality_mode,n.is_active AND NOT n.deletion_mark,
 EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=n.id AND enforced),
 COALESCE((SELECT enabled FROM sys_crypto_network_policies WHERE network_id=n.id),FALSE),
 EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE network_id=n.id AND status='open'),
 (SELECT COUNT(*) FROM scopes WHERE network_id=n.id),
 (SELECT COUNT(*) FROM scopes s JOIN cat_integration_accounts a ON a.id=s.provider_account_id WHERE s.network_id=n.id AND a.provider='testnet_aml'),
 (SELECT COUNT(DISTINCT merchant_id) FROM scopes WHERE network_id=n.id),
 (SELECT COUNT(DISTINCT s.merchant_id) FROM scopes s WHERE s.network_id=n.id AND NOT EXISTS(SELECT 1 FROM scopes native JOIN cat_tokens t ON t.id=native.token_id AND t.token_standard='native' WHERE native.merchant_id=s.merchant_id AND native.network_id=s.network_id)),
 (SELECT COUNT(*) FROM cat_wallets w WHERE w.network_id=n.id AND w.custody_merchant_id IS NULL AND w.wallet_role IN ('merchant_pool','liquidity_hot') AND EXISTS(SELECT 1 FROM reg_crypto_balance_balances b WHERE b.wallet_id=w.id AND b.amount<>0 AND EXISTS(SELECT 1 FROM scopes s WHERE s.network_id=n.id AND s.token_id=b.token_id))),
 (SELECT COUNT(*) FROM doc_withdrawal_requests request JOIN cat_tokens t ON t.id=request.token_id WHERE t.network_id=n.id AND request.status IN ('pending_signature','pending_approval','approved','signing','broadcast','held') AND EXISTS(SELECT 1 FROM scopes s WHERE s.merchant_id=request.merchant_id AND s.token_id=request.token_id AND s.network_id=n.id) AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans p WHERE p.request_id=request.id)),
 (SELECT COUNT(*) FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.enabled AND p.automatic_baseline JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark WHERE p.network_id=n.id AND EXISTS(SELECT 1 FROM scopes scope LEFT JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=scope.token_id
 WHERE scope.merchant_id=p.merchant_id AND scope.network_id=p.network_id AND (p.wallet_role<>'network_fee' OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=scope.token_id AND native.token_standard='native'))
 AND (p.token_id IS NULL OR scope.token_id=p.token_id OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=scope.token_id AND native.token_standard='native')) AND baseline.wallet_id IS NULL)),
 (SELECT COUNT(*) FROM reg_crypto_aml_preflight_jobs j JOIN scopes s ON s.token_id=j.token_id JOIN cat_wallets w ON w.id=j.wallet_id AND w.custody_merchant_id=s.merchant_id AND w.network_id=s.network_id WHERE s.network_id=n.id AND j.status IN ('retry_wait','processing')),
 (SELECT COUNT(*) FROM reg_crypto_aml_preflight_jobs j JOIN scopes s ON s.token_id=j.token_id JOIN cat_wallets w ON w.id=j.wallet_id AND w.custody_merchant_id=s.merchant_id AND w.network_id=s.network_id WHERE s.network_id=n.id AND j.status='needs_attention')
 FROM cat_blockchain_networks n WHERE ($1<>'' AND n.code=$1) OR ($1='' AND EXISTS(SELECT 1 FROM scopes s WHERE s.network_id=n.id)) ORDER BY n.code,n.id`, networkCode)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var network crypto.AMLNetworkConfiguration
			if err := rows.Scan(&network.Network.ID, &network.Network.Code, &network.Network.Family, &network.Network.NetworkIdentifier, &network.Network.NativeTokenID, &network.Network.NativeTokenSymbol, &network.Network.NativeDecimals, &network.Network.RequiredConfirmations, &network.Network.FinalityMode, &network.Active, &network.CustodyEnforced, &network.DispatchEnabled, &network.NetworkHeld, &network.EnabledScopes, &network.EmulatedScopes, &network.Merchants, &network.MissingNativeScopes, &network.UnownedPrincipalWallets, &network.UnplannedPendingWithdrawals, &network.UnbaselinedAutomaticWallets, &network.BaselinePending, &network.BaselineNeedsAttention); err != nil {
				return err
			}
			network.Blockers = network.ConfigurationBlockers()
			result.Networks = append(result.Networks, network)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result.Networks) == 0 {
			result.Blockers = append(result.Blockers, "no_aml_networks_found")
		}
		return nil
	})
	return result, err
}
