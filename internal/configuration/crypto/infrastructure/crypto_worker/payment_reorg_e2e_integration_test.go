//go:build integration

package crypto_worker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// e2eChainState is what the fake network answers about the payment transfer.
type e2eChainState int

const (
	e2eCanonical  e2eChainState = iota + 1 // in block 100
	e2eOrphaned                            // block 100 no longer holds it
	e2eReincluded                          // in block 101
)

// e2eChainDriver proves the payment transfer according to the current chain
// state, from two independent sources, outside any database transaction.
type e2eChainDriver struct {
	receiptProofDriver
	state *e2eChainState
}

func (d e2eChainDriver) VerifyTransfer(ctx context.Context, event crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	proof, err := d.receiptProofDriver.VerifyTransfer(ctx, event)
	if err != nil {
		return proof, err
	}
	switch *d.state {
	case e2eCanonical:
		proof.Event.BlockNumber, proof.Event.BlockHash = 100, e2eBlockHash("b")
	case e2eOrphaned:
		proof.Event.BlockNumber, proof.Event.BlockHash = 100, e2eBlockHash("b")
		proof.Event.Canonicality, proof.Event.EventType = crypto.CanonicalityOrphaned, crypto.EventTypeReorg
		proof.Outcome, proof.AccountingIdentity = "absent", nil
	case e2eReincluded:
		proof.Event.BlockNumber, proof.Event.BlockHash = 101, e2eBlockHash("d")
		proof.Event.Confirmations = 19
		identity, identityErr := crypto.NewAccountingEffectIdentity(proof.Event, crypto.ChainFamilyTRON, 0)
		if identityErr != nil {
			return proof, identityErr
		}
		proof.AccountingIdentity = &identity
	}
	// Both sources saw the same head at height 119.
	proof.Event.Confirmations = int(119 - proof.Event.BlockNumber + 1)
	return proof, nil
}

func e2eBlockHash(fill string) string { return strings.Repeat(fill, 64) }

type paymentE2EFixture struct {
	ctx                                      context.Context
	pool                                     *pgxpool.Pool
	processor                                *CryptoProcessor
	state                                    *e2eChainState
	network, native, token, merchant, wallet id.ID
	invoice                                  id.ID
	walletAddress, contract, payer, txHash   string
}

func e2eApprovalKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(public)
}

// newPaymentE2EFixture is a TRON network with independent proof, a leased
// deposit wallet and a 100 USDT invoice of a merchant with a 3 USDT tariff,
// served by a fully wired worker.
func newPaymentE2EFixture(t *testing.T) paymentE2EFixture {
	t.Helper()
	ctx, pool := custodyTestDatabase(t)
	f := paymentE2EFixture{ctx: ctx, pool: pool, network: id.New(), native: id.New(), token: id.New(), merchant: id.New(),
		wallet: id.New(), invoice: id.New(), walletAddress: testTRONAddress(), payer: testTRONAddress()}
	suffix := id.Compact(f.network)
	f.contract, f.txHash = "E2E-"+suffix+"-contract", strings.ReplaceAll(id.New().String()+id.New().String(), "-", "")
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed)
		VALUES($1,$2,'Payment E2E','tron',$2,'TRX',6,19);
		INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
		VALUES($3,$2||'-N','Native',$1,'TRX',6,'native',''),($4,$2||'-T','Token',$1,'USDT',6,'TRC-20',$5);
		INSERT INTO cat_merchants(id,code,name) VALUES($6,$2,'Payment E2E merchant');
		INSERT INTO reg_fee_schedule(merchant_id,token_id,direction,fixed_fee) VALUES($6,$4,'processing',3000000);
		INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,tier,wallet_role,status,vault_key_id,
			allocation_mode,is_active,chain_activation_status,leased_for_id,leased_until)
		VALUES($7,$2||'-W','Deposit',$1,$8,$8,'pool','merchant_pool','leased','test/deposit','transient',TRUE,'active',$9,$10::timestamptz+interval '1 hour');
		INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,status,expires_at,ttl_minutes,creation_source)
		VALUES($9,$11,$2,$10::timestamptz,$6,$4,$7,100000000,'created',$10::timestamptz+interval '1 hour',60,'portal');
		INSERT INTO reg_crypto_wallet_allocations(id,wallet_id,invoice_id,merchant_id,token_id,network_id,leased_at)
		VALUES(gen_random_uuid_v7(),$7,$9,$6,$4,$1,$10::timestamptz-interval '1 minute');
		INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version)
		VALUES($1,0,'e2e',0,'shadow',2);
	`, pgx.QueryExecModeSimpleProtocol, f.network, "E2E-"+suffix, f.native, f.token, f.contract, f.merchant,
		f.wallet, f.walletAddress, f.invoice, now, "ci_"+strings.ReplaceAll(f.invoice.String(), "-", ""))
	require.NoError(t, err)

	state := e2eCanonical
	f.state = &state
	calls := 0
	registry := crypto.NewChainDriverRegistry()
	require.NoError(t, registry.Register(e2eChainDriver{state: f.state, receiptProofDriver: receiptProofDriver{calls: &calls,
		fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{ID: f.network, Code: "E2E", Family: crypto.ChainFamilyTRON,
			NetworkIdentifier: "nile", NativeTokenID: f.native, NativeDecimals: 6, RequiredConfirmations: 19,
			FinalityMode: crypto.FinalityByConfirmations}, receipt: crypto.ChainReceipt{ObservedAt: now}}}}))
	f.processor = NewCryptoProcessor(CryptoProcessorConfig{TenantID: "e2e",
		ApprovalPolicyPublicKey: e2eApprovalKey(t), IntentSigningPublicKey: e2eApprovalKey(t),
		RiskAssessmentSigningPublicKey: e2eApprovalKey(t), PolicyDecisionSigningPublicKey: e2eApprovalKey(t),
		AdminApprovalSigningPublicKey: e2eApprovalKey(t), RiskApprovalSigningPublicKey: e2eApprovalKey(t)}, nil)
	f.processor.drivers = registry
	return f
}

// watch delivers a watcher observation and processes the observation queue.
func (f paymentE2EFixture) watch(t *testing.T, event crypto.BlockchainEvent) {
	t.Helper()
	require.NoError(t, f.processor.Process(f.ctx, event))
	require.NoError(t, f.processor.processChainObservations(f.ctx))
}

func (f paymentE2EFixture) transfer(block int64, hash string, finality crypto.FinalityStatus) crypto.BlockchainEvent {
	return crypto.BlockchainEvent{NetworkID: f.network, TxHash: f.txHash, EventKind: crypto.EventKindContractLog, EventIndex: 0,
		FromAddress: f.payer, ToAddress: f.walletAddress, TokenContract: f.contract, Amount: types.MustRawAmount("100000000"),
		BlockNumber: block, BlockHash: hash, Confirmations: 1, RequiredConfs: 19, Finality: finality,
		Canonicality: crypto.CanonicalityCanonical, EventType: crypto.EventTypeTransfer, Direction: crypto.TransferDirectionIncoming,
		DecoderVersion: crypto.TRONTransferEvidenceSchema, Timestamp: time.Now().UTC()}
}

// confirm runs the confirmation loop until the payment is due again.
func (f paymentE2EFixture) confirm(t *testing.T) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `UPDATE doc_crypto_payments SET next_confirmation_check_at=NOW() WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	require.NoError(t, f.processor.pollConfirmations(f.ctx))
}

type paymentE2EState struct {
	payment                          crypto_payment.PaymentStatus
	invoice                          string
	merchant, fee, suspense, custody string
	receipts                         int
	receipt                          *id.ID
}

func (f paymentE2EFixture) read(t *testing.T) paymentE2EState {
	t.Helper()
	var s paymentE2EState
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT p.status,i.status,p.receipt_id,
		COALESCE((SELECT amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$2 AND token_id=$3),0)::text,
		COALESCE((SELECT balance FROM reg_crypto_fee_balances WHERE merchant_id=$2 AND token_id=$3 AND fee_type='processing'),0)::text,
		COALESCE((SELECT SUM(s.amount) FROM reg_crypto_receipt_suspense_balances s JOIN doc_crypto_custody_receipts d ON d.id=s.receipt_id WHERE d.wallet_id=$4),0)::text,
		COALESCE((SELECT amount FROM reg_crypto_balance_balances WHERE wallet_id=$4 AND token_id=$3),0)::text,
		(SELECT count(*) FROM doc_crypto_custody_receipts WHERE wallet_id=$4)
		FROM doc_crypto_payments p JOIN doc_crypto_invoices i ON i.id=p.invoice_id WHERE p.invoice_id=$1`,
		f.invoice, f.merchant, f.token, f.wallet).Scan(&s.payment, &s.invoice, &s.receipt, &s.merchant, &s.fee, &s.suspense, &s.custody, &s.receipts))
	return s
}

func (f paymentE2EFixture) networkHolds(t *testing.T, reason string) int {
	t.Helper()
	var holds int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND reason=$2 AND status='open'`,
		f.network, reason).Scan(&holds))
	return holds
}

// settle detects the transfer at block 100 and confirms it to finality.
func (f paymentE2EFixture) settle(t *testing.T) paymentE2EState {
	t.Helper()
	f.watch(t, f.transfer(100, e2eBlockHash("b"), crypto.FinalityProvisional))
	f.requireUnsettled(t)
	f.confirm(t)
	s := f.read(t)
	require.NotNil(t, s.receipt)
	return s
}

func TestPaymentE2EReorgCompensatesAndReinclusionCreditsAgain(t *testing.T) {
	f := newPaymentE2EFixture(t)
	settled := f.settle(t)
	require.Equal(t, crypto_payment.PaymentStatusSettled, settled.payment)
	require.Equal(t, "completed", settled.invoice)
	require.Equal(t, []string{"97000000", "3000000", "0", "100000000", "1"},
		[]string{settled.merchant, settled.fee, settled.suspense, settled.custody, itoa(settled.receipts)},
		"one receipt; the payment credits the merchant net and the platform the fee")

	// The watcher reports the block gone; the absence is proven independently.
	*f.state = e2eOrphaned
	reorg := f.transfer(100, e2eBlockHash("c"), crypto.FinalityProvisional)
	reorg.EventType, reorg.Canonicality, reorg.Amount = crypto.EventTypeReorg, crypto.CanonicalityOrphaned, types.ZeroRawAmount()
	f.watch(t, reorg)
	reorged := f.read(t)
	require.Equal(t, crypto_payment.PaymentStatusReorged, reorged.payment)
	require.Equal(t, []string{"0", "0", "100000000", "100000000"}, []string{reorged.merchant, reorged.fee, reorged.suspense, reorged.custody},
		"the merchant credit and fee are reversed, the receipt keeps custody")
	require.Equal(t, 1, f.networkHolds(t, "posted_payment_reorg"))
	journal, err := cryptoapp.NewReceiptJournal(crypto_repo.NewUnidentifiedReceiptRepo(), crypto_repo.NewWalletFundingJournalRepo()).List(f.ctx, cryptoapp.ReceiptJournalOpen, false, 50, 0)
	require.NoError(t, err)
	require.Len(t, journal.Items, 1)
	require.Equal(t, "payment_reorged", journal.Items[0].State, "no manual credit while the payment may come back")

	// The same transfer is mined again in block 101.
	*f.state = e2eReincluded
	f.watch(t, f.transfer(101, e2eBlockHash("d"), crypto.FinalityProvisional))
	f.requireUnsettled(t)
	f.confirm(t)
	again := f.read(t)
	require.Equal(t, crypto_payment.PaymentStatusSettled, again.payment)
	require.Equal(t, "completed", again.invoice)
	require.Equal(t, []string{"97000000", "3000000", "0", "100000000", "1"},
		[]string{again.merchant, again.fee, again.suspense, again.custody, itoa(again.receipts)})
	require.Equal(t, *settled.receipt, *again.receipt, "the re-included transfer is attributed through the same receipt")
	var anchored string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT v.block_hash FROM reg_crypto_custody_receipt_audits a
		JOIN reg_crypto_transfer_verifications v ON v.id=a.anchor_verification_id WHERE a.receipt_id=$1`, *again.receipt).Scan(&anchored))
	require.Equal(t, e2eBlockHash("d"), anchored, "the receipt audit follows the transfer into block 101")
}

func TestPaymentE2EReceiptAuditOrphanCompensatesPaymentInOneTransaction(t *testing.T) {
	f := newPaymentE2EFixture(t)
	settled := f.settle(t)
	require.Equal(t, "97000000", settled.merchant)

	// The receipt audit, the only absence check of a posted payment, finds the
	// transfer gone at its next run.
	*f.state = e2eOrphaned
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET next_check_at=NOW() WHERE receipt_id=$1`, *settled.receipt)
	require.NoError(t, err)
	orphaned, err := f.processor.receipts.auditCanonicality(f.ctx, f.processor.drivers, "e2e-audit")
	require.NoError(t, err)
	require.Equal(t, 1, orphaned)
	s := f.read(t)
	require.Equal(t, crypto_payment.PaymentStatusReorged, s.payment)
	require.Equal(t, []string{"0", "0", "100000000"}, []string{s.merchant, s.fee, s.suspense})
	var audit string
	var incidents int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT a.status,(SELECT count(*) FROM reg_crypto_security_incidents WHERE network_id=$2 AND tx_hash=$3)
		FROM reg_crypto_custody_receipt_audits a WHERE a.receipt_id=$1`, *settled.receipt, f.network, f.txHash).Scan(&audit, &incidents))
	require.Equal(t, "orphaned", audit)
	require.Equal(t, 2, incidents, "the receipt and the payment incidents are opened by one audit transaction")
	require.Equal(t, 1, f.networkHolds(t, "posted_payment_reorg"))
	require.Equal(t, 1, f.networkHolds(t, "deep_reorg"))

	// A repeated audit pass finds nothing left to do.
	orphaned, err = f.processor.receipts.auditCanonicality(f.ctx, f.processor.drivers, "e2e-audit")
	require.NoError(t, err)
	require.Zero(t, orphaned)
}

func TestPaymentE2EReceiptAuditRollsBackOrphanWhenPaymentCannotBeCompensated(t *testing.T) {
	f := newPaymentE2EFixture(t)
	settled := f.settle(t)
	*f.state = e2eOrphaned
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_custody_receipt_audits SET next_check_at=NOW() WHERE receipt_id=$1`, *settled.receipt)
	require.NoError(t, err)
	f.processor.receipts.paymentReorg = nil
	orphaned, err := f.processor.receipts.auditCanonicality(f.ctx, f.processor.drivers, "e2e-audit")
	require.NoError(t, err)
	require.Zero(t, orphaned)
	var audit, lastError string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,last_error FROM reg_crypto_custody_receipt_audits WHERE receipt_id=$1`,
		*settled.receipt).Scan(&audit, &lastError))
	require.Equal(t, "canonical", audit, "the receipt is never orphaned while its payment keeps the merchant credit")
	require.Contains(t, lastError, "without a payment reorg handler")
	require.Equal(t, "97000000", f.read(t).merchant)
}

func itoa(v int) string { return types.NewRawAmountFromInt64(int64(v)).String() }

// requireUnsettled: the provisional observation created (or reactivated) the
// payment, but nothing is credited before finality.
func (f paymentE2EFixture) requireUnsettled(t *testing.T) {
	t.Helper()
	s := f.read(t)
	require.Contains(t, []crypto_payment.PaymentStatus{crypto_payment.PaymentStatusDetected, crypto_payment.PaymentStatusConfirming}, s.payment)
	require.Equal(t, []string{"0", "0"}, []string{s.merchant, s.fee})
}
