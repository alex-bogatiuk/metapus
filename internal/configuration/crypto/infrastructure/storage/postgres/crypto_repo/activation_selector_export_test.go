//go:build integration

package crypto_repo

import (
	"context"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type ActivationSelectorFixture struct {
	Ctx                                  context.Context
	Pool                                 *pgxpool.Pool
	Network, Native, Hot, A, B, Merchant id.ID
}

func NewActivationSelectorFixture(t *testing.T, owned ...bool) ActivationSelectorFixture {
	t.Helper()
	f := ActivationSelectorFixture{Network: id.New(), Native: id.New(), Hot: id.New(), A: id.New(), B: id.New()}
	if len(owned) > 0 && owned[0] {
		base := newAMLRoutingFixture(t, false)
		f.Ctx, f.Pool, f.Network, f.A, f.Merchant = base.ctx, base.pool, base.input.Request.NetworkID, base.input.WalletID, base.input.Request.MerchantID
	} else {
		f.Ctx, f.Pool = amlTestDatabase(t)
		_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Activation selector','tron',$1::uuid::text,'TRX',6)`, f.Network)
		require.NoError(t, err)
	}
	var merchant *id.ID
	if !id.IsNil(f.Merchant) {
		merchant = &f.Merchant
	}
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Activation native',$2,'TRX',6,'native');
 INSERT INTO cat_wallets(id,code,name,network_id,custody_merchant_id,address,vault_key_id,tier,wallet_role,chain_activation_status)
 VALUES($3,'ACT-H-'||$3::uuid::text,'Activation payer',$2,$5,$6,'selector-key','hot','network_fee','active'),
 ($4,'ACT-B-'||$4::uuid::text,'Activation target B',$2,$5,'target-'||$4::uuid::text,'selector-key','pool','merchant_pool','unknown');
 INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($3,$1,1000)`, pgx.QueryExecModeSimpleProtocol, f.Native, f.Network, f.Hot, f.B, merchant, testTRONAddress())
	require.NoError(t, err)
	if id.IsNil(f.Merchant) {
		_, err = f.Pool.Exec(f.Ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,chain_activation_status) VALUES($1,'ACT-A-'||$1::uuid::text,'Activation target A',$2,'target-'||$1::uuid::text,'selector-key','pool','merchant_pool','unknown')`, f.A, f.Network)
		require.NoError(t, err)
	}
	_, err = f.Pool.Exec(f.Ctx, `INSERT INTO sys_crypto_wallet_pool_policies(network_id,enabled,low_watermark,target_ready,max_total_wallets,max_provision_batch,max_concurrent_activations,activation_reserve_minor,min_hot_native_balance_minor,daily_activation_budget_minor,retry_after_seconds,derivation_namespace,needs_review)
 VALUES($1,TRUE,0,2,10,2,2,10,0,100,30,$1::uuid::text,FALSE) ON CONFLICT(network_id) DO UPDATE SET enabled=TRUE,low_watermark=0,target_ready=2,max_total_wallets=10,max_provision_batch=2,max_concurrent_activations=2,activation_reserve_minor=10,min_hot_native_balance_minor=0,daily_activation_budget_minor=100,retry_after_seconds=30,derivation_namespace=EXCLUDED.derivation_namespace,needs_review=FALSE`, f.Network)
	require.NoError(t, err)
	return f
}

func (f ActivationSelectorFixture) SeedTasks(t *testing.T) (id.ID, id.ID) {
	t.Helper()
	a, b := id.New(), id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status,updated_at) VALUES($1,$3,$5,'claimed',now()-interval '1 day'),($2,$4,$5,'claimed',now()); UPDATE cat_wallets SET chain_activation_status='activating' WHERE id IN ($3,$4)`, pgx.QueryExecModeSimpleProtocol, a, b, f.A, f.B, f.Network)
	require.NoError(t, err)
	return a, b
}

func (f ActivationSelectorFixture) AssertLockOrder(t *testing.T, first, next, writer func(context.Context) error) {
	t.Helper()
	assertAccountingLockOrder(t, f.Ctx, f.Pool, first, next, writer)
}

func (f ActivationSelectorFixture) AssertSerializableLockOrder(t *testing.T, first, next, writer func(context.Context) error) {
	t.Helper()
	assertAccountingLockOrder(t, f.Ctx, f.Pool, first, next, writer, postgres.SerializableTxOptions())
}
