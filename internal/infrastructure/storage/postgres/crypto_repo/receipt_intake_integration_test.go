//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type receiptIntakeFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	service *cryptoapp.ReceiptIntakeService
	engine  *posting.Engine
	repo    *ReceiptIntakeRepo
	request cryptoapp.RecordCustodyReceiptCommand
	proof   crypto.TransferVerification
}

func newReceiptIntakeFixture(t *testing.T, publisher domain.OutboxPublisher) receiptIntakeFixture {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	tokenID, walletID := id.New(), id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,contract_address,decimal_places,token_standard)
		VALUES($1,$2,'Receipt asset',$3,'USDT',$4,6,'ERC-20')`, tokenID, "RI-"+id.Compact(tokenID), proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,derivation_path)
		VALUES($1,$2,'Receipt wallet',$3,$4,'receipt-key',$2)`, walletID, "RI-"+id.Compact(walletID), proof.Event.NetworkID, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1", proof.Event.NetworkID)
	require.NoError(t, err)
	proofID, err := NewBlockIngestionRepo().SaveVerification(ctx, proof)
	require.NoError(t, err)
	event := proof.Event
	event.VerificationID = &proofID
	basis := crypto.AccountingTransferBasis{NetworkID: event.NetworkID, WalletID: walletID, TokenID: tokenID,
		TxHash: event.TxHash, EventKind: event.EventKind, EventIndex: event.EventIndex, FromAddress: event.FromAddress,
		Amount: event.Amount, BlockNumber: event.BlockNumber, BlockHash: event.BlockHash, RequiredConfs: 19, VerificationID: &proofID}
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&posting.CryptoBalanceVisitor{})
	engine.AddVisitor(&posting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(posting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(register_repo.NewCryptoReceiptSuspenseRepo())))
	repo := NewReceiptIntakeRepo()
	if publisher == nil {
		publisher = postgres.NewOutboxPublisher()
	}
	service := cryptoapp.NewReceiptIntakeService(postgres.MustGetTxManager(ctx), repo, NewBlockIngestionRepo(),
		engine, numerator.New(), publisher, time.Now)
	return receiptIntakeFixture{ctx: ctx, pool: pool, service: service, engine: engine, repo: repo,
		request: cryptoapp.RecordCustodyReceiptCommand{Basis: basis, Event: event, Reason: crypto_custody_receipt.ReasonNoAllocation}, proof: proof}
}

func TestCustodyReceiptAtomicPostingConcurrentReplayAndFreeze(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	const count = 8
	docs, errs := make([]*crypto_custody_receipt.CryptoCustodyReceipt, count), make([]error, count)
	var wg sync.WaitGroup
	for index := range count {
		wg.Go(func() { docs[index], errs[index] = f.service.Record(f.ctx, f.request) })
	}
	wg.Wait()
	for index := range count {
		require.NoError(t, errs[index])
		require.Equal(t, docs[0].ID, docs[index].ID)
	}
	doc := docs[0]
	var custody, suspense string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2",
		doc.WalletID, doc.TokenID).Scan(&custody))
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$1", doc.ID).Scan(&suspense))
	require.Equal(t, []string{"23", "23"}, []string{custody, suspense})
	var receipts, claims, audits, outbox, merchant, fees int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM doc_crypto_custody_receipts),
		(SELECT count(*) FROM reg_crypto_custody_effect_recognitions),
		(SELECT count(*) FROM reg_crypto_receipt_intake_audit),
		(SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoCustodyReceipt'),
		(SELECT count(*) FROM reg_crypto_merchant_balance_movements),
		(SELECT count(*) FROM reg_crypto_fee_movements)`).Scan(&receipts, &claims, &audits, &outbox, &merchant, &fees))
	require.Equal(t, []int{1, 1, 1, 1, 0, 0}, []int{receipts, claims, audits, outbox, merchant, fees})
	changed := f.request
	changed.Reason = crypto_custody_receipt.ReasonDust
	_, err := f.service.Record(f.ctx, changed)
	require.ErrorContains(t, err, "replay differs")
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_custody_receipts SET amount=24 WHERE id=$1", doc.ID)
	require.ErrorContains(t, err, "first posting")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,
		per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=100 WHERE network_id=$1`, doc.NetworkID)
	require.ErrorContains(t, err, "cutover is not available")
	for _, direction := range []string{"receipt", "expense"} {
		_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,'LegacyWriter',1,NOW(),$2,$3,$4,1)`, id.New(), direction, doc.WalletID, doc.TokenID)
		require.ErrorContains(t, err, "availability cutover")
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'CryptoCustodyReceipt',1,NOW(),'receipt',$2,$3,1)`, id.New(), doc.WalletID, doc.TokenID)
	require.ErrorContains(t, err, "requires its accounting document")
}

type failingReceiptPublisher struct{}

func (failingReceiptPublisher) Publish(context.Context, domain.DomainEvent) error {
	return errors.New("injected outbox failure")
}

type missingReceiptPublisher struct{}

func (missingReceiptPublisher) Publish(context.Context, domain.DomainEvent) error { return nil }

type missingReceiptAudit struct{ *ReceiptIntakeRepo }

func (missingReceiptAudit) AppendIntakeAudit(context.Context, *crypto_custody_receipt.CryptoCustodyReceipt) error {
	return nil
}

func TestCustodyReceiptRollsBackEveryFinancialComponentOnFailure(t *testing.T) {
	f := newReceiptIntakeFixture(t, failingReceiptPublisher{})
	_, err := f.service.Record(f.ctx, f.request)
	require.ErrorContains(t, err, "injected outbox failure")
	for _, table := range []string{"doc_crypto_custody_receipts", "reg_crypto_balance_movements",
		"reg_crypto_receipt_suspense_movements", "reg_crypto_custody_effect_postings",
		"reg_crypto_custody_effect_recognitions", "reg_crypto_receipt_intake_audit"} {
		var count int
		require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestCustodyReceiptDeferredConstraintsRejectPartialCommit(t *testing.T) {
	for _, scenario := range []string{"missing_outbox", "missing_audit", "missing_suspense"} {
		t.Run(scenario, func(t *testing.T) {
			f := newReceiptIntakeFixture(t, nil)
			var repo cryptoapp.ReceiptIntakeRepository = f.repo
			var publisher domain.OutboxPublisher = postgres.NewOutboxPublisher()
			engine := f.engine
			switch scenario {
			case "missing_outbox":
				publisher = missingReceiptPublisher{}
			case "missing_audit":
				repo = missingReceiptAudit{f.repo}
			case "missing_suspense":
				engine = posting.NewEngine(nil)
				engine.AddVisitor(&posting.CryptoBalanceVisitor{})
				engine.AddVisitor(&posting.CryptoReceiptSuspenseVisitor{})
				engine.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
			}
			service := cryptoapp.NewReceiptIntakeService(postgres.MustGetTxManager(f.ctx), repo,
				NewBlockIngestionRepo(), engine, numerator.New(), publisher, time.Now)
			_, err := service.Record(f.ctx, f.request)
			require.ErrorContains(t, err, "complete atomic accounting")
			for _, table := range []string{"doc_crypto_custody_receipts", "reg_crypto_balance_movements",
				"reg_crypto_receipt_suspense_movements", "reg_crypto_custody_effect_postings", "sys_outbox"} {
				var count int
				require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count))
				require.Zero(t, count, table)
			}
		})
	}
}

func TestCustodyReceiptDifferentEffectsInSameTransactionRemainSeparate(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	first, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	second := f.proof
	second.Event.EventIndex = 8
	identity, err := crypto.NewAccountingEffectIdentity(second.Event, crypto.ChainFamilyEVM, 3)
	require.NoError(t, err)
	second.AccountingIdentity = &identity
	secondID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, second)
	require.NoError(t, err)
	request := f.request
	request.Event = second.Event
	request.Event.VerificationID = &secondID
	request.Basis.EventIndex, request.Basis.VerificationID = 8, &secondID
	next, err := f.service.Record(f.ctx, request)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, next.ID)
	var amount string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2",
		first.WalletID, first.TokenID).Scan(&amount))
	require.Equal(t, "46", amount)
}

func TestCustodyReceiptDoesNotAdmitSupersededProof(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	absence := f.proof
	absence.Outcome, absence.ExecutionStatus = "absent", crypto.ReceiptPending
	absence.Event.Canonicality, absence.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	absence.VerifiedAt = absence.VerifiedAt.Add(time.Second)
	_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, absence)
	require.NoError(t, err)
	_, err = f.service.Record(f.ctx, f.request)
	require.ErrorContains(t, err, "exact physical movements")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements").Scan(&count))
	require.Zero(t, count)
}

func TestCustodyReceiptSerializesAgainstLegacyWriterAlreadyWaiting(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	ready, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	defer resume()
	receiptDone, legacyDone := make(chan error, 1), make(chan error, 1)
	go func() {
		receiptDone <- postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
			_, err := f.service.Record(ctx, f.request)
			close(ready)
			if err != nil {
				return err
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("receipt did not reach commit boundary")
	}
	go func() {
		_, err := f.pool.Exec(f.ctx, `/* receipt_overlap */ INSERT INTO reg_crypto_balance_movements(
			recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,'LegacyConcurrent',1,NOW(),'receipt',$2,$3,23)`,
			id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
		legacyDone <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND query LIKE '/* receipt_overlap */%' AND wait_event_type='Lock')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "legacy writer must be waiting on the same balance lock")
	resume()
	require.NoError(t, <-receiptDone)
	require.ErrorContains(t, <-legacyDone, "availability cutover")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements").Scan(&count))
	require.Equal(t, 1, count)
}

func TestCustodyReceiptRejectsInternalUnadoptedAndLiveInventory(t *testing.T) {
	for _, scenario := range []string{"managed_sender", "unadopted_inventory", "live_network"} {
		t.Run(scenario, func(t *testing.T) {
			f := newReceiptIntakeFixture(t, nil)
			switch scenario {
			case "managed_sender":
				_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,derivation_path)
					VALUES($1,$2,'Internal sender',$3,$4,'sender-key',$2)`, id.New(), "IS-"+id.Compact(id.New()), f.request.Basis.NetworkID, f.proof.Event.FromAddress)
				require.NoError(t, err)
			case "unadopted_inventory":
				_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
					VALUES($1,'LegacyManual',1,NOW(),'receipt',$2,$3,23)`, id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
				require.NoError(t, err)
			case "live_network":
				_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,
					per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=100 WHERE network_id=$1`, f.request.Basis.NetworkID)
				require.NoError(t, err)
			}
			_, err := f.service.Record(f.ctx, f.request)
			require.Error(t, err)
			var count int
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM doc_crypto_custody_receipts").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestCustodyReceiptSingleOwnerAcrossForkAndRecorder(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	doc, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	var effectID id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT effect_id FROM reg_crypto_custody_effect_postings WHERE id=$1", doc.PostingID).Scan(&effectID))
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_custody_effect_postings(
		effect_id,occurrence_id,verification_id,recorder_id,recorder_type,recorder_version)
		VALUES($1,$2,$3,$4,'CryptoPayment',1)`, effectID, doc.OccurrenceID, doc.VerificationID, id.New())
	require.Error(t, err)
	reincluded := f.proof
	reincluded.Event.EventIndex, reincluded.Event.BlockNumber = 91, 101
	reincluded.Event.BlockHash = "0x" + strings.Repeat("c", 64)
	reincluded.Event.Confirmations = 19
	reincluded.VerifiedAt = reincluded.VerifiedAt.Add(time.Second)
	newProofID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, reincluded)
	require.NoError(t, err)
	request := f.request
	request.Event = reincluded.Event
	request.Event.VerificationID = &newProofID
	request.Basis.EventIndex, request.Basis.BlockNumber = 91, 101
	request.Basis.BlockHash, request.Basis.VerificationID = reincluded.Event.BlockHash, &newProofID
	_, err = f.service.Record(f.ctx, request)
	require.ErrorContains(t, err, "accounting investigation")
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 77)
	require.ErrorContains(t, err, "requires forward migration")
}

func TestCustodyReceiptDrainChecksChainOutcomeRatherThanAuthorizationStatus(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	auth := crypto.SigningAuthorization{
		SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(),
		NetworkID: f.request.Basis.NetworkID, NetworkCode: "ACCT", ChainFamily: crypto.ChainFamilyEVM,
		NetworkIdentifier: "11155111", Purpose: crypto.SigningPurposeSweep, BasisType: "ReceiptDrainTest", BasisID: id.New(),
		SourceAddress: f.proof.Event.ToAddress, VaultKeyID: "receipt-key",
		DestinationAddress: "0x0000000000000000000000000000000000000012",
		AssetStandard:      "ERC-20", ContractAddress: f.proof.Event.TokenContract,
		AmountMinorUnits: types.MustCryptoAmount("1"), MaxNetworkFeeMinor: types.MustCryptoAmount("1"),
		UnsignedFingerprint: "receipt-drain", ExpiresAt: time.Now().Add(time.Hour), PolicyVersion: "evm-v1",
	}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1,
		UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, Payload: []byte("{}")}
	authID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	hash := "0x" + strings.Repeat("e", 64)
	envelope, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1,
		UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, TxHash: hash, Payload: []byte("{}")})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations
		SET status='signed',tx_hash=$2,signed_envelope=$3::jsonb WHERE id=$1`, authID, hash, string(envelope))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, "UPDATE reg_crypto_signing_authorizations SET status='broadcast' WHERE id=$1", authID)
	require.NoError(t, err)
	_, err = f.service.Record(f.ctx, f.request)
	require.ErrorContains(t, err, "disabled drained network", "unknown broadcast still requires recovery")
	native := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard)
		VALUES($1,$2,'Drain native asset',$3,'ETH',18,'native')`, native, "DN-"+id.Compact(native), auth.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_chain_settlements(
		network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,
		native_token_id,total_fee_minor,raw_receipt,observed_at)
		VALUES($1,$2,$3,$4,'succeeded',repeat('f',64),100,'solidified',$5,0,'{}',NOW())`,
		auth.NetworkID, auth.BasisType, auth.BasisID, hash, native)
	require.NoError(t, err)
	_, err = f.service.Record(f.ctx, f.request)
	require.NoError(t, err, "historical broadcast status with a final settlement is drained")
}

func TestCustodyReceiptMigrationPreservesLegacyInventoryAndEmptyRollback(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 77)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(
		recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'LegacyWriter',1,NOW(),'receipt',$2,$3,23)`, id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
	require.NoError(t, err)
	for range 2 {
		_, err = provider.Up(f.ctx)
		require.NoError(t, err)
		var count int
		var balance string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
			(SELECT count(*) FROM doc_crypto_custody_receipts),
			(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2)`,
			f.request.Basis.WalletID, f.request.Basis.TokenID).Scan(&count, &balance))
		require.Zero(t, count, "migration must not invent receipts for legacy inventory")
		require.Equal(t, "23", balance)
		_, err = f.service.Record(f.ctx, f.request)
		require.ErrorContains(t, err, "accounting investigation")
		_, err = provider.DownTo(f.ctx, 77)
		require.NoError(t, err, "an unused schema expansion can be rolled back")
	}
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(
		recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'LegacyWriter',1,NOW(),'expense',$2,$3,1)`, id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
	require.NoError(t, err, "legacy writers continue on inventory that has no v2 receipt")
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_balance_balances
		WHERE wallet_id=$1 AND token_id=$2`, f.request.Basis.WalletID, f.request.Basis.TokenID).Scan(&balance))
	require.Equal(t, "22", balance)
}
