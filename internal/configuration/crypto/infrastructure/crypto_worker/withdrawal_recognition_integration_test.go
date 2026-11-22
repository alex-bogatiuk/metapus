//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

type withdrawalFinalFixture struct {
	ctx       context.Context
	tm        *sdktest.TxManager
	doc       *crypto_withdrawal.CryptoWithdrawal
	request   *withdrawal_request.WithdrawalRequest
	task      outboundapp.SettlementTask
	execution outboundapp.WithdrawalTask
	receipt   crypto.ChainReceipt
}

func newWithdrawalFinalFixture(t *testing.T, ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) withdrawalFinalFixture {
	t.Helper()
	q := tm.GetQuerier(ctx)
	var networkIdentifier string
	require.NoError(t, q.QueryRow(ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, c.network).Scan(&networkIdentifier))
	merchant, address := id.New(), id.New()
	_, err := q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Withdrawal owner')`, merchant)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'fixture',0,'shadow',2)`, c.network)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100) ON CONFLICT DO NOTHING`, c.hot, c.token)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100) ON CONFLICT DO NOTHING`, c.hot, c.native)
	require.NoError(t, err)
	dest := "external-" + address.String()
	_, err = q.Exec(ctx, `INSERT INTO reg_withdrawal_addresses(id,merchant_id,network_id,address,address_key) VALUES($1,$2,$3,$4,$4)`, address, merchant, c.network, dest)
	require.NoError(t, err)
	r := withdrawal_request.NewWithdrawalRequest(merchant, c.token, address, dest, types.NewRawAmountFromInt64(23))
	r.Number = "WR2-" + id.Compact(r.ID)
	r.Posted = true
	r.PostedVersion = 1
	r.Status = withdrawal_request.StatusSigning
	_, err = q.Exec(ctx, `INSERT INTO doc_withdrawal_requests(id,number,date,merchant_id,token_id,amount,dest_address,address_id,status,posted,posted_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'signing',true,1)`, r.ID, r.Number, r.Date, merchant, c.token, r.Amount, dest, address)
	require.NoError(t, err)
	require.NoError(t, crypto_register_repo.NewCryptoMerchantBalanceRepo().CreateMovements(ctx, []crypto_merchant_balance.CryptoMerchantBalanceMovement{
		crypto_merchant_balance.NewCryptoMerchantBalanceMovement(id.New(), "Opening", 1, r.Date, entity.RecordTypeReceipt, merchant, c.token, types.NewRawAmountFromInt64(100)),
		crypto_merchant_balance.NewCryptoMerchantBalanceMovement(r.ID, "WithdrawalRequest", 1, r.Date, entity.RecordTypeExpense, merchant, c.token, r.Amount),
	}))
	repo := NewOutboundRepo()
	source, err := repo.ReserveHotWalletForWithdrawal(ctx, r.ID, merchant, c.network, c.token, r.Amount)
	require.NoError(t, err)
	d := crypto_withdrawal.NewCryptoWithdrawal(merchant, c.token, c.hot, dest, r.Amount)
	d.Number = "CW2-" + id.Compact(d.ID)
	d.BasisType = "WithdrawalRequest"
	d.BasisID = &r.ID
	d, err = repo.EnsureWithdrawalExecutionDocument(ctx, r.ID, d, crypto_document_repo.NewCryptoWithdrawalRepo())
	require.NoError(t, err)
	var standard, contract string
	require.NoError(t, q.QueryRow(ctx, "SELECT token_standard,contract_address FROM cat_tokens WHERE id=$1", c.token).Scan(&standard, &contract))
	hash := strings.ReplaceAll(d.ID.String()+d.ID.String(), "-", "")
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: "withdrawal-test", MerchantID: &merchant, Purpose: crypto.SigningPurposeWithdrawal,
		BasisType: "WithdrawalRequest", BasisID: r.ID, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: c.network, NetworkCode: "WITHDRAWAL-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier,
		SourceAddress: source.Address, FeePayerAddress: source.Address, VaultKeyID: source.VaultKeyID, DestinationAddress: dest, AssetStandard: standard, ContractAddress: contract, AmountMinorUnits: r.Amount,
		MaxNetworkFeeMinor: types.NewRawAmountFromInt64(10), UnsignedFingerprint: "withdrawal-" + d.ID.String(), PolicyVersion: "tron-v1", ApprovalBundleDigest: "test-approval"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
	_, err = repo.InsertSigningAuthorization(ctx, auth, "test-jws", unsigned, crypto.ApprovalBundle{IntentJWS: "test-intent"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkSigningAuthorizationSigned(ctx, auth.AuthorizationID, crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: hash, Payload: []byte(`{}`)}))
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status)
		VALUES($1,$2,$3,$4,'WithdrawalRequest',$5,'withdrawal',1,'native_burn',10,'reserved')`, c.network, c.hot, c.native, c.token, r.ID)
	require.NoError(t, err)
	f := withdrawalFinalFixture{ctx: ctx, tm: tm, doc: d, request: r,
		task:      outboundapp.SettlementTask{Kind: outboundapp.SettlementWithdrawal, DocumentID: d.ID, RequestID: r.ID, MerchantID: merchant, NetworkID: c.network, OperationTokenID: c.token, SourceWalletID: c.hot, TxHash: hash, Amount: r.Amount, PostedVersion: 1},
		execution: outboundapp.WithdrawalTask{RequestID: r.ID, MerchantID: merchant, TokenID: c.token, NetworkID: c.network, Amount: r.Amount, DestAddress: dest, TokenStandard: token.TokenStandard(standard), ContractAddress: contract},
		receipt: crypto.ChainReceipt{NetworkID: c.network, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHeight: 100, BlockHash: strings.Repeat("b", 64), Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(),
			Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: c.native}, TotalFee: types.NewRawAmountFromInt64(2)}}}
	t.Cleanup(func() {
		_, _ = q.Exec(context.Background(), "UPDATE doc_crypto_withdrawals SET settlement_lease_expires_at='infinity' WHERE id=$1", d.ID)
	})
	return f
}

func (f withdrawalFinalFixture) registry(t *testing.T) *crypto.ChainDriverRegistry {
	t.Helper()
	var networkIdentifier string
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, f.receipt.NetworkID).Scan(&networkIdentifier))
	registry := crypto.NewChainDriverRegistry()
	require.NoError(t, registry.Register(sweepReceiptDriver{fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.receipt.NetworkID, Code: "TEST", Family: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier, NativeTokenID: f.receipt.Fee.NativeAsset.TokenID, NativeDecimals: 6, RequiredConfirmations: 19, FinalityMode: crypto.FinalityByConfirmations}, receipt: f.receipt}}))
	return registry
}
func (f withdrawalFinalFixture) executor(t *testing.T) *outboundapp.WithdrawalExecutor {
	t.Helper()
	engine := posting.NewLockingEngine(postgres.NewDocLocker())
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoFeeVisitor{})
	return outboundapp.NewWithdrawalExecutor(outboundapp.WithdrawalConfig{Drivers: f.registry(t), Documents: crypto_document_repo.NewCryptoWithdrawalRepo(), Repository: NewOutboundRepo(), Posting: engine, Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, TenantID: "withdrawal-test"})
}
func (f withdrawalFinalFixture) broadcast(t *testing.T) {
	t.Helper()
	require.NoError(t, f.executor(t).Execute(f.ctx, f.execution))
	var money, active int
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),
		(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$2 AND status='active')`, f.doc.ID, f.request.ID).Scan(&money, &active))
	require.Zero(t, money)
	require.Equal(t, 1, active)
}
func (f withdrawalFinalFixture) service(t *testing.T) *outboundapp.SettlementService {
	t.Helper()
	p := CryptoProcessor{drivers: f.registry(t), outboundRepo: NewOutboundRepo(), feeBudget: NewFeeBudgetManager()}
	return p.settlementApplication()
}

func TestWithdrawalV2ConcurrentFinalPrincipalKeepsOriginalMerchantDebit(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
		f := newWithdrawalFinalFixture(t, ctx, tm, c)
		f.broadcast(t)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		service := f.service(t)
		for range 8 {
			wg.Go(func() { errs <- service.Settle(ctx, f.task) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var physical, merchant string
		var debit, bindings, outbox int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
			(SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$3 AND token_id=$2),
			(SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$4),(SELECT count(*) FROM reg_crypto_withdrawal_custody_bindings WHERE withdrawal_id=$5),
			(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$5 AND event_type='custody_recognized')`, c.hot, c.token, f.request.MerchantID, f.request.ID, f.doc.ID).Scan(&physical, &merchant, &debit, &bindings, &outbox))
		require.Equal(t, []string{"77", "77"}, []string{physical, merchant})
		require.Equal(t, []int{1, 1, 1}, []int{debit, bindings, outbox})
		require.NoError(t, f.executor(t).Execute(ctx, f.execution))
	})
}

func TestWithdrawalV2RevertRefundsMerchantWithoutPhysicalStorno(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
		f := newWithdrawalFinalFixture(t, ctx, tm, c)
		f.broadcast(t)
		f.receipt.Status = crypto.ReceiptReverted
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		var physical, merchant, native string
		var refund, principal int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
			(SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$3 AND token_id=$2),(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$4),
			(SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$5 AND record_type='receipt'),(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$6)`, c.hot, c.token, f.request.MerchantID, c.native, f.request.ID, f.doc.ID).Scan(&physical, &merchant, &native, &refund, &principal))
		require.Equal(t, []string{"100", "100", "98"}, []string{physical, merchant, native})
		require.Equal(t, []int{1, 0}, []int{refund, principal})
		require.NoError(t, f.executor(t).Execute(ctx, f.execution))
	})
}

type incompleteWithdrawalRepo struct {
	*OutboundRepo
	component string
}

func (r incompleteWithdrawalRepo) PrepareWithdrawalSettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	if r.component == "claim" {
		return true, nil
	}
	proceed, err := r.OutboundRepo.PrepareWithdrawalSettlement(ctx, task, receipt)
	if err == nil && r.component == "outbox" {
		_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "DELETE FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized'", task.DocumentID)
	}
	return proceed, err
}

func (r incompleteWithdrawalRepo) RecordOutboundSettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) error {
	if r.component == "writer" {
		return errors.New("injected withdrawal writer failure")
	}
	return r.OutboundRepo.RecordOutboundSettlement(ctx, task, receipt)
}

func TestWithdrawalV2IncompleteSetRollsBackAndRetries(t *testing.T) {
	for _, component := range []string{"claim", "outbox", "writer"} {
		t.Run(component, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
				f := newWithdrawalFinalFixture(t, ctx, tm, c)
				f.broadcast(t)
				service := outboundapp.NewSettlementService(outboundapp.SettlementConfig{Drivers: f.registry(t), Repository: incompleteWithdrawalRepo{OutboundRepo: NewOutboundRepo(), component: component}, Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, WithdrawalEvidence: outboundapp.NewWithdrawalEvidenceService(crypto_repo.NewWithdrawalCustodyRepo())})
				require.Error(t, service.Settle(ctx, f.task))
				var money, bindings, active int
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id IN($1,$2)),
					(SELECT count(*) FROM reg_crypto_withdrawal_custody_bindings WHERE withdrawal_id=$1),(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$2 AND status='active')`, f.doc.ID, f.request.ID).Scan(&money, &bindings, &active))
				require.Equal(t, []int{0, 0, 1}, []int{money, bindings, active})
				require.NoError(t, f.service(t).Settle(ctx, f.task))
			})
		})
	}
}

func TestWithdrawalV2NativePrincipalCannotBeReleasedOrSpentOnGas(t *testing.T) {
	for _, actual := range []int64{0, 2} {
		runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
			c.token = c.native
			f := newWithdrawalFinalFixture(t, ctx, tm, c)
			f.receipt.Fee.TotalFee = types.NewRawAmountFromInt64(actual)
			f.broadcast(t)
			fee, found, err := NewFeeBudgetManager().Get(ctx, "WithdrawalRequest", f.request.ID, crypto.SigningPurposeWithdrawal, 1)
			require.NoError(t, err)
			require.True(t, found)
			require.Error(t, NewFeeBudgetManager().Settle(ctx, fee.ID, f.receipt))
			q := tm.GetQuerier(ctx)
			_, err = q.Exec(ctx, "UPDATE reg_crypto_balance_reservations SET status='released' WHERE document_id=$1", f.request.ID)
			require.Error(t, err)
			_, err = q.Exec(ctx, "UPDATE reg_crypto_fee_reservations SET status='released' WHERE id=$1", fee.ID)
			require.Error(t, err)
			_, err = q.Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=true,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=1000,
				fee_funding_mode='source_only',min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0 WHERE network_id=$1`, c.network)
			require.NoError(t, err)
			_, err = NewFeeBudgetManager().Reserve(ctx, FeeReservationRequest{NetworkID: c.network, PayerWalletID: c.hot, NativeTokenID: c.native, OperationTokenID: c.native,
				BasisType: "CryptoNetworkOperation", BasisID: id.New(), Purpose: crypto.SigningPurposeActivation, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewRawAmountFromInt64(80)})
			require.ErrorIs(t, err, crypto.ErrFeeBalanceResidual)
			require.NoError(t, f.service(t).Settle(ctx, f.task))
			var balance string
			require.NoError(t, q.QueryRow(ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", c.hot, c.native).Scan(&balance))
			require.Equal(t, types.NewRawAmountFromInt64(77-actual).String(), balance)
		})
	}
}

func TestWithdrawalV2MissingOrContradictoryProofRetainsReservations(t *testing.T) {
	for _, mode := range []string{"no_quorum", "wrong_amount"} {
		t.Run(mode, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
				f := newWithdrawalFinalFixture(t, ctx, tm, c)
				f.broadcast(t)
				base, err := f.registry(t).Resolve(c.network)
				require.NoError(t, err)
				registry := crypto.NewChainDriverRegistry()
				require.NoError(t, registry.Register(treasuryBadProofDriver{NetworkDriver: base, mode: mode}))
				service := outboundapp.NewSettlementService(outboundapp.SettlementConfig{Drivers: registry, Repository: NewOutboundRepo(), Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, WithdrawalEvidence: outboundapp.NewWithdrawalEvidenceService(crypto_repo.NewWithdrawalCustodyRepo())})
				require.Error(t, service.Settle(ctx, f.task))
				var active int
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND status='active'", f.request.ID).Scan(&active))
				require.Equal(t, 1, active)
				require.NoError(t, f.service(t).Settle(ctx, f.task))
			})
		})
	}
}

func TestWithdrawalV2FinancialBasisAndFinalHistoryAreImmutable(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
		f := newWithdrawalFinalFixture(t, ctx, tm, c)
		q := tm.GetQuerier(ctx)
		for _, query := range []string{"UPDATE doc_withdrawal_requests SET amount=24 WHERE id=$1", "UPDATE reg_crypto_balance_reservations SET amount=24 WHERE document_id=$1"} {
			_, err := q.Exec(ctx, query, f.request.ID)
			require.Error(t, err, query)
		}
		f.broadcast(t)
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		for _, query := range []string{
			"UPDATE doc_crypto_withdrawals SET posted=false WHERE id=$1",
			"UPDATE doc_crypto_withdrawals SET status='broadcast' WHERE id=$1",
			`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
			 SELECT gen_random_uuid_v7(),recorder_id,recorder_type,2,now(),'receipt',wallet_id,token_id,amount,line_id FROM reg_crypto_balance_movements WHERE recorder_id=$1`,
		} {
			_, err := q.Exec(ctx, query, f.doc.ID)
			require.Error(t, err, query)
		}
		_, err := q.Exec(ctx, "UPDATE doc_withdrawal_requests SET status='broadcast' WHERE id=$1", f.request.ID)
		require.Error(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,'AMLWithdrawalRevert',1,now(),'receipt',$2,$3,23)`, f.request.ID, c.hot, c.token)
		require.Error(t, err)
	})
}

func TestWithdrawalV2ExpectedPrincipalCannotBecomeAnotherCustodyExpense(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
		f := newWithdrawalFinalFixture(t, ctx, tm, c)
		f.broadcast(t)
		outflow, adjustment, legacy := id.New(), id.New(), id.New()
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount)
			SELECT $1,x.network_id,x.token_id,$2,x.tx_hash,x.event_kind,0,x.source_address_key,x.destination_address_key,x.source_address_key,x.destination_address_key,x.contract_address_key,x.contract_address_key,x.amount
			FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id WHERE a.basis_type='WithdrawalRequest' AND a.basis_id=$3`, outflow, c.hot, f.request.ID)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO doc_crypto_custody_adjustments(id,external_outflow_id,network_id,wallet_id,token_id,amount,maker_id) VALUES($1,$2,$3,$4,$5,23,$6)`, adjustment, outflow, c.network, c.hot, c.token, id.New())
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,'CryptoCustodyAdjustment',1,now(),'expense',$2,$3,23)`, adjustment, c.hot, c.token)
		require.ErrorContains(t, err, "expected withdrawal principal requires its settlement owner")
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		_, err = q.Exec(ctx, `INSERT INTO doc_crypto_withdrawals(id,merchant_id,token_id,source_wallet_id,dest_address,amount,tx_hash,status)
			VALUES($1,$2,$3,$4,$5,23,$6,'broadcast')`, legacy, f.request.MerchantID, c.token, c.hot, f.request.DestAddress, f.task.TxHash)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,'CryptoWithdrawal',1,now(),'expense',$2,$3,23)`, legacy, c.hot, c.token)
		require.ErrorContains(t, err, "expected withdrawal principal requires its settlement owner")
	})
}

func TestWithdrawalV2FailedAuthorizationLabelCannotReleaseSignedUnknownOutcome(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *sdktest.TxManager, c sweepCoordinationFixture) {
		f := newWithdrawalFinalFixture(t, ctx, tm, c)
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed' WHERE basis_type='WithdrawalRequest' AND basis_id=$1`, f.request.ID)
		require.NoError(t, err)
		for _, query := range []string{
			"UPDATE reg_crypto_balance_reservations SET status='released' WHERE document_id=$1",
			"UPDATE reg_crypto_fee_reservations SET status='released' WHERE basis_type='WithdrawalRequest' AND basis_id=$1",
		} {
			_, err = q.Exec(ctx, query, f.request.ID)
			require.Error(t, err, query)
		}
	})
}
