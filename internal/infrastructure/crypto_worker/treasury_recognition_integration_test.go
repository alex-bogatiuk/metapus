//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"

	"github.com/stretchr/testify/require"
)

type treasuryFinalFixture struct {
	ctx       context.Context
	tm        *postgres.TxManager
	doc       *treasury_withdrawal.TreasuryWithdrawal
	task      outboundapp.SettlementTask
	execution outboundapp.TreasuryTask
	receipt   crypto.ChainReceipt
	merchant  id.ID
}

func newTreasuryFinalFixture(t *testing.T, ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) treasuryFinalFixture {
	return newTreasuryFixture(t, ctx, tm, c, nil)
}

// newTreasuryRefundFixture returns the 25 of an unidentified receipt with 2
// withheld for gas, so the payer receives the same 23 a revenue withdrawal sends.
func newTreasuryRefundFixture(t *testing.T, ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) (treasuryFinalFixture, id.ID) {
	t.Helper()
	rowID := seedUnmatchedTransfer(t, ctx, tm, c, crypto.MatchReasonWrongToken, 0)
	intake, registry, _ := receiptIntakeForTest(t, c)
	recorded, err := intake.poll(ctx, registry)
	require.NoError(t, err)
	require.Equal(t, 1, recorded, receiptError(t, ctx, tm, rowID))
	var receipt id.ID
	require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT receipt_id FROM reg_crypto_inbound_chain_events WHERE id=$1`, rowID).Scan(&receipt))
	return newTreasuryFixture(t, ctx, tm, c, &receipt), receipt
}

func newTreasuryFixture(t *testing.T, ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture, refundOf *id.ID) treasuryFinalFixture {
	t.Helper()
	q := tm.GetQuerier(ctx)
	var networkIdentifier string
	require.NoError(t, q.QueryRow(ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, c.network).Scan(&networkIdentifier))
	_, err := q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'fixture',0,'shadow',2) ON CONFLICT DO NOTHING`, c.network)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100) ON CONFLICT(wallet_id,token_id) DO NOTHING`, c.hot, c.native)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100) ON CONFLICT(wallet_id,token_id) DO NOTHING`, c.hot, c.token)
	require.NoError(t, err)
	merchant, address := id.New(), id.New()
	destination := "external-" + address.String()
	_, err = q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Treasury revenue owner')`, merchant)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_balances(merchant_id,token_id,fee_type,balance) VALUES($1,$2,'processing',10),($1,$2,'withdrawal',90)`, merchant, c.token)
	require.NoError(t, err)
	doc := treasury_withdrawal.NewTreasuryWithdrawal(c.token, c.hot, address, destination, types.NewCryptoAmountFromInt64(23))
	if refundOf != nil {
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_receipt_refund_destinations(id,receipt_id,network_id,address,address_key,evidence,created_by)
			VALUES($1,$2,$3,$4,$4,'Sender proved the address',$5)`, address, *refundOf, c.network, destination, id.New())
		require.NoError(t, err)
		doc, err = treasury_withdrawal.NewReceiptRefund(*refundOf, c.token, c.hot, address, destination,
			types.NewCryptoAmountFromInt64(25), types.NewCryptoAmountFromInt64(2), treasury_withdrawal.GasPayerRecipient, "Sent by mistake")
		require.NoError(t, err)
	} else {
		_, err = q.Exec(ctx, `INSERT INTO reg_treasury_withdrawal_addresses(id,network_id,address,address_key) VALUES($1,$2,$3,$3)`, address, c.network, destination)
		require.NoError(t, err)
		doc.Status = treasury_withdrawal.StatusApproved
	}
	doc.Number = "TW2-" + id.Compact(doc.ID)
	require.NoError(t, document_repo.NewTreasuryWithdrawalRepo().Create(ctx, doc))
	var source, key, contract, standard string
	require.NoError(t, q.QueryRow(ctx, `SELECT w.address,w.vault_key_id,t.contract_address,t.token_standard FROM cat_wallets w,cat_tokens t WHERE w.id=$1 AND t.id=$2`, c.hot, c.token).Scan(&source, &key, &contract, &standard))
	execution := outboundapp.TreasuryTask{WithdrawalID: doc.ID, TokenID: c.token, SourceWalletID: c.hot, NetworkID: c.network, Amount: doc.Amount, DestAddress: destination, TokenStandard: token.TokenStandard(standard), ContractAddress: contract}
	repo := NewOutboundRepo()
	require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := repo.ReserveFeeForTreasury(ctx, doc.ID, c.token, doc.Amount); err != nil {
			return err
		}
		if refundOf == nil {
			return nil
		}
		// Approval opens the refund payable in the same transaction.
		doc.Status = treasury_withdrawal.StatusApproved
		_, err := tm.GetQuerier(ctx).Exec(ctx, `UPDATE doc_treasury_withdrawals SET status='approved' WHERE id=$1`, doc.ID)
		return err
	}))
	_, err = repo.ReserveHotWalletForTreasury(ctx, execution)
	require.NoError(t, err)
	hash := strings.ReplaceAll(doc.ID.String()+doc.ID.String(), "-", "")
	fee := id.New()
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status)
		VALUES($1,$2,$3,$4,$5,'TreasuryWithdrawal',$6,'treasury_withdrawal',1,'native_burn',10,'reserved')`, fee, c.network, c.hot, c.native, c.token, doc.ID)
	require.NoError(t, err)
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeTreasury,
		BasisType: "TreasuryWithdrawal", BasisID: doc.ID, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: c.network, NetworkCode: "TW-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier,
		SourceAddress: source, FeePayerAddress: source, VaultKeyID: key, DestinationAddress: destination, AssetStandard: standard, ContractAddress: contract, AmountMinorUnits: doc.Amount,
		MaxNetworkFeeMinor: types.NewCryptoAmountFromInt64(10), UnsignedFingerprint: "treasury-" + doc.ID.String(), PolicyVersion: "tron-v1", ApprovalBundleDigest: "test-approval"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
	_, err = repo.InsertSigningAuthorization(ctx, auth, "test-jws", unsigned, crypto.ApprovalBundle{IntentJWS: "test-intent"})
	require.NoError(t, err)
	require.NoError(t, repo.MarkSigningAuthorizationSigned(ctx, auth.AuthorizationID, crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: hash, Payload: []byte(`{}`)}))
	f := treasuryFinalFixture{ctx: ctx, tm: tm, doc: doc, execution: execution, merchant: merchant,
		task: outboundapp.SettlementTask{Kind: outboundapp.SettlementTreasury, DocumentID: doc.ID, NetworkID: c.network, OperationTokenID: c.token, SourceWalletID: c.hot, Amount: doc.Amount, TxHash: hash, PostedVersion: 1},
		receipt: crypto.ChainReceipt{NetworkID: c.network, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHeight: 100, BlockHash: strings.Repeat("b", 64), Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(),
			Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: c.native}, TotalFee: types.NewCryptoAmountFromInt64(2)}}}
	t.Cleanup(func() {
		_, _ = q.Exec(context.Background(), "UPDATE doc_treasury_withdrawals SET settlement_lease_expires_at='infinity',status=CASE WHEN posted THEN status ELSE 'failed' END WHERE id=$1", doc.ID)
	})
	return f
}

func (f treasuryFinalFixture) registry(t *testing.T) *crypto.ChainDriverRegistry {
	t.Helper()
	var networkIdentifier string
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, f.receipt.NetworkID).Scan(&networkIdentifier))
	registry := crypto.NewChainDriverRegistry()
	require.NoError(t, registry.Register(sweepReceiptDriver{fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.receipt.NetworkID, Code: "TEST", Family: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier, NativeTokenID: f.receipt.Fee.NativeAsset.TokenID, NativeDecimals: 6, RequiredConfirmations: 19, FinalityMode: crypto.FinalityByConfirmations}, receipt: f.receipt}}))
	return registry
}

func (f treasuryFinalFixture) executor(t *testing.T) *outboundapp.TreasuryExecutor {
	t.Helper()
	engine := posting.NewEngine(postgres.NewDocLocker())
	engine.AddVisitor(&posting.CryptoBalanceVisitor{})
	engine.AddVisitor(&posting.CryptoFeeVisitor{})
	return outboundapp.NewTreasuryExecutor(f.registry(t), nil, document_repo.NewTreasuryWithdrawalRepo(), NewOutboundRepo(), nil, engine, nil, nil, NewFeeBudgetManager(), nil, nil, "test", nil, nil, nil)
}

func (f treasuryFinalFixture) broadcast(t *testing.T) {
	t.Helper()
	require.NoError(t, f.executor(t).Execute(f.ctx, f.execution))
	var money, active int
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1)+(SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=$1),
		(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND status='active')`, f.doc.ID).Scan(&money, &active))
	require.Zero(t, money)
	require.Equal(t, 1, active)
}

func (f treasuryFinalFixture) service(t *testing.T) *outboundapp.SettlementService {
	t.Helper()
	p := CryptoProcessor{drivers: f.registry(t), outboundRepo: NewOutboundRepo(), feeBudget: NewFeeBudgetManager()}
	return p.settlementApplication()
}

func TestTreasuryV2ConcurrentFinalPrincipalAndRevenue(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newTreasuryFinalFixture(t, ctx, tm, c)
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
		var principal, revenue string
		var bindings, outbox, merchant int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
			(SELECT sum(balance)::text FROM reg_crypto_fee_balances WHERE merchant_id=$3 AND token_id=$2),
			(SELECT count(*) FROM reg_crypto_treasury_custody_bindings WHERE treasury_id=$4),(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$4 AND event_type='custody_recognized'),
			(SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$4)`, c.hot, c.token, f.merchant, f.doc.ID).Scan(&principal, &revenue, &bindings, &outbox, &merchant))
		require.Equal(t, []string{"77", "77"}, []string{principal, revenue})
		require.Equal(t, []int{1, 1, 0}, []int{bindings, outbox, merchant})
		require.NoError(t, f.executor(t).Execute(ctx, f.execution), "stale broadcaster must preserve final state")
	})
}

func TestTreasuryV2RevertedReleasesWithoutPrincipal(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newTreasuryFinalFixture(t, ctx, tm, c)
		f.broadcast(t)
		f.receipt.Status = crypto.ReceiptReverted
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		var money, feeLines, released int
		var native string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='TreasuryWithdrawal'),
			(SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=$1),(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND status='released'),
			(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$2 AND token_id=$3)`, f.doc.ID, c.hot, c.native).Scan(&money, &feeLines, &released, &native))
		require.Equal(t, []int{0, 0, 1}, []int{money, feeLines, released})
		require.Equal(t, "98", native)
		require.NoError(t, f.executor(t).Execute(ctx, f.execution))
	})
}

type incompleteTreasuryRepo struct {
	*OutboundRepo
	component string
}

func (r incompleteTreasuryRepo) PrepareTreasurySettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	if r.component == "claim" {
		return true, nil
	}
	proceed, err := r.OutboundRepo.PrepareTreasurySettlement(ctx, task, receipt)
	if err != nil {
		return false, err
	}
	if r.component == "outbox" {
		_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "DELETE FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized'", task.DocumentID)
	}
	return proceed, err
}
func (r incompleteTreasuryRepo) RecordOutboundSettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) error {
	if r.component == "writer" {
		return errors.New("injected treasury writer failure")
	}
	return r.OutboundRepo.RecordOutboundSettlement(ctx, task, receipt)
}

func TestTreasuryV2IncompleteSetRollsBackAndRetries(t *testing.T) {
	for _, component := range []string{"claim", "outbox", "writer"} {
		t.Run(component, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				f := newTreasuryFinalFixture(t, ctx, tm, c)
				f.broadcast(t)
				service := outboundapp.NewSettlementService(outboundapp.SettlementConfig{Drivers: f.registry(t), Repository: incompleteTreasuryRepo{OutboundRepo: NewOutboundRepo(), component: component}, Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, TreasuryEvidence: outboundapp.NewTreasuryEvidenceService(crypto_repo.NewTreasuryCustodyRepo())})
				require.Error(t, service.Settle(ctx, f.task))
				var money, bindings, active int
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),
			(SELECT count(*) FROM reg_crypto_treasury_custody_bindings WHERE treasury_id=$1),(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND status='active')`, f.doc.ID).Scan(&money, &bindings, &active))
				require.Equal(t, []int{0, 0, 1}, []int{money, bindings, active})
				require.NoError(t, f.service(t).Settle(ctx, f.task))
			})
		})
	}
}

func TestTreasuryV2NativePrincipalAndFeeStayReservedUntilFinal(t *testing.T) {
	for _, actual := range []int64{0, 2} {
		runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
			c.token = c.native
			f := newTreasuryFinalFixture(t, ctx, tm, c)
			f.receipt.Fee.TotalFee = types.NewCryptoAmountFromInt64(actual)
			f.broadcast(t)
			fee, found, err := NewFeeBudgetManager().Get(ctx, "TreasuryWithdrawal", f.doc.ID, crypto.SigningPurposeTreasury, 1)
			require.NoError(t, err)
			require.True(t, found)
			require.Error(t, NewFeeBudgetManager().Settle(ctx, fee.ID, f.receipt), "fee cannot commit without principal/revenue outcome")
			require.Error(t, NewOutboundRepo().ReleaseTreasuryReservations(ctx, f.doc.ID), "unknown signed outcome cannot release principal")
			require.NoError(t, f.service(t).Settle(ctx, f.task))
			var balance string
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", c.hot, c.native).Scan(&balance))
			require.Equal(t, types.NewCryptoAmountFromInt64(77-actual).String(), balance)
		})
	}
}

func TestTreasuryV2BasisAndFinalHistoryAreImmutable(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newTreasuryFinalFixture(t, ctx, tm, c)
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, "UPDATE doc_treasury_withdrawals SET amount=24 WHERE id=$1", f.doc.ID)
		require.Error(t, err)
		_, err = q.Exec(ctx, "UPDATE reg_crypto_revenue_reservations SET amount=amount+1 WHERE document_id=$1", f.doc.ID)
		require.Error(t, err)
		f.broadcast(t)
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		for _, query := range []string{
			"UPDATE doc_treasury_withdrawals SET posted=false WHERE id=$1",
			"UPDATE doc_treasury_withdrawals SET status='broadcast' WHERE id=$1",
			"UPDATE reg_crypto_balance_reservations SET status='released' WHERE document_id=$1",
			`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
			 SELECT gen_random_uuid_v7(),recorder_id,recorder_type,2,now(),'receipt',wallet_id,token_id,amount,line_id FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='TreasuryWithdrawal'`,
		} {
			_, err = q.Exec(ctx, query, f.doc.ID)
			require.Error(t, err, query)
		}
	})
}

func TestTreasuryV2NativeFeeCannotSpendPrincipalReservation(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		c.token = c.native
		_ = newTreasuryFinalFixture(t, ctx, tm, c)
		_, err := tm.GetQuerier(ctx).Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=true,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=1000,
			fee_funding_mode='source_only',daily_jit_funding_budget_minor=0,min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0,jit_funding_max_amount_minor=0 WHERE network_id=$1`, c.network)
		require.NoError(t, err)
		_, err = NewFeeBudgetManager().Reserve(ctx, FeeReservationRequest{NetworkID: c.network, PayerWalletID: c.hot, NativeTokenID: c.native, OperationTokenID: c.native,
			BasisType: "CryptoNetworkOperation", BasisID: id.New(), Purpose: crypto.SigningPurposeActivation, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewCryptoAmountFromInt64(80)})
		require.ErrorIs(t, err, crypto.ErrFeeBalanceResidual)
	})
}

func TestNativeSweepPrincipalDoesNotConsumeDailyNetworkFeeBudget(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=true,per_transaction_max_fee_minor=20,daily_network_fee_budget_minor=20,
			fee_funding_mode='source_only',daily_jit_funding_budget_minor=0,min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0,jit_funding_max_amount_minor=0 WHERE network_id=$1`, c.network)
		require.NoError(t, err)
		reserve, err := NewFeeBudgetManager().Reserve(ctx, FeeReservationRequest{NetworkID: c.network, PayerWalletID: c.wallet, NativeTokenID: c.native, OperationTokenID: c.native,
			ReservedPrincipal: types.NewCryptoAmountFromInt64(23), BasisType: "CryptoSweepAttempt", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewCryptoAmountFromInt64(10)})
		require.NoError(t, err)
		require.Equal(t, "23", reserve.ReservedPrincipal.String())
		require.NoError(t, NewFeeBudgetManager().IncreaseMaxFee(ctx, reserve.ID, types.NewCryptoAmountFromInt64(15), 2), "replacement budgets charge only fee delta")
	})
}

type treasuryBadProofDriver struct {
	crypto.NetworkDriver
	mode string
}

func (d treasuryBadProofDriver) VerifyExpectedTransfer(ctx context.Context, e crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	if d.mode == "no_quorum" {
		return crypto.TransferVerification{}, crypto.ErrVerificationPending
	}
	proof, err := d.NetworkDriver.(crypto.ExpectedTransferVerifier).VerifyExpectedTransfer(ctx, e)
	if err != nil {
		return proof, err
	}
	proof.Event.Amount = proof.Event.Amount.Add(types.NewCryptoAmountFromInt64(1))
	return proof, nil
}

func TestTreasuryV2MissingOrContradictoryProofRetainsReservations(t *testing.T) {
	for _, mode := range []string{"no_quorum", "wrong_amount"} {
		t.Run(mode, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				f := newTreasuryFinalFixture(t, ctx, tm, c)
				f.broadcast(t)
				base, err := f.registry(t).Resolve(c.network)
				require.NoError(t, err)
				registry := crypto.NewChainDriverRegistry()
				require.NoError(t, registry.Register(treasuryBadProofDriver{NetworkDriver: base, mode: mode}))
				service := outboundapp.NewSettlementService(outboundapp.SettlementConfig{Drivers: registry, Repository: NewOutboundRepo(), Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, TreasuryEvidence: outboundapp.NewTreasuryEvidenceService(crypto_repo.NewTreasuryCustodyRepo())})
				require.Error(t, service.Settle(ctx, f.task))
				var money, active int
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),
			(SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND status='active')`, f.doc.ID).Scan(&money, &active))
				require.Equal(t, []int{0, 1}, []int{money, active})
				require.NoError(t, f.service(t).Settle(ctx, f.task))
			})
		})
	}
}

func TestTreasuryV2ExpectedOutflowCannotBecomeAnotherCustodyExpense(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newTreasuryFinalFixture(t, ctx, tm, c)
		f.broadcast(t)
		outflow, adjustment := id.New(), id.New()
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount)
			SELECT $1,x.network_id,x.token_id,$2,x.tx_hash,x.event_kind,0,x.source_address_key,x.destination_address_key,x.source_address_key,x.destination_address_key,x.contract_address_key,x.contract_address_key,x.amount
			FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id WHERE a.basis_type='TreasuryWithdrawal' AND a.basis_id=$3`, outflow, c.hot, f.doc.ID)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO doc_crypto_custody_adjustments(id,external_outflow_id,network_id,wallet_id,token_id,amount,maker_id) VALUES($1,$2,$3,$4,$5,23,$6)`, adjustment, outflow, c.network, c.hot, c.token, id.New())
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$1,'CryptoCustodyAdjustment',1,now(),'expense',$2,$3,23)`, adjustment, c.hot, c.token)
		require.ErrorContains(t, err, "expected treasury principal requires its settlement owner")
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		legacy := id.New()
		_, err = q.Exec(ctx, `INSERT INTO doc_treasury_withdrawals(id,token_id,source_wallet_id,address_id,dest_address,amount,tx_hash,status)
			VALUES($1,$2,$3,$4,$5,23,$6,'broadcast')`, legacy, c.token, c.hot, f.doc.AddressID, f.doc.DestAddress, f.task.TxHash)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$1,'TreasuryWithdrawal',1,now(),'expense',$2,$3,23)`, legacy, c.hot, c.token)
		require.ErrorContains(t, err, "expected treasury principal requires its settlement owner")
	})
}

func TestTreasuryV2ConcurrentReservationsCannotOversubscribe(t *testing.T) {
	for _, resource := range []string{"wallet", "revenue"} {
		t.Run(resource, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				f := newTreasuryFinalFixture(t, ctx, tm, c)
				documents := make([]*treasury_withdrawal.TreasuryWithdrawal, 2)
				for i := range documents {
					documents[i] = treasury_withdrawal.NewTreasuryWithdrawal(c.token, c.hot, f.doc.AddressID, f.doc.DestAddress, types.NewCryptoAmountFromInt64(50))
					documents[i].Status = treasury_withdrawal.StatusApproved
					require.NoError(t, document_repo.NewTreasuryWithdrawalRepo().Create(ctx, documents[i]))
				}
				t.Cleanup(func() {
					_, _ = tm.GetQuerier(ctx).Exec(context.Background(), "UPDATE doc_treasury_withdrawals SET status='failed' WHERE id=ANY($1) AND posted=false", []id.ID{f.doc.ID, documents[0].ID, documents[1].ID})
				})
				var wg sync.WaitGroup
				results := make(chan error, 2)
				for _, doc := range documents {
					wg.Go(func() {
						repo := NewOutboundRepo()
						if resource == "revenue" {
							results <- repo.ReserveFeeForTreasury(ctx, doc.ID, c.token, doc.Amount)
							return
						}
						_, err := repo.ReserveHotWalletForTreasury(ctx, outboundapp.TreasuryTask{WithdrawalID: doc.ID, TokenID: c.token, SourceWalletID: c.hot, NetworkID: c.network, Amount: doc.Amount})
						results <- err
					})
				}
				wg.Wait()
				close(results)
				success := 0
				for err := range results {
					if err == nil {
						success++
					}
				}
				require.Equal(t, 1, success, "only 77 of 100 units remain available")
			})
		})
	}
}

func receiptRefundAmounts(t *testing.T, ctx context.Context, tm *postgres.TxManager, receipt, refund id.ID) (suspense, payable string, feeLines int) {
	t.Helper()
	require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_receipt_suspense_balances WHERE receipt_id=$1),
		COALESCE((SELECT amount::text FROM reg_crypto_refund_payable_balances WHERE receipt_id=$1),'0'),
		(SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=$2)`, receipt, refund).Scan(&suspense, &payable, &feeLines))
	return
}

func TestTreasuryReceiptRefundPayoutClosesThePayable(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f, receipt := newTreasuryRefundFixture(t, ctx, tm, c)
		suspense, payable, _ := receiptRefundAmounts(t, ctx, tm, receipt, f.doc.ID)
		require.Equal(t, []string{"0", "23"}, []string{suspense, payable}, "approval takes 25 from suspense and owes the payer 23")
		f.broadcast(t)
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		var principal, status string
		var bindings int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
			(SELECT status FROM doc_treasury_withdrawals WHERE id=$3),(SELECT count(*) FROM reg_crypto_treasury_custody_bindings WHERE treasury_id=$3)`,
			c.hot, c.token, f.doc.ID).Scan(&principal, &status, &bindings))
		require.Equal(t, []string{"77", "confirmed"}, []string{principal, status})
		require.Equal(t, 1, bindings)
		suspense, payable, feeLines := receiptRefundAmounts(t, ctx, tm, receipt, f.doc.ID)
		require.Equal(t, []string{"0", "0"}, []string{suspense, payable}, "the payout closes the payable; the withheld 2 stays with the platform")
		require.Zero(t, feeLines, "a refund spends no platform revenue")
	})
}

func TestTreasuryReceiptRefundRevertedGivesTheReceiptBack(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f, receipt := newTreasuryRefundFixture(t, ctx, tm, c)
		f.broadcast(t)
		f.receipt.Status = crypto.ReceiptReverted
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		require.NoError(t, f.service(t).Settle(ctx, f.task))
		suspense, payable, _ := receiptRefundAmounts(t, ctx, tm, receipt, f.doc.ID)
		require.Equal(t, []string{"25", "0"}, []string{suspense, payable}, "a reverted refund returns the whole receipt to suspense")
		var status, native string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT status,(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$2 AND token_id=$3)
			FROM doc_treasury_withdrawals WHERE id=$1`, f.doc.ID, c.hot, c.native).Scan(&status, &native))
		require.Equal(t, []string{"failed", "98"}, []string{status, native}, "the platform still bears the burnt gas")
	})
}
