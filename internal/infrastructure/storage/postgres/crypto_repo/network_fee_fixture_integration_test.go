//go:build integration

package crypto_repo

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// testTRONAddress returns a unique syntactically valid TRON base58 address.
func testTRONAddress() string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	raw := id.New()
	out := []byte{'T'}
	for i := 0; len(out) < 34; i++ {
		out = append(out, alphabet[int(raw[i%len(raw)]+byte(i*7))%len(alphabet)])
	}
	return string(out)
}

// proveTestNetworkFee attaches the independent TRON fee proof that two
// providers would report for the reservation payer of r.
func proveTestNetworkFee(ctx context.Context, payer string, r *crypto.ChainReceipt) error {
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
	return p.ValidateReceipt(*r)
}

// recognizeTestNetworkFee settles a broadcast fee reservation exactly as the
// fee budget does: normalized settlement, components, independent proof and
// binding, payer expense, platform cost and the terminal reservation state.
// It must run inside the caller's financial transaction.
func recognizeTestNetworkFee(ctx context.Context, reservationID id.ID, r crypto.ChainReceipt) error {
	return recognizeTestNetworkFeeFor(ctx, reservationID, r, id.New(), "", id.ID{})
}

// recognizeTestNetworkFeeFor records the fee for an explicit document owner
// (activation: CryptoNetworkOperation) with a caller-chosen settlement ID.
func recognizeTestNetworkFeeFor(ctx context.Context, reservationID id.ID, r crypto.ChainReceipt, settlement id.ID, ownerType string, ownerID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var f crypto.FeeReservation
	var payer, purpose string
	if err := q.QueryRow(ctx, `SELECT f.id,f.network_id,f.payer_wallet_id,f.native_token_id,f.operation_token_id,f.basis_type,f.basis_id,f.purpose,f.tx_hash,w.address_key
		FROM reg_crypto_fee_reservations f JOIN cat_wallets w ON w.id=f.payer_wallet_id WHERE f.id=$1 FOR UPDATE OF f`, reservationID).
		Scan(&f.ID, &f.NetworkID, &f.PayerWalletID, &f.NativeTokenID, &f.OperationTokenID, &f.BasisType, &f.BasisID, &purpose, &f.TxHash, &payer); err != nil {
		return err
	}
	f.Purpose = crypto.SigningPurpose(purpose)
	r.NetworkID, r.TxHash, r.Fee.NativeAsset.TokenID = f.NetworkID, f.TxHash, f.NativeTokenID
	r.ObservedAt = r.ObservedAt.UTC().Truncate(1000)
	if r.FeeProof == nil {
		if err := proveTestNetworkFee(ctx, payer, &r); err != nil {
			return err
		}
	}
	status := "succeeded"
	if r.Status == crypto.ReceiptReverted {
		status = "reverted"
	}
	basisType, basisID := f.BasisType, f.BasisID
	if ownerType != "" {
		basisType, basisID = ownerType, ownerID
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at,network_fee_reservation_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'{}',$12,$13)`, settlement, f.NetworkID, basisType, basisID, f.TxHash, status, r.BlockHash, r.BlockHeight, r.Finality, f.NativeTokenID, r.Fee.TotalFee, r.ObservedAt, f.ID); err != nil {
		return err
	}
	for _, c := range r.Fee.Components {
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlement_components(settlement_id,component,amount_minor,quantity,metadata) VALUES($1,$2,$3,$4,'{}')`, settlement, c.Name, c.Amount, c.Quantity); err != nil {
			return err
		}
	}
	if err := NewNetworkFeeRepo().Prepare(ctx, f, r, settlement, ownerType, ownerID); err != nil {
		return err
	}
	recorderType, recorderID, movementLine, costLine := "ChainSettlement", f.BasisID, f.ID, f.ID
	var operationToken any = f.OperationTokenID
	if ownerType != "" {
		operationToken = nil
		recorderType, recorderID = ownerType, ownerID
		movementLine, costLine = uuid.NewSHA1(ownerID, []byte("network-fee-expense")), uuid.NewSHA1(ownerID, []byte("network-fee-cost"))
	}
	if r.Fee.TotalFee.IsPositive() {
		costPurpose := purpose
		if f.Purpose == crypto.SigningPurposeTreasury {
			costPurpose = "treasury"
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,$7,1,$3,'expense',$4,$5,$6)`, movementLine, recorderID, r.ObservedAt, f.PayerWalletID, f.NativeTokenID, r.Fee.TotalFee, recorderType); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount)
			VALUES($1,$2,$10,1,$3,'expense',$4,$5,$6,'chain_network_fee',$7,'chain',$8,$9)`, costLine, recorderID, r.ObservedAt, f.NetworkID, operationToken, f.NativeTokenID, costPurpose, f.TxHash, r.Fee.TotalFee, recorderType); err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx, `UPDATE reg_crypto_fee_reservations SET status='settled',actual_fee_minor=$2,settled_at=NOW(),hold_reason='' WHERE id=$1`, f.ID, r.Fee.TotalFee)
	return err
}
