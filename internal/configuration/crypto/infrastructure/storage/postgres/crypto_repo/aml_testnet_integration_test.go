//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	amlruntime "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/aml/runtime"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"

	"github.com/stretchr/testify/require"
)

func prepareTestnetEmulator(t *testing.T, family, identifier string) (amlRoutingFixture, aml.ConfigureTestnetRequest) {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	f := amlRoutingFixture{ctx: ctx, pool: pool, repo: NewAMLRepository()}
	request := aml.ConfigureTestnetRequest{AccountID: id.New(), MerchantID: id.New(), Config: aml.EmulatorConfig{Seed: "integration-seed", MaxDelaySeconds: 0, UnknownRateBP: 1000}, Actor: "integration-test"}
	_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Testnet merchant')`, request.MerchantID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE chain_family=$1 AND network_identifier=$2`, family, identifier).Scan(&request.NetworkID)
	if errors.Is(err, pgx.ErrNoRows) {
		request.NetworkID = id.New()
		_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active) VALUES($1,$1::uuid::text,'Synthetic testnet',$2,$3,'TRX',6,TRUE)`, request.NetworkID, family, identifier)
		require.NoError(t, err)
		token := id.New()
		_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,is_active) VALUES($1,$1::uuid::text,'Native test asset',$2,'TRX',6,'native',TRUE)`, token, request.NetworkID)
	}
	require.NoError(t, err)
	if family == "tron" {
		token := id.New()
		_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard,is_active) VALUES($1,$1::uuid::text,'Test contract',$2,'test-contract','TEST',6,'TRC-20',TRUE)`, token, request.NetworkID)
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `UPDATE cat_blockchain_networks SET is_active=TRUE WHERE id=$1`, request.NetworkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1`, request.NetworkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, request.NetworkID)
	require.NoError(t, err)
	return f, request
}

func TestAMLTestnetEmulationRunsWithoutCredentialsCreditsOrPaidCalls(t *testing.T) {
	for _, network := range []struct{ family, identifier string }{{"tron", "nile"}, {"evm", "11155111"}} {
		t.Run(network.family, func(t *testing.T) {
			f, request := prepareTestnetEmulator(t, network.family, network.identifier)
			cfg, err := f.repo.ConfigureAMLTestnet(f.ctx, request)
			require.NoError(t, err)
			require.Equal(t, request.Config, cfg)
			_, err = f.repo.ConfigureAMLTestnet(f.ctx, request)
			require.NoError(t, err, "configuration replay is idempotent")
			var token id.ID
			var contract string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id,COALESCE(contract_address,'') FROM cat_tokens WHERE network_id=$1 AND token_standard='native' LIMIT 1`, request.NetworkID).Scan(&token, &contract))
			wallet := id.New()
			address := "T-synthetic-" + wallet.String()
			if network.family == "evm" {
				address = "0x" + id.Compact(wallet) + "00000000"
			}
			_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id) VALUES($1,$1::uuid::text,'Synthetic subject',$2,$3,'synthetic',$4)`, wallet, request.NetworkID, address, request.MerchantID)
			require.NoError(t, err)
			check := aml.CheckRequest{MerchantID: request.MerchantID, NetworkID: request.NetworkID, TokenID: token, Kind: aml.AddressSubject, Address: address, ContractAddress: contract}
			jobID, err := f.repo.EnqueueAMLCheck(f.ctx, EnqueueAMLInput{WalletID: wallet, ProviderAccountID: request.AccountID, MappingVersion: aml.TestnetContract, Request: check, DeadlineAt: time.Now().Add(time.Hour)})
			require.NoError(t, err)
			// Mark work due explicitly: host and PostgreSQL clocks can differ by a
			// few milliseconds, so NOW() is not a synchronization point for the Go worker.
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET next_attempt_at=$2 WHERE id=$1`, jobID, time.Now().Add(-time.Second))
			require.NoError(t, err)
			service := cryptoapp.NewAMLJobService(f.repo, amlruntime.NewResolver(f.repo, nil))
			claimed, err := service.RunOnce(f.ctx)
			require.NoError(t, err)
			require.True(t, claimed)
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET next_attempt_at=$2 WHERE id=$1`, jobID, time.Now().Add(-time.Second))
			require.NoError(t, err)
			claimed, err = service.RunOnce(f.ctx)
			require.NoError(t, err)
			require.True(t, claimed)
			applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
			require.NoError(t, err)
			require.True(t, applied)
			var provider, status, adapter string
			var emulated bool
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT j.provider,j.status,r.adapter_version,(convert_from(r.raw_response,'UTF8')::jsonb)->>'emulated'='true' FROM reg_crypto_aml_jobs j JOIN reg_crypto_aml_provider_checks c ON c.job_id=j.id JOIN reg_crypto_aml_reports r ON r.provider_check_id=c.id WHERE j.id=$1`, jobID).Scan(&provider, &status, &adapter, &emulated))
			require.Equal(t, aml.TestnetProvider, provider)
			require.Equal(t, "succeeded", status)
			require.Equal(t, aml.TestnetAdapter, adapter)
			require.True(t, emulated)
			// A real verified incoming-effect path must select the emulator
			// from the scope account, rather than hardcoding the live vendor.
			var transferToken id.ID
			var transferContract string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id,contract_address FROM cat_tokens WHERE network_id=$1 AND token_standard IN ('TRC-20','ERC-20','trc20','erc20') AND is_active AND NOT deletion_mark LIMIT 1`, request.NetworkID).Scan(&transferToken, &transferContract))
			f.input = EnqueueAMLInput{WalletID: wallet, ProviderAccountID: request.AccountID, MappingVersion: aml.TestnetContract, Request: aml.CheckRequest{MerchantID: request.MerchantID, NetworkID: request.NetworkID, TokenID: transferToken, Address: address, ContractAddress: transferContract}}
			proof := f.proof(address, "emulated-incoming", 1)
			proof.Event.TokenContract = transferContract
			proof.Event.DecoderVersion = crypto.TransferEvidenceSchemaV1
			_, effect := f.save(t, proof)
			claimed, err = service.RunOnce(f.ctx)
			require.NoError(t, err)
			require.True(t, claimed)
			_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_jobs SET next_attempt_at=$1 WHERE status='awaiting_result'`, time.Now().Add(-time.Second))
			require.NoError(t, err)
			claimed, err = service.RunOnce(f.ctx)
			require.NoError(t, err)
			require.True(t, claimed)
			applied, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
			require.NoError(t, err)
			require.True(t, applied)
			rescreen, err := f.repo.RequestAMLRescreen(f.ctx, effect, 1, "test later risk classification", "operator")
			require.NoError(t, err)
			var rescreenProvider string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT provider FROM reg_crypto_aml_jobs WHERE id=$1`, rescreen).Scan(&rescreenProvider))
			require.Equal(t, aml.TestnetProvider, rescreenProvider)
			var paid, credits, money int
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT COUNT(*) FROM reg_crypto_aml_paid_admissions),(SELECT COUNT(*) FROM reg_crypto_aml_package_credits),(SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements)`).Scan(&paid, &credits, &money))
			require.Zero(t, paid)
			require.Zero(t, credits)
			require.Zero(t, money)
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_aml_package_credits(account_id,units,external_reference,authorized_by) VALUES($1,1,'forbidden','test')`, request.AccountID)
			require.Error(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE cat_blockchain_networks SET network_identifier='1' WHERE id=$1`, request.NetworkID)
			require.Error(t, err)
			_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_testnet_emulators SET seed='changed' WHERE account_id=$1`, request.AccountID)
			require.Error(t, err)
		})
	}
}

func TestAMLTestnetConfigurationRefusesMainnet(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	repo := NewAMLRepository()
	network := id.New()
	merchant := id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Merchant')`, merchant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Nile impostor','evm','1','ETH',18)`, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, network)
	require.NoError(t, err)
	_, err = repo.ConfigureAMLTestnet(ctx, aml.ConfigureTestnetRequest{AccountID: id.New(), MerchantID: merchant, NetworkID: network, Config: aml.EmulatorConfig{Seed: "test"}, Actor: "test"})
	require.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM cat_integration_accounts WHERE provider='testnet_aml'`).Scan(&count))
	require.Zero(t, count)
	account := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_integration_accounts(id,code,name,provider) VALUES($1,$1::uuid::text,'Mainnet impostor','testnet_aml')`, account)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_testnet_emulators(account_id,network_id,seed,configured_by) VALUES($1,$2,'test','operator')`, account, network)
	require.Error(t, err, "SQL must enforce the same testnet restriction")
}
