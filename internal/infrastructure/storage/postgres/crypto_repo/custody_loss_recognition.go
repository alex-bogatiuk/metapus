package crypto_repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func (*ExternalOutflowWorkflowRepo) lockCustodyAdjustment(ctx context.Context, document id.ID, resources bool) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("custody adjustment decision requires a transaction")
	}
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoCustodyAdjustment", document); err != nil {
		return err
	}
	q := tm.GetQuerier(ctx)
	var wallet, asset, outflow id.ID
	if err := q.QueryRow(ctx, `SELECT wallet_id,token_id,external_outflow_id FROM doc_crypto_custody_adjustments WHERE id=$1`, document).Scan(&wallet, &asset, &outflow); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewNotFound("custody adjustment", document.String())
		}
		return err
	}
	if err := lockExternalOutflowResources(ctx, outflow); err != nil {
		return err
	}
	if resources {
		if err := NewCustodyRecognitionRepo().LockWallet(ctx, wallet, asset); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `SELECT id FROM reg_crypto_external_outflows WHERE id=$1 FOR UPDATE`, outflow)
		return err
	}
	return nil
}

// Approval consumes previously persisted independent chain evidence. Detection,
// classification and checker permission alone never prove a physical expense.
func (*ExternalOutflowWorkflowRepo) prepareCustodyLossRecognition(ctx context.Context, a *crypto.CryptoCustodyAdjustment, checker id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var e crypto.BlockchainEvent
	var required int
	err := q.QueryRow(ctx, `SELECT network_id,tx_hash,event_kind,event_index,from_address,to_address,contract_address,amount,block_number,block_hash,confirmations,required_confs
  FROM reg_crypto_external_outflows WHERE id=$1 AND network_id=$2 AND source_wallet_id=$3 AND token_id=$4 AND amount=$5`, a.ExternalOutflowID, a.NetworkID, a.WalletID, a.TokenID, a.Amount).
		Scan(&e.NetworkID, &e.TxHash, &e.EventKind, &e.EventIndex, &e.FromAddress, &e.ToAddress, &e.TokenContract, &e.Amount, &e.BlockNumber, &e.BlockHash, &e.Confirmations, &required)
	if err != nil {
		return err
	}
	var proof id.ID
	err = q.QueryRow(ctx, `SELECT v.id FROM reg_crypto_transfer_verifications v JOIN reg_crypto_accounting_effect_evidence indexed ON indexed.verification_id=v.id
  WHERE v.network_id=$1 AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower($2),'^0x','') AND v.event_kind=$3 AND v.event_index=$4
    AND regexp_replace(lower(v.block_hash),'^0x','')=regexp_replace(lower($5),'^0x','') AND v.block_number=$6
  ORDER BY v.verified_at DESC,v.id DESC LIMIT 1`, e.NetworkID, e.TxHash, e.EventKind, e.EventIndex, e.BlockHash, e.BlockNumber).Scan(&proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NewConflict("custody loss requires independent final chain proof").WithCause(crypto.ErrVerificationPending)
	}
	if err != nil {
		return err
	}
	b := crypto.AccountingTransferBasis{NetworkID: a.NetworkID, WalletID: a.WalletID, TokenID: a.TokenID, RecordType: entity.RecordTypeExpense,
		TxHash: e.TxHash, EventKind: e.EventKind, EventIndex: e.EventIndex, ToAddress: e.ToAddress, Amount: a.Amount, BlockNumber: e.BlockNumber, BlockHash: e.BlockHash, RequiredConfs: required, VerificationID: &proof}
	if err := NewCustodyRecognitionRepo().LockTransfer(ctx, b); err != nil {
		return err
	}
	// Serialize outflow writers by economic identity even through wallet/token
	// aliases. Their SQL trigger takes this same lock before admitting a write.
	if _, err = q.Exec(ctx, `SELECT fn_crypto_lock_outflow_economics(network_id,tx_hash,source_address_key,destination_address_key,contract_address_key,amount)
  FROM reg_crypto_external_outflows WHERE id=$1`, a.ExternalOutflowID); err != nil {
		return err
	}
	expected, err := NewBlockIngestionRepo().ExpectedAccountingTransfer(ctx, b)
	if err != nil {
		return err
	}
	expected.VerificationID, expected.Confirmations = &proof, e.Confirmations
	expected.Finality, expected.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityCanonical
	// An unauthorized loss is already an observed chain fact. New-dispatch and
	// intake pauses must not suppress its recognition; proof checks remain strict.
	evidence, err := NewBlockIngestionRepo().AuthorizeSettlementCustodyEffect(ctx, b, expected)
	if err != nil {
		if errors.Is(err, crypto.ErrVerificationPending) || errors.Is(err, crypto.ErrVerificationMismatch) {
			return apperror.NewConflict("custody loss proof does not confirm the final external transfer").WithCause(err)
		}
		return err
	}
	var blocked bool
	if err := q.QueryRow(ctx, `SELECT fn_crypto_custody_loss_conflicting_owner($1,$2)`, a.ExternalOutflowID, a.ID).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return apperror.NewConflict("custody loss must be an external transfer without an existing accounting or signed execution owner")
	}
	a.RecognitionPeriod = time.Now().UTC().Truncate(time.Microsecond)
	posting := id.New()
	if err := NewCustodyRecognitionRepo().ClaimOutgoing(ctx, evidence, posting, a.ID, "CryptoCustodyAdjustment", a.WalletID, a.TokenID, a.PrincipalLineID()); err != nil {
		return err
	}
	if _, err = q.Exec(ctx, `INSERT INTO reg_crypto_custody_loss_bindings(posting_id,adjustment_id,external_outflow_id,maker_id,checker_id,period)
  VALUES($1,$2,$3,$4,$5,$6)`, posting, a.ID, a.ExternalOutflowID, a.MakerID, checker, a.RecognitionPeriod); err != nil {
		return err
	}
	return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoCustodyAdjustment", AggregateID: a.ID, EventType: "custody_recognized", Payload: map[string]any{
		"entityType": "CryptoCustodyAdjustment", "entityId": a.ID.String(), "externalOutflowId": a.ExternalOutflowID.String(), "action": "custody_recognized", "postingId": posting, "generation": 1}})
}
