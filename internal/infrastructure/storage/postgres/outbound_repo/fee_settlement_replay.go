package outbound_repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

func validateFeeComponents(report crypto.FeeSettlementReport) error {
	seen := make(map[string]bool, len(report.Components))
	total := types.ZeroRawAmount()
	for _, component := range report.Components {
		if strings.TrimSpace(component.Name) == "" || seen[component.Name] || component.Quantity < 0 {
			return apperror.NewValidation("fee components require unique names and nonnegative quantities")
		}
		seen[component.Name] = true
		total = total.Add(component.Amount)
	}
	// Every chain reports its fee as components that add up exactly.
	if !total.Equal(report.TotalFee) {
		return apperror.NewValidation("fee component total differs from reported network fee")
	}
	return nil
}

func loadFeeSettlementReplay(ctx context.Context, tx pgx.Tx, f crypto.FeeReservation, r crypto.ChainReceipt, owner string, document id.ID, movements bool) (FeeSettlementResult, error) {
	result := FeeSettlementResult{ReservationID: f.ID}
	if owner == "" {
		owner, document = f.BasisType, f.BasisID
	}
	var matches bool
	var period time.Time
	err := tx.QueryRow(ctx, `SELECT id,total_fee_minor,observed_at,
		basis_type=$3 AND basis_id=$4 AND status=$5 AND block_height=$6
		AND regexp_replace(lower(block_hash),'^0x','')=regexp_replace(lower($7),'^0x','')
		AND native_token_id=$8 AND total_fee_minor=$9 AND total_fee_minor=$10
		AND network_fee_reservation_id IS NOT DISTINCT FROM $11::uuid
		FROM reg_crypto_chain_settlements WHERE network_id=$1 AND tx_hash=$2`, f.NetworkID, r.TxHash, owner, document,
		r.Status, r.BlockHeight, r.BlockHash, f.NativeTokenID, r.Fee.TotalFee, f.ActualFee, f.ID).Scan(&result.SettlementID, &result.ActualFee, &period, &matches)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.NewConflict("recorded fee settlement is missing; reconciliation is required")
	}
	if err != nil {
		return result, err
	}
	if !matches {
		return result, apperror.NewConflict("fee settlement replay differs from its recorded owner or outcome")
	}
	rows, err := tx.Query(ctx, `SELECT component,amount_minor,quantity FROM reg_crypto_chain_settlement_components WHERE settlement_id=$1`, result.SettlementID)
	if err != nil {
		return result, err
	}
	requested := make(map[string]crypto.FeeComponent, len(r.Fee.Components))
	for _, component := range r.Fee.Components {
		requested[component.Name] = component
	}
	count := 0
	for rows.Next() {
		var name string
		var amount types.RawAmount
		var quantity int64
		if err := rows.Scan(&name, &amount, &quantity); err != nil {
			rows.Close()
			return result, err
		}
		wanted, ok := requested[name]
		matches = matches && ok && wanted.Amount.Equal(amount) && wanted.Quantity == quantity
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if !matches || count != len(requested) {
		return result, apperror.NewConflict("fee settlement replay differs from its normalized components")
	}
	if movements {
		err = validateFeeSettlementMovements(ctx, tx, f, result.ActualFee, period)
	}
	return result, err
}

func validateFeeSettlementMovements(ctx context.Context, tx pgx.Tx, f crypto.FeeReservation, amount types.RawAmount, period time.Time) error {
	purpose := string(f.Purpose)
	if f.Purpose == crypto.SigningPurposeTreasury {
		purpose = "treasury"
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT CASE WHEN $4::numeric=0 THEN
		NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=$1)
		AND NOT EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE line_id=$1)
		ELSE EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=$1 AND recorder_id=$2 AND recorder_type='ChainSettlement'
		AND recorder_version=1 AND record_type='expense' AND wallet_id=$3 AND amount=$4 AND token_id=$5 AND period=$6 AND reversal_of_line_id IS NULL)
		AND EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE line_id=$1 AND recorder_id=$2 AND recorder_type='ChainSettlement'
		AND recorder_version=1 AND record_type='expense' AND network_id=$7 AND operation_token_id=$8 AND cost_token_id=$5
		AND cost_type='chain_network_fee' AND purpose=$9 AND provider='chain' AND external_order_id=$10 AND amount=$4 AND period=$6 AND reversal_of_line_id IS NULL) END`,
		f.ID, f.BasisID, f.PayerWalletID, amount, f.NativeTokenID, period, f.NetworkID, f.OperationTokenID, purpose, f.TxHash).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return apperror.NewConflict("existing fee movements differ from their settlement")
	}
	return nil
}
