package crypto_repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func (NativeFundingSettlementRepo) PrepareNativeFundingPrincipal(ctx context.Context, funding crypto.NativeFunding, receipt crypto.ChainReceipt) error {
	tm := postgres.MustGetTxManager(ctx)
	q := tm.GetQuerier(ctx)
	// GetNativeFundingForUpdate already owns all scopes and both catalog wallets.
	// Reserve both physical balance rows before the effect and fee settlement.
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_receipt_balance(w,$3) FROM
		(SELECT unnest(ARRAY[$1::uuid,$2::uuid]) w ORDER BY w) ordered`, funding.SourceWalletID, funding.DestinationWalletID, funding.NativeTokenID); err != nil {
		return err
	}
	var proofID id.ID
	err := q.QueryRow(ctx, `SELECT v.id FROM reg_crypto_transfer_verifications v
		JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
		JOIN reg_crypto_accounting_effect_occurrences o ON o.id=x.occurrence_id
		JOIN cat_wallets s ON s.id=$4 JOIN cat_wallets d ON d.id=$5
		WHERE v.network_id=$1 AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower($2),'^0x','')
		AND v.event_kind='native_value' AND v.event_index=0 AND v.block_hash=$3
		AND o.source_address=s.address_key AND o.destination_address=d.address_key AND o.amount=$6
		ORDER BY v.verified_at DESC,v.confirmations DESC LIMIT 1`, funding.NetworkID, funding.TxHash, receipt.BlockHash,
		funding.SourceWalletID, funding.DestinationWalletID, funding.Amount).Scan(&proofID)
	if err != nil {
		return fmt.Errorf("native funding requires indexed principal proof: %w", err)
	}
	guard := NewBlockIngestionRepo()
	event, err := (NativeFundingEvidenceRepo{}).ExpectedFundingTransfer(ctx, funding.ID, receipt)
	if err != nil {
		return err
	}
	event.VerificationID = &proofID
	basis := crypto.AccountingTransferBasis{NetworkID: funding.NetworkID, WalletID: funding.DestinationWalletID,
		TokenID: funding.NativeTokenID, TxHash: funding.TxHash, EventKind: event.EventKind, FromAddress: event.FromAddress,
		Amount: funding.Amount, BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, RequiredConfs: 1, VerificationID: &proofID}
	if err := NewCustodyRecognitionRepo().LockIncoming(ctx, basis); err != nil {
		return err
	}
	event, err = guard.ExpectedAccountingTransfer(ctx, basis)
	if err != nil {
		return err
	}
	event.VerificationID, event.Confirmations = &proofID, receipt.Confirmations
	event.Finality, event.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityCanonical
	evidence, err := guard.AuthorizeSettlementCustodyEffect(ctx, basis, event)
	if err != nil {
		return err
	}
	postingID := id.New()
	sourceLine, destinationLine := nativeFundingPrincipalLines(funding.ID)
	if err := NewCustodyRecognitionRepo().ClaimIncoming(ctx, evidence, postingID, funding.ID,
		crypto.NativeFundingBasisType, funding.DestinationWalletID, funding.NativeTokenID, destinationLine); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_recognitions(posting_id,wallet_id,token_id,record_type,movement_line_id)
		VALUES($1,$2,$3,'expense',$4)`, postingID, funding.SourceWalletID, funding.NativeTokenID, sourceLine); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_native_funding_custody_bindings(posting_id,funding_id,period)
		VALUES($1,$2,$3)`, postingID, funding.ID, nativeFundingPeriod(receipt)); err != nil {
		return err
	}
	return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: crypto.NativeFundingBasisType,
		AggregateID: funding.ID, EventType: "custody_recognized", Payload: map[string]any{
			"entityType": crypto.NativeFundingBasisType, "entityId": funding.ID.String(), "action": "custody_recognized",
			"postingId": postingID, "generation": 1}})
}

func nativeFundingPrincipalLines(fundingID id.ID) (id.ID, id.ID) {
	return uuid.NewSHA1(fundingID, []byte("native-funding-source")), uuid.NewSHA1(fundingID, []byte("native-funding-destination"))
}
