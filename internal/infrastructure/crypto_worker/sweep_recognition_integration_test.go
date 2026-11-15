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
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"

	"github.com/stretchr/testify/require"
)

type sweepFinalFixture struct {
	ctx      context.Context
	tm       *postgres.TxManager
	doc      *crypto_sweep.CryptoSweep
	tasks    []outboundapp.SettlementTask
	group    []outboundapp.SweepTask
	receipts []crypto.ChainReceipt
}

type sweepReceiptDriver struct{ fundingReceiptDriver }

func (d sweepReceiptDriver) VerifyExpectedTransfer(ctx context.Context, e crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	proof, err := d.VerifyTransfer(ctx, e)
	proof.MatchingEffects = 1
	return proof, err
}

func newSweepFinalFixture(t *testing.T, ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture, count int) sweepFinalFixture {
	t.Helper()
	q := tm.GetQuerier(ctx)
	var networkIdentifier string
	require.NoError(t, q.QueryRow(ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, f.network).Scan(&networkIdentifier))
	_, err := q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version)
		VALUES($1,0,'fixture',0,'shadow',2)`, f.network)
	require.NoError(t, err)
	doc := crypto_sweep.NewCryptoSweep(f.token, f.hot)
	doc.Number = "SV2-" + id.Compact(doc.ID)
	fixture := sweepFinalFixture{ctx: ctx, tm: tm, doc: doc}
	for i := 0; i < count; i++ {
		wallet := f.wallet
		if i > 0 {
			wallet = id.New()
			_, err = q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,status,chain_activation_status)
				VALUES($1,$1::uuid::text,'Sweep source',$2,$3,'source-key','pool','merchant_pool','sweeping','active')`, wallet, f.network, testTRONAddress())
			require.NoError(t, err)
			_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,1000),($1,$3,1000)`, wallet, f.token, f.native)
			require.NoError(t, err)
		}
		_, err = q.Exec(ctx, "UPDATE cat_wallets SET status='sweeping' WHERE id=$1", wallet)
		require.NoError(t, err)
		line, fee := id.New(), id.New()
		hash := strings.ReplaceAll(line.String()+line.String(), "-", "")
		var source, destination, key, contract, standard string
		require.NoError(t, q.QueryRow(ctx, `SELECT s.address,d.address,s.vault_key_id,t.contract_address,t.token_standard FROM cat_wallets s,cat_wallets d,cat_tokens t
			WHERE s.id=$1 AND d.id=$2 AND t.id=$3`, wallet, f.hot, f.token).Scan(&source, &destination, &key, &contract, &standard))
		amount := types.NewCryptoAmountFromInt64(23)
		doc.Lines = append(doc.Lines, crypto_sweep.CryptoSweepLine{LineID: line, LineNo: i + 1, WalletID: wallet, Amount: amount, SettlementStatus: outbound_settlement.StatusNotApplicable})
		fixture.tasks = append(fixture.tasks, outboundapp.SettlementTask{Kind: outboundapp.SettlementSweep, DocumentID: doc.ID, LineID: line, NetworkID: f.network, OperationTokenID: f.token, SourceWalletID: wallet, HotWalletID: f.hot, TxHash: hash, Amount: amount, PostedVersion: 1})
		fixture.group = append(fixture.group, outboundapp.SweepTask{AttemptID: line, SweepID: &doc.ID, NetworkID: f.network, TokenID: f.token, TokenStandard: token.TokenStandard(standard), WalletID: wallet, Address: source, HotWalletID: f.hot, Balance: amount})
		fixture.receipts = append(fixture.receipts, crypto.ChainReceipt{NetworkID: f.network, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHeight: 100, BlockHash: strings.Repeat("b", 64), Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.native}, TotalFee: types.NewCryptoAmountFromInt64(2)}})
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,23,'claimed',now()+interval '1 hour')`, line, wallet, f.token, f.network, f.hot, source, destination)
		require.NoError(t, err)
		principal := types.ZeroCryptoAmount()
		if f.token == f.native {
			principal = amount
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status)
			VALUES($1,$2,$3,$4,$5,$8,'CryptoSweepAttempt',$6,'sweep',1,'native_burn',10,$7,'broadcast')`, fee, f.network, wallet, f.native, f.token, line, hash, principal)
		require.NoError(t, err)
		auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeSweep,
			BasisType: "CryptoSweepAttempt", BasisID: line, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.network, NetworkCode: "SWEEP-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier,
			SourceAddress: source, FeePayerAddress: source, VaultKeyID: key, DestinationAddress: destination, AssetStandard: standard, ContractAddress: contract, AmountMinorUnits: amount,
			MaxNetworkFeeMinor: types.NewCryptoAmountFromInt64(10), UnsignedFingerprint: "sweep-" + line.String(), PolicyVersion: "tron-v1"}
		unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
		repo := NewOutboundRepo()
		_, err = repo.InsertSigningAuthorization(ctx, auth, "test-jws", unsigned, crypto.ApprovalBundle{})
		require.NoError(t, err)
		require.NoError(t, repo.MarkSigningAuthorizationSigned(ctx, auth.AuthorizationID, crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: hash, Payload: []byte(`{}`)}))
		require.NoError(t, repo.MarkSigningAuthorizationBroadcast(ctx, auth.AuthorizationID, hash))
		_, err = q.Exec(ctx, "UPDATE reg_crypto_sweep_attempts SET status='broadcast' WHERE id=$1", line)
		require.NoError(t, err)
	}
	doc.TotalAmount = types.NewCryptoAmountFromInt64(int64(count * 23))
	docs := crypto_document_repo.NewCryptoSweepRepo()
	require.NoError(t, tm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := docs.Create(txCtx, doc); err != nil {
			return err
		}
		if err := docs.SaveLines(txCtx, doc.ID, doc.Lines); err != nil {
			return err
		}
		_, err := tm.GetQuerier(txCtx).Exec(txCtx, "UPDATE reg_crypto_sweep_attempts SET sweep_id=$1 WHERE id=ANY($2)", doc.ID, outboundapp.SweepGroupAttemptIDs(fixture.group))
		return err
	}))
	t.Cleanup(func() {
		_, _ = q.Exec(context.Background(), "UPDATE doc_crypto_sweeps__lines SET settlement_lease_expires_at='infinity' WHERE document_id=$1", doc.ID)
	})
	return fixture
}

func (f sweepFinalFixture) broadcast(t *testing.T) {
	t.Helper()
	require.NoError(t, f.executor(t).ExecuteGroup(f.ctx, f.group))
	var movements int
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1", f.doc.ID).Scan(&movements))
	require.Zero(t, movements, "broadcast cannot debit or credit principal")
}

func (f sweepFinalFixture) executor(t *testing.T) *outboundapp.SweepExecutor {
	t.Helper()
	engine := posting.NewEngine(postgres.NewDocLocker())
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	return outboundapp.NewSweepExecutor(outboundapp.SweepConfig{Repository: NewOutboundRepo(), Documents: crypto_document_repo.NewCryptoSweepRepo(), Posting: engine, Drivers: f.registry(t, 0)})
}

func (f sweepFinalFixture) registry(t *testing.T, index int) *crypto.ChainDriverRegistry {
	t.Helper()
	var networkIdentifier string
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT network_identifier FROM cat_blockchain_networks WHERE id=$1`, f.receipts[index].NetworkID).Scan(&networkIdentifier))
	registry := crypto.NewChainDriverRegistry()
	d := sweepReceiptDriver{fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.receipts[index].NetworkID, Code: "TEST", Family: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier, NativeTokenID: f.receipts[index].Fee.NativeAsset.TokenID, NativeDecimals: 6, RequiredConfirmations: 19, FinalityMode: crypto.FinalityByConfirmations}, receipt: f.receipts[index]}}
	require.NoError(t, registry.Register(d))
	return registry
}

func (f sweepFinalFixture) service(t *testing.T, index int) *outboundapp.SettlementService {
	t.Helper()
	registry := f.registry(t, index)
	p := CryptoProcessor{drivers: registry, outboundRepo: NewOutboundRepo(), feeBudget: NewFeeBudgetManager()}
	return p.settlementApplication()
}

func TestSweepV2FinalLinesAreIndependentAndConcurrent(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newSweepFinalFixture(t, ctx, tm, c, 2)
		f.broadcast(t)
		var state string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT status FROM cat_wallets WHERE id=$1", c.wallet).Scan(&state))
		require.Equal(t, "sweeping", state)
		service := f.service(t, 0)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() { errs <- service.Settle(ctx, f.tasks[0]) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var principal string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", c.hot, c.token).Scan(&principal))
		require.Equal(t, "23", principal)
		require.NoError(t, f.service(t, 1).Settle(ctx, f.tasks[1]))
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2", c.hot, c.token).Scan(&principal))
		require.Equal(t, "46", principal)
		var parents, sides, outbox int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_custody_effect_postings WHERE recorder_id=$1),
			(SELECT count(*) FROM reg_crypto_sweep_custody_bindings WHERE sweep_id=$1),
			(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized')`, f.doc.ID).Scan(&parents, &sides, &outbox))
		require.Equal(t, []int{2, 2, 2}, []int{parents, sides, outbox})
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT status FROM doc_crypto_sweeps WHERE id=$1", f.doc.ID).Scan(&state))
		require.Equal(t, "confirmed", state)
		require.NoError(t, f.executor(t).ExecuteGroup(ctx, f.group), "stale broadcaster must skip final lines")
		require.NoError(t, NewOutboundRepo().MarkSweepAttemptBroadcast(ctx, f.tasks[0].LineID), "late status update must preserve terminal attempt")
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1", f.tasks[0].LineID).Scan(&state))
		require.Equal(t, "posted", state)
	})
}

func TestSweepV2RevertedLineHasOnlyActualFee(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newSweepFinalFixture(t, ctx, tm, c, 1)
		f.broadcast(t)
		f.receipts[0].Status = crypto.ReceiptReverted
		require.NoError(t, f.service(t, 0).Settle(ctx, f.tasks[0]))
		var count int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1", f.doc.ID).Scan(&count))
		require.Zero(t, count)
		var confirmed bool
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT confirmed FROM doc_crypto_sweeps__lines WHERE line_id=$1", f.tasks[0].LineID).Scan(&confirmed))
		require.False(t, confirmed)
		var fee string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT actual_fee_minor::text FROM reg_crypto_fee_reservations WHERE basis_id=$1", f.tasks[0].LineID).Scan(&fee))
		require.Equal(t, "2", fee)
		var attempt, walletState, authorization string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT a.status,w.status,s.status FROM reg_crypto_sweep_attempts a
			JOIN cat_wallets w ON w.id=a.wallet_id JOIN reg_crypto_signing_authorizations s ON s.basis_type='CryptoSweepAttempt' AND s.basis_id=a.id WHERE a.id=$1`, f.tasks[0].LineID).Scan(&attempt, &walletState, &authorization))
		require.Equal(t, []string{"failed", "sweep_pending", "failed"}, []string{attempt, walletState, authorization})
		require.NoError(t, f.executor(t).ExecuteGroup(ctx, f.group), "reverted line must never be signed again")
		require.NoError(t, NewOutboundRepo().MarkSweepAttemptBroadcast(ctx, f.tasks[0].LineID))
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1", f.tasks[0].LineID).Scan(&attempt))
		require.Equal(t, "failed", attempt)
	})
}

type incompleteSweepRepo struct {
	*OutboundRepo
	component string
}

func (r incompleteSweepRepo) PrepareSweepSettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) (bool, error) {
	if r.component == "claim" {
		return true, nil
	}
	proceed, err := r.OutboundRepo.PrepareSweepSettlement(ctx, task, receipt)
	if err != nil {
		return false, err
	}
	if r.component == "outbox" {
		_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "DELETE FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized' AND payload->>'lineId'=$2", task.DocumentID, task.LineID.String())
	}
	return proceed, err
}

func (r incompleteSweepRepo) RecordOutboundSettlement(ctx context.Context, task outboundapp.SettlementTask, receipt crypto.ChainReceipt) error {
	if r.component == "writer" {
		return errors.New("injected sweep writer failure")
	}
	return r.OutboundRepo.RecordOutboundSettlement(ctx, task, receipt)
}

func TestSweepV2IncompleteSetRollsBackAndRetries(t *testing.T) {
	for _, component := range []string{"claim", "outbox", "writer"} {
		t.Run(component, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				f := newSweepFinalFixture(t, ctx, tm, c, 1)
				f.broadcast(t)
				service := outboundapp.NewSettlementService(outboundapp.SettlementConfig{Drivers: f.registry(t, 0), Repository: incompleteSweepRepo{OutboundRepo: NewOutboundRepo(), component: component},
					Fees: NewFeeBudgetManager(), Transactions: contextTxManager{}, SweepEvidence: outboundapp.NewSweepEvidenceService(crypto_repo.NewSweepCustodyRepo())})
				require.Error(t, service.Settle(ctx, f.tasks[0]))
				var money, bindings, outbox int
				var status string
				require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id IN ($1,$2)),
					(SELECT count(*) FROM reg_crypto_sweep_custody_bindings WHERE sweep_id=$1),(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1),status
					FROM reg_crypto_fee_reservations WHERE basis_id=$2`, f.doc.ID, f.tasks[0].LineID).Scan(&money, &bindings, &outbox, &status))
				require.Equal(t, []int{0, 0, 0}, []int{money, bindings, outbox})
				require.Equal(t, "broadcast", status)
				require.NoError(t, f.service(t, 0).Settle(ctx, f.tasks[0]))
			})
		})
	}
}

func TestSweepV2FeeCannotReleasePrincipalSeparately(t *testing.T) {
	for _, actual := range []int64{0, 2} {
		runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
			f := newSweepFinalFixture(t, ctx, tm, c, 1)
			f.broadcast(t)
			f.receipts[0].Fee.TotalFee = types.NewCryptoAmountFromInt64(actual)
			var fee id.ID
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT id FROM reg_crypto_fee_reservations WHERE basis_id=$1", f.tasks[0].LineID).Scan(&fee))
			require.Error(t, NewFeeBudgetManager().Settle(ctx, fee, f.receipts[0]))
			require.NoError(t, f.service(t, 0).Settle(ctx, f.tasks[0]))
		})
	}
}

func TestSweepV2NativePrincipalAndFeeAreAtomic(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		c.token = c.native
		f := newSweepFinalFixture(t, ctx, tm, c, 1)
		f.broadcast(t)
		var reserved string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT reserved_principal_minor::text FROM reg_crypto_fee_reservations WHERE basis_id=$1 AND status='broadcast'", f.tasks[0].LineID).Scan(&reserved))
		require.Equal(t, "23", reserved)
		require.NoError(t, f.service(t, 0).Settle(ctx, f.tasks[0]))
		var source, destination string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT
			(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$3),
			(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$2 AND token_id=$3)`, c.wallet, c.hot, c.native).Scan(&source, &destination))
		require.Equal(t, []string{"199999975", "23"}, []string{source, destination})
	})
}

func TestSweepV2FinalBasisAndPrincipalCannotBeRewritten(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
		f := newSweepFinalFixture(t, ctx, tm, c, 1)
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, "UPDATE doc_crypto_sweeps SET date=date+interval '1 second' WHERE id=$1", f.doc.ID)
		require.Error(t, err, "signing freezes the basis before broadcast is recorded")
		f.broadcast(t)
		_, err = q.Exec(ctx, "UPDATE doc_crypto_sweeps__lines SET amount=amount+1 WHERE line_id=$1", f.tasks[0].LineID)
		require.Error(t, err)
		require.NoError(t, f.service(t, 0).Settle(ctx, f.tasks[0]))
		for _, query := range []string{
			"UPDATE doc_crypto_sweeps SET posted=false WHERE id=$1",
			"DELETE FROM doc_crypto_sweeps__lines WHERE document_id=$1",
			`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id)
			 SELECT gen_random_uuid_v7(),recorder_id,recorder_type,2,now(),CASE record_type WHEN 'receipt' THEN 'expense' ELSE 'receipt' END,wallet_id,token_id,amount,line_id
			 FROM reg_crypto_balance_movements WHERE recorder_id=$1`,
			`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			 SELECT gen_random_uuid_v7(),recorder_id,'ChainSettlement',1,now(),record_type,wallet_id,token_id,1
			 FROM reg_crypto_balance_movements WHERE recorder_id=$1 LIMIT 1`,
		} {
			_, err = q.Exec(ctx, query, f.doc.ID)
			require.Error(t, err, query)
		}
	})
}
