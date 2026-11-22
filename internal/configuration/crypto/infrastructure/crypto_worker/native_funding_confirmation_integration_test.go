//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/types"
)

func seedNativeFundingConfirmation(t *testing.T, ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture, round ...int) (id.ID, id.ID, crypto.ChainReceipt) {
	t.Helper()
	attempt := 1
	if len(round) > 0 {
		attempt = round[0]
	}
	return seedNativeFunding(t, ctx, txm, f, attempt, true)
}

func seedNativeFunding(t *testing.T, ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture, attempt int, verified bool) (id.ID, id.ID, crypto.ChainReceipt) {
	t.Helper()
	fundingID, feeID, attemptID, sweepID := id.New(), id.New(), id.New(), id.New()
	txHash := strings.ReplaceAll(fundingID.String()+fundingID.String(), "-", "")
	_, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE cat_wallets SET status='free' WHERE id=$1;
		INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($2,$3,1000);
		INSERT INTO doc_crypto_sweeps(id,number,token_id,hot_wallet_id,total_amount,status)
		VALUES($10,$10::text,$11,$2,999999,'created');
		INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,sweep_id,source_address,destination_address,amount,status,expires_at)
		VALUES($9,$1,$11,$4,$2,$10,'source','destination',999999,'expired',NOW()-interval '1 hour');
		INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status)
		VALUES($6,$4,$2,$3,$3,100,'CryptoNativeFunding',$5,'native_funding',1,'native_funding',10,$7,'broadcast');
		INSERT INTO doc_crypto_native_fundings(id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,amount_minor,fee_reservation_id,status,tx_hash)
		VALUES($5,'CryptoSweepAttempt',$9,$12,$4,$3,$2,$1,100,$6,'broadcast',$7);
		INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,held_by)
		VALUES($1,'chain_balance_mismatch','reconciliation_incident',$8,'test');
	`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.hot, f.native, f.network, fundingID, feeID, txHash, fundingID.String(), attemptID, sweepID, f.token, attempt)
	require.NoError(t, err)
	if txm.GetTx(ctx) == nil {
		t.Cleanup(func() {
			_, _ = txm.GetQuerier(ctx).Exec(context.Background(), "UPDATE doc_crypto_native_fundings SET next_attempt_at='infinity' WHERE id=$1", fundingID)
		})
	}
	receipt := crypto.ChainReceipt{
		NetworkID: f.network, TxHash: txHash, Status: crypto.ReceiptSucceeded,
		BlockHash: strings.Repeat("b", 64), BlockHeight: 100, Confirmations: 20,
		Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
		ObservedAt: time.Now().UTC(),
		Fee:        crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.native}, TotalFee: types.NewRawAmountFromInt64(2)},
	}
	if verified {
		proof := nativeFundingProof(t, ctx, txm, fundingID, receipt)
		_, err = crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, proof)
		require.NoError(t, err)
	}
	receipt, err = attachNetworkFeeProof(ctx, receipt)
	require.NoError(t, err)
	return fundingID, sweepID, receipt
}

// reproveNetworkFee replaces the fixture fee proof after a test changes the
// chain outcome it reports.
func reproveNetworkFee(t *testing.T, ctx context.Context, r crypto.ChainReceipt) crypto.ChainReceipt {
	t.Helper()
	r.FeeProof = nil
	r, err := attachNetworkFeeProof(ctx, r)
	require.NoError(t, err)
	return r
}

func nativeFundingProof(t *testing.T, ctx context.Context, txm *sdktest.TxManager, fundingID id.ID, receipt crypto.ChainReceipt) crypto.TransferVerification {
	t.Helper()
	_, err := txm.GetQuerier(ctx).Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'fixture',0,'shadow',2) ON CONFLICT DO NOTHING`, receipt.NetworkID)
	require.NoError(t, err)
	event, err := (crypto_repo.NativeFundingEvidenceRepo{}).ExpectedFundingTransfer(ctx, fundingID, receipt)
	require.NoError(t, err)
	identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyTRON, 0)
	require.NoError(t, err)
	return crypto.TransferVerification{Event: event, AccountingIdentity: &identity, DecoderVersion: 2,
		Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded, VerifiedAt: receipt.ObservedAt,
		Sources: []crypto.VerificationSource{
			{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "funding", SourceDigest: "funding-source", DecoderVersion: 2, HeadHeight: 119, HeadHash: "head"},
			{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "funding", SourceDigest: "funding-source", DecoderVersion: 2, HeadHeight: 119, HeadHash: "head"},
		}}
}

type fundingReceiptDriver struct {
	fakeSweepNetworkDriver
	network crypto.NetworkRef
	receipt crypto.ChainReceipt
}

func (d fundingReceiptDriver) Network() crypto.NetworkRef { return d.network }
func (d fundingReceiptDriver) Capabilities() crypto.ChainFamilyCapabilities {
	c := d.fakeSweepNetworkDriver.Capabilities()
	c.WalletProvisioning = crypto.WalletProvisioningUnsupported
	return c
}
func (d fundingReceiptDriver) Finality() crypto.FinalityReader { return d }
func (d fundingReceiptDriver) VerificationRequired() bool      { return true }
func (d fundingReceiptDriver) VerifyTransfer(ctx context.Context, event crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return crypto.TransferVerification{}, errors.New("proof RPC called inside financial transaction")
	}
	identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyTRON, 0)
	return crypto.TransferVerification{Event: event, AccountingIdentity: &identity, DecoderVersion: 2,
		Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded, VerifiedAt: d.receipt.ObservedAt,
		Sources: []crypto.VerificationSource{
			{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "funding", SourceDigest: "funding-source", DecoderVersion: 2, HeadHeight: 119, HeadHash: "head"},
			{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "funding", SourceDigest: "funding-source", DecoderVersion: 2, HeadHeight: 119, HeadHash: "head"},
		}}, err
}
func (d fundingReceiptDriver) Receipt(ctx context.Context, hash string) (crypto.ChainReceipt, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return crypto.ChainReceipt{}, errors.New("RPC called inside financial transaction")
	}
	if hash != d.receipt.TxHash {
		return crypto.ChainReceipt{}, errors.New("unexpected receipt")
	}
	return attachNetworkFeeProof(ctx, d.receipt)
}

// attachNetworkFeeProof gives a receipt the independent fee proof a reserved
// payer would obtain from two providers, unless the test supplied its own.
func attachNetworkFeeProof(ctx context.Context, r crypto.ChainReceipt) (crypto.ChainReceipt, error) {
	if r.FeeProof != nil || id.IsNil(r.Fee.NativeAsset.TokenID) || !r.FinanciallyFinal() {
		return r, nil
	}
	var payer string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT w.address_key FROM reg_crypto_fee_reservations f
		JOIN cat_wallets w ON w.id=f.payer_wallet_id WHERE f.network_id=$1 AND f.tx_hash=$2 ORDER BY f.attempt DESC LIMIT 1`, r.NetworkID, r.TxHash).Scan(&payer)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return crypto.ChainReceipt{}, err
	}
	if r.Fee.TotalFee.IsZero() {
		r.Fee.TotalFee = types.ZeroRawAmount()
	}
	r.Fee.Components = []crypto.FeeComponent{{Name: "energy_fee", Amount: types.ZeroRawAmount()}, {Name: "bandwidth_fee", Amount: r.Fee.TotalFee, Quantity: 2}}
	head := r.BlockHeight + int64(r.Confirmations) - 1
	p := crypto.NetworkFeeProof{SchemaVersion: 1, Family: crypto.ChainFamilyTRON, NetworkID: r.NetworkID, TxHash: r.TxHash, PayerAddress: payer, NativeTokenID: r.Fee.NativeAsset.TokenID,
		BlockHash: r.BlockHash, BlockNumber: r.BlockHeight, Status: r.Status, TotalFee: r.Fee.TotalFee, Components: r.Fee.Components, Confirmations: r.Confirmations,
		Finality: r.Finality, Canonicality: r.Canonicality, VerifiedAt: r.ObservedAt,
		Sources: []crypto.VerificationSource{{ProviderID: "one", TrustGroup: "one", SourceDigest: strings.Repeat("a", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: head},
			{ProviderID: "two", TrustGroup: "two", SourceDigest: strings.Repeat("b", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: head}}}
	for i := range p.Sources {
		p.Sources[i].Digest = p.Digest()
	}
	r.FeeProof = &p
	return r, p.ValidateReceipt(r)
}
func (d fundingReceiptDriver) FinalizedBlock(context.Context) (int64, string, error) {
	return 100, "canonical-block", nil
}

func TestNativeFundingConfirmationOutlivesExpiredParentAndKillSwitch(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		fundingID, sweepID, receipt := seedNativeFundingConfirmation(t, ctx, txm, f)
		registry := crypto.NewChainDriverRegistry()
		driver := fundingReceiptDriver{network: crypto.NetworkRef{ID: f.network, Code: "TEST", Family: crypto.ChainFamilyTRON,
			NetworkIdentifier: "nile", NativeTokenID: f.native, NativeDecimals: 6,
			RequiredConfirmations: 12, FinalityMode: crypto.FinalityByConfirmations}, receipt: receipt}
		require.NoError(t, registry.Register(driver))
		// No signer or authorization service, disabled default network policy,
		// an expired parent and a wallet hold: only finality/accounting may run.
		processor := &CryptoProcessor{drivers: registry, outboundRepo: NewOutboundRepo(), feeBudget: NewFeeBudgetManager(), workerID: "test"}
		confirmations := processor.nativeFundingConfirmationApplication()
		require.NoError(t, confirmations.RunBatch(ctx, "test-confirmation-1", _outboundBatchLimit))
		require.NoError(t, confirmations.RunBatch(ctx, "test-confirmation-2", _outboundBatchLimit))
		q := txm.GetQuerier(ctx)
		var status string
		var movements, settlements, costs, holds int
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM doc_crypto_native_fundings WHERE id=$1`, fundingID).Scan(&status))
		require.Equal(t, "settled", status)
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, fundingID).Scan(&movements))
		require.Equal(t, 3, movements, "one principal debit, one principal credit and one fee debit")
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_chain_settlements WHERE basis_id=$1`, fundingID).Scan(&settlements))
		require.Equal(t, 1, settlements)
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1`, fundingID).Scan(&costs))
		require.Equal(t, 1, costs)
		var reconciliationRequests int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reconciliation_requests WHERE source_id=$1`, fundingID).Scan(&reconciliationRequests))
		require.Equal(t, 2, reconciliationRequests, "both custody wallets are rechecked through the durable queue")
		var source, destination string
		require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.hot, f.native).Scan(&source))
		require.NoError(t, q.QueryRow(ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.wallet, f.native).Scan(&destination))
		require.Equal(t, "898", source)
		require.Equal(t, "200000100", destination)
		require.NoError(t, NewOutboundRepo().ExpireStaleSweepAttempts(ctx))
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM doc_crypto_sweeps WHERE id=$1`, sweepID).Scan(&status))
		require.Equal(t, "partial_failed", status)
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='open'`, f.wallet).Scan(&holds))
		require.Equal(t, 1, holds, "only reconciliation may resolve its hold")
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "free", status, "do not schedule fresh execution through a hold")
		// The owner workflow resolves the exact hold after verifying balances.
		holdsService := cryptoapp.NewOperationalHoldService(contextTxManager{}, crypto_repo.NewOperationalHoldRepo())
		require.NoError(t, holdsService.ResolveOwner(ctx, "reconciliation_incident", fundingID.String(), "reconciliation-worker", "verified test receipt"))
		require.NoError(t, NewOutboundRepo().ExpireStaleSweepAttempts(ctx))
		retry, err := NewOutboundRepo().ClaimSweepPendingWallets(ctx, 100)
		require.NoError(t, err)
		var own []SweepTask
		for _, task := range retry {
			if task.WalletID == f.wallet {
				own = append(own, task)
			}
		}
		require.Len(t, own, 1)
		require.Equal(t, f.token, own[0].TokenID)
		require.Equal(t, "999999", own[0].Balance.String())
		var fundings int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM doc_crypto_native_fundings WHERE destination_wallet_id=$1`, f.wallet).Scan(&fundings))
		require.Equal(t, 1, fundings, "retry consumes the settled balance; it does not create another JIT")
	})
}

func TestNativeFundingConfirmationLeaseFencesConcurrentWorkers(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		fundingID, _, receipt := seedNativeFundingConfirmation(t, ctx, txm, f)
		start := make(chan struct{})
		results := make(chan []crypto.NativeFunding, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, owner := range []string{"first", "second"} {
			wg.Go(func() {
				<-start
				records, err := (nativeFundingConfirmationStore{}).ClaimNativeFundingConfirmations(ctx, owner, 100)
				results <- records
				errs <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		count := 0
		for records := range results {
			for _, record := range records {
				if record.ID == fundingID {
					count++
				}
			}
		}
		require.Equal(t, 1, count)
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `UPDATE doc_crypto_native_fundings SET lease_expires_at=NOW()-interval '1 minute' WHERE id=$1`, fundingID)
		require.NoError(t, err)
		recovered, err := (nativeFundingConfirmationStore{}).ClaimNativeFundingConfirmations(ctx, "after-crash", 100)
		require.NoError(t, err)
		found := false
		for _, record := range recovered {
			found = found || record.ID == fundingID
		}
		require.True(t, found)
		require.NoError(t, (nativeFundingConfirmationStore{}).ReleaseNativeFundingConfirmation(ctx, fundingID, "first"))
		var owner string
		require.NoError(t, q.QueryRow(ctx, `SELECT claimed_by FROM doc_crypto_native_fundings WHERE id=$1`, fundingID).Scan(&owner))
		require.Equal(t, "after-crash", owner, "a stale worker cannot release a newer lease")
		_, err = cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager()).Settle(ctx, fundingID, receipt)
		require.NoError(t, err)
	})
}

func TestNativeFundingConcurrentSettlementIsOneEconomicEffect(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		fundingID, _, receipt := seedNativeFundingConfirmation(t, ctx, txm, f)
		service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
		start := make(chan struct{})
		errs := make(chan error, 20)
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() { <-start; _, err := service.Settle(ctx, fundingID, receipt); errs <- err })
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var movements int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, fundingID).Scan(&movements))
		require.Equal(t, 3, movements)
	})
}

type failingFundingPrincipalRepo struct{ nativeFundingSettlementRepo }

func (failingFundingPrincipalRepo) RecordNativeFundingPrincipal(context.Context, crypto.NativeFunding, crypto.ChainReceipt) error {
	return errors.New("injected principal failure")
}

func TestNativeFundingSettlementRollsBackFeeAndRetries(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		fundingID, _, receipt := seedNativeFundingConfirmation(t, ctx, txm, f)
		broken := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, failingFundingPrincipalRepo{}, NewFeeBudgetManager())
		_, err := broken.Settle(ctx, fundingID, receipt)
		require.ErrorContains(t, err, "injected principal failure")
		var movements int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1`, fundingID).Scan(&movements))
		require.Zero(t, movements, "fee and principal must roll back together")
		service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
		settled, err := service.Settle(ctx, fundingID, receipt)
		require.NoError(t, err)
		require.True(t, settled)
	})
}

func TestNativeFundingRevertedAndProvisionalReceipts(t *testing.T) {
	for _, state := range []string{"provisional", "reverted", "wrong_network", "cap_exceeded"} {
		t.Run(state, func(t *testing.T) {
			withSweepCoordinationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
				fundingID, _, receipt := seedNativeFundingConfirmation(t, ctx, txm, f)
				switch state {
				case "provisional":
					receipt.Finality = crypto.FinalityProvisional
				case "reverted":
					receipt.Status = crypto.ReceiptReverted
				case "wrong_network":
					receipt.NetworkID = id.New()
				case "cap_exceeded":
					receipt.Fee.TotalFee = types.NewRawAmountFromInt64(11)
				}
				receipt = reproveNetworkFee(t, ctx, receipt)
				service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
				settled, err := service.Settle(ctx, fundingID, receipt)
				require.False(t, settled)
				if state == "wrong_network" || state == "cap_exceeded" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				var principal int
				require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNativeFunding'`, fundingID).Scan(&principal))
				require.Zero(t, principal)
				var status string
				require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM doc_crypto_native_fundings WHERE id=$1`, fundingID).Scan(&status))
				want := "broadcast"
				if state == "reverted" {
					want = "failed"
				}
				if state == "cap_exceeded" {
					want = "held"
				}
				require.Equal(t, want, status)
			})
		})
	}
}

func TestNativeFundingAdditionalRoundSettlesItsOwnFirstAuthorizationOnce(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		fundingID, _, receipt := seedNativeFundingConfirmation(t, ctx, txm, f, 2)
		// Each new round is immutable from creation and has its own first auth.
		service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
		for range 2 {
			settled, err := service.Settle(ctx, fundingID, receipt)
			require.NoError(t, err)
			require.True(t, settled)
		}
		var count int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_type='CryptoNativeFunding' AND recorder_id=$1", fundingID).Scan(&count))
		require.Equal(t, 2, count, "one expense and one receipt, without repeating an earlier funding round")
	})
}
