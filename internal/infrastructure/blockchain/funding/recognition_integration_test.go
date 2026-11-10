//go:build integration

package funding

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/register_repo"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type fundingFixture struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	target Target
	proof  crypto.TransferVerification
}

func newFundingFixture(t *testing.T, native bool) fundingFixture {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_ADMIN_DSN is required for isolated funding integration tests")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	name := "funding_test_" + id.Compact(id.New())
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	address, err := url.Parse(dsn)
	require.NoError(t, err)
	address.Path = "/" + name
	_, err = migration.RunAll(address.String())
	require.NoError(t, err)
	pool, err := postgres.OpenPool(t.Context(), address.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f := fundingFixture{pool: pool}
	f.ctx = tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	f.target = Target{NetworkID: id.New(), WalletID: id.New(), TokenID: id.New(), TokenCode: "FUND", TokenStandard: token.TokenStandardERC20,
		Address: "0x00000000000000000000000000000000000000CD", ContractAddress: "0x00000000000000000000000000000000000000EF"}
	kind, ordinal := crypto.EventKindContractLog, int64(2)
	if native {
		f.target.TokenStandard, f.target.ContractAddress = token.TokenStandardNative, ""
		kind, ordinal = crypto.EventKindNativeValue, 0
	}
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed)
		VALUES($1,$2,'Funding fixture','evm',$2,'ETH',18,19)`, f.target.NetworkID, "F-"+id.Compact(f.target.NetworkID))
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,contract_address,decimal_places,token_standard)
		VALUES($1,'FUND','Funding asset',$2,'TEST',$3,18,$4)`, f.target.TokenID, f.target.NetworkID, f.target.ContractAddress, f.target.TokenStandard)
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id)
		VALUES($1,'FUND','Funding wallet',$2,$3,'funding-key')`, f.target.WalletID, f.target.NetworkID, strings.ToLower(f.target.Address))
	require.NoError(t, err)
	e := crypto.BlockchainEvent{NetworkID: f.target.NetworkID, TxHash: "0x" + strings.Repeat("a", 64), EventKind: kind, EventIndex: 7,
		FromAddress: "0x00000000000000000000000000000000000000AB", ToAddress: f.target.Address, TokenContract: f.target.ContractAddress,
		Amount: types.MustCryptoAmount("23"), BlockNumber: 100, BlockHash: "0x" + strings.Repeat("b", 64), Confirmations: 20,
		Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, DecoderVersion: 1, Timestamp: time.Unix(1750000003, 0).UTC()}
	if native {
		e.EventIndex = 0
	}
	identity, err := crypto.NewAccountingEffectIdentity(e, crypto.ChainFamilyEVM, ordinal)
	require.NoError(t, err)
	f.proof = crypto.TransferVerification{Event: e, AccountingIdentity: &identity, DecoderVersion: 1, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded,
		Sources: []crypto.VerificationSource{{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "receipt", HeadHash: "head", HeadHeight: 119},
			{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "receipt", HeadHash: "head", HeadHeight: 119}}}
	if native {
		f.proof.MatchingEffects = 1
	}
	proofID, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, f.proof)
	require.NoError(t, err)
	f.proof.ID = proofID
	return f
}

func (f fundingFixture) transfer() ConfirmedTransfer {
	e := f.proof.Event
	return ConfirmedTransfer{VerificationID: &f.proof.ID, EventKind: e.EventKind, EventIndex: e.EventIndex, BlockHash: e.BlockHash,
		FromAddress: e.FromAddress, ToAddress: e.ToAddress, Amount: e.Amount, BlockNumber: e.BlockNumber, BlockTimestamp: e.Timestamp, Confirmations: e.Confirmations}
}

func (f fundingFixture) record() (Result, error) {
	return Record(f.ctx, "FUND", f.target, f.proof.Event.TxHash, f.transfer())
}

func (f fundingFixture) receipt() error {
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&posting.CryptoBalanceVisitor{})
	engine.AddVisitor(&posting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(posting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(register_repo.NewCryptoReceiptSuspenseRepo())))
	svc := cryptoapp.NewReceiptIntakeService(postgres.MustGetTxManager(f.ctx), crypto_repo.NewReceiptIntakeRepo(), crypto_repo.NewBlockIngestionRepo(), engine, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
	e := f.proof.Event
	e.VerificationID = &f.proof.ID
	_, err := svc.Record(f.ctx, cryptoapp.RecordCustodyReceiptCommand{Reason: crypto_custody_receipt.ReasonNoAllocation, Event: e,
		Basis: crypto.AccountingTransferBasis{NetworkID: f.target.NetworkID, WalletID: f.target.WalletID, TokenID: f.target.TokenID,
			TxHash: e.TxHash, EventKind: e.EventKind, EventIndex: e.EventIndex, FromAddress: e.FromAddress, Amount: e.Amount, BlockNumber: e.BlockNumber,
			BlockHash: e.BlockHash, RequiredConfs: 19, VerificationID: &f.proof.ID}})
	return err
}

func (f fundingFixture) insertPayment(ctx context.Context) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO doc_crypto_payments(
		id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,event_index,amount,status,verification_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,23,'confirmed',$10)`, id.New(), id.New(), id.New(), f.target.NetworkID, f.target.TokenID, f.target.WalletID,
		f.proof.Event.TxHash, f.proof.Event.EventKind, f.proof.Event.EventIndex, f.proof.ID)
	return err
}

func TestFundingRecognitionConcurrentReplayAndSeparateEffects(t *testing.T) {
	f := newFundingFixture(t, false)
	const workers = 8
	results, errs := make([]Result, workers), make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() { results[i], errs[i] = f.record() })
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
		require.Equal(t, results[0].ID, results[i].ID)
	}
	var receipts, movements, claims, audits, outbox int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM reg_crypto_wallet_fundings),(SELECT count(*) FROM reg_crypto_balance_movements),
		(SELECT count(*) FROM reg_crypto_custody_effect_recognitions),(SELECT count(*) FROM reg_crypto_wallet_funding_audit),
		(SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoWalletFunding')`).Scan(&receipts, &movements, &claims, &audits, &outbox))
	require.Equal(t, []int{1, 1, 1, 1, 1}, []int{receipts, movements, claims, audits, outbox})
	changed := f.transfer()
	changed.BlockTimestamp = changed.BlockTimestamp.Add(time.Second)
	_, err := Record(f.ctx, "FUND", f.target, f.proof.Event.TxHash, changed)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch, "a replay cannot assert a different receipt timestamp")
	second := f
	second.proof.Event.EventIndex = 8
	identity, err := crypto.NewAccountingEffectIdentity(second.proof.Event, crypto.ChainFamilyEVM, 3)
	require.NoError(t, err)
	second.proof.AccountingIdentity = &identity
	second.proof.ID = id.Nil()
	second.proof.ID, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, second.proof)
	require.NoError(t, err)
	next, err := second.record()
	require.NoError(t, err)
	require.NotEqual(t, results[0].ID, next.ID)
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.target.WalletID, f.target.TokenID).Scan(&balance))
	require.Equal(t, "46", balance)
}

type fundingTestPublisher struct{ fail bool }

func (p fundingTestPublisher) Publish(context.Context, domain.DomainEvent) error {
	if p.fail {
		return errors.New("injected funding outbox failure")
	}
	return nil
}

func TestFundingAtomicRollbackAndMandatoryOutbox(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "publisher_error", false: "missing_outbox"}[fail], func(t *testing.T) {
			f := newFundingFixture(t, true)
			_, err := record(f.ctx, "FUND", f.target, f.proof.Event.TxHash, f.transfer(), fundingTestPublisher{fail: fail})
			require.Error(t, err)
			for _, table := range []string{"reg_crypto_wallet_fundings", "reg_crypto_balance_movements", "reg_crypto_custody_effect_postings", "reg_crypto_custody_effect_recognitions", "reg_crypto_wallet_funding_audit", "sys_outbox"} {
				var count int
				require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count))
				require.Zero(t, count, table)
			}
			var status string
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT chain_activation_status FROM cat_wallets WHERE id=$1", f.target.WalletID).Scan(&status))
			require.NotEqual(t, "active", status, "activation state rolls back with the financial set")
		})
	}
}

func TestFundingAndReceiptHaveOnePrincipalOwner(t *testing.T) {
	for _, order := range []string{"funding_first", "receipt_first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			f := newFundingFixture(t, false)
			_, err := f.pool.Exec(f.ctx, "UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1", f.target.NetworkID)
			require.NoError(t, err)
			var fundingErr, receiptErr error
			switch order {
			case "funding_first":
				_, fundingErr = f.record()
				receiptErr = f.receipt()
			case "receipt_first":
				receiptErr = f.receipt()
				_, fundingErr = f.record()
			case "concurrent":
				var wg sync.WaitGroup
				wg.Go(func() { _, fundingErr = f.record() })
				wg.Go(func() { receiptErr = f.receipt() })
				wg.Wait()
			}
			require.NotEqual(t, fundingErr == nil, receiptErr == nil, "exactly one workflow owns the principal")
			var balance string
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", f.target.WalletID, f.target.TokenID).Scan(&balance))
			require.Equal(t, "23", balance)
		})
	}
}

func TestFundingRejectsManagedSenderAndMerchantOwner(t *testing.T) {
	for _, scenario := range []string{"managed_sender", "merchant_owner"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFundingFixture(t, true)
			var err error
			if scenario == "managed_sender" {
				_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,'SENDER','Managed sender',$2,$3,'sender-key')`, id.New(), f.target.NetworkID, f.proof.Event.FromAddress)
			} else {
				merchant := id.New()
				_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,'OWNER','Owner')`, merchant)
				require.NoError(t, err)
				_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET custody_merchant_id=$2 WHERE id=$1`, f.target.WalletID, merchant)
			}
			require.NoError(t, err)
			_, err = f.record()
			require.Error(t, err)
			var count int
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestFundingAndLegacyPaymentCannotRecognizeSamePrincipal(t *testing.T) {
	for _, order := range []string{"payment_first", "funding_first"} {
		t.Run(order, func(t *testing.T) {
			f := newFundingFixture(t, false)
			if order == "payment_first" {
				require.NoError(t, f.insertPayment(f.ctx))
				_, err := f.record()
				require.ErrorContains(t, err, "another accounting owner")
			} else {
				_, err := f.record()
				require.NoError(t, err)
				require.ErrorContains(t, f.insertPayment(f.ctx), "already belongs to wallet funding")
			}
		})
	}
}

func TestFundingReinclusionAndSupersededProofDoNotCreditAgain(t *testing.T) {
	f := newFundingFixture(t, true)
	_, err := f.record()
	require.NoError(t, err)
	reincluded := f
	reincluded.proof.ID = id.Nil()
	reincluded.proof.Event.BlockNumber = 101
	reincluded.proof.Event.Confirmations = 19
	reincluded.proof.Event.BlockHash = "0x" + strings.Repeat("c", 64)
	reincluded.proof.VerifiedAt = reincluded.proof.VerifiedAt.Add(time.Second)
	reincluded.proof.ID, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, reincluded.proof)
	require.NoError(t, err)
	_, err = reincluded.record()
	require.Error(t, err)
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", f.target.WalletID, f.target.TokenID).Scan(&balance))
	require.Equal(t, "23", balance)
	// A different effect has not yet been posted. A newer orphan observation
	// must prevent admitting its historical positive proof.
	f.proof.Event.TxHash = "0x" + strings.Repeat("e", 64)
	identity, err := crypto.NewAccountingEffectIdentity(f.proof.Event, crypto.ChainFamilyEVM, 0)
	require.NoError(t, err)
	f.proof.AccountingIdentity, f.proof.ID = &identity, id.Nil()
	f.proof.ID, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, f.proof)
	require.NoError(t, err)
	orphan := f.proof
	orphan.ID = id.Nil()
	orphan.Outcome, orphan.ExecutionStatus = "absent", crypto.ReceiptPending
	orphan.Event.Canonicality, orphan.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
	orphan.VerifiedAt = orphan.VerifiedAt.Add(2 * time.Second)
	_, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, orphan)
	require.NoError(t, err)
	_, err = f.record()
	require.ErrorContains(t, err, "exact physical movements")
}

func TestFundingSerializesWithPaymentAlreadyWaiting(t *testing.T) {
	f := newFundingFixture(t, false)
	ready, release := make(chan error, 1), make(chan struct{})
	fundingDone, paymentDone := make(chan error, 1), make(chan error, 1)
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	go func() {
		fundingDone <- postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
			_, err := Record(ctx, "FUND", f.target, f.proof.Event.TxHash, f.transfer())
			ready <- err
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
	require.NoError(t, <-ready)
	go func() { paymentDone <- f.insertPayment(f.ctx) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
			AND wait_event_type='Lock' AND query LIKE '%INSERT INTO doc_crypto_payments%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	resume()
	require.NoError(t, <-fundingDone)
	require.ErrorContains(t, <-paymentDone, "already belongs to wallet funding")
}

func TestFundingDoesNotBlockAnotherIndexedEffectOfSameTransaction(t *testing.T) {
	f := newFundingFixture(t, false)
	_, err := f.record()
	require.NoError(t, err)
	f.proof.ID, f.proof.Event.EventIndex = id.Nil(), 8
	identity, err := crypto.NewAccountingEffectIdentity(f.proof.Event, crypto.ChainFamilyEVM, 3)
	require.NoError(t, err)
	f.proof.AccountingIdentity = &identity
	f.proof.ID, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, f.proof)
	require.NoError(t, err)
	require.NoError(t, f.insertPayment(f.ctx), "a distinct proven log is not the funding principal")
}

func TestFundingRejectsOrphanMovementsAndAttributedFinancialWrites(t *testing.T) {
	f := newFundingFixture(t, true)
	result, err := f.record()
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(
		line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,$1,'CryptoWalletFunding',1,NOW(),'receipt',$2,$3,23)`, id.New(), f.target.WalletID, f.target.TokenID)
	require.ErrorContains(t, err, "requires its recognized receipt")
	merchant := id.New()
	_, err = f.pool.Exec(f.ctx, "INSERT INTO cat_merchants(id,code,name) VALUES($1,'UNRELATED','Unrelated merchant')", merchant)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_merchant_balance_movements(
		recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
		VALUES($1,'CryptoWalletFunding',1,NOW(),'receipt',$2,$3,23)`, result.ID, merchant, f.target.TokenID)
	require.ErrorContains(t, err, "cannot create attributed financial movements")
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", f.target.WalletID, f.target.TokenID).Scan(&balance))
	require.Equal(t, "23", balance, "rejected direct writes must not change the ledger")
}

func TestFundingRequiresAdoptionOfManualCorrectionAndProjectionMismatch(t *testing.T) {
	for _, scenario := range []string{"manual_correction", "projection_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFundingFixture(t, true)
			var err error
			if scenario == "manual_correction" {
				service := cryptoapp.NewFinancialAdjustmentService(postgres.NewContextTxManager(), crypto_repo.NewFinancialAdjustmentRepo())
				_, err = service.Propose(f.ctx, cryptoapp.ProposeFinancialAdjustmentCommand{Mode: "wallet_balance", WalletID: f.target.WalletID, TokenID: f.target.TokenID,
					Direction: "receipt", Amount: "23", Direct: true, ReasonCode: "wallet_other", MakerID: id.New(), IdempotencyKey: id.New().String(),
					Evidence: json.RawMessage(`{"description":"manual historical wallet correction"}`)})
			} else {
				_, err = f.pool.Exec(f.ctx, "INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,23) ON CONFLICT(wallet_id,token_id) DO UPDATE SET amount=EXCLUDED.amount", f.target.WalletID, f.target.TokenID)
			}
			require.NoError(t, err)
			_, err = f.record()
			require.Error(t, err)
			var balance string
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", f.target.WalletID, f.target.TokenID).Scan(&balance))
			require.Equal(t, "23", balance, "neither case permits another physical receipt")
			var claims int
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_custody_effect_postings").Scan(&claims))
			require.Zero(t, claims)
		})
	}
}

func TestFundingSQLRejectsUnknownEVMNativeCoverage(t *testing.T) {
	f := newFundingFixture(t, true)
	f.proof.ID, f.proof.MatchingEffects = id.Nil(), 0
	for i := range f.proof.Sources {
		f.proof.Sources[i].Digest = "unknown-native-coverage"
	}
	var err error
	f.proof.ID, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(f.ctx, f.proof)
	require.NoError(t, err)
	_, err = f.record()
	require.ErrorContains(t, err, "exact platform-owned receipt evidence")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements").Scan(&count))
	require.Zero(t, count)
}
