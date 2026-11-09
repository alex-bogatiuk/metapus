package crypto_repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type NetworkFeeRepo struct{}

func NewNetworkFeeRepo() *NetworkFeeRepo { return &NetworkFeeRepo{} }

func (*NetworkFeeRepo) LockEffect(ctx context.Context, f crypto.FeeReservation, r crypto.ChainReceipt) error {
	if r.FeeProof == nil {
		return crypto.ErrVerificationPending
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_receipt_balance($1,$2)`, f.PayerWalletID, f.NativeTokenID); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('network-fee:'||$1,0))`, r.FeeProof.EffectKey()); err != nil {
		return err
	}
	var blocked bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements WHERE network_id=$1 AND regexp_replace(lower(tx_hash),'^0x','')=regexp_replace(lower($2),'^0x',''))`, f.NetworkID, r.TxHash).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return apperror.NewConflict("network fee history requires explicit reconciliation before recognition")
	}
	return nil
}

func (*NetworkFeeRepo) LockReservation(ctx context.Context, reservation id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("network fee admission requires a transaction")
	}
	var wallet, asset id.ID
	if err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT payer_wallet_id,native_token_id FROM reg_crypto_fee_reservations WHERE id=$1`, reservation).Scan(&wallet, &asset); err != nil {
		return err
	}
	return NewCustodyRecognitionRepo().LockWallet(ctx, wallet, asset)
}

func (*NetworkFeeRepo) ValidateReplay(ctx context.Context, f crypto.FeeReservation, r crypto.ChainReceipt, documentType string, documentID id.ID) error {
	if r.FeeProof == nil {
		return crypto.ErrVerificationPending
	}
	if err := r.FeeProof.ValidateReceipt(r); err != nil {
		return err
	}
	ownerType, ownerID := "ChainSettlement", f.BasisID
	if documentType != "" {
		ownerType, ownerID = documentType, documentID
	}
	var matches bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings b
  JOIN reg_crypto_network_fee_proofs p ON p.id=b.proof_id JOIN reg_crypto_chain_settlements c ON c.id=b.settlement_id
  WHERE b.reservation_id=$1 AND p.proof_digest=$2 AND b.recorder_type=$3 AND b.recorder_id=$4 AND c.status=$5
  AND regexp_replace(lower(c.block_hash),'^0x','')=regexp_replace(lower($6),'^0x','') AND c.block_height=$7 AND c.native_token_id=$8 AND c.total_fee_minor=$9)`,
		f.ID, r.FeeProof.Digest(), ownerType, ownerID, r.Status, r.BlockHash, r.BlockHeight, r.Fee.NativeAsset.TokenID, r.Fee.TotalFee).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return apperror.NewConflict("network fee replay conflicts with its recognized outcome")
	}
	return nil
}

func (*NetworkFeeRepo) ValidatePayer(ctx context.Context, f crypto.FeeReservation, r crypto.ChainReceipt) error {
	if r.FeeProof == nil {
		return crypto.ErrVerificationPending
	}
	p := *r.FeeProof
	if err := p.ValidateReceipt(r); err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var valid bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id
  WHERE w.id=$1 AND t.id=$2 AND n.id=$3 AND n.chain_family=$4 AND w.address_key=$5 AND w.vault_key_id<>'' AND t.token_standard='native'
  AND COALESCE(t.contract_address,'')='' AND n.confirmations_needed<=$6)`, f.PayerWalletID, f.NativeTokenID, f.NetworkID, p.Family, p.PayerKey(), p.Confirmations).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("network fee proof does not match its controlled payer and native asset")
	}
	return nil
}

func (repo *NetworkFeeRepo) Prepare(ctx context.Context, f crypto.FeeReservation, r crypto.ChainReceipt, settlement id.ID, documentType string, documentID id.ID) error {
	if err := repo.ValidatePayer(ctx, f, r); err != nil {
		return err
	}
	p := *r.FeeProof
	p.VerifiedAt = p.VerifiedAt.UTC().Truncate(time.Microsecond)
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	// Wallet/balance precede fee identity, including callers whose principal is
	// already protected by its own effect lock. Fee has an independent role lock.
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_receipt_balance($1,$2)`, f.PayerWalletID, f.NativeTokenID); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('network-fee:'||$1,0))`, p.EffectKey()); err != nil {
		return err
	}
	var existing id.ID
	err := q.QueryRow(ctx, `SELECT reservation_id FROM reg_crypto_network_fee_bindings WHERE effect_key=$1`, p.EffectKey()).Scan(&existing)
	if err == nil {
		return apperror.NewConflict("network fee already has an accounting owner")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	proofID := id.New()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = q.Exec(ctx, `INSERT INTO reg_crypto_network_fee_proofs(id,effect_key,network_id,tx_hash,family,payer_address_key,native_token_id,block_hash,block_number,status,total_fee_minor,proof_digest,confirmations,evidence,verified_at)
  VALUES($1,$2,$3,regexp_replace(lower($4),'^0x',''),$5,$6,$7,regexp_replace(lower($8),'^0x',''),$9,$10,$11,$12,$13,$14::jsonb,$15)`,
		proofID, p.EffectKey(), p.NetworkID, p.TxHash, p.Family, p.PayerKey(), p.NativeTokenID, p.BlockHash, p.BlockNumber, p.Status, p.TotalFee, p.Digest(), p.Confirmations, string(raw), p.VerifiedAt); err != nil {
		return err
	}
	ownerType, ownerID := "ChainSettlement", f.BasisID
	period := r.ObservedAt.UTC().Truncate(time.Microsecond)
	var movement, cost any
	if r.Fee.TotalFee.IsPositive() {
		movement = f.ID
		cost = f.ID
	}
	if documentType != "" {
		if documentType != "CryptoNetworkOperation" || f.Purpose != crypto.SigningPurposeActivation {
			return apperror.NewValidation("unsupported fee accounting owner")
		}
		ownerType, ownerID = documentType, documentID
		if r.Fee.TotalFee.IsPositive() {
			movement = uuid.NewSHA1(documentID, []byte("network-fee-expense"))
			cost = uuid.NewSHA1(documentID, []byte("network-fee-cost"))
		}
	}
	if _, err = q.Exec(ctx, `INSERT INTO reg_crypto_network_fee_bindings(reservation_id,effect_key,proof_id,settlement_id,recorder_type,recorder_id,movement_line_id,cost_line_id,period)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, f.ID, p.EffectKey(), proofID, settlement, ownerType, ownerID, movement, cost, period); err != nil {
		return err
	}
	return postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoNetworkFee", AggregateID: f.ID, EventType: "fee_recognized", Payload: map[string]any{
		"entityType": "CryptoNetworkFee", "entityId": f.ID.String(), "action": "fee_recognized", "effectKey": p.EffectKey(), "proofId": proofID.String(), "generation": 1}})
}
