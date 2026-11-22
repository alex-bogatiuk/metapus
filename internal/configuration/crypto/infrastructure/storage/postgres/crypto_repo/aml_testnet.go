package crypto_repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/postgres/catalog_repo"
)

// ConfigureAMLTestnet is explicit control-plane configuration. It neither
// enrolls custody nor enables a network, and refuses to replace live scopes.
func (r *AMLRepository) ConfigureAMLTestnet(ctx context.Context, request aml.ConfigureTestnetRequest) (aml.EmulatorConfig, error) {
	result := request.Config
	if id.IsNil(request.AccountID) || id.IsNil(request.MerchantID) || id.IsNil(request.NetworkID) || strings.TrimSpace(request.Actor) == "" {
		return result, apperror.NewValidation("testnet AML requires account, merchant, network and actor")
	}
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `INSERT INTO sys_crypto_network_policies(network_id) VALUES($1) ON CONFLICT DO NOTHING`, request.NetworkID); err != nil {
			return err
		}
		var family, identifier string
		var paused, custody bool
		if err := q.QueryRow(ctx, `SELECT n.chain_family,n.network_identifier,NOT p.enabled,EXISTS(SELECT 1 FROM sys_crypto_custody_policies c WHERE c.network_id=n.id AND c.enforced) FROM cat_blockchain_networks n JOIN sys_crypto_network_policies p ON p.network_id=n.id WHERE n.id=$1 AND n.is_active AND NOT n.deletion_mark FOR UPDATE OF p`, request.NetworkID).Scan(&family, &identifier, &paused, &custody); err != nil {
			return err
		}
		if !aml.EmulationNetwork(family, identifier) || !paused || !custody {
			return apperror.NewValidation("testnet AML requires a paused Nile/Sepolia network with enforced merchant custody")
		}
		var previous aml.EmulatorConfig
		var network id.ID
		err := q.QueryRow(ctx, `SELECT network_id,seed,max_delay_seconds,unknown_rate_bp FROM sys_crypto_aml_testnet_emulators WHERE account_id=$1`, request.AccountID).Scan(&network, &previous.Seed, &previous.MaxDelaySeconds, &previous.UnknownRateBP)
		if err == nil {
			if result.Seed == "" {
				result.Seed = previous.Seed
			}
			if network != request.NetworkID || result != previous {
				return apperror.NewConflict("testnet emulator binding is immutable; use a new account for another seed or distribution")
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			if result.Seed == "" {
				var raw [32]byte
				if _, err := rand.Read(raw[:]); err != nil {
					return err
				}
				result.Seed = hex.EncodeToString(raw[:])
			}
			if err := result.Validate(family, identifier); err != nil {
				return err
			}
			code := "AMLTESTNET-" + strings.ReplaceAll(request.AccountID.String(), "-", "")
			if err := catalog_repo.CreateKeylessAccount(ctx, request.AccountID, code, "AML эмулятор — "+identifier, aml.TestnetProvider); err != nil {
				return err
			}
			_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_testnet_emulators(account_id,network_id,seed,max_delay_seconds,unknown_rate_bp,configured_by) VALUES($1,$2,$3,$4,$5,$6)`, request.AccountID, request.NetworkID, result.Seed, result.MaxDelaySeconds, result.UnknownRateBP, request.Actor)
			if err != nil {
				return err
			}
		} else {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_account_policies(account_id,allow_submissions) VALUES($1,TRUE) ON CONFLICT DO NOTHING`, request.AccountID)
		if err != nil {
			return err
		}
		// An immutable emulator policy accepts the two synthetic signal names.
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_risk_policies(version,low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals) VALUES
('aml-testnet.v1',3000,7000,86400,86400,ARRAY['wallet','sanctions'],ARRAY['sanctions']),
('aml-testnet.v2',3000,7000,86400,86400,ARRAY['wallet','sanctions'],ARRAY['sanctions'])
ON CONFLICT DO NOTHING`)
		if err != nil {
			return err
		}
		var policyMatches bool
		if err = q.QueryRow(ctx, `SELECT low_max_bp=3000 AND medium_max_bp=7000 AND max_report_age_seconds=86400 AND decision_ttl_seconds=86400 AND accepted_signals=ARRAY['wallet','sanctions']::text[] AND critical_signals=ARRAY['sanctions']::text[] FROM sys_crypto_aml_risk_policies WHERE version=$1`, aml.TestnetPolicyVersion).Scan(&policyMatches); err != nil {
			return err
		}
		if !policyMatches {
			return apperror.NewConflict("testnet policy version has different immutable rules")
		}
		var native bool
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_tokens WHERE network_id=$1 AND token_standard='native' AND is_active AND NOT deletion_mark)`, request.NetworkID).Scan(&native); err != nil {
			return err
		}
		if !native {
			return apperror.NewValidation("testnet AML requires the network native asset")
		}
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses)
 SELECT t.network_id,t.id,'aml-testnet.v1','aml-testnet.v1',COALESCE(t.contract_address,''),CASE WHEN n.chain_family='tron' THEN 'TRX' ELSE 'ETH' END,CASE WHEN t.token_standard='native' THEN '' ELSE t.id::text END,t.token_standard='native',t.decimal_places,TRUE,'local emulator contract only',jsonb_build_object('emulated',TRUE,'networkId',n.id),ARRAY['TESTNET_PENDING']
 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id WHERE t.network_id=$1 AND t.is_active AND NOT t.deletion_mark AND t.token_standard IN ('native','trc20','erc20','TRC-20','ERC-20') ON CONFLICT DO NOTHING`, request.NetworkID)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 SELECT $1,t.network_id,t.id,TRUE,$4,$3,'aml-testnet.v1' FROM cat_tokens t WHERE t.network_id=$2 AND t.is_active AND NOT t.deletion_mark AND t.token_standard IN ('native','trc20','erc20','TRC-20','ERC-20')
 ON CONFLICT (merchant_id,network_id,token_id) DO UPDATE SET policy_version=EXCLUDED.policy_version,version=sys_crypto_aml_routing_scopes.version+1,updated_at=NOW()
 WHERE sys_crypto_aml_routing_scopes.policy_version<>EXCLUDED.policy_version AND sys_crypto_aml_routing_scopes.provider_account_id=$3 AND sys_crypto_aml_routing_scopes.mapping_version='aml-testnet.v1'`, request.MerchantID, request.NetworkID, request.AccountID, aml.TestnetPolicyVersion)
		if err != nil {
			return err
		}
		var conflict bool
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id WHERE s.merchant_id=$1 AND s.network_id=$2 AND t.is_active AND NOT t.deletion_mark AND (NOT s.enabled OR s.provider_account_id<>$3 OR s.mapping_version<>'aml-testnet.v1' OR s.policy_version<>$4))`, request.MerchantID, request.NetworkID, request.AccountID, aml.TestnetPolicyVersion).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return apperror.NewConflict("existing AML scopes differ; no live scope is replaced automatically")
		}
		return nil
	})
	return result, err
}
