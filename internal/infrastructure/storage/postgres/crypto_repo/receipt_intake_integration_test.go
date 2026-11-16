//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
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
				engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
				engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
				engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
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

func TestCustodyReceiptRejectsInternalTransfer(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,derivation_path)
		VALUES($1,$2,'Internal sender',$3,$4,'sender-key',$2)`, id.New(), "IS-"+id.Compact(id.New()), f.request.Basis.NetworkID, f.proof.Event.FromAddress)
	require.NoError(t, err)
	_, err = f.service.Record(f.ctx, f.request)
	require.ErrorContains(t, err, "internal transfer")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM doc_crypto_custody_receipts").Scan(&count))
	require.Zero(t, count)
}

func TestCustodyReceiptWorksOnLiveNetworkWithExistingInventory(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,
		per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=100 WHERE network_id=$1`, f.request.Basis.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'TestInventory',1,NOW(),'receipt',$2,$3,5)`, id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
	require.NoError(t, err)
	doc, err := f.service.Record(f.ctx, f.request)
	require.NoError(t, err)
	var balance, suspense string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT b.amount::text,s.amount::text FROM reg_crypto_balance_balances b
		JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=$3 WHERE b.wallet_id=$1 AND b.token_id=$2`,
		f.request.Basis.WalletID, f.request.Basis.TokenID, doc.ID).Scan(&balance, &suspense))
	require.Equal(t, f.request.Basis.Amount.Add(types.MustRawAmount("5")).String(), balance)
	require.Equal(t, f.request.Basis.Amount.String(), suspense)
	// Other writers keep using the wallet after a receipt; the receipt owns only its effect.
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'TestInventory',2,NOW(),'expense',$2,$3,1)`, id.New(), f.request.Basis.WalletID, f.request.Basis.TokenID)
	require.NoError(t, err)
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
	// A re-included transfer is the same logical effect: it resolves to the
	// original receipt instead of creating a second custody owner.
	again, err := f.service.Record(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, doc.ID, again.ID)
}
