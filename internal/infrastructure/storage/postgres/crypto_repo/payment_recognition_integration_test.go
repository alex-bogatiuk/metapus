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

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_fee"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type paymentRecognitionFixture struct {
	receiptIntakeFixture
	payment     *crypto_payment.CryptoPayment
	recognition *cryptoapp.PaymentRecognitionService
	posting     *posting.Engine
}

func newPaymentRecognitionFixture(t *testing.T, publisher domain.OutboxPublisher) paymentRecognitionFixture {
	t.Helper()
	f := paymentRecognitionFixture{receiptIntakeFixture: newReceiptIntakeFixture(t, nil)}
	merchant := id.New()
	_, err := f.pool.Exec(f.ctx, "INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Payment recognition merchant')", merchant, "PR-"+id.Compact(merchant))
	require.NoError(t, err)
	e := f.proof.Event
	f.payment = crypto_payment.NewCryptoPayment(id.New(), merchant, f.request.Basis.TokenID, f.request.Basis.WalletID, e.NetworkID, e.TxHash, e.EventIndex, string(e.EventKind), e.BlockHash, e.FromAddress, e.Amount, e.BlockNumber, 19)
	f.payment.Number = "PR-" + id.Compact(f.payment.ID)
	f.payment.Date = time.Unix(1750000003, 0).UTC()
	f.payment.Status = crypto_payment.PaymentStatusConfirming
	f.payment.Confirmations, f.payment.Canonicality, f.payment.FinalityStatus = 20, "canonical", "solidified"
	f.payment.FeeFixed = types.MustCryptoAmount("3")
	// The worker assigns VerificationID in memory immediately before posting;
	// the database receives it only in the posting callback.
	require.NoError(t, document_repo.NewCryptoPaymentRepo().Create(f.ctx, f.payment))
	f.payment.VerificationID = f.request.Basis.VerificationID
	if publisher == nil {
		publisher = postgres.NewOutboxPublisher()
	}
	f.recognition = cryptoapp.NewPaymentRecognitionService(postgres.NewContextTxManager(), NewPaymentRecognitionRepo(), publisher)
	f.posting = f.engineWith(true, true, true)
	return f
}

func (f paymentRecognitionFixture) engineWith(bind, merchant, fee bool) *posting.Engine {
	return f.engineWithBalanceRepo(bind, merchant, fee, register_repo.NewCryptoBalanceRepo())
}

func (f paymentRecognitionFixture) engineWithBalanceRepo(bind, merchant, fee bool, balance crypto_balance.Repository) *posting.Engine {
	e := posting.NewEngine(postgres.NewDocLocker())
	e.AddVisitor(&posting.CryptoBalanceVisitor{})
	e.AddVisitor(&posting.CryptoMerchantBalanceVisitor{})
	e.AddVisitor(&posting.CryptoFeeVisitor{})
	e.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(balance)))
	if merchant {
		e.AddRecorder(posting.NewCryptoMerchantBalanceRecorder(crypto_merchant_balance.NewService(register_repo.NewCryptoMerchantBalanceRepo())))
	}
	if fee {
		e.AddRecorder(posting.NewCryptoFeeRecorder(crypto_fee.NewService(register_repo.NewCryptoFeeRepo())))
	}
	if bind {
		e.ValidateInTransaction(f.recognition.ValidatePosting)
		e.PrepareMovementsInTransaction(f.recognition.PrepareMovements)
	}
	if _, historical := balance.(historicalPaymentBalanceRepo); !historical {
		e.ValidateInTransaction(NewBlockIngestionRepo().ValidatePosting)
	}
	return e
}

func (f paymentRecognitionFixture) post(p *crypto_payment.CryptoPayment, e *posting.Engine) error {
	return e.Post(f.ctx, p, func(ctx context.Context) error {
		p.Status = crypto_payment.PaymentStatusConfirmed
		return document_repo.NewCryptoPaymentRepo().Update(ctx, p)
	})
}

func (f paymentRecognitionFixture) balances(t *testing.T) []string {
	t.Helper()
	var custody, merchant, fee string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		COALESCE((SELECT amount FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),0)::text,
		COALESCE((SELECT amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$3 AND token_id=$2),0)::text,
		COALESCE((SELECT SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FROM reg_crypto_fee_movements WHERE merchant_id=$3 AND token_id=$2),0)::text`,
		f.payment.WalletID, f.payment.TokenID, f.payment.MerchantID).Scan(&custody, &merchant, &fee))
	return []string{custody, merchant, fee}
}

func TestPaymentRecognitionPostsExactThreePartSetAndFreezesBasis(t *testing.T) {
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
	var claims, bindings, outbox int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_custody_effect_recognitions),
		(SELECT count(*) FROM reg_crypto_payment_custody_bindings),(SELECT count(*) FROM sys_outbox WHERE event_type='custody_recognized')`).Scan(&claims, &bindings, &outbox))
	require.Equal(t, []int{1, 1, 1}, []int{claims, bindings, outbox})
	_, err := f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET fee_fixed=4 WHERE id=$1", f.payment.ID)
	require.ErrorContains(t, err, "fee snapshot are frozen")
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET posted=FALSE WHERE id=$1", f.payment.ID)
	require.ErrorContains(t, err, "complete financial compensation")
	_, err = f.pool.Exec(f.ctx, "DELETE FROM doc_crypto_payments WHERE id=$1", f.payment.ID)
	require.ErrorContains(t, err, "history is immutable")
}

func TestPaymentRecognitionMissingComponentsRollbackAtomically(t *testing.T) {
	for _, scenario := range []string{"no_claim", "missing_fee", "missing_merchant", "outbox_error", "missing_outbox"} {
		t.Run(scenario, func(t *testing.T) {
			var publisher domain.OutboxPublisher
			if scenario == "outbox_error" {
				publisher = failingReceiptPublisher{}
			}
			if scenario == "missing_outbox" {
				publisher = missingReceiptPublisher{}
			}
			f := newPaymentRecognitionFixture(t, publisher)
			e := f.posting
			if scenario == "no_claim" {
				e = f.engineWith(false, true, true)
			}
			if scenario == "missing_fee" {
				e = f.engineWith(true, true, false)
			}
			if scenario == "missing_merchant" {
				e = f.engineWith(true, false, true)
			}
			require.Error(t, f.post(f.payment, e))
			require.Equal(t, []string{"0", "0", "0"}, f.balances(t))
			for _, table := range []string{"reg_crypto_balance_movements", "reg_crypto_merchant_balance_movements", "reg_crypto_fee_movements", "reg_crypto_custody_effect_postings", "reg_crypto_payment_custody_bindings", "sys_outbox"} {
				var count int
				require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count))
				require.Zero(t, count, table)
			}
		})
	}
}

func TestPaymentRecognitionRequiresFullPriorCompensationForNextGeneration(t *testing.T) {
	for _, scenario := range []string{"none", "custody_only", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPaymentRecognitionFixture(t, nil)
			require.NoError(t, f.post(f.payment, f.posting))
			amount := "23"
			if scenario == "partial" {
				amount = "10"
			}
			if scenario != "none" {
				// Exercise generation checks independently of manual-document admission.
				_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id,reversal_reason)
					SELECT $1,'TestPaymentCompensation',1,NOW(),'expense',wallet_id,token_id,$2,line_id,'test partial'
					FROM reg_crypto_balance_movements WHERE recorder_id=$3 AND recorder_type='CryptoPayment'`, id.New(), types.MustCryptoAmount(amount), f.payment.ID)
				require.NoError(t, err)
			}
			before := f.balances(t)
			copy := *f.payment
			copy.MarkUnposted() // Attempt to skip the engine's normal complete reversal.
			require.Error(t, f.post(&copy, f.posting))
			require.Equal(t, before, f.balances(t))
			var count int
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_payment_custody_bindings").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestPaymentRecognitionRepostPreservesOneActiveGrossNetAndFee(t *testing.T) {
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
	var generation int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT max(generation) FROM reg_crypto_custody_effect_postings").Scan(&generation))
	require.Equal(t, 2, generation)
	f.payment.FeeFixed = types.MustCryptoAmount("4")
	require.Error(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
}

func TestPaymentRecognitionReorgAndReinclusionUseSameOwner(t *testing.T) {
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	previous := *f.payment
	orphan := f.proof
	orphan.Outcome, orphan.ExecutionStatus = "absent", crypto.ReceiptPending
	orphan.Event.Canonicality, orphan.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	orphan.VerifiedAt = orphan.VerifiedAt.Add(time.Second)
	_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, orphan)
	require.NoError(t, err)
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		if err := NewFinancialReorgRepo().RecordFinancialReorg(ctx, f.payment, orphan.Event); err != nil {
			return err
		}
		f.payment.MarkUnposted()
		f.payment.Status = crypto_payment.PaymentStatusReorged
		f.payment.Canonicality = "orphaned"
		return document_repo.NewCryptoPaymentRepo().Update(ctx, f.payment)
	})
	require.NoError(t, err)
	require.Equal(t, []string{"0", "0", "0"}, f.balances(t))
	reincluded := f.proof
	reincluded.Event.BlockHash = "0x" + strings.Repeat("c", 64)
	reincluded.Event.BlockNumber, reincluded.Event.EventIndex, reincluded.Event.Confirmations = 101, 91, 19
	reincluded.VerifiedAt = reincluded.VerifiedAt.Add(2 * time.Second)
	proofID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, reincluded)
	require.NoError(t, err)
	f.payment.BlockNumber, f.payment.BlockHash, f.payment.EventIndex = 101, reincluded.Event.BlockHash, 91
	f.payment.Confirmations, f.payment.VerificationID, f.payment.Canonicality = 19, &proofID, "canonical"
	f.payment.Status = crypto_payment.PaymentStatusConfirming
	require.NoError(t, document_repo.NewCryptoPaymentRepo().Update(f.ctx, f.payment))
	require.NoError(t, f.post(f.payment, f.posting))
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
	var effects, generations int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(DISTINCT effect_id),count(*) FROM reg_crypto_custody_effect_postings").Scan(&effects, &generations))
	require.Equal(t, []int{1, 2}, []int{effects, generations})
	beforeLate := financialReorgSnapshot(t, f)
	require.Error(t, NewFinancialReorgRepo().RecordFinancialReorg(f.ctx, &previous, orphan.Event), "old posting cannot compensate a newer generation")
	require.Equal(t, beforeLate, financialReorgSnapshot(t, f))
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := NewBlockIngestionRepo().AuthorizeReorg(ctx, &previous, orphan.Event)
		return err
	})
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch, "an old generation cannot hold or compensate its canonical replacement")
	var holds int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND owner_type='payment_recognition'", f.payment.NetworkID).Scan(&holds))
	require.Zero(t, holds)
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
}

func TestPaymentRecognitionRejectsForgedReversalDimensions(t *testing.T) {
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
		SELECT $1,'CryptoAdjustment',1,NOW(),'receipt',wallet_id,token_id,10,line_id FROM reg_crypto_balance_movements WHERE recorder_id=$2 AND recorder_type='CryptoPayment'`, id.New(), f.payment.ID)
	require.ErrorContains(t, err, "exact opposite source dimensions")
	var source id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT line_id FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoPayment'", f.payment.ID).Scan(&source))
	reversal := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
		VALUES($1,$2,'TestPaymentCompensation',1,NOW(),'expense',$3,$4,10,$5)`, reversal, id.New(), f.payment.WalletID, f.payment.TokenID, source)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
		VALUES($1,'CryptoAdjustment',1,NOW(),'receipt',$2,$3,10,$4)`, id.New(), f.payment.WalletID, f.payment.TokenID, reversal)
	require.ErrorContains(t, err, "exact opposite source dimensions")
}

// The historical adapter exposes the pre-89 storage interface while seeding
// schema 79. Production binaries require their exact schema version.
type historicalPaymentBalanceRepo struct{ crypto_balance.Repository }

func legacyPaymentForAdoption(t *testing.T) paymentRecognitionFixture {
	t.Helper()
	f := newPaymentRecognitionFixture(t, nil)
	provider := paymentMigrationProvider(t, f)
	_, err := provider.DownTo(f.ctx, 79)
	require.NoError(t, err)
	require.NoError(t, f.post(f.payment, f.engineWithBalanceRepo(false, true, true,
		historicalPaymentBalanceRepo{register_repo.NewCryptoBalanceRepo()})))
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	return f
}

func paymentMigrationProvider(t *testing.T, f paymentRecognitionFixture) *goose.Provider {
	t.Helper()
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	return provider
}

func TestPaymentRecognitionAdoptionIsConcurrentAndCreatesNoMoney(t *testing.T) {
	f := legacyPaymentForAdoption(t)
	before := f.balances(t)
	cmd := cryptoapp.AdoptPaymentRecognitionCommand{PaymentID: f.payment.ID, VerificationID: *f.payment.VerificationID, Actor: "reviewer", Reference: "verified legacy source"}
	const workers = 8
	results, errs := make([]cryptoapp.PaymentRecognitionResult, workers), make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { results[i], errs[i] = f.recognition.Adopt(f.ctx, cmd) })
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
		require.Equal(t, results[0].PostingID, results[i].PostingID)
	}
	require.Equal(t, before, f.balances(t))
	var custody, merchant, fee int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements),
		(SELECT count(*) FROM reg_crypto_merchant_balance_movements),(SELECT count(*) FROM reg_crypto_fee_movements)`).Scan(&custody, &merchant, &fee))
	require.Equal(t, []int{1, 1, 1}, []int{custody, merchant, fee})
	cmd.Reference = "changed attestation"
	_, err := f.recognition.Adopt(f.ctx, cmd)
	require.ErrorContains(t, err, "attestation")
	_, err = paymentMigrationProvider(t, f).DownTo(f.ctx, 79)
	require.ErrorContains(t, err, "requires forward migration")
}

func TestPaymentRecognitionCannotDisguiseOnlineAdoptionAsPosting(t *testing.T) {
	f := legacyPaymentForAdoption(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,
		per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=100 WHERE network_id=$1`, f.payment.NetworkID)
	require.NoError(t, err)
	_, err = f.recognition.Adopt(f.ctx, cryptoapp.AdoptPaymentRecognitionCommand{PaymentID: f.payment.ID, VerificationID: *f.payment.VerificationID, Actor: "reviewer", Reference: "legacy"})
	require.ErrorContains(t, err, "disabled drained network")
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		original, err := NewPaymentRecognitionRepo().OriginalMovement(ctx, f.payment)
		if err != nil {
			return err
		}
		copy := *f.payment
		copy.PostedVersion = 0
		set := posting.NewMovementSet()
		// The source interface generates new identities; use the old one here
		// to prove that SQL rejects a forged posting over a committed movement.
		set.SetExtension("crypto_balance", []entity.CryptoBalanceMovement{original})
		return f.recognition.PrepareMovements(ctx, &copy, set)
	})
	require.Error(t, err)
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
}

func TestPaymentRecognitionLegacyMetadataUpdatesRemainUsable(t *testing.T) {
	f := legacyPaymentForAdoption(t)
	_, err := f.pool.Exec(f.ctx, "UPDATE doc_crypto_payments SET confirmations=confirmations+1 WHERE id=$1", f.payment.ID)
	require.NoError(t, err)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_payment_custody_bindings").Scan(&count))
	require.Zero(t, count)
}

func TestPaymentRecognitionReorgRequiresProofAndHoldsBeforeIngestionCutover(t *testing.T) {
	f := newPaymentRecognitionFixture(t, nil)
	require.NoError(t, f.post(f.payment, f.posting))
	orphan := f.proof
	orphan.Outcome, orphan.ExecutionStatus = "absent", crypto.ReceiptPending
	orphan.Event.Canonicality, orphan.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	orphan.VerifiedAt = orphan.VerifiedAt.Add(time.Second)
	guard := NewBlockIngestionRepo()
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := guard.AuthorizeReorg(ctx, f.payment, orphan.Event)
		require.ErrorIs(t, err, crypto.ErrVerificationPending, "an unverified reorg cannot enter automatic compensation")
		return nil
	})
	require.NoError(t, err)
	proofID, err := guard.SaveVerification(f.ctx, orphan)
	require.NoError(t, err)
	orphan.Event.VerificationID = &proofID
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := guard.AuthorizeReorg(ctx, f.payment, orphan.Event)
		require.ErrorIs(t, err, crypto.ErrVerificationPending)
		return nil // Persist incident and hold, without any partial compensation.
	})
	require.NoError(t, err)
	var holds int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND owner_type='payment_recognition' AND status='open'", f.payment.NetworkID).Scan(&holds))
	require.Equal(t, 1, holds)
	require.Equal(t, []string{"23", "20", "3"}, f.balances(t))
}

func TestPaymentRecognitionProofAndIncomingLocksStartWithAMLScope(t *testing.T) {
	for _, scenario := range []string{"proof", "incoming_event"} {
		t.Run(scenario, func(t *testing.T) {
			a := newAMLRoutingFixture(t, false, crypto.ChainFamilyEVM)
			proof := a.proof(a.input.Request.Address, "0x"+strings.Repeat("d", 64), 1)
			identity, err := crypto.NewAccountingEffectIdentity(proof.Event, crypto.ChainFamilyEVM, 0)
			require.NoError(t, err)
			proof.AccountingIdentity = &identity
			proofID, err := NewBlockIngestionRepo().SaveVerification(a.ctx, proof)
			require.NoError(t, err)
			var effect id.ID
			require.NoError(t, a.pool.QueryRow(a.ctx, `SELECT o.effect_id FROM reg_crypto_accounting_effect_evidence v
				JOIN reg_crypto_accounting_effect_occurrences o ON o.id=v.occurrence_id WHERE v.verification_id=$1`, proofID).Scan(&effect))
			ready, proceed := make(chan error, 1), make(chan struct{})
			controlDone, writerDone := make(chan error, 1), make(chan error, 1)
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			defer resume()
			go func() {
				controlDone <- postgres.MustGetTxManager(a.ctx).RunInTransaction(a.ctx, func(ctx context.Context) error {
					q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
					err := NewCustodyRecognitionRepo().LockScopes(ctx, a.input.WalletID)
					ready <- err
					if err != nil {
						return err
					}
					select {
					case <-proceed:
					case <-ctx.Done():
						return ctx.Err()
					}
					bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
					defer cancel()
					if _, err := q.Exec(bounded, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE", a.input.WalletID); err != nil {
						return err
					}
					if _, err := q.Exec(bounded, "SELECT fn_crypto_lock_receipt_balance($1,$2)", a.input.WalletID, a.input.Request.TokenID); err != nil {
						return err
					}
					_, err = q.Exec(bounded, "SELECT id FROM reg_crypto_accounting_effects WHERE id=$1 FOR UPDATE", effect)
					return err
				})
			}()
			require.NoError(t, <-ready)
			go func() {
				if scenario == "proof" {
					proof.Event.Confirmations++
					for i := range proof.Sources {
						proof.Sources[i].HeadHeight++
					}
					_, err := NewBlockIngestionRepo().SaveVerification(a.ctx, proof)
					writerDone <- err
				} else {
					writerDone <- postgres.MustGetTxManager(a.ctx).RunInTransaction(a.ctx, func(ctx context.Context) error { return NewBlockIngestionRepo().LockIncomingEvent(ctx, proof.Event) })
				}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := a.pool.QueryRow(a.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
					AND wait_event_type='Lock' AND query LIKE '%fn_crypto_lock_custody_%')`).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			resume()
			require.NoError(t, <-controlDone, "a waiting proof/incoming writer must hold neither wallet nor effect ahead of the scope")
			require.NoError(t, <-writerDone)
		})
	}
}
