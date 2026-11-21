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

type TreasuryCustodyRepo struct{ *BlockIngestionRepo }

func NewTreasuryCustodyRepo() *TreasuryCustodyRepo {
	return &TreasuryCustodyRepo{NewBlockIngestionRepo()}
}

func (*TreasuryCustodyRepo) ExpectedTreasuryTransfer(ctx context.Context, document id.ID, receipt crypto.ChainReceipt) (crypto.BlockchainEvent, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		return crypto.BlockchainEvent{}, apperror.NewValidation("treasury proof must be read before the financial transaction")
	}
	e := crypto.BlockchainEvent{EventType: crypto.EventTypeTransfer, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash,
		Confirmations: receipt.Confirmations, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical}
	var family, standard string
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT t.network_id,d.tx_hash,d.amount,w.address,d.dest_address,COALESCE(t.contract_address,''),n.chain_family,t.token_standard
		FROM doc_treasury_withdrawals d JOIN cat_wallets w ON w.id=d.source_wallet_id JOIN cat_tokens t ON t.id=d.token_id
		JOIN cat_blockchain_networks n ON n.id=t.network_id WHERE d.id=$1 AND t.network_id=$2 AND d.tx_hash=$3`, document, receipt.NetworkID, receipt.TxHash).
		Scan(&e.NetworkID, &e.TxHash, &e.Amount, &e.FromAddress, &e.ToAddress, &e.TokenContract, &family, &standard)
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

func (*TreasuryCustodyRepo) Prepare(ctx context.Context, b crypto.TreasuryAccountingBasis, receipt crypto.ChainReceipt) (bool, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return false, apperror.NewValidation("treasury settlement requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if err := postgres.NewDocLocker().LockDocument(ctx, "TreasuryWithdrawal", b.DocumentID); err != nil {
		return false, err
	}
	var platformBasis bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_treasury_withdrawals d JOIN cat_wallets w ON w.id=d.source_wallet_id
		WHERE d.id=$1 AND w.id=$2 AND w.network_id=$3 AND w.custody_merchant_id IS NULL)`, b.DocumentID, b.SourceWalletID, b.NetworkID).Scan(&platformBasis); err != nil {
		return false, err
	}
	if !platformBasis {
		return false, crypto.ErrVerificationMismatch
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_aml_lock_resource_scopes(id) FROM reg_crypto_fee_reservations WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1 AND purpose='treasury_withdrawal' AND attempt=1`, b.DocumentID); err != nil {
		return false, err
	}
	if err := NewCustodyRecognitionRepo().LockWallet(ctx, b.SourceWalletID, b.TokenID); err != nil {
		return false, err
	}
	// Payment/adjustment writers also acquire custody before fee revenue. Taking
	// revenue first can deadlock when both writers touch the treasury wallet.
	if _, err := q.Exec(ctx, `SELECT f.merchant_id FROM reg_crypto_fee_balances f WHERE EXISTS(
		SELECT 1 FROM reg_crypto_revenue_reservations r WHERE r.document_id=$1 AND r.document_type='TreasuryWithdrawal'
		AND r.merchant_id=f.merchant_id AND r.token_id=f.token_id AND r.fee_type=f.fee_type)
		ORDER BY f.merchant_id,f.token_id,f.fee_type FOR UPDATE OF f`, b.DocumentID); err != nil {
		return false, err
	}
	var status, destination string
	var valid bool
	err := q.QueryRow(ctx, `SELECT d.settlement_status,d.dest_address,d.token_id=$2 AND d.source_wallet_id=$3
		AND d.amount=$4 AND d.tx_hash=$5 AND t.network_id=$6 AND w.custody_merchant_id IS NULL AND w.network_id=t.network_id
		FROM doc_treasury_withdrawals d JOIN cat_tokens t ON t.id=d.token_id JOIN cat_wallets w ON w.id=d.source_wallet_id WHERE d.id=$1 FOR UPDATE OF d`,
		b.DocumentID, b.TokenID, b.SourceWalletID, b.Amount, b.TxHash, b.NetworkID).Scan(&status, &destination, &valid)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, crypto.ErrVerificationMismatch
	}
	if status == "recorded" {
		var matches bool
		err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1
			AND network_id=$2 AND tx_hash=$3 AND block_hash=$4 AND block_height=$5 AND status=$6 AND native_token_id=$7 AND total_fee_minor=$8)`,
			b.DocumentID, b.NetworkID, b.TxHash, receipt.BlockHash, receipt.BlockHeight, receipt.Status, receipt.Fee.NativeAsset.TokenID, receipt.Fee.TotalFee).Scan(&matches)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, apperror.NewConflict("treasury replay conflicts with its final occurrence")
		}
		return false, nil
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_fee_reservations WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1 AND purpose='treasury_withdrawal' AND attempt=1 FOR UPDATE`, b.DocumentID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_balance_reservations WHERE document_id=$1 ORDER BY id FOR UPDATE`, b.DocumentID); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `SELECT id FROM reg_crypto_revenue_reservations WHERE document_id=$1 ORDER BY id FOR UPDATE`, b.DocumentID); err != nil {
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
	basis := crypto.AccountingTransferBasis{NetworkID: b.NetworkID, WalletID: b.SourceWalletID, TokenID: b.TokenID, TxHash: b.TxHash, EventKind: e.EventKind, EventIndex: e.EventIndex,
		RecordType: entity.RecordTypeExpense, ToAddress: destination, Amount: b.Amount, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, RequiredConfs: 1, VerificationID: b.VerificationID}
	if err := NewCustodyRecognitionRepo().LockTransfer(ctx, basis); err != nil {
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
	var authorization id.ID
	err = q.QueryRow(ctx, `SELECT a.id FROM reg_crypto_signing_authorizations a JOIN reg_crypto_expected_chain_effects x ON x.signing_authorization_id=a.id
		WHERE a.basis_type='TreasuryWithdrawal' AND a.basis_id=$1 AND a.signed_envelope IS NOT NULL AND x.network_id=$2
		AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower($3),'^0x','') AND x.token_id=$4 AND x.amount=$5 ORDER BY a.generation DESC LIMIT 1`,
		b.DocumentID, b.NetworkID, b.TxHash, b.TokenID, b.Amount).Scan(&authorization)
	if err != nil {
		return false, err
	}
	posting := id.New()
	if err := NewCustodyRecognitionRepo().ClaimOutgoing(ctx, evidence, posting, b.DocumentID, "TreasuryWithdrawal", b.SourceWalletID, b.TokenID, treasuryPrincipalLine(b.DocumentID)); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_treasury_custody_bindings(posting_id,treasury_id,signing_authorization_id,period) VALUES($1,$2,$3,$4)`, posting, b.DocumentID, authorization, receipt.ObservedAt); err != nil {
		return false, err
	}
	err = postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "TreasuryWithdrawal", AggregateID: b.DocumentID, EventType: "custody_recognized", Payload: map[string]any{
		"entityType": "TreasuryWithdrawal", "entityId": b.DocumentID.String(), "action": "custody_recognized", "postingId": posting, "generation": 1}})
	return err == nil, err
}

func (*TreasuryCustodyRepo) RecordPrincipalAndRevenue(ctx context.Context, b crypto.TreasuryAccountingBasis, receipt crypto.ChainReceipt) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,$2,'TreasuryWithdrawal',1,$3,'expense',$4,$5,$6)`, treasuryPrincipalLine(b.DocumentID), b.DocumentID, receipt.ObservedAt, b.SourceWalletID, b.TokenID, b.Amount); err != nil {
		return fmt.Errorf("record proven treasury principal: %w", err)
	}
	// A receipt refund closes its payable with the payout; a revenue withdrawal
	// spends the revenue it reserved.
	tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_refund_payable_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
		SELECT id,'TreasuryWithdrawal',2,$2,'expense',receipt_id,token_id,amount FROM doc_treasury_withdrawals WHERE id=$1 AND purpose='receipt_refund'`, b.DocumentID, receipt.ObservedAt)
	if err != nil {
		return fmt.Errorf("close paid receipt refund payable: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
		SELECT id,$1,'TreasuryWithdrawal',1,$2,'expense',merchant_id,token_id,fee_type,amount FROM reg_crypto_revenue_reservations
		WHERE document_type='TreasuryWithdrawal' AND document_id=$1 AND status='active' ORDER BY merchant_id,token_id,fee_type,id`, b.DocumentID, receipt.ObservedAt)
	return err
}

func treasuryPrincipalLine(document id.ID) id.ID {
	return uuid.NewSHA1(document, []byte("treasury-principal"))
}
