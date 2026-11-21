package crypto_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
)

type WithdrawalCustodyRepo struct{ *BlockIngestionRepo }

func NewWithdrawalCustodyRepo() *WithdrawalCustodyRepo {
	return &WithdrawalCustodyRepo{NewBlockIngestionRepo()}
}

func (*WithdrawalCustodyRepo) ExpectedWithdrawalTransfer(ctx context.Context, document id.ID, receipt crypto.ChainReceipt) (crypto.BlockchainEvent, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		return crypto.BlockchainEvent{}, apperror.NewValidation("withdrawal proof must be read before the financial transaction")
	}
	e := crypto.BlockchainEvent{EventType: crypto.EventTypeTransfer, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, Confirmations: receipt.Confirmations, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical}
	var family, standard string
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT t.network_id,d.tx_hash,d.amount,w.address,d.dest_address,COALESCE(t.contract_address,''),n.chain_family,t.token_standard
		FROM doc_crypto_withdrawals d JOIN cat_wallets w ON w.id=d.source_wallet_id JOIN cat_tokens t ON t.id=d.token_id JOIN cat_blockchain_networks n ON n.id=t.network_id
		WHERE d.id=$1 AND t.network_id=$2 AND d.tx_hash=$3`, document, receipt.NetworkID, receipt.TxHash).Scan(&e.NetworkID, &e.TxHash, &e.Amount, &e.FromAddress, &e.ToAddress, &e.TokenContract, &family, &standard)
	e.EventKind = crypto.EventKindContractLog
	if standard == "native" {
		e.EventKind = crypto.EventKindNativeValue
	}
	if err != nil {
		return e, err
	}
	e.DecoderVersion, err = crypto.TransferEvidenceSchemaFor(crypto.ChainFamily(family))
	return e, err
}

func (*WithdrawalCustodyRepo) Prepare(ctx context.Context, b crypto.WithdrawalAccountingBasis, receipt crypto.ChainReceipt) (bool, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return false, apperror.NewValidation("withdrawal settlement requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoWithdrawal", b.DocumentID); err != nil {
		return false, err
	}
	basis, basisID := b.SigningBasis()
	var valid bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_crypto_withdrawals d JOIN cat_wallets w ON w.id=d.source_wallet_id JOIN cat_tokens t ON t.id=d.token_id
		WHERE d.id=$1 AND d.basis_type=$2 AND d.basis_id=$3 AND d.merchant_id=$4 AND d.token_id=$5 AND w.id=$6 AND w.network_id=$7
		AND t.network_id=w.network_id AND (w.custody_merchant_id IS NULL OR w.custody_merchant_id=d.merchant_id) AND d.tx_hash=$8 AND d.amount=$9)`,
		b.DocumentID, basis, basisID, b.MerchantID, b.TokenID, b.SourceWalletID, b.NetworkID, b.TxHash, b.Amount).Scan(&valid)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, crypto.ErrVerificationMismatch
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_aml_lock_resource_scopes(id) FROM reg_crypto_fee_reservations WHERE basis_type=$1 AND basis_id=$2 AND purpose='withdrawal' AND attempt=1`, basis, basisID); err != nil {
		return false, err
	}
	if _, err := NewAMLRepository().LockAMLWithdrawalScope(ctx, b.MerchantID, b.NetworkID, b.TokenID); err != nil {
		return false, err
	}
	if err := NewCustodyRecognitionRepo().LockWallet(ctx, b.SourceWalletID, b.TokenID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT id FROM doc_withdrawal_requests WHERE id=$1 AND merchant_id=$2 AND token_id=$3 FOR UPDATE`, b.RequestID, b.MerchantID, b.TokenID); err != nil {
		return false, err
	}
	if !id.IsNil(b.LineID) {
		var lineMatches bool
		if err := q.QueryRow(ctx, `SELECT request_id=$2 AND withdrawal_id=$3 AND wallet_id=$4 AND token_id=$5 AND amount=$6 FROM reg_crypto_aml_withdrawal_lines WHERE id=$1 FOR UPDATE`,
			b.LineID, b.RequestID, b.DocumentID, b.SourceWalletID, b.TokenID, b.Amount).Scan(&lineMatches); err != nil {
			return false, err
		}
		if !lineMatches {
			return false, crypto.ErrVerificationMismatch
		}
	}
	var status, destination string
	err = q.QueryRow(ctx, `SELECT d.settlement_status,d.dest_address,d.basis_type=$2 AND d.basis_id=$3 AND d.merchant_id=$4 AND d.token_id=$5 AND d.source_wallet_id=$6 AND d.tx_hash=$7 AND d.amount=$8
		FROM doc_crypto_withdrawals d WHERE d.id=$1 FOR UPDATE`, b.DocumentID, basis, basisID, b.MerchantID, b.TokenID, b.SourceWalletID, b.TxHash, b.Amount).Scan(&status, &destination, &valid)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, crypto.ErrVerificationMismatch
	}
	if status == "recorded" {
		var matches bool
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements WHERE basis_type=$1 AND basis_id=$2 AND network_id=$3 AND tx_hash=$4 AND block_hash=$5
			AND block_height=$6 AND status=$7 AND native_token_id=$8 AND total_fee_minor=$9)`, basis, basisID, b.NetworkID, b.TxHash, receipt.BlockHash, receipt.BlockHeight, receipt.Status, receipt.Fee.NativeAsset.TokenID, receipt.Fee.TotalFee).Scan(&matches)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, apperror.NewConflict("withdrawal replay conflicts with its final occurrence")
		}
		return false, nil
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_fee_reservations WHERE basis_type=$1 AND basis_id=$2 AND purpose='withdrawal' AND attempt=1 FOR UPDATE`, basis, basisID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_balance_reservations WHERE document_type=$1 AND document_id=$2 ORDER BY id FOR UPDATE`, basis, basisID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_receipt_balance(w,t) FROM (SELECT $1::uuid w,$2::uuid t UNION SELECT $1::uuid,$3::uuid) keys ORDER BY w,t`, b.SourceWalletID, b.TokenID, receipt.Fee.NativeAsset.TokenID); err != nil {
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
	transfer := crypto.AccountingTransferBasis{NetworkID: b.NetworkID, WalletID: b.SourceWalletID, TokenID: b.TokenID, TxHash: b.TxHash, EventKind: e.EventKind, EventIndex: e.EventIndex,
		RecordType: entity.RecordTypeExpense, ToAddress: destination, Amount: b.Amount, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, RequiredConfs: 1, VerificationID: b.VerificationID}
	if err := NewCustodyRecognitionRepo().LockTransfer(ctx, transfer); err != nil {
		return false, err
	}
	e, err = NewBlockIngestionRepo().ExpectedAccountingTransfer(ctx, transfer)
	if err != nil {
		return false, err
	}
	e.VerificationID = b.VerificationID
	e.Confirmations = receipt.Confirmations
	e.Finality = crypto.FinalitySolidified
	e.Canonicality = crypto.CanonicalityCanonical
	evidence, err := NewBlockIngestionRepo().AuthorizeSettlementCustodyEffect(ctx, transfer, e)
	if err != nil {
		return false, err
	}
	var authorization, debit id.ID
	err = q.QueryRow(ctx, `SELECT a.id FROM reg_crypto_signing_authorizations a JOIN reg_crypto_expected_chain_effects x ON x.signing_authorization_id=a.id
		WHERE a.basis_type=$1 AND a.basis_id=$2 AND a.attempt=1 AND a.merchant_id=$3 AND a.signed_envelope IS NOT NULL AND x.network_id=$4 AND x.token_id=$5 AND x.amount=$6
		AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower($7),'^0x','') ORDER BY a.generation DESC LIMIT 1`, basis, basisID, b.MerchantID, b.NetworkID, b.TokenID, b.Amount, b.TxHash).Scan(&authorization)
	if err != nil {
		return false, err
	}
	err = q.QueryRow(ctx, `SELECT m.line_id FROM reg_crypto_merchant_balance_movements m JOIN doc_withdrawal_requests r ON r.id=m.recorder_id
		WHERE r.id=$1 AND r.merchant_id=$2 AND r.token_id=$3 AND r.posted AND r.posted_version=1 AND m.recorder_type='WithdrawalRequest' AND m.recorder_version=1
		AND m.record_type='expense' AND m.merchant_id=r.merchant_id AND m.token_id=r.token_id AND m.amount=r.amount AND m.reversal_of_line_id IS NULL`, b.RequestID, b.MerchantID, b.TokenID).Scan(&debit)
	if err != nil {
		return false, err
	}
	posting := id.New()
	if err := NewCustodyRecognitionRepo().ClaimOutgoing(ctx, evidence, posting, b.DocumentID, "CryptoWithdrawal", b.SourceWalletID, b.TokenID, withdrawalPrincipalLine(b.DocumentID)); err != nil {
		return false, err
	}
	var line any
	if !id.IsNil(b.LineID) {
		line = b.LineID
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_withdrawal_custody_bindings(posting_id,withdrawal_id,request_id,line_id,signing_authorization_id,merchant_debit_line_id,period) VALUES($1,$2,$3,$4,$5,$6,$7)`, posting, b.DocumentID, b.RequestID, line, authorization, debit, receipt.ObservedAt); err != nil {
		return false, err
	}
	err = postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoWithdrawal", AggregateID: b.DocumentID, EventType: "custody_recognized", Payload: map[string]any{
		"entityType": "CryptoWithdrawal", "entityId": b.DocumentID.String(), "requestId": b.RequestID.String(), "action": "custody_recognized", "postingId": posting, "generation": 1}})
	return err == nil, err
}

func (*WithdrawalCustodyRepo) RecordOutcome(ctx context.Context, b crypto.WithdrawalAccountingBasis, receipt crypto.ChainReceipt) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if receipt.Status == crypto.ReceiptSucceeded {
		_, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,'CryptoWithdrawal',1,$3,'expense',$4,$5,$6)`, withdrawalPrincipalLine(b.DocumentID), b.DocumentID, receipt.ObservedAt, b.SourceWalletID, b.TokenID, b.Amount)
		return err
	}
	refund := b.LineID
	if id.IsNil(refund) {
		refund = uuid.NewSHA1(b.DocumentID, []byte("withdrawal-merchant-refund"))
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
		VALUES($1,$2,'WithdrawalRequest',2,$3,'receipt',$4,$5,$6)`, refund, b.RequestID, receipt.ObservedAt, b.MerchantID, b.TokenID, b.Amount); err != nil {
		return fmt.Errorf("restore failed withdrawal merchant debit: %w", err)
	}
	return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoWithdrawal", AggregateID: b.DocumentID, EventType: "execution_reverted", Payload: map[string]any{
		"entityType": "CryptoWithdrawal", "entityId": b.DocumentID.String(), "requestId": b.RequestID.String(), "action": "execution_reverted"}})
}

func withdrawalPrincipalLine(document id.ID) id.ID {
	return uuid.NewSHA1(document, []byte("withdrawal-principal"))
}
