//go:build integration

package crypto_repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

func newFinancialReorgReplayFixture(t *testing.T) (paymentRecognitionFixture, crypto.BlockchainEvent) {
	t.Helper()
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	orphan := f.proof
	orphan.Outcome, orphan.ExecutionStatus = "absent", crypto.ReceiptPending
	orphan.Event.Canonicality, orphan.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	orphan.VerifiedAt = orphan.VerifiedAt.Add(time.Second)
	_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, orphan)
	require.NoError(t, err)
	return f, orphan.Event
}

func financialReorgSnapshot(t *testing.T, f paymentRecognitionFixture) string {
	t.Helper()
	var result string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'compensations',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM reg_crypto_reorg_compensations x WHERE payment_id=$1),
 'incidents',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM reg_crypto_security_incidents x WHERE network_id=$2),
 'holds',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM reg_crypto_network_holds x WHERE network_id=$2),
 'custody',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY line_id),'[]') FROM reg_crypto_balance_movements x WHERE wallet_id=$3),
 'merchant',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY line_id),'[]') FROM reg_crypto_merchant_balance_movements x WHERE merchant_id=$4),
 'fee',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY line_id),'[]') FROM reg_crypto_fee_movements x WHERE merchant_id=$4))::text`, f.payment.ID, f.payment.NetworkID, f.payment.WalletID, f.payment.MerchantID).Scan(&result))
	return result
}

func TestFinancialReorgConcurrentReplayAndResolvedHold(t *testing.T) {
	f, event := newFinancialReorgReplayFixture(t)
	r := NewFinancialReorgRepo()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- r.RecordFinancialReorg(f.ctx, f.payment, event) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, []string{"0", "0", "0"}, f.balances(t))
	var compensations, reversals int
	var incident id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*),min(incident_id::text)::uuid FROM reg_crypto_reorg_compensations WHERE payment_id=$1`, f.payment.ID).Scan(&compensations, &incident))
	require.Equal(t, 1, compensations)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_type='CryptoPaymentReorg' AND reversal_of_line_id IS NOT NULL`).Scan(&reversals))
	require.Equal(t, 1, reversals)
	holds := cryptoapp.NewOperationalHoldService(postgres.NewContextTxManager(), NewOperationalHoldRepo())
	require.NoError(t, holds.ResolveOwner(f.ctx, "security_incident", incident.String(), "reviewer", "verified compensation"))
	before := financialReorgSnapshot(t, f)
	event.Confirmations++
	event.Timestamp = event.Timestamp.Add(time.Second)
	require.NoError(t, r.RecordFinancialReorg(f.ctx, f.payment, event))
	require.Equal(t, before, financialReorgSnapshot(t, f))
}

func TestFinancialReorgRejectsChangedRequestAndStalePosting(t *testing.T) {
	f, event := newFinancialReorgReplayFixture(t)
	r := NewFinancialReorgRepo()
	require.NoError(t, r.RecordFinancialReorg(f.ctx, f.payment, event))
	before := financialReorgSnapshot(t, f)
	for _, change := range []func(*crypto.BlockchainEvent){
		func(e *crypto.BlockchainEvent) { e.BlockNumber++ }, func(e *crypto.BlockchainEvent) { e.BlockHash = "another-block" },
		func(e *crypto.BlockchainEvent) { e.TxHash = "replacement" }, func(e *crypto.BlockchainEvent) { e.EventIndex++ },
		func(e *crypto.BlockchainEvent) { e.Amount = types.MustCryptoAmount("99") }, func(e *crypto.BlockchainEvent) { e.NetworkID = id.New() },
	} {
		altered := event
		change(&altered)
		require.Error(t, r.RecordFinancialReorg(f.ctx, f.payment, altered))
		require.Equal(t, before, financialReorgSnapshot(t, f))
	}
	for _, change := range []func(*crypto_payment.CryptoPayment){
		func(p *crypto_payment.CryptoPayment) { p.Amount = types.MustCryptoAmount("99") }, func(p *crypto_payment.CryptoPayment) { p.WalletID = id.New() },
		func(p *crypto_payment.CryptoPayment) { p.TokenID = id.New() }, func(p *crypto_payment.CryptoPayment) { p.InvoiceID = id.New() },
		func(p *crypto_payment.CryptoPayment) { p.MerchantID = id.New() }, func(p *crypto_payment.CryptoPayment) { p.TxHash = "changed" },
		func(p *crypto_payment.CryptoPayment) { p.EventIndex++ }, func(p *crypto_payment.CryptoPayment) { p.PostedVersion++ },
		func(p *crypto_payment.CryptoPayment) { p.FeeFixed = types.MustCryptoAmount("4") }, func(p *crypto_payment.CryptoPayment) { p.BlockHash = "changed" },
	} {
		altered := *f.payment
		change(&altered)
		require.Error(t, r.RecordFinancialReorg(f.ctx, &altered, event))
		require.Equal(t, before, financialReorgSnapshot(t, f))
	}
}

func TestFinancialReorgRejectsForeignIncidentAndIncompleteCompensation(t *testing.T) {
	for _, scenario := range []string{"foreign_incident", "changed_basis", "missing_movements"} {
		t.Run(scenario, func(t *testing.T) {
			f, event := newFinancialReorgReplayFixture(t)
			owner := f.payment.ID
			if scenario == "foreign_incident" {
				other := *f.payment
				other.ID = id.New()
				other.TxHash = "other-" + other.ID.String()
				other.MarkUnposted()
				other.PostedVersion = 0
				require.NoError(t, document_repo.NewCryptoPaymentRepo().Create(f.ctx, &other))
				owner = other.ID
			}
			incident := id.New()
			_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,payment_id,incident_kind,tx_hash,event_kind,event_index,details)
 VALUES($1,$2,$3,'posted_payment_reorg',$4,$5,$6,'{}')`, incident, f.payment.NetworkID, owner, f.payment.TxHash, f.payment.EventKind, f.payment.EventIndex)
			require.NoError(t, err)
			if scenario != "foreign_incident" {
				basis, err := financialReorgRequestBasis(f.payment, event)
				require.NoError(t, err)
				if scenario == "changed_basis" {
					basis = []byte(`{"schemaVersion":1,"amount":"999"}`)
				}
				_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reorg_compensations(id,incident_id,payment_id,posted_version,observed_block_number,observed_block_hash,request_basis)
 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, id.New(), incident, f.payment.ID, f.payment.PostedVersion, event.BlockNumber, event.BlockHash, string(basis))
				require.NoError(t, err)
			}
			before := financialReorgSnapshot(t, f)
			require.Error(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, f.payment, event))
			require.Equal(t, before, financialReorgSnapshot(t, f))
			require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
		})
	}
}

func TestFinancialReorgWalletPrecedesPaymentAndIsolation(t *testing.T) {
	f, event := newFinancialReorgReplayFixture(t)
	assertAccountingLockOrder(t, f.ctx, f.pool, func(ctx context.Context) error {
		return NewCustodyRecognitionRepo().LockWallets(ctx, f.payment.WalletID)
	}, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_payments WHERE id=$1 FOR UPDATE`, f.payment.ID)
		return err
	}, func(ctx context.Context) error {
		return NewFinancialReorgRepo().RecordFinancialReorg(ctx, f.payment, event)
	})
	before := financialReorgSnapshot(t, f)
	require.NoError(t, postgres.MustGetTxManager(f.ctx).RunInTransactionWithOptions(f.ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		require.ErrorContains(t, NewFinancialReorgRepo().RecordFinancialReorg(ctx, f.payment, event), "read committed")
		return nil
	}))
	require.Equal(t, before, financialReorgSnapshot(t, f))
}

func TestNetworkIncidentStableReplayAndLegacyIdentity(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%t", legacy), func(t *testing.T) {
			ctx, pool := amlTestDatabase(t)
			network := id.New()
			_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Incident','evm',$1::uuid::text,'ETH',18)`, network)
			require.NoError(t, err)
			var original id.ID
			if legacy {
				original = id.New()
				_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,incident_kind,tx_hash,details) VALUES($1,$2,'deep_reorg','old-unordered-key','{"checkpoint":10,"hash":"a"}')`, original, network)
				require.NoError(t, err)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 12)
			for range 12 {
				wg.Go(func() {
					errs <- NewNetworkHoldRepo().HoldNetwork(ctx, network, crypto.OperationalHoldDeepReorg, map[string]any{"hash": "a", "checkpoint": 10})
				})
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			var count, holds int
			var incident id.ID
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*),min(id::text)::uuid FROM reg_crypto_security_incidents WHERE network_id=$1`, network).Scan(&count, &incident))
			require.Equal(t, 1, count)
			if legacy {
				require.Equal(t, original, incident)
			}
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1`, network).Scan(&holds))
			require.Equal(t, 1, holds)
		})
	}
}

func TestNetworkIncidentRejectsCanonicalKeyCollision(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	network := id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Incident','evm',$1::uuid::text,'ETH',18)`, network)
	require.NoError(t, err)
	details := map[string]any{"checkpoint": 10}
	encoded, err := json.MarshalDeterministic(details)
	require.NoError(t, err)
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(network.String()+"|deep_reorg|"+string(encoded))))
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,incident_kind,tx_hash,details) VALUES($1,$2,'deep_reorg',$3,'{"checkpoint":99}')`, id.New(), network, key)
	require.NoError(t, err)
	require.ErrorContains(t, NewNetworkHoldRepo().HoldNetwork(ctx, network, crypto.OperationalHoldDeepReorg, details), "original evidence")
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1`, network).Scan(&count))
	require.Zero(t, count)
}

func TestFinancialReorgMigrationPreservesLegacyAndProtectsFacts(t *testing.T) {
	f, event := newFinancialReorgReplayFixture(t)
	dsn := f.pool.Config().ConnConfig.ConnString()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	root, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, root)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 96)
	require.NoError(t, err)
	incident, compensation := id.New(), id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,payment_id,incident_kind,tx_hash,event_kind,event_index) VALUES($1,$2,$3,'posted_payment_reorg',$4,$5,$6)`, incident, f.payment.NetworkID, f.payment.ID, f.payment.TxHash, f.payment.EventKind, f.payment.EventIndex)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reorg_compensations(id,incident_id,payment_id,posted_version,observed_block_number,observed_block_hash) VALUES($1,$2,$3,$4,$5,$6)`, compensation, incident, f.payment.ID, f.payment.PostedVersion, event.BlockNumber, event.BlockHash)
	require.NoError(t, err)
	// A legacy completed compensation has the same three source-linked reversals.
	for _, table := range []string{"reg_crypto_balance_movements", "reg_crypto_merchant_balance_movements", "reg_crypto_fee_movements"} {
		dimensions := "wallet_id,token_id"
		if table != "reg_crypto_balance_movements" {
			dimensions = "merchant_id,token_id"
		}
		if table == "reg_crypto_fee_movements" {
			dimensions += ",fee_type"
		}
		_, err = f.pool.Exec(f.ctx, `INSERT INTO `+table+`(line_id,recorder_id,recorder_type,recorder_version,period,record_type,`+dimensions+`,amount,reversal_of_line_id,reversal_reason)
 SELECT gen_random_uuid_v7(),$1,'CryptoPaymentReorg',1,NOW(),'expense',`+dimensions+`,amount,line_id,'posted_payment_reorg' FROM `+table+` WHERE recorder_id=$2 AND recorder_type='CryptoPayment' AND recorder_version=$3`, compensation, f.payment.ID, f.payment.PostedVersion)
		require.NoError(t, err)
	}
	var before, after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(c)::text FROM reg_crypto_reorg_compensations c WHERE id=$1`, compensation).Scan(&before))
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reorg_compensations(id,incident_id,payment_id,posted_version,observed_block_number) VALUES($1,$2,$3,77,0)`, id.New(), incident, f.payment.ID)
	require.ErrorContains(t, err, "requires request basis version 1")
	require.NoError(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, f.payment, event))
	var legacyBasisIsNull bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT request_basis IS NULL FROM reg_crypto_reorg_compensations WHERE id=$1`, compensation).Scan(&legacyBasisIsNull))
	require.True(t, legacyBasisIsNull)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (to_jsonb(c)-'request_basis')::text FROM reg_crypto_reorg_compensations c WHERE id=$1`, compensation).Scan(&after))
	require.Equal(t, before, after)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_reorg_compensations SET observed_block_hash='changed' WHERE id=$1`, compensation)
	require.ErrorContains(t, err, "immutable")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM reg_crypto_reorg_compensations WHERE id=$1`, compensation)
	require.ErrorContains(t, err, "immutable")
	// A new generation creates a basis-bearing fact and cannot be rolled back.
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reorg_compensations(id,incident_id,payment_id,posted_version,observed_block_number,request_basis) VALUES($1,$2,$3,99,0,'{"schemaVersion":1}')`, id.New(), incident, f.payment.ID)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 96)
	require.ErrorContains(t, err, "forward recovery")
}
