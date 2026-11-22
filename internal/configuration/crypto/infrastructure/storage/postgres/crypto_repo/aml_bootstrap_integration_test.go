//go:build integration

package crypto_repo

import (
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	amlruntime "metapus/internal/configuration/crypto/infrastructure/aml/runtime"
	"metapus/pkg/sdk/id"

	"github.com/stretchr/testify/require"
)

func TestAMLTestnetBootstrapPreservesMoneyAndAcceptsLaterRandomChecks(t *testing.T) {
	f, config := prepareTestnetEmulator(t, "tron", "nile")
	config.Config.UnknownRateBP = 0
	_, err := f.repo.ConfigureAMLTestnet(f.ctx, config)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),'test')`, id.New())
	require.NoError(t, err)
	wallet := id.New()
	address := testTRONAddress()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id) VALUES($1,$1::uuid::text,'Opening wallet',$2,$3,'synthetic',$4)`, wallet, config.NetworkID, address, config.MerchantID)
	require.NoError(t, err)
	var token, native id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND token_standard='TRC-20' LIMIT 1`, config.NetworkID).Scan(&token))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND token_standard='native' LIMIT 1`, config.NetworkID).Scan(&native))
	for asset, amount := range map[id.ID]int{token: 100, native: 50} {
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'LegacyTestReceipt',1,NOW(),'receipt',$2,$3,$4)`, id.New(), wallet, asset, amount)
		require.NoError(t, err)
		run, check := id.New(), id.New()
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_runs(id,network_id,status,completed_at) VALUES($1,$2,'completed',NOW())`, run, config.NetworkID)
		require.NoError(t, err)
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_checks(id,run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount) VALUES($1,$2,$3,$4,$5,$6,$6,0)`, check, run, config.NetworkID, wallet, asset, amount)
		require.NoError(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) VALUES($1,'LegacyTestReceipt',1,NOW(),'receipt',$2,$3,90)`, id.New(), config.MerchantID, token)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount) VALUES(gen_random_uuid_v7(),$1,'LegacyTestReceipt',1,NOW(),'receipt',$2,$3,'processing',10)`, id.New(), config.MerchantID, token)
	require.NoError(t, err)
	plan, err := f.repo.PlanAMLTestnetBootstrap(f.ctx, aml.TestnetBootstrap{Schema: "aml-testnet-bootstrap.v1", ID: id.New(), MerchantID: config.MerchantID, NetworkID: config.NetworkID, BlockNumber: 1, BlockHash: "opening-cutoff", ExpiresAt: time.Now().Add(90 * 24 * time.Hour), Actor: "test"})
	require.NoError(t, err)
	require.Len(t, plan.Openings, 2)
	bad := plan
	bad.ID = id.New()
	bad.Openings = append([]aml.TestnetOpening(nil), plan.Openings...)
	for i := range bad.Openings {
		if bad.Openings[i].TokenID == token {
			bad.Openings[i].Principal = bad.Openings[i].Principal.Add(minAMLBootstrapAmount(bad.Openings[i].ServiceFee, bad.Openings[i].ServiceFee))
			bad.Openings[i].ServiceFee = bad.Openings[i].ServiceFee.Sub(bad.Openings[i].ServiceFee)
		}
	}
	require.Error(t, f.repo.ApplyAMLTestnetBootstrap(f.ctx, bad), "cannot replace fee claim with extra merchant principal")
	require.NoError(t, f.repo.ApplyAMLTestnetBootstrap(f.ctx, plan))
	require.NoError(t, f.repo.ApplyAMLTestnetBootstrap(f.ctx, plan))
	changed := plan
	changed.Actor = "changed"
	require.Error(t, f.repo.ApplyAMLTestnetBootstrap(f.ctx, changed))
	for range 4 {
		_, err = f.repo.RebuildNextAMLRiskProjection(f.ctx, time.Now())
		require.NoError(t, err)
	}
	snapshot, err := cryptoapp.NewAMLBalanceService(f.repo).Get(f.ctx, config.MerchantID)
	require.NoError(t, err)
	for _, item := range snapshot.Items {
		if item.TokenID == token {
			require.Equal(t, "90", item.Buckets[0].Free.String())
			require.Equal(t, "90", item.Total.String())
			require.True(t, item.TechnicalHold.IsZero())
		}
	}
	var operating string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_aml_resource_positions WHERE wallet_id=$1 AND token_id=$2`, wallet, native).Scan(&operating))
	require.Equal(t, "50", operating)
	var physical, financial, fees int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT COUNT(*) FROM reg_crypto_balance_movements),(SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements),(SELECT COUNT(*) FROM reg_crypto_fee_movements)`).Scan(&physical, &financial, &fees))
	require.Equal(t, 2, physical)
	require.Equal(t, 1, financial)
	require.Equal(t, 1, fees)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_testnet_openings SET principal=principal+1`)
	require.Error(t, err)
	// A subsequent ordinary receipt uses the random provider and durable payment
	// allocations; it does not inherit the synthetic opening assessment.
	f.input = EnqueueAMLInput{WalletID: wallet, ProviderAccountID: config.AccountID, MappingVersion: aml.TestnetContract, Request: aml.CheckRequest{MerchantID: config.MerchantID, NetworkID: config.NetworkID, TokenID: token, Address: address, ContractAddress: "test-contract"}}
	proof := f.proof(address, "after-opening", 1)
	proofID, _ := f.save(t, proof)
	f.postPayment(t, proof, proofID, wallet)
	f.creditNet(t, proof.Event.TxHash)
	service := cryptoapp.NewAMLJobService(f.repo, amlruntime.NewResolver(f.repo, nil))
	progressed, err := service.RunOnce(f.ctx)
	require.NoError(t, err)
	require.True(t, progressed)
	// The claim uses the application clock; make the fixture unambiguously
	// due even when PostgreSQL's clock is slightly ahead of the host.
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET next_attempt_at='epoch' WHERE status='awaiting_result'`)
	require.NoError(t, err)
	progressed, err = service.RunOnce(f.ctx)
	require.NoError(t, err)
	require.True(t, progressed)
	_, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	for range 5 {
		_, err = f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
		require.NoError(t, err)
		_, err = f.repo.RebuildNextAMLRiskProjection(f.ctx, time.Now())
		require.NoError(t, err)
	}
	var covered bool
	var principal string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_total_inventory_covered(wallet_id,token_id),principal::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1 AND token_id=$2`, wallet, token).Scan(&covered, &principal))
	require.True(t, covered)
	require.Equal(t, "180", principal)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_balances SET amount=amount+1 WHERE wallet_id=$1 AND token_id=$2`, wallet, token)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_total_inventory_covered($1,$2)`, wallet, token).Scan(&covered))
	require.False(t, covered)
}

func TestAMLTestnetBootstrapRejectsMainnetAndUnbalancedClaims(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	repo := NewAMLRepository()
	network, merchant := id.New(), id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Merchant')`, merchant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Mainnet','evm','1','ETH',18)`, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_network_policies(network_id,enabled) VALUES($1,FALSE) ON CONFLICT DO NOTHING`, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),'test')`, id.New())
	require.NoError(t, err)
	plan := aml.TestnetBootstrap{Schema: "aml-testnet-bootstrap.v1", ID: id.New(), MerchantID: merchant, NetworkID: network, BlockNumber: 1, BlockHash: "test", Actor: "test", ExpiresAt: time.Now().Add(time.Hour), Openings: []aml.TestnetOpening{{WalletID: id.New(), TokenID: id.New(), CheckID: id.New(), LedgerHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	require.Error(t, repo.ApplyAMLTestnetBootstrap(ctx, plan))
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_aml_testnet_bootstraps`).Scan(&count))
	require.Zero(t, count)
}

func TestAMLTestnetOpeningOperatingFundsSupportSignedJITAndLaterCosts(t *testing.T) {
	f, config := prepareTestnetEmulator(t, "tron", "nile")
	_, err := f.repo.ConfigureAMLTestnet(f.ctx, config)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),'test')`, id.New())
	require.NoError(t, err)
	source, target := id.New(), id.New()
	var native id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM cat_tokens WHERE network_id=$1 AND token_standard='native'`, config.NetworkID).Scan(&native))
	for wallet, role := range map[id.ID]string{source: "network_fee", target: "merchant_pool"} {
		_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id,wallet_role) VALUES($1,$1::uuid::text,'Opening resource wallet',$2,$3,'resource-key',$4,$5)`, wallet, config.NetworkID, testTRONAddress(), config.MerchantID, role)
		require.NoError(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'LegacyOperating',1,NOW(),'receipt',$2,$3,100)`, id.New(), source, native)
	require.NoError(t, err)
	// The bootstrap requires a fresh chain reconciliation for every owned asset.
	rows, err := f.pool.Query(f.ctx, `SELECT w.id,t.id,COALESCE(b.amount,0) FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=t.id WHERE w.custody_merchant_id=$1 AND w.network_id=$2`, config.MerchantID, config.NetworkID)
	require.NoError(t, err)
	type observed struct {
		w, t   id.ID
		amount string
	}
	var checks []observed
	for rows.Next() {
		var o observed
		require.NoError(t, rows.Scan(&o.w, &o.t, &o.amount))
		checks = append(checks, o)
	}
	rows.Close()
	for _, o := range checks {
		run, check := id.New(), id.New()
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_runs(id,network_id,status,completed_at) VALUES($1,$2,'completed',NOW())`, run, config.NetworkID)
		require.NoError(t, err)
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_checks(id,run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount) VALUES($1,$2,$3,$4,$5,$6,$6,0)`, check, run, config.NetworkID, o.w, o.t, o.amount)
		require.NoError(t, err)
	}
	plan, err := f.repo.PlanAMLTestnetBootstrap(f.ctx, aml.TestnetBootstrap{Schema: "aml-testnet-bootstrap.v1", ID: id.New(), MerchantID: config.MerchantID, NetworkID: config.NetworkID, BlockNumber: 1, BlockHash: "opening-checkpoint", ExpiresAt: time.Now().Add(time.Hour), Actor: "test"})
	require.NoError(t, err)
	require.NoError(t, f.repo.ApplyAMLTestnetBootstrap(f.ctx, plan))
	f.input = EnqueueAMLInput{WalletID: source, Request: aml.CheckRequest{MerchantID: config.MerchantID, NetworkID: config.NetworkID, TokenID: native, Address: "opening-" + source.String()}}
	f.rebuild(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	hash := signResourceFunding(t, f, target, fee, funding)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_native_fundings SET status='signed' WHERE id=$1`, funding)
	require.NoError(t, err)
	var matches bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_spend_plan_matches('CryptoNativeFunding',$1,'native_funding',$2,$3,$4,20)`, funding, source, native, walletAddress(t, f, target)).Scan(&matches))
	require.True(t, matches)
	settleResourceEvidence(t, f, fee, funding, hash, "reverted", "3", false)
	f.rebuild(t)
	require.Equal(t, []string{"97", "0"}, resourceAmounts(t, f, target))
	var status string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&status))
	require.Equal(t, "failed", status)
}

func walletAddress(t *testing.T, f amlRoutingFixture, wallet id.ID) string {
	t.Helper()
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, wallet).Scan(&address))
	return address
}
