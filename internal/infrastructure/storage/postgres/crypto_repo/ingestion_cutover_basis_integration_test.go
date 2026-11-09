//go:build integration

package crypto_repo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

type cutoverFixture109 struct {
	base                   amlRoutingFixture
	network, wallet, asset id.ID
	assets                 []crypto.AssetRef
	repo                   *BlockIngestionRepo
}

func newCutoverFixture109(t *testing.T) cutoverFixture109 {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	f := cutoverFixture109{base: amlRoutingFixture{ctx: ctx, pool: pool}, network: id.New(), wallet: id.New(), asset: id.New(), repo: NewBlockIngestionRepo()}
	_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
 VALUES($1,$1::uuid::text,'Registration test','tron',$1::uuid::text,'TRX',6)`, f.network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Watched wallet',$2,$3,'synthetic-key')`, f.wallet, f.network, "address-"+f.wallet.String())
	require.NoError(t, err)
	contract := "contract-" + f.asset.String()
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address) VALUES($1,$1::uuid::text,'Watched asset',$2,'CT',6,'TRC-20',$3)`, f.asset, f.network, contract)
	require.NoError(t, err)
	f.assets = []crypto.AssetRef{{TokenID: f.asset, Standard: "TRC-20", ContractAddress: contract, Decimals: 6}}
	return f
}

func cutoverBlock109(height int64) crypto.WatcherBlockFingerprint {
	return crypto.WatcherBlockFingerprint{BlockNumber: height, BlockHash: fmt.Sprintf("%064x", height), Timestamp: 1750000000000 + height*3000}
}

func (f cutoverFixture109) snapshot(t *testing.T) string {
	t.Helper()
	var data string
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT jsonb_build_object('ingestion',(SELECT to_jsonb(i) FROM sys_crypto_block_ingestion i WHERE network_id=$1),
 'targets',(SELECT jsonb_agg(to_jsonb(t) ORDER BY target_kind,target_key) FROM sys_crypto_watch_targets t WHERE network_id=$1))::text`, f.network).Scan(&data))
	return data
}

func TestIngestionCutoverRetainsRegistrationAndFinancialBoundary(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	first := cutoverBlock109(10)
	_, err := f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, first, "shadow", f.assets)
	require.NoError(t, err)
	before := f.snapshot(t)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, first, "shadow", f.assets)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t), "exact replay is read-only")
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(15), "shadow", f.assets)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t), "a later head is a hint, not a new initial registration")
	wrong := first
	wrong.BlockHash = strings.Repeat("e", 64)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, wrong, "shadow", f.assets)
	require.ErrorContains(t, err, "conflicts")
	for _, change := range []func(*crypto.AssetRef){
		func(a *crypto.AssetRef) { a.TokenID = id.New() },
		func(a *crypto.AssetRef) { a.Decimals++ },
		func(a *crypto.AssetRef) { a.ContractAddress = "foreign-contract" },
		func(a *crypto.AssetRef) { a.Standard = "ERC-20" },
	} {
		bad := f.assets[0]
		change(&bad)
		_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(20), "hybrid", []crypto.AssetRef{f.assets[0], bad})
		require.Error(t, err)
		require.Equal(t, before, f.snapshot(t), "invalid later asset rolls back the whole mode change")
	}
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network, NetworkIdentifier: "foreign-network"}, first, "shadow", f.assets)
	require.ErrorContains(t, err, "network identity")
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(5), "hybrid", f.assets)
	require.ErrorContains(t, err, "cannot begin before")
	promoted, err := f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(20), "hybrid", f.assets)
	require.NoError(t, err)
	require.Equal(t, int64(20), promoted.Height)
	require.NotNil(t, promoted.ActivatedAt)
	var registration, activation string
	require.NoError(t, pool.QueryRow(ctx, `SELECT registration_basis->>'height',activation_basis->>'height' FROM sys_crypto_block_ingestion WHERE network_id=$1`, f.network).Scan(&registration, &activation))
	require.Equal(t, "10", registration)
	require.Equal(t, "20", activation)
	paused, err := f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(40), "paused", f.assets)
	require.NoError(t, err)
	require.Equal(t, promoted.Height, paused.Height)
	require.Equal(t, promoted.ActivatedAt, paused.ActivatedAt)
	pausedSnapshot := f.snapshot(t)
	// Replaying the original SQL registration never downgrades the current mode.
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode)
 VALUES($1,$2,$3,$4,'shadow') ON CONFLICT DO NOTHING`, f.network, first.BlockNumber, first.BlockHash, first.Timestamp)
	require.NoError(t, err)
	require.Equal(t, pausedSnapshot, f.snapshot(t))
	for _, mutation := range []string{
		`UPDATE sys_crypto_block_ingestion SET start_height=start_height+1 WHERE network_id=$1`,
		`UPDATE sys_crypto_block_ingestion SET activated_at=NULL WHERE network_id=$1`,
		`UPDATE sys_crypto_block_ingestion SET activation_basis='{}' WHERE network_id=$1`,
		`UPDATE sys_crypto_block_ingestion SET mode='shadow' WHERE network_id=$1`,
		`DELETE FROM sys_crypto_block_ingestion WHERE network_id=$1`,
	} {
		_, err = pool.Exec(ctx, mutation, f.network)
		require.Error(t, err)
	}
	require.Equal(t, pausedSnapshot, f.snapshot(t))
}

func TestWatchRegistrationKeepsFirstOwnerBoundaryAndRetiredCatalog(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	_, err := f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.NoError(t, err)
	before := f.snapshot(t)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source)
 SELECT network_id,target_kind,target_key,owner_id,999,'later_refresh' FROM sys_crypto_watch_targets WHERE network_id=$1 ON CONFLICT DO NOTHING`, f.network)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t))
	for _, mutation := range []string{
		`UPDATE sys_crypto_watch_targets SET start_height=999 WHERE network_id=$1`,
		`UPDATE sys_crypto_watch_targets SET owner_id=$1 WHERE network_id=$1`,
		`DELETE FROM sys_crypto_watch_targets WHERE network_id=$1`,
	} {
		_, err = pool.Exec(ctx, mutation, f.network)
		require.ErrorContains(t, err, "immutable")
	}
	for _, mutation := range []string{
		`UPDATE cat_wallets SET address='rebound' WHERE id=$1`,
		`UPDATE cat_wallets SET id=$2 WHERE id=$1`,
	} {
		args := []any{f.wallet}
		if strings.Contains(mutation, "$2") {
			args = append(args, id.New())
		}
		_, err = pool.Exec(ctx, mutation, args...)
		require.ErrorContains(t, err, "watched catalog identity")
	}
	_, err = pool.Exec(ctx, `UPDATE cat_tokens SET decimal_places=18 WHERE id=$1`, f.asset)
	require.ErrorContains(t, err, "watched catalog identity")
	_, err = pool.Exec(ctx, `UPDATE cat_tokens SET deletion_mark=TRUE WHERE id=$1`, f.asset)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET deletion_mark=TRUE,is_active=FALSE WHERE id=$1`, f.wallet)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t), "retiring catalog entries retains their watch history")
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id)
 SELECT $2,$2::uuid::text,'Another owner',network_id,address,'synthetic' FROM cat_wallets WHERE id=$1`, f.wallet, id.New())
	require.Error(t, err, "an already used address cannot be reassigned")
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
 SELECT $2,$2::uuid::text,'Another asset owner',network_id,'ALT',decimal_places,token_standard,contract_address FROM cat_tokens WHERE id=$1`, f.asset, id.New())
	require.ErrorContains(t, err, "first owner")
}

func TestCutoverNetworkGatePreventsPartialCatalogPublicationAndReverseWait(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	require.NoError(t, tx.QueryRow(ctx, `SELECT fn_crypto_try_lock_watch_network($1)`, f.network).Scan(&locked))
	require.True(t, locked)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.ErrorContains(t, err, "busy")
	newWallet := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Concurrent managed wallet',$2,$1::uuid::text,'synthetic')`, newWallet, f.network)
	require.ErrorContains(t, err, "55P03")
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets WHERE id=$1)`, newWallet).Scan(&exists))
	require.False(t, exists)
	require.NoError(t, tx.Rollback(ctx))
	// Simulate an existing managed wallet not yet inventoried by the old
	// reader. Unlike a refresh, this first registration needs its wallet lock.
	provider := history107Provider(t, f.base)
	_, err = provider.DownTo(ctx, 101)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM sys_crypto_watch_targets WHERE owner_id=$1`, f.wallet)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.wallet)
	require.NoError(t, err)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.ErrorIs(t, err, crypto.ErrIngestionRegistrationBusy)
	// Registration does not retain its network gate after failing the wallet
	// prefix, so this already-locked catalog writer can complete normally.
	_, err = tx.Exec(ctx, `UPDATE cat_wallets SET vault_key_id=vault_key_id WHERE id=$1`, f.wallet)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.NoError(t, err)
}

func TestCutoverMigrationRetainsShadowBasisAndLegacyUnknownHistory(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	_, err := f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.NoError(t, err)
	before := f.snapshot(t)
	provider := history107Provider(t, f.base)
	_, err = provider.DownTo(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t), "Down retains the additive basis columns")
	legacy := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
 VALUES($1,$1::uuid::text,'Legacy boundary','tron',$1::uuid::text,'TRX',6)`, legacy)
	require.NoError(t, err)
	b := cutoverBlock109(10)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,activated_at) VALUES($1,$2,$3,$4,'hybrid',NOW())`, legacy, b.BlockNumber, b.BlockHash, b.Timestamp)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, before, f.snapshot(t))
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: legacy}, cutoverBlock109(20), "paused", nil)
	require.NoError(t, err)
	var unknown bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT registration_basis IS NULL AND activation_basis IS NULL FROM sys_crypto_block_ingestion WHERE network_id=$1`, legacy).Scan(&unknown))
	require.True(t, unknown)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(20), "hybrid", f.assets)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 101)
	require.ErrorContains(t, err, "forward recovery")
}

func TestCutoverRequiresActivationTimeForFinancialMode(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	b := cutoverBlock109(10)
	_, err := pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode)
 VALUES($1,$2,$3,$4,'hybrid')`, f.network, b.BlockNumber, b.BlockHash, b.Timestamp)
	require.ErrorContains(t, err, "requires its activation time")
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, b, "shadow", f.assets)
	require.NoError(t, err)
	before := f.snapshot(t)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET mode='solidified' WHERE network_id=$1`, f.network)
	require.ErrorContains(t, err, "requires its activation time")
	require.Equal(t, before, f.snapshot(t))
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, b, "solidified", f.assets)
	require.NoError(t, err)
}

func TestCutoverMigrationRejectsUnknownFinancialActivation(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	provider := history107Provider(t, f.base)
	_, err := provider.DownTo(ctx, 101)
	require.NoError(t, err)
	b := cutoverBlock109(10)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode)
 VALUES($1,$2,$3,$4,'hybrid')`, f.network, b.BlockNumber, b.BlockHash, b.Timestamp)
	require.NoError(t, err)
	before := f.snapshot(t)
	_, err = provider.Up(ctx)
	require.ErrorContains(t, err, "requires review before upgrade")
	require.Equal(t, before, f.snapshot(t), "the migration never invents missing financial activation history")
}

func TestCutoverCatalogGuardRejectsStaleTransactionSnapshot(t *testing.T) {
	f := newCutoverFixture109(t)
	ctx, pool := f.base.ctx, f.base.pool
	provider := history107Provider(t, f.base)
	_, err := provider.DownTo(ctx, 101)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM sys_crypto_watch_targets WHERE owner_id=$1`, f.wallet)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var watched bool
	require.NoError(t, tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_watch_targets WHERE owner_id=$1)`, f.wallet).Scan(&watched))
	require.False(t, watched)
	_, err = f.repo.EnsureCutover(ctx, crypto.NetworkRef{ID: f.network}, cutoverBlock109(10), "shadow", f.assets)
	require.NoError(t, err)
	before := f.snapshot(t)
	// The catalog row itself did not change while registration committed.
	// Repeatable read could otherwise miss the new watch owner and rebind it.
	_, err = tx.Exec(ctx, `UPDATE cat_wallets SET address='stale-snapshot-address' WHERE id=$1`, f.wallet)
	require.ErrorContains(t, err, "requires read committed isolation")
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, before, f.snapshot(t))
}
