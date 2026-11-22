package crypto_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
)

type SweepCustodyRepo struct{ *BlockIngestionRepo }

func NewSweepCustodyRepo() *SweepCustodyRepo { return &SweepCustodyRepo{NewBlockIngestionRepo()} }

func (*SweepCustodyRepo) ExpectedSweepTransfer(ctx context.Context, lineID id.ID, receipt crypto.ChainReceipt) (crypto.BlockchainEvent, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		return crypto.BlockchainEvent{}, apperror.NewValidation("sweep proof must be read before the financial transaction")
	}
	e := crypto.BlockchainEvent{EventType: crypto.EventTypeTransfer, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash,
		Confirmations: receipt.Confirmations, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical}
	var family, standard string
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT a.network_id,l.tx_hash,l.amount,a.source_address,a.destination_address,COALESCE(t.contract_address,''),n.chain_family,t.token_standard
		FROM doc_crypto_sweeps__lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id
		JOIN reg_crypto_sweep_attempts a ON a.id=l.line_id AND a.sweep_id=s.id
		JOIN cat_tokens t ON t.id=s.token_id JOIN cat_blockchain_networks n ON n.id=t.network_id
		WHERE l.line_id=$1 AND a.network_id=$2 AND l.tx_hash=$3`, lineID, receipt.NetworkID, receipt.TxHash).
		Scan(&e.NetworkID, &e.TxHash, &e.Amount, &e.FromAddress, &e.ToAddress, &e.TokenContract, &family, &standard)
	if standard == "native" {
		e.EventKind = crypto.EventKindNativeValue
	} else {
		e.EventKind = crypto.EventKindContractLog
	}
	if err != nil {
		return e, err
	}
	e.DecoderVersion, err = crypto.TransferEvidenceSchemaFor(crypto.ChainFamily(family))
	return e, err
}

func (*SweepCustodyRepo) Prepare(ctx context.Context, b crypto.SweepAccountingBasis, receipt crypto.ChainReceipt) (bool, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return false, apperror.NewValidation("sweep settlement requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoSweep", b.DocumentID); err != nil {
		return false, err
	}
	var sameOwner bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets s JOIN cat_wallets d ON d.id=$2
		WHERE s.id=$1 AND s.network_id=$3 AND d.network_id=$3 AND s.custody_merchant_id IS NOT DISTINCT FROM d.custody_merchant_id)`, b.SourceWalletID, b.DestinationWalletID, b.NetworkID).Scan(&sameOwner); err != nil {
		return false, err
	}
	if !sameOwner {
		return false, apperror.NewConflict("sweep wallets require the same network and custody owner")
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT $1::uuid UNION SELECT $2::uuid UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations
		WHERE basis_type='CryptoSweepAttempt' AND basis_id=$3 AND purpose='sweep' AND attempt=1))`, b.SourceWalletID, b.DestinationWalletID, b.LineID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, "SELECT id FROM doc_crypto_sweeps WHERE id=$1 FOR UPDATE", b.DocumentID); err != nil {
		return false, err
	}
	var status, result string
	var valid bool
	err := q.QueryRow(ctx, `SELECT l.settlement_status,l.chain_result,
		 s.token_id=$3 AND s.hot_wallet_id=$4 AND l.wallet_id=$5 AND l.tx_hash=$6 AND l.amount=$7
		 FROM doc_crypto_sweeps__lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id WHERE l.line_id=$1 AND s.id=$2 FOR UPDATE OF l`, b.LineID, b.DocumentID, b.TokenID, b.DestinationWalletID, b.SourceWalletID, b.TxHash, b.Amount).Scan(&status, &result, &valid)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, crypto.ErrVerificationMismatch
	}
	if status == "recorded" {
		var matches bool
		err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements c WHERE c.basis_type='CryptoSweepAttempt' AND c.basis_id=$1
			AND c.tx_hash=$2 AND c.block_hash=$3 AND c.block_height=$4 AND c.status=$5 AND c.total_fee_minor=$6)`, b.LineID, b.TxHash, receipt.BlockHash, receipt.BlockHeight, receipt.Status, receipt.Fee.TotalFee).Scan(&matches)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, apperror.NewConflict("sweep settlement replay conflicts with its final occurrence")
		}
		return false, nil
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoSweepAttempt' AND basis_id=$1 AND purpose='sweep' AND attempt=1 FOR UPDATE`, b.LineID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_receipt_balance(w,t) FROM (
		SELECT $1::uuid w,$3::uuid t UNION SELECT $2::uuid,$3::uuid UNION SELECT $1::uuid,$4::uuid) keys ORDER BY w,t`, b.SourceWalletID, b.DestinationWalletID, b.TokenID, receipt.Fee.NativeAsset.TokenID); err != nil {
		return false, err
	}
	if receipt.Status == crypto.ReceiptReverted {
		return true, nil
	}
	if b.VerificationID == nil {
		return false, crypto.ErrVerificationPending
	}
	var raw []byte
	if err := q.QueryRow(ctx, "SELECT evidence FROM reg_crypto_transfer_verifications WHERE id=$1", b.VerificationID).Scan(&raw); err != nil {
		return false, err
	}
	var proof crypto.TransferVerification
	if err := json.Unmarshal(raw, &proof); err != nil {
		return false, err
	}
	if err := proof.Validate(); err != nil {
		return false, err
	}
	e := proof.Event
	basis := crypto.AccountingTransferBasis{NetworkID: b.NetworkID, WalletID: b.DestinationWalletID, TokenID: b.TokenID, TxHash: b.TxHash,
		EventKind: e.EventKind, EventIndex: e.EventIndex, FromAddress: e.FromAddress, Amount: b.Amount, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, RequiredConfs: 1, VerificationID: b.VerificationID}
	if err := NewCustodyRecognitionRepo().LockIncoming(ctx, basis); err != nil {
		return false, err
	}
	e, err = NewBlockIngestionRepo().ExpectedAccountingTransfer(ctx, basis)
	if err != nil {
		return false, err
	}
	e.VerificationID = b.VerificationID
	e.Confirmations = receipt.Confirmations
	e.Finality = crypto.FinalitySolidified
	e.Canonicality = crypto.CanonicalityCanonical
	evidence, err := NewBlockIngestionRepo().AuthorizeSettlementCustodyEffect(ctx, basis, e)
	if err != nil {
		return false, err
	}
	posting := id.New()
	source, destination := sweepPrincipalLines(b.LineID)
	if err := NewCustodyRecognitionRepo().ClaimIncoming(ctx, evidence, posting, b.DocumentID, "CryptoSweep", b.DestinationWalletID, b.TokenID, destination); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_recognitions(posting_id,wallet_id,token_id,record_type,movement_line_id) VALUES($1,$2,$3,'expense',$4)`, posting, b.SourceWalletID, b.TokenID, source); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_sweep_custody_bindings(posting_id,sweep_id,line_id,period) VALUES($1,$2,$3,$4)`, posting, b.DocumentID, b.LineID, receipt.ObservedAt); err != nil {
		return false, err
	}
	err = postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoSweep", AggregateID: b.DocumentID, EventType: "custody_recognized", Payload: map[string]any{
		"entityType": "CryptoSweep", "entityId": b.DocumentID.String(), "action": "custody_recognized", "lineId": b.LineID, "postingId": posting, "generation": 1}})
	return err == nil, err
}

func (*SweepCustodyRepo) RecordPrincipal(ctx context.Context, b crypto.SweepAccountingBasis, receipt crypto.ChainReceipt) error {
	source, destination := sweepPrincipalLines(b.LineID)
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,$3,'CryptoSweep',1,$4,'expense',$5,$7,$8),($2,$3,'CryptoSweep',1,$4,'receipt',$6,$7,$8)`, source, destination, b.DocumentID, receipt.ObservedAt, b.SourceWalletID, b.DestinationWalletID, b.TokenID, b.Amount)
	if err != nil {
		return fmt.Errorf("record proven sweep principal: %w", err)
	}
	return nil
}

func sweepPrincipalLines(lineID id.ID) (id.ID, id.ID) {
	return uuid.NewSHA1(lineID, []byte("sweep-source")), uuid.NewSHA1(lineID, []byte("sweep-destination"))
}
