package outbound_repo

import (
	"context"
	"sort"
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"
)

// Provider movements and observations share the account lock. Take it before
// the rental row, and reject a changed FK rather than acquiring another account
// after the rental lock. No provider RPC runs in this transaction.
func lockRentalFinancialResources(ctx context.Context, rental id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return apperror.NewValidation("resource rental financial writes require read committed isolation")
	}
	var account, current id.ID
	if err := q.QueryRow(ctx, `SELECT provider_account_id FROM doc_crypto_resource_rentals WHERE id=$1`, rental).Scan(&account); err != nil {
		return err
	}
	if err := q.QueryRow(ctx, `SELECT id FROM cat_crypto_provider_accounts WHERE id=$1 FOR UPDATE`, account).Scan(&current); err != nil {
		return err
	}
	if err := q.QueryRow(ctx, `SELECT provider_account_id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, rental).Scan(&current); err != nil {
		return err
	}
	if current != account {
		return apperror.NewConflict("resource rental provider account changed; retry the command")
	}
	return nil
}

type rentalFinancialIdentity struct {
	rental, account, network, token id.ID
	provider, serial                string
	gross, refund                   types.RawAmount
}

// The cumulative refund sequence is reconstructed from immutable provider
// facts. Each event must have the exact platform counterpart, including the
// original period. A partial historical set is never filled in by replay.
func validateRentalFinancials(ctx context.Context, q postgres.Querier, f rentalFinancialIdentity) error {
	type fact struct {
		line               id.ID
		version            int
		ref                string
		amount, cumulative types.RawAmount
		period             time.Time
		matches            bool
	}
	rows, err := q.Query(ctx, `SELECT line_id,recorder_version,external_ref,amount,period,
 recorder_type='CryptoResourceRental' AND provider_account_id=$2 AND token_id=$3
 AND ((recorder_version=1 AND record_type='expense' AND operation_type='rental_charge')
 OR (recorder_version=2 AND record_type='receipt' AND operation_type='refund'))
 FROM reg_crypto_provider_balance_movements WHERE recorder_id=$1`, f.rental, f.account, f.token)
	if err != nil {
		return err
	}
	var facts []fact
	for rows.Next() {
		var item fact
		if err := rows.Scan(&item.line, &item.version, &item.ref, &item.amount, &item.period, &item.matches); err != nil {
			rows.Close()
			return err
		}
		facts = append(facts, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	conflict := func() error {
		return apperror.NewConflict("resource rental financial history differs from its recorded charge or refund; reconciliation required")
	}
	charges := 0
	for i := range facts {
		item := &facts[i]
		if !item.matches || !item.amount.IsPositive() {
			return conflict()
		}
		if item.version == 1 {
			charges++
			if item.line != f.rental || item.ref != f.serial || !item.amount.Equal(f.gross) {
				return conflict()
			}
		} else {
			prefix := f.serial + ":refund:"
			if !strings.HasPrefix(item.ref, prefix) {
				return conflict()
			}
			item.cumulative, err = types.NewRawAmountFromString(strings.TrimPrefix(item.ref, prefix))
			if err != nil || item.ref != prefix+item.cumulative.String() || item.line != deterministicMovementID(f.rental, "provider:"+item.ref) {
				return conflict()
			}
		}
		platform := f.rental
		kind := "expense"
		if item.version == 2 {
			platform = deterministicMovementID(f.rental, "platform:"+item.ref)
			kind = "receipt"
		}
		var matches bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE line_id=$1
 AND recorder_id=$2 AND recorder_type='CryptoResourceRental' AND recorder_version=$3 AND period=$4
 AND record_type=$5 AND network_id=$6 AND operation_token_id IS NULL AND cost_token_id=$7
 AND cost_type='chain_resource_rent' AND purpose='resource_rental' AND provider=$8 AND external_order_id=$9 AND amount=$10 AND reversal_of_line_id IS NULL)`,
			platform, f.rental, item.version, item.period, kind, f.network, f.token, f.provider, item.ref, item.amount).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return conflict()
		}
	}
	if (f.gross.IsPositive() && charges != 1) || (!f.gross.IsPositive() && len(facts) != 0) {
		return conflict()
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].cumulative.Cmp(facts[j].cumulative) < 0 })
	total := types.ZeroRawAmount()
	for _, item := range facts {
		if item.version != 2 {
			continue
		}
		total = total.Add(item.amount)
		if !total.Equal(item.cumulative) {
			return conflict()
		}
	}
	if !total.Equal(f.refund) || total.Cmp(f.gross) > 0 {
		return conflict()
	}
	var costs int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1`, f.rental).Scan(&costs); err != nil {
		return err
	}
	if costs != len(facts) {
		return conflict()
	}
	return nil
}
