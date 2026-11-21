//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/storage/postgres"
)

type withdrawalRecoveryFixture struct {
	task       WithdrawalTask
	documentID id.ID
	auth       crypto.SigningAuthorization
	unsigned   crypto.UnsignedTxEnvelope
	signed     crypto.SignedTxEnvelope
	feeID      id.ID
}

func seedWithdrawalRecovery(t *testing.T, ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) withdrawalRecoveryFixture {
	t.Helper()
	wr := withdrawalRecoveryFixture{documentID: id.New(), feeID: id.New()}
	wr.task = WithdrawalTask{RequestID: id.New(), MerchantID: id.New(), TokenID: f.token, NetworkID: f.network,
		Amount: types.NewRawAmountFromInt64(100), DestAddress: "external", ContractAddress: "COORD-" + id.Compact(f.network) + "-contract", TokenStandard: token.TokenStandardTRC20, PostedVersion: 1}
	suffix := id.Compact(wr.task.RequestID)
	q := txm.GetQuerier(ctx)
	_, err := q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Recovery merchant')`, wr.task.MerchantID, suffix)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	merchantRepo := crypto_register_repo.NewCryptoMerchantBalanceRepo()
	require.NoError(t, merchantRepo.CreateMovements(ctx, []crypto_merchant_balance.CryptoMerchantBalanceMovement{
		crypto_merchant_balance.NewCryptoMerchantBalanceMovement(id.New(), "Opening", 1, time.Now(), entity.RecordTypeReceipt, wr.task.MerchantID, f.token, types.NewRawAmountFromInt64(1000)),
		crypto_merchant_balance.NewCryptoMerchantBalanceMovement(wr.task.RequestID, withdrawal_request.DocumentType, 1, now, entity.RecordTypeExpense, wr.task.MerchantID, f.token, types.NewRawAmountFromInt64(100)),
	}))
	_, err = q.Exec(ctx, `
		INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($6,$5,1000) ON CONFLICT DO NOTHING;
		INSERT INTO doc_crypto_withdrawals(id,number,merchant_id,token_id,source_wallet_id,dest_address,amount,status,basis_type,basis_id)
		VALUES($2,$4,$1,$5,$6,'external',100,'created','WithdrawalRequest',$3);
		INSERT INTO doc_withdrawal_requests(id,number,date,merchant_id,token_id,amount,dest_address,status,posted,posted_version,withdrawal_id,updated_at)
		VALUES($3,$4,$8,$1,$5,100,'external','signing',TRUE,1,$2,NOW()-interval '3 minutes');
		INSERT INTO reg_crypto_balance_reservations(id,wallet_id,token_id,amount,purpose,document_type,document_id,status)
		VALUES($7,$6,$5,100,'withdrawal','WithdrawalRequest',$3,'active');
	`, pgx.QueryExecModeSimpleProtocol, wr.task.MerchantID, wr.documentID, wr.task.RequestID, suffix, f.token, f.hot, id.New(), now)
	require.NoError(t, err)
	wr.auth = crypto.SigningAuthorization{
		SchemaVersion: crypto.SigningAuthorizationSchemaVersionFor(crypto.SequenceNone), AuthorizationID: "recovery-" + suffix,
		TenantID: id.New().String(), MerchantID: &wr.task.MerchantID, Purpose: crypto.SigningPurposeWithdrawal,
		BasisType: withdrawal_request.DocumentType, BasisID: wr.task.RequestID, Attempt: 1, Generation: 1,
		ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.network, NetworkCode: "TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile",
		SourceAddress: f.hotAddress, FeePayerAddress: f.hotAddress, VaultKeyID: "test/hot",
		DestinationAddress: "external", AssetStandard: "TRC-20", ContractAddress: wr.task.ContractAddress,
		AmountMinorUnits: wr.task.Amount, MaxNetworkFeeMinor: types.NewRawAmountFromInt64(10), UnsignedFingerprint: "fp-" + suffix,
		ApprovalBundleDigest: "expired-bundle", PolicyVersion: crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyTRON),
	}
	wr.unsigned = crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: wr.auth.UnsignedFingerprint, Payload: []byte(`{"raw":"tx"}`)}
	wr.signed = crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: wr.auth.UnsignedFingerprint, TxHash: "tx-" + suffix, Payload: []byte(`{"signed":"tx"}`)}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status,unsigned_fingerprint)
		VALUES($1,$2,$3,$4,$5,'WithdrawalRequest',$6,'withdrawal',1,'native_burn',10,'held',$7)`, wr.feeID, f.network, f.hot, f.native, f.token, wr.task.RequestID, wr.auth.UnsignedFingerprint)
	require.NoError(t, err)
	return wr
}

func recoveryExecutor(wr withdrawalRecoveryFixture, f sweepCoordinationFixture, receipt crypto.ChainReceipt) *WithdrawalExecutor {
	registry := crypto.NewChainDriverRegistry()
	_ = registry.Register(fundingReceiptDriver{network: crypto.NetworkRef{ID: f.network, Code: "TEST", Family: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", NativeTokenID: f.native, NativeDecimals: 6, RequiredConfirmations: 12, FinalityMode: crypto.FinalityByConfirmations}, receipt: receipt})
	engine := posting.NewEngine(postgres.NewDocLocker(), cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	return &WithdrawalExecutor{outboundRepo: NewOutboundRepo(), tenantID: wr.auth.TenantID, drivers: registry,
		withdrawalRepo: crypto_document_repo.NewCryptoWithdrawalRepo(), postingEngine: engine, feeBudget: NewFeeBudgetManager(),
		merchantBalanceSvc: crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo()), now: func() time.Time { return wr.auth.ExpiresAt.Add(24 * time.Hour) }}
}

func assertMerchantBalance(t *testing.T, ctx context.Context, txm *postgres.TxManager, wr withdrawalRecoveryFixture, want string) {
	t.Helper()
	var balance string
	require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, wr.task.MerchantID, wr.task.TokenID).Scan(&balance))
	require.Equal(t, want, balance)
}

func TestWithdrawalRecoveryAfterExpiryNeverReleasesBalance(t *testing.T) {
	for _, phase := range []string{"authorization-issued", "signed", "broadcast-accepted-before-commit"} {
		t.Run(phase, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
				wr := seedWithdrawalRecovery(t, ctx, txm, f)
				repo := NewOutboundRepo()
				_, err := repo.InsertSigningAuthorization(ctx, wr.auth, "signed-authorization", wr.unsigned, crypto.ApprovalBundle{IntentJWS: "expired"})
				require.NoError(t, err)
				if phase != "authorization-issued" {
					require.NoError(t, repo.MarkSigningAuthorizationSigned(ctx, wr.auth.AuthorizationID, wr.signed))
				}
				e := recoveryExecutor(wr, f, crypto.ChainReceipt{NetworkID: f.network, TxHash: wr.signed.TxHash, Status: crypto.ReceiptPending})
				require.Error(t, e.processOne(ctx, wr.task))
				err = e.failRequest(ctx, wr.task, errors.New("expired"))
				require.ErrorIs(t, err, cryptoapp.ErrWithdrawalMayHaveExternalEffect)
				assertMerchantBalance(t, ctx, txm, wr, "900")
				var status string
				require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM reg_crypto_balance_reservations WHERE document_id=$1`, wr.task.RequestID).Scan(&status))
				require.Equal(t, "active", status)
			})
		})
	}
}

func TestWithdrawalRecoveryFinalityCompletesOnceWithTwoWorkers(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		wr := seedWithdrawalRecovery(t, ctx, txm, f)
		repo := NewOutboundRepo()
		_, err := repo.InsertSigningAuthorization(ctx, wr.auth, "signed-authorization", wr.unsigned, crypto.ApprovalBundle{IntentJWS: "expired"})
		require.NoError(t, err)
		require.NoError(t, repo.MarkSigningAuthorizationSigned(ctx, wr.auth.AuthorizationID, wr.signed))
		receipt := crypto.ChainReceipt{NetworkID: f.network, TxHash: wr.signed.TxHash, Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized}
		e := recoveryExecutor(wr, f, receipt)
		// Crash/failure after broadcast, while the local posting transaction is
		// writing the document: ledger writes and fee transitions must roll back.
		realRepo := e.withdrawalRepo
		e.withdrawalRepo = failingWithdrawalUpdate{realRepo}
		require.Error(t, e.processOne(ctx, wr.task))
		assertMerchantBalance(t, ctx, txm, wr, "900")
		var beforeMovements int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, wr.documentID).Scan(&beforeMovements))
		require.Zero(t, beforeMovements)
		e.withdrawalRepo = realRepo
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		start := make(chan struct{})
		for range 2 {
			wg.Go(func() { <-start; errs <- e.processOne(ctx, wr.task) })
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		assertMerchantBalance(t, ctx, txm, wr, "900")
		var movements, version int
		var status, hash string
		q := txm.GetQuerier(ctx)
		require.NoError(t, q.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, wr.documentID).Scan(&movements))
		require.Zero(t, movements, "principal is recognized only at final settlement")
		require.NoError(t, q.QueryRow(ctx, `SELECT posted_version,tx_hash FROM doc_crypto_withdrawals WHERE id=$1`, wr.documentID).Scan(&version, &hash))
		require.Equal(t, 1, version)
		require.Equal(t, wr.signed.TxHash, hash)
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM reg_crypto_balance_reservations WHERE document_id=$1`, wr.task.RequestID).Scan(&status))
		require.Equal(t, "active", status, "the principal reservation is consumed only at final settlement")
	})
}

type failingWithdrawalUpdate struct{ crypto_withdrawal.Repository }

func (failingWithdrawalUpdate) Update(context.Context, *crypto_withdrawal.CryptoWithdrawal) error {
	return errors.New("injected document write failure")
}

type failingCompensationWrite struct{ *OutboundRepo }

func (failingCompensationWrite) MarkWithdrawalExecutionFailed(context.Context, id.ID, string) error {
	return errors.New("injected final compensation write failure")
}

func TestWithdrawalCompensationFailureRollsBackBalanceAndReservation(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		wr := seedWithdrawalRecovery(t, ctx, txm, f)
		s := cryptoapp.NewWithdrawalFailureService(contextTxManager{}, failingCompensationWrite{NewOutboundRepo()}, crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo()))
		changed, err := s.FailWithdrawalBeforeEffect(ctx, wr.task.RequestID, "approval expired")
		require.Error(t, err)
		require.False(t, changed)
		assertMerchantBalance(t, ctx, txm, wr, "900")
		var requestStatus, reservationStatus string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT wr.status,r.status FROM doc_withdrawal_requests wr JOIN reg_crypto_balance_reservations r ON r.document_id=wr.id WHERE wr.id=$1`, wr.task.RequestID).Scan(&requestStatus, &reservationStatus))
		require.Equal(t, "signing", requestStatus)
		require.Equal(t, "active", reservationStatus)
	})
}

func TestWithdrawalCompensationRacesAuthorizationPublication(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		wr := seedWithdrawalRecovery(t, ctx, txm, f)
		repo := NewOutboundRepo()
		s := cryptoapp.NewWithdrawalFailureService(contextTxManager{}, repo, crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo()))
		start := make(chan struct{})
		signed := make(chan error, 1)
		failed := make(chan error, 1)
		go func() {
			<-start
			_, err := repo.InsertSigningAuthorization(ctx, wr.auth, "authorization", wr.unsigned, crypto.ApprovalBundle{IntentJWS: "intent"})
			signed <- err
		}()
		go func() {
			<-start
			_, err := s.FailWithdrawalBeforeEffect(ctx, wr.task.RequestID, "expiry")
			failed <- err
		}()
		close(start)
		signErr, failErr := <-signed, <-failed
		require.NotEqual(t, signErr == nil, failErr == nil, "exactly one publication or compensation may commit")
		if failErr == nil {
			assertMerchantBalance(t, ctx, txm, wr, "1000")
			changed, err := s.FailWithdrawalBeforeEffect(ctx, wr.task.RequestID, "expiry")
			require.NoError(t, err)
			require.False(t, changed)
		} else {
			require.ErrorIs(t, failErr, cryptoapp.ErrWithdrawalMayHaveExternalEffect)
			assertMerchantBalance(t, ctx, txm, wr, "900")
		}
	})
}

var _ crypto_withdrawal.Repository = (*crypto_document_repo.CryptoWithdrawalRepo)(nil)
