//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

func accountingProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool) crypto.TransferVerification {
	t.Helper()
	network := id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES($1,$2,'Accounting identity fixture','evm',$2,'ETH',18)`, network, "ACCT-"+id.Compact(network))
	require.NoError(t, err)
	event := crypto.BlockchainEvent{
		NetworkID: network, TxHash: "0x" + strings.Repeat("a", 64),
		EventKind: crypto.EventKindContractLog, EventIndex: 7, DecoderVersion: 1,
		FromAddress:   "0x00000000000000000000000000000000000000AB",
		ToAddress:     "0x00000000000000000000000000000000000000CD",
		TokenContract: "0x00000000000000000000000000000000000000EF",
		Amount:        types.MustCryptoAmount("23"), BlockNumber: 100,
		BlockHash: "0x" + strings.Repeat("b", 64), Confirmations: 20,
		Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical,
	}
	identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyEVM, 2)
	require.NoError(t, err)
	return crypto.TransferVerification{
		AccountingIdentity: &identity, Event: event, DecoderVersion: 1,
		VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded,
		Sources: []crypto.VerificationSource{
			{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "receipt", HeadHeight: 119, HeadHash: "head"},
			{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "receipt", HeadHeight: 119, HeadHash: "head"},
		},
	}
}

func TestAccountingEffectEvidenceConcurrentReplayAndReinclusion(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	repo := NewBlockIngestionRepo()
	const workers = 12
	ids, errs := make([]id.ID, workers), make([]error, workers)
	var wg sync.WaitGroup
	for index := range workers {
		wg.Go(func() { ids[index], errs[index] = repo.SaveVerification(ctx, proof) })
	}
	wg.Wait()
	for index := range workers {
		require.NoError(t, errs[index])
		require.Equal(t, ids[0], ids[index])
	}
	var effects, occurrences, links, money int
	require.NoError(t, pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM reg_crypto_accounting_effects),
		(SELECT COUNT(*) FROM reg_crypto_accounting_effect_occurrences),
		(SELECT COUNT(*) FROM reg_crypto_accounting_effect_evidence),
		(SELECT COUNT(*) FROM reg_crypto_balance_movements)`).Scan(&effects, &occurrences, &links, &money))
	require.Equal(t, []int{1, 1, 1, 0}, []int{effects, occurrences, links, money})
	key, err := proof.AccountingIdentity.Key()
	require.NoError(t, err)
	var storedKey string
	require.NoError(t, pool.QueryRow(ctx, "SELECT logical_key FROM reg_crypto_accounting_effects").Scan(&storedKey))
	require.Equal(t, key, storedKey, "SQL and Go identity preimages must match")

	reincluded := proof
	reincluded.Event.EventIndex, reincluded.Event.BlockNumber = 91, 101
	reincluded.Event.BlockHash = "0x" + strings.Repeat("c", 64)
	reincluded.Event.Confirmations = 19
	secondID, err := repo.SaveVerification(ctx, reincluded)
	require.NoError(t, err)
	require.NotEqual(t, ids[0], secondID)
	require.NoError(t, pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM reg_crypto_accounting_effects),
		(SELECT COUNT(*) FROM reg_crypto_accounting_effect_occurrences),
		(SELECT COUNT(DISTINCT economics_hash) FROM reg_crypto_accounting_effect_occurrences)`).Scan(&effects, &occurrences, &links))
	require.Equal(t, []int{1, 2, 1}, []int{effects, occurrences, links})

	conflicting := proof
	conflicting.Event.Amount = types.MustCryptoAmount("24")
	_, err = repo.SaveVerification(ctx, conflicting)
	require.ErrorContains(t, err, "accounting occurrence evidence conflicts")
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1", proof.Event.NetworkID).Scan(&links))
	require.Equal(t, 2, links, "failed occurrence must roll back its new verification")
	changedAlias := proof
	changedAlias.Event.EventIndex = 8
	_, err = repo.SaveVerification(ctx, changedAlias)
	require.ErrorContains(t, err, "does not match verified occurrence", "one block occurrence cannot have two observation aliases")

	for _, table := range []string{"reg_crypto_accounting_effects", "reg_crypto_accounting_effect_occurrences", "reg_crypto_accounting_effect_evidence"} {
		_, err = pool.Exec(ctx, "DELETE FROM "+table)
		require.ErrorContains(t, err, "immutable")
	}
	_, err = pool.Exec(ctx, "UPDATE reg_crypto_accounting_effect_occurrences SET amount=amount+1")
	require.ErrorContains(t, err, "immutable")
}

func TestAccountingTRONInternalEvidenceUsesCompleteTraceIdentity(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	_, err := pool.Exec(ctx, "UPDATE cat_blockchain_networks SET chain_family='tron' WHERE id=$1", proof.Event.NetworkID)
	require.NoError(t, err)
	// SaveVerification accepts current TRON proof only in its configured schema.
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode)
		VALUES($1,0,repeat('f',64),0,'shadow')`, proof.Event.NetworkID)
	require.NoError(t, err)
	proof.AccountingIdentity = nil
	proof.DecoderVersion, proof.Event.DecoderVersion = 2, 2
	proof.Event.TxHash, proof.Event.BlockHash = proof.Event.TxHash[2:], proof.Event.BlockHash[2:]
	proof.Event.EventKind, proof.Event.TokenContract = crypto.EventKindInternalValue, ""
	proof.Event.TraceHash = strings.Repeat("e", 64)
	callIndex := int64(2)
	proof.Event.CallValueIndex = &callIndex
	for index := range proof.Sources {
		proof.Sources[index].DecoderVersion, proof.Sources[index].SourceDigest = 2, "source"
	}
	require.NoError(t, proof.BindTRONAccountingIdentity())
	_, err = NewBlockIngestionRepo().SaveVerification(ctx, proof)
	require.NoError(t, err)
	key, err := proof.AccountingIdentity.Key()
	require.NoError(t, err)
	var sqlKey string
	require.NoError(t, pool.QueryRow(ctx, "SELECT logical_key FROM reg_crypto_accounting_effects").Scan(&sqlKey))
	require.Equal(t, key, sqlKey)
}

func TestAccountingEffectLegacyProofReplayDoesNotRewriteHistory(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	legacy := proof
	legacy.AccountingIdentity = nil
	repo := NewBlockIngestionRepo()
	first, err := repo.SaveVerification(ctx, legacy)
	require.NoError(t, err)
	second, err := repo.SaveVerification(ctx, proof)
	require.NoError(t, err)
	require.Equal(t, first, second)
	var hasIdentity bool
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT evidence ? 'accountingIdentity' FROM reg_crypto_transfer_verifications WHERE id=$1", first).Scan(&hasIdentity))
	require.False(t, hasIdentity)
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM reg_crypto_accounting_effects").Scan(&count))
	require.Zero(t, count, "a transient in-memory ordinal must not silently upgrade persisted old evidence")
}

func TestAccountingEffectDatabaseRequiresExactPersistedProof(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	proofID, err := NewBlockIngestionRepo().SaveVerification(ctx, proof)
	require.NoError(t, err)
	// A directly inserted occurrence cannot survive without a proof link.
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_accounting_effect_occurrences(
		effect_id,block_number,block_hash,economics_hash,source_address,destination_address,contract_address,amount)
		SELECT id,101,repeat('c',64),repeat('d',64),'from','to','contract',23 FROM reg_crypto_accounting_effects`)
	require.ErrorContains(t, err, "requires verified evidence")

	// Direct SQL cannot assign an already persisted proof a different ordinal.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var effectID, occurrenceID id.ID
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO reg_crypto_accounting_effects(family,network_id,tx_hash,event_kind,effect_ordinal)
		VALUES('evm',$1,repeat('a',64),'contract_log',3) RETURNING id`, proof.Event.NetworkID).Scan(&effectID))
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO reg_crypto_accounting_effect_occurrences(
		effect_id,block_number,block_hash,economics_hash,source_address,destination_address,contract_address,amount)
		VALUES($1,100,repeat('b',64),repeat('d',64),$2,$3,$4,23) RETURNING id`,
		effectID, strings.ToLower(proof.Event.FromAddress), strings.ToLower(proof.Event.ToAddress),
		strings.ToLower(proof.Event.TokenContract)).Scan(&occurrenceID))
	_, err = tx.Exec(ctx, "INSERT INTO reg_crypto_accounting_effect_evidence(verification_id,occurrence_id) VALUES($1,$2)", proofID, occurrenceID)
	require.ErrorContains(t, err, "does not match verified occurrence")
}

func TestAccountingEffectMigrationPreservesLegacyAndProtectsEvidenceHistory(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	legacy := proof
	legacy.AccountingIdentity = nil
	repo := NewBlockIngestionRepo()
	legacyID, err := repo.SaveVerification(ctx, legacy)
	require.NoError(t, err)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 76)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var hasIdentity bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT evidence ? 'accountingIdentity' FROM reg_crypto_transfer_verifications WHERE id=$1", legacyID).Scan(&hasIdentity))
	require.False(t, hasIdentity, "schema expansion must preserve existing transfer evidence")
	proof.Event.Confirmations++
	for index := range proof.Sources {
		proof.Sources[index].HeadHeight++
	}
	_, err = repo.SaveVerification(ctx, proof)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 76)
	require.ErrorContains(t, err, "accounting evidence history requires a forward-compatible binary")
	var version int
	require.NoError(t, pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
	require.Equal(t, 77, version)
}
