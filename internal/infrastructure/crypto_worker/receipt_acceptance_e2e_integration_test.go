//go:build integration

package crypto_worker

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/cryptoposting"
	infraNumerator "metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// The acceptance scenarios of the receipt accounting drive the fully wired
// worker of paymentE2EFixture through the watcher, the observation queue, the
// confirmation loop and the receipt intake on a real database. Only the
// network is faked: e2eChainDriver answers every proof from two independent
// trust groups.

func (f paymentE2EFixture) freeWallet(t *testing.T, role string) (id.ID, string) {
	t.Helper()
	walletID, address := id.New(), testTRONAddress()
	tier := "pool"
	if role != "merchant_pool" {
		tier = "hot"
	}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,tier,wallet_role,status,vault_key_id,
		allocation_mode,is_active,chain_activation_status)
		VALUES($1,$2,'Acceptance wallet',$3,$4,$4,$5,$6,'free','test/acceptance','transient',TRUE,'active')`,
		walletID, "ACC-"+id.Compact(walletID), f.network, address, tier, role)
	require.NoError(t, err)
	return walletID, address
}

// incoming is a provisional watcher observation of a transfer to a managed wallet.
func (f paymentE2EFixture) incoming(to string, native bool, amount string) crypto.BlockchainEvent {
	event := f.transfer(100, e2eBlockHash("b"), crypto.FinalityProvisional)
	event.TxHash = strings.ReplaceAll(id.New().String()+id.New().String(), "-", "")
	event.ToAddress, event.Amount = to, types.MustCryptoAmount(amount)
	if native {
		event.EventKind, event.TokenContract = crypto.EventKindNativeValue, ""
	}
	return event
}

type acceptanceInbox struct {
	status, reason string
	receipt        *id.ID
	receiptError   string
}

func (f paymentE2EFixture) inbox(t *testing.T, txHash string) acceptanceInbox {
	t.Helper()
	var row acceptanceInbox
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT match_status,COALESCE(match_reason,''),receipt_id,COALESCE(receipt_error,'')
		FROM reg_crypto_inbound_chain_events WHERE network_id=$1 AND tx_hash=$2`, f.network, txHash).
		Scan(&row.status, &row.reason, &row.receipt, &row.receiptError))
	return row
}

// age moves an observation past the late invoice matching window.
func (f paymentE2EFixture) age(t *testing.T, txHash string) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_inbound_chain_events SET created_at=NOW()-$3::interval,receipt_check_at=NULL
		WHERE network_id=$1 AND tx_hash=$2`, f.network, txHash, (_receiptUnallocatedGrace + time.Minute).String())
	require.NoError(t, err)
}

func (f paymentE2EFixture) intake(t *testing.T) int {
	t.Helper()
	recorded, err := f.processor.receipts.poll(f.ctx, f.processor.drivers)
	require.NoError(t, err)
	return recorded
}

func (f paymentE2EFixture) receiptAmounts(t *testing.T, receiptID id.ID) (amount, suspense, reason, token string) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT d.amount::text,s.amount::text,d.reason_code,d.token_id::text
		FROM doc_crypto_custody_receipts d JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id=d.id WHERE d.id=$1`, receiptID).
		Scan(&amount, &suspense, &reason, &token))
	return
}

func (f paymentE2EFixture) merchantMoney(t *testing.T) (merchant, fee string) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
		COALESCE((SELECT SUM(amount) FROM reg_crypto_merchant_balance_movements WHERE merchant_id=$1),0)::text,
		COALESCE((SELECT SUM(amount) FROM reg_crypto_fee_movements WHERE merchant_id=$1),0)::text`, f.merchant).Scan(&merchant, &fee))
	return
}

func (f paymentE2EFixture) journal(t *testing.T, includeDust bool) []cryptoapp.UnidentifiedReceiptRow {
	t.Helper()
	list, err := cryptoapp.NewReceiptJournal(crypto_repo.NewUnidentifiedReceiptRepo(), crypto_repo.NewWalletFundingJournalRepo()).
		List(f.ctx, cryptoapp.ReceiptJournalOpen, includeDust, 50, 0)
	require.NoError(t, err)
	return list.Items
}

// restart replaces the worker with a fresh process over the same database.
func (f paymentE2EFixture) restart(t *testing.T) paymentE2EFixture {
	t.Helper()
	drivers := f.processor.drivers
	f.processor = NewCryptoProcessor(CryptoProcessorConfig{TenantID: "e2e",
		ApprovalPolicyPublicKey: e2eApprovalKey(t), IntentSigningPublicKey: e2eApprovalKey(t),
		RiskAssessmentSigningPublicKey: e2eApprovalKey(t), PolicyDecisionSigningPublicKey: e2eApprovalKey(t),
		AdminApprovalSigningPublicKey: e2eApprovalKey(t), RiskApprovalSigningPublicKey: e2eApprovalKey(t)}, nil)
	f.processor.drivers = drivers
	return f
}

// A01: a full invoice payment produces one receipt, credits the merchant net
// and the platform the fee, and completes the invoice only at finality. A
// replayed observation after a worker restart changes nothing.
func TestAcceptanceA01FullInvoicePayment(t *testing.T) {
	f := newPaymentE2EFixture(t)
	settled := f.settle(t)
	require.Equal(t, crypto_payment.PaymentStatusSettled, settled.payment)
	require.Equal(t, "completed", settled.invoice)
	require.Equal(t, []string{"97000000", "3000000", "0", "100000000", "1"},
		[]string{settled.merchant, settled.fee, settled.suspense, settled.custody, itoa(settled.receipts)})
	require.Empty(t, f.journal(t, true), "an attributed receipt leaves the open journal")

	f = f.restart(t)
	f.watch(t, f.transfer(100, e2eBlockHash("b"), crypto.FinalityProvisional))
	f.confirm(t)
	require.Zero(t, f.intake(t))
	again := f.read(t)
	require.Equal(t, []string{"97000000", "3000000", "0", "100000000", "1"},
		[]string{again.merchant, again.fee, again.suspense, again.custody, itoa(again.receipts)})
	require.Equal(t, *settled.receipt, *again.receipt)
}

// A02: a transfer to a free pool wallet waits out the late invoice window,
// then becomes a receipt in suspense without any merchant or platform money.
// The two-operator credit above the threshold is covered by
// TestMerchantCreditAboveThresholdNeedsAnotherOperator.
func TestAcceptanceA02TransferWithoutInvoice(t *testing.T) {
	f := newPaymentE2EFixture(t)
	wallet, address := f.freeWallet(t, "merchant_pool")
	event := f.incoming(address, false, "40000000")
	f.watch(t, event)
	row := f.inbox(t, event.TxHash)
	require.Equal(t, []string{"unmatched", crypto.MatchReasonNoAllocation}, []string{row.status, row.reason})

	require.Zero(t, f.intake(t), "a late invoice may still claim the transfer")
	f.age(t, event.TxHash)
	require.Equal(t, 1, f.intake(t), f.inbox(t, event.TxHash).receiptError)
	row = f.inbox(t, event.TxHash)
	require.NotNil(t, row.receipt)
	amount, suspense, reason, _ := f.receiptAmounts(t, *row.receipt)
	require.Equal(t, []string{"40000000", "40000000", crypto.MatchReasonNoAllocation}, []string{amount, suspense, reason})
	merchant, fee := f.merchantMoney(t)
	require.Equal(t, []string{"0", "0"}, []string{merchant, fee})
	var custody string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`,
		wallet, f.token).Scan(&custody))
	require.Equal(t, "40000000", custody)
	journal := f.journal(t, false)
	require.Len(t, journal, 1)
	require.Equal(t, "40000000", journal[0].Remainder)
	require.Equal(t, event.TxHash, journal[0].TxHash)
}

// A03: TRX sent to a wallet leased for a USDT invoice is accounted as TRX in
// suspense at once, and the invoice stays unpaid.
func TestAcceptanceA03WrongToken(t *testing.T) {
	f := newPaymentE2EFixture(t)
	event := f.incoming(f.walletAddress, true, "100000000")
	f.watch(t, event)
	row := f.inbox(t, event.TxHash)
	require.Equal(t, []string{"unmatched", crypto.MatchReasonWrongToken}, []string{row.status, row.reason})
	require.Equal(t, 1, f.intake(t), f.inbox(t, event.TxHash).receiptError)

	row = f.inbox(t, event.TxHash)
	amount, suspense, reason, token := f.receiptAmounts(t, *row.receipt)
	require.Equal(t, []string{"100000000", "100000000", crypto.MatchReasonWrongToken, f.native.String()},
		[]string{amount, suspense, reason, token}, "the actual TRX is accounted, not the invoiced USDT")
	var invoice string
	var payments int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM doc_crypto_payments WHERE invoice_id=$1)
		FROM doc_crypto_invoices WHERE id=$1`, f.invoice).Scan(&invoice, &payments))
	require.Equal(t, "created", invoice)
	require.Zero(t, payments)
	merchant, fee := f.merchantMoney(t)
	require.Equal(t, []string{"0", "0"}, []string{merchant, fee})
}

// A05: a transfer below the dust threshold is accounted exactly in minor
// units and hidden from the journal by default.
func TestAcceptanceA05Dust(t *testing.T) {
	f := newPaymentE2EFixture(t)
	event := f.incoming(f.walletAddress, false, "999")
	f.watch(t, event)
	row := f.inbox(t, event.TxHash)
	require.Equal(t, []string{"unmatched", crypto.MatchReasonDust}, []string{row.status, row.reason})
	require.Equal(t, 1, f.intake(t), f.inbox(t, event.TxHash).receiptError)

	row = f.inbox(t, event.TxHash)
	amount, suspense, reason, _ := f.receiptAmounts(t, *row.receipt)
	require.Equal(t, []string{"999", "999", crypto.MatchReasonDust}, []string{amount, suspense, reason})
	require.Empty(t, f.journal(t, false), "dust is hidden by default")
	all := f.journal(t, true)
	require.Len(t, all, 1)
	require.Equal(t, "999", all[0].Amount)
	var invoice string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM doc_crypto_invoices WHERE id=$1`, f.invoice).Scan(&invoice))
	require.Equal(t, "created", invoice, "dust never pays an invoice")
}

// A10: a transfer between managed wallets never becomes an external receipt,
// neither when the watcher labels it internal nor when it reports it as
// incoming because the sender was not yet known to it. Sweep and JIT funding
// recognition under concurrency are covered by the SweepV2 and NativeFunding
// recognition tests.
func TestAcceptanceA10InternalTransferIsNotAnExternalReceipt(t *testing.T) {
	f := newPaymentE2EFixture(t)
	_, sender := f.freeWallet(t, "liquidity_hot")
	_, address := f.freeWallet(t, "merchant_pool")

	labelled := f.incoming(address, false, "50000000")
	labelled.FromAddress, labelled.Direction = sender, crypto.TransferDirectionInternal
	f.watch(t, labelled)
	var rows int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_inbound_chain_events WHERE tx_hash=$1`, labelled.TxHash).Scan(&rows))
	require.Zero(t, rows)

	mislabelled := f.incoming(address, false, "50000000")
	mislabelled.FromAddress = sender
	f.watch(t, mislabelled)
	f.age(t, mislabelled.TxHash)
	require.Zero(t, f.intake(t))
	row := f.inbox(t, mislabelled.TxHash)
	require.Nil(t, row.receipt)
	require.Contains(t, row.receiptError, "internal transfer")
	var receipts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM doc_crypto_custody_receipts WHERE network_id=$1`, f.network).Scan(&receipts))
	require.Zero(t, receipts)
}

// A16: suspense is consumed only by an attribution of its receipt. A
// withdrawal, a sweep, a funding or a network operation cannot write it, with
// or without AML, because the register admits only attribution recorders.
func TestAcceptanceA16SuspenseIsNotSpentByOutflows(t *testing.T) {
	f := newPaymentE2EFixture(t)
	event := f.incoming(f.walletAddress, true, "100000000")
	f.watch(t, event)
	require.Equal(t, 1, f.intake(t))
	receipt := *f.inbox(t, event.TxHash).receipt

	for _, recorder := range []string{"CryptoWithdrawal", "TreasuryWithdrawal", "CryptoSweep", "CryptoNativeFunding",
		"CryptoNetworkOperation", "ChainSettlement", "CryptoCustodyAdjustment"} {
		_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
			VALUES($1,$2,1,NOW(),'expense',$3,$4,1)`, id.New(), recorder, receipt, f.native)
		require.Error(t, err, recorder)
	}
	_, suspense, _, _ := f.receiptAmounts(t, receipt)
	require.Equal(t, "100000000", suspense)
}

// A33: a worker that dies between the proof and the posting leaves nothing
// but the saved proof; the restarted worker accounts the transfer once, and
// replays of the observation and the intake never add a second receipt.
func TestAcceptanceA33CrashBetweenProofAndPosting(t *testing.T) {
	f := newPaymentE2EFixture(t)
	event := f.incoming(f.walletAddress, true, "100000000")
	f.watch(t, event)

	// The broken worker has no suspense recorder: its posting fails inside the
	// receipt transaction after the custody lines were written.
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	broken := newReceiptIntake(engine, infraNumerator.New())
	recorded, err := broken.poll(f.ctx, f.processor.drivers)
	require.NoError(t, err)
	require.Zero(t, recorded)
	var receipts, custody, proofs int
	counts := func() {
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT
			(SELECT count(*) FROM doc_crypto_custody_receipts WHERE network_id=$1),
			(SELECT count(*) FROM reg_crypto_balance_movements WHERE wallet_id=$2 AND recorder_type='CryptoCustodyReceipt'),
			(SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1 AND tx_hash=$3)`,
			f.network, f.wallet, event.TxHash).Scan(&receipts, &custody, &proofs))
	}
	counts()
	require.Equal(t, []int{0, 0}, []int{receipts, custody}, "a failed posting rolls back every financial line")
	require.Positive(t, proofs, "the proof obtained before the transaction survives")
	require.NotEmpty(t, f.inbox(t, event.TxHash).receiptError)

	f = f.restart(t)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_inbound_chain_events SET receipt_check_at=NULL WHERE tx_hash=$1`, event.TxHash)
	require.NoError(t, err)
	require.Equal(t, 1, f.intake(t), f.inbox(t, event.TxHash).receiptError)
	f.watch(t, event)
	f.age(t, event.TxHash)
	require.Zero(t, f.intake(t))
	counts()
	require.Equal(t, []int{1, 1}, []int{receipts, custody})
	_, suspense, _, _ := f.receiptAmounts(t, *f.inbox(t, event.TxHash).receipt)
	require.Equal(t, "100000000", suspense)
}
