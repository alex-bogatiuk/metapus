package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

var (
	ErrResourceCapacityInsufficient = outboundapp.ErrResourceCapacityInsufficient
	ErrResourceReclaimPending       = outboundapp.ErrResourceReclaimPending
	ErrResourceRentalExists         = outboundapp.ErrResourceRentalExists
)

// ResourceRequest describes one TRC-20 transaction that may consume a shared lease.
type ResourceRequest = outboundapp.ResourceRequest

type ProviderAccountRecord struct {
	ID                 id.ID
	Code               string
	Name               string
	Provider           string
	NetworkID          id.ID
	BalanceTokenID     id.ID
	ExternalAccountRef string
	CredentialsEnc     []byte
	CredentialsVersion int
	IsActive           bool
	IsFolder           bool
}

type ResourceRentalRecord = outboundapp.ResourceRental

type ResourceAllocationRecord = outboundapp.ResourceAllocation

type OrphanedResourceAllocation = outboundapp.OrphanedResourceAllocation

func deterministicMovementID(namespace id.ID, key string) id.ID {
	return uuid.NewSHA1(namespace, []byte(key))
}

func (r *OutboundRepo) GetProviderAccount(ctx context.Context, accountID id.ID) (*ProviderAccountRecord, error) {
	return r.getProviderAccount(ctx, accountID, false)
}

func (r *OutboundRepo) GetProviderAccountForRental(ctx context.Context, accountID id.ID) (*ProviderAccountRecord, error) {
	return r.getProviderAccount(ctx, accountID, true)
}

func (r *OutboundRepo) getProviderAccount(ctx context.Context, accountID id.ID, includeDeleted bool) (*ProviderAccountRecord, error) {
	txm := postgres.MustGetTxManager(ctx)
	var acc ProviderAccountRecord
	err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT id, code, name, provider, network_id, balance_token_id,
		       COALESCE(external_account_ref, ''),
		       credentials_enc, credentials_version, is_active, is_folder
		FROM cat_crypto_provider_accounts
		WHERE id = $1 AND ($2 OR deletion_mark = FALSE)
	`, accountID, includeDeleted).Scan(
		&acc.ID, &acc.Code, &acc.Name, &acc.Provider, &acc.NetworkID, &acc.BalanceTokenID,
		&acc.ExternalAccountRef, &acc.CredentialsEnc, &acc.CredentialsVersion, &acc.IsActive, &acc.IsFolder,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider account: %w", err)
	}
	return &acc, nil
}

func (r *OutboundRepo) GetActiveNetworkProviderAccounts(ctx context.Context, networkID id.ID) ([]*ProviderAccountRecord, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT id, code, name, provider, network_id, balance_token_id,
		       COALESCE(external_account_ref, ''), credentials_enc, credentials_version, is_active, is_folder
		FROM cat_crypto_provider_accounts
		WHERE network_id = $1 AND is_active = TRUE AND is_folder = FALSE AND deletion_mark = FALSE
		  AND credentials_enc IS NOT NULL AND length(credentials_enc) > 0
		ORDER BY name ASC, id ASC
	`, networkID)
	if err != nil {
		return nil, fmt.Errorf("list network resource provider accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]*ProviderAccountRecord, 0)
	for rows.Next() {
		account := &ProviderAccountRecord{}
		if err := rows.Scan(&account.ID, &account.Code, &account.Name, &account.Provider,
			&account.NetworkID, &account.BalanceTokenID, &account.ExternalAccountRef,
			&account.CredentialsEnc, &account.CredentialsVersion, &account.IsActive, &account.IsFolder); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (r *OutboundRepo) GetResourceAllocationByBasis(ctx context.Context, basisType string, basisID id.ID) (*ResourceAllocationRecord, error) {
	txm := postgres.MustGetTxManager(ctx)
	var result ResourceAllocationRecord
	err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT id, rental_id, generation, status, reserved_amount
		FROM reg_crypto_resource_allocations
		WHERE basis_type = $1 AND basis_id = $2 AND resource_type = 'energy'
		  AND status IN ('reserved','broadcast','confirmed')
		ORDER BY generation DESC
		LIMIT 1
	`, basisType, basisID).Scan(&result.ID, &result.RentalID, &result.Generation, &result.Status, &result.ReservedAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get resource allocation: %w", err)
	}
	return &result, nil
}

func scanResourceRental(row pgx.Row, claimed ...bool) (*ResourceRentalRecord, error) {
	var rental ResourceRentalRecord
	fields := []any{&rental.ID, &rental.ProviderAccountID, &rental.NetworkID, &rental.ReceiveWalletID,
		&rental.CostTokenID, &rental.RequestedAmount, &rental.DelegatedAmount,
		&rental.GrossCostAmount, &rental.RefundAmount, &rental.Provider,
		&rental.OutTradeNo, &rental.ExternalSerial, &rental.Status, &rental.ExpiresAt, &rental.ReceiveAddress}
	if len(claimed) > 0 && claimed[0] {
		fields = append(fields, &rental.MaintenanceClaimToken)
	}
	err := row.Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rental, nil
}

func (r *OutboundRepo) GetOpenWalletRental(ctx context.Context, networkID, walletID id.ID) (*ResourceRentalRecord, error) {
	txm := postgres.MustGetTxManager(ctx)
	rental, err := scanResourceRental(txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT id, provider_account_id, network_id, receive_wallet_id, cost_token_id,
		       requested_amount, delegated_amount, gross_cost_amount, refund_amount,
		       provider, out_trade_no, external_serial, status, expires_at,
		       (SELECT address FROM cat_wallets WHERE id=receive_wallet_id)
		FROM doc_crypto_resource_rentals
		WHERE network_id = $1 AND receive_wallet_id = $2
		  AND resource_type = 'energy'
		  AND status IN ('created','ordering','delegated','active','reclaiming','held')
		  AND deletion_mark = FALSE
		ORDER BY created_at DESC LIMIT 1
	`, networkID, walletID))
	if err != nil {
		return nil, fmt.Errorf("get open wallet resource rental: %w", err)
	}
	return rental, nil
}

func (r *OutboundRepo) TryReserveResource(ctx context.Context, req ResourceRequest, required int64, cutoff time.Duration) (*ResourceAllocationRecord, *ResourceRentalRecord, error) {
	if required <= 0 {
		return nil, nil, fmt.Errorf("required energy must be positive")
	}
	txm := postgres.MustGetTxManager(ctx)
	var allocation *ResourceAllocationRecord
	var selected *ResourceRentalRecord
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		querier := txm.GetQuerier(ctx)
		// Serialize all generations of one immutable business resource intent.
		// A released/failed reservation is historical evidence, not a reusable
		// active reservation, so a retry receives a fresh generation.
		if _, err := querier.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2::text || ':energy', 0))`, req.BasisType, req.BasisID); err != nil {
			return err
		}
		existing, err := r.GetResourceAllocationByBasis(ctx, req.BasisType, req.BasisID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := validateResourceReservationReplay(ctx, existing, req, required); err != nil {
				return err
			}
			allocation = existing
			return nil
		}
		rental, err := scanResourceRental(querier.QueryRow(ctx, `
			SELECT id, provider_account_id, network_id, receive_wallet_id, cost_token_id,
			       requested_amount, delegated_amount, gross_cost_amount, refund_amount,
			       provider, out_trade_no, external_serial, status, expires_at,
			       (SELECT address FROM cat_wallets WHERE id=receive_wallet_id)
			FROM doc_crypto_resource_rentals
			WHERE network_id = $1 AND receive_wallet_id = $2
			  AND resource_type = 'energy' AND status = 'active'
			  AND (expires_at IS NULL OR expires_at > NOW() + ($3 * interval '1 second'))
			  AND deletion_mark = FALSE
			ORDER BY created_at DESC LIMIT 1 FOR UPDATE
		`, req.NetworkID, req.SourceWalletID, int64(cutoff.Seconds())))
		if err != nil {
			return err
		}
		if rental == nil {
			selected, err = r.GetOpenWalletRental(ctx, req.NetworkID, req.SourceWalletID)
			return err
		}
		selected = rental
		var committed int64
		if err := querier.QueryRow(ctx, `
			SELECT COALESCE(SUM(CASE WHEN status = 'confirmed' THEN LEAST(actual_amount, reserved_amount)
			                              WHEN status IN ('reserved','broadcast') THEN reserved_amount ELSE 0 END), 0)
			FROM reg_crypto_resource_allocations WHERE rental_id = $1
		`, rental.ID).Scan(&committed); err != nil {
			return err
		}
		if rental.DelegatedAmount-committed < required {
			return ErrResourceCapacityInsufficient
		}
		allocationID := id.New()
		var generation int
		if err := querier.QueryRow(ctx, `
			SELECT COALESCE(MAX(generation),0)+1
			FROM reg_crypto_resource_allocations
			WHERE basis_type=$1 AND basis_id=$2 AND resource_type='energy'
		`, req.BasisType, req.BasisID).Scan(&generation); err != nil {
			return err
		}
		_, err = querier.Exec(ctx, `
			INSERT INTO reg_crypto_resource_allocations (
				id, rental_id, basis_type, basis_id, purpose, operation_token_id,
				source_wallet_id, resource_type, generation, status, reserved_amount
			) VALUES ($1,$2,$3,$4,$5,$6,$7,'energy',$8,'reserved',$9)
		`, allocationID, rental.ID, req.BasisType, req.BasisID, req.Purpose,
			req.OperationTokenID, req.SourceWalletID, generation, required)
		if err != nil {
			return err
		}
		_, err = querier.Exec(ctx, `
			UPDATE doc_crypto_resource_rentals SET reserved_amount = $2, updated_at = NOW(), version = version + 1 WHERE id = $1
		`, rental.ID, committed+required)
		if err != nil {
			return err
		}
		_, err = querier.Exec(ctx, `
			INSERT INTO reg_crypto_resource_allocation_events (allocation_id,event_type,new_status,details)
			VALUES ($1,'reserved','reserved',jsonb_build_object('reservedAmount',$2::bigint,'generation',$3::int))
		`, allocationID, required, generation)
		if err != nil {
			return err
		}
		allocation = &ResourceAllocationRecord{ID: allocationID, RentalID: rental.ID, Generation: generation, Status: "reserved", ReservedAmount: required}
		return nil
	})
	return allocation, selected, err
}

func (r *OutboundRepo) CreateResourceRental(ctx context.Context, rental *ResourceRentalRecord, req ResourceRequest, requestJSON []byte) error {
	rentalBasisType := req.RentalBasisType
	rentalBasisID := req.RentalBasisID
	if rentalBasisType == "" || id.IsNil(rentalBasisID) {
		rentalBasisType = req.BasisType
		rentalBasisID = req.BasisID
	}
	txm := postgres.MustGetTxManager(ctx)
	tag, err := txm.GetQuerier(ctx).Exec(ctx, `
		INSERT INTO doc_crypto_resource_rentals (
			id, number, date, basis_type, basis_id, provider_account_id, network_id,
			receive_wallet_id, cost_token_id, resource_type, rental_mode, rental_period,
			requested_amount, provider, out_trade_no, status, request_json, expires_at
		) VALUES ($1,$2,NOW(),$3,$4,$5,$6,$7,$8,'energy','fixed','1H',$9,$10,$11,'ordering',$12::jsonb,$13)
		ON CONFLICT DO NOTHING
	`, rental.ID, "RR-"+id.Compact(rental.ID),
		rentalBasisType, rentalBasisID, rental.ProviderAccountID, rental.NetworkID,
		rental.ReceiveWalletID, rental.CostTokenID, rental.RequestedAmount,
		rental.Provider, rental.OutTradeNo, string(requestJSON), rental.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create crypto resource rental: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrResourceRentalExists
	}
	return nil
}

func (r *OutboundRepo) ActivateResourceRental(ctx context.Context, rentalID id.ID, serial string, delegated int64, cost types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, expiresAt time.Time) error {
	if delegated <= 0 || strings.TrimSpace(serial) == "" {
		return apperror.NewValidation("resource rental requires a provider serial and positive delegation")
	}
	if len(responseJSON) == 0 {
		responseJSON = []byte(`{}`)
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		if err := lockRentalFinancialResources(ctx, rentalID); err != nil {
			return err
		}
		if err := validateResourceRentalClaim(ctx, rentalID, nil); err != nil {
			return err
		}
		var f rentalFinancialIdentity
		f.rental = rentalID
		var existingSerial, existingStatus string
		var existingDelegated int64
		var posted bool
		var postedVersion int
		if err := q.QueryRow(ctx, `SELECT provider_account_id,network_id,cost_token_id,provider,external_serial,delegated_amount,gross_cost_amount,refund_amount,status,posted,posted_version
 FROM doc_crypto_resource_rentals WHERE id=$1`, rentalID).Scan(&f.account, &f.network, &f.token, &f.provider, &existingSerial, &existingDelegated, &f.gross, &f.refund, &existingStatus, &posted, &postedVersion); err != nil {
			return err
		}
		f.serial = existingSerial
		if posted {
			if postedVersion != 1 || existingSerial != serial || existingDelegated != delegated || !f.gross.Equal(cost) {
				return apperror.NewConflict("resource rental activation replay differs from its original delegation or cost")
			}
			if err := validateRentalFinancials(ctx, q, f); err != nil {
				return err
			}
			if providerBalance != nil {
				return r.RecordProviderBalanceObservation(ctx, f.account, f.token, "rental-replay:"+rentalID.String(), *providerBalance)
			}
			return nil
		}
		if (existingStatus != "ordering" && existingStatus != "delegated" && existingStatus != "held") || f.gross.IsPositive() || f.refund.IsPositive() {
			return apperror.NewConflict("resource rental has an incompatible pending charge; reconciliation required")
		}
		// Even exact orphaned charge facts need reconciliation, not completion
		// of a partially persisted accounting generation.
		var pending bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=$1)
 OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1)`, rentalID).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return apperror.NewConflict("resource rental already has pending financial facts; reconciliation required")
		}
		var providerAccountID, networkID, costTokenID id.ID
		var provider, outTradeNo string
		if err := q.QueryRow(ctx, `
			UPDATE doc_crypto_resource_rentals
			SET external_serial=$2, delegated_amount=$3, gross_cost_amount=$4,
			    provider_balance_after=$5, response_json=$6::jsonb, status='active',
			    cost_status='recorded', posted=TRUE, posted_version=1,
			    delegated_at=COALESCE(delegated_at,NOW()), activated_at=COALESCE(activated_at,NOW()),
			    expires_at=$7, error_message='', updated_at=NOW(), version=version+1
			WHERE id=$1 AND status IN ('ordering','delegated','held')
			RETURNING provider_account_id, network_id, cost_token_id, provider, out_trade_no
		`, rentalID, serial, delegated, cost, providerBalance, string(responseJSON), expiresAt).
			Scan(&providerAccountID, &networkID, &costTokenID, &provider, &outTradeNo); err != nil {
			return fmt.Errorf("activate resource rental: %w", err)
		}
		if cost.IsPositive() {
			_, err := q.Exec(ctx, `
				INSERT INTO reg_crypto_platform_cost_movements (
					line_id,recorder_id,recorder_type,recorder_version,period,record_type,
					network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount
				) VALUES ($1,$1,'CryptoResourceRental',1,NOW(),'expense',$2,NULL,$3,'chain_resource_rent','resource_rental',$4,$5,$6)
				ON CONFLICT DO NOTHING
			`, rentalID, networkID, costTokenID, provider, serial, cost)
			if err != nil {
				return fmt.Errorf("record rental platform cost: %w", err)
			}
			_, err = q.Exec(ctx, `
				INSERT INTO reg_crypto_provider_balance_movements (
					line_id,recorder_id,recorder_type,recorder_version,period,record_type,
					provider_account_id,token_id,operation_type,external_ref,amount
				) VALUES ($1,$1,'CryptoResourceRental',1,NOW(),'expense',$2,$3,'rental_charge',$4,$5)
				ON CONFLICT DO NOTHING
			`, rentalID, providerAccountID, costTokenID, serial, cost)
			if err != nil {
				return fmt.Errorf("record provider rental charge: %w", err)
			}
		}
		f.serial, f.gross = serial, cost
		if err := validateRentalFinancials(ctx, q, f); err != nil {
			return err
		}
		if providerBalance != nil {
			return r.RecordProviderBalanceObservation(ctx, providerAccountID, costTokenID, outTradeNo, *providerBalance)
		}
		return nil
	})
}

type providerBalanceQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func recordProviderBalanceObservation(ctx context.Context, q providerBalanceQuerier, accountID, tokenID id.ID, externalRef string, providerAmount types.CryptoAmount) error {
	// Caller owns the transaction. Serialize the ledger snapshot with opening
	// decisions and all provider movement inserts (also guarded by PostgreSQL).
	var lockedAccount id.ID
	if err := q.QueryRow(ctx, `SELECT id FROM cat_crypto_provider_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&lockedAccount); err != nil {
		return err
	}
	// A provider account can legitimately have a negative internal ledger before
	// its opening balance is posted: the first observed rental charge is an
	// expense, while the pre-existing provider balance is still unexplained.
	// Preserve that signed evidence instead of forcing it through CryptoAmount.
	var ledgerAmount types.CryptoDelta
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0)
		FROM reg_crypto_provider_balance_movements WHERE provider_account_id=$1 AND token_id=$2
	`, accountID, tokenID).Scan(&ledgerAmount); err != nil {
		return err
	}
	providerDelta := types.ZeroCryptoAmount().Difference(providerAmount).Neg()
	delta := providerDelta.Add(ledgerAmount.Neg())
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_provider_balance_observations
		(provider_account_id,token_id,external_ref,ledger_amount,provider_amount,delta_amount)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, accountID, tokenID, externalRef, ledgerAmount, providerAmount, delta); err != nil {
		return err
	}
	if delta.IsZero() {
		_, err := q.Exec(ctx, `UPDATE reg_crypto_provider_balance_incidents
			SET status='resolved',delta_amount=0,ledger_amount=$3,provider_amount=$4,
			    resolution_reason='auto_reconciled',resolved_at=NOW(),updated_at=NOW()
			WHERE provider_account_id=$1 AND token_id=$2 AND status IN ('open','acknowledged')`, accountID, tokenID, ledgerAmount, providerAmount)
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_provider_balance_incidents
		(provider_account_id,token_id,ledger_amount,provider_amount,delta_amount)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (provider_account_id,token_id) WHERE status IN ('open','acknowledged')
		DO UPDATE SET ledger_amount=EXCLUDED.ledger_amount,provider_amount=EXCLUDED.provider_amount,
		              delta_amount=EXCLUDED.delta_amount,last_seen_at=NOW(),
		              occurrence_count=reg_crypto_provider_balance_incidents.occurrence_count+1,updated_at=NOW()
	`, accountID, tokenID, ledgerAmount, providerAmount, delta)
	return err
}

func (r *OutboundRepo) MarkResourceRentalHeld(ctx context.Context, rentalID id.ID, reason string, responseJSON []byte) error {
	return r.markResourceRentalHeld(ctx, rentalID, nil, reason, responseJSON)
}
func (r *OutboundRepo) markResourceRentalHeld(ctx context.Context, rentalID id.ID, claim *outboundapp.ResourceRentalClaim, reason string, responseJSON []byte) error {
	if len(responseJSON) == 0 {
		responseJSON = []byte(`{}`)
	}
	return mutateResourceRentalClaim(ctx, rentalID, claim, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET status='held',error_message=$2,response_json=$3::jsonb,updated_at=NOW(),version=version+1 WHERE id=$1 AND status NOT IN ('closed','failed')`, rentalID, statusFailedReason(errors.New(reason)), string(responseJSON))
		// Keep the original lease on held as the existing provider retry delay.
		return err
	})
}
func (r *OutboundRepo) MarkResourceRentalReclaiming(ctx context.Context, rentalID id.ID) error {
	return r.markResourceRentalReclaiming(ctx, rentalID, nil)
}
func (r *OutboundRepo) markResourceRentalReclaiming(ctx context.Context, rentalID id.ID, claim *outboundapp.ResourceRentalClaim) error {
	return mutateResourceRentalClaim(ctx, rentalID, claim, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_resource_rentals SET status='reclaiming',reclaim_requested_at=COALESCE(reclaim_requested_at,NOW()),updated_at=NOW(),version=version+1 WHERE id=$1 AND status IN ('active','held')`, rentalID)
		return err
	})
}

func (r *OutboundRepo) CloseResourceRental(ctx context.Context, rentalID id.ID, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error {
	return r.closeResourceRental(ctx, rentalID, nil, refund, providerBalance, responseJSON, status)
}
func (r *OutboundRepo) closeResourceRental(ctx context.Context, rentalID id.ID, claim *outboundapp.ResourceRentalClaim, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error {
	if status != "closed" && status != "failed" {
		return apperror.NewValidation("resource rental close requires a terminal outcome")
	}
	if len(responseJSON) == 0 {
		responseJSON = []byte(`{}`)
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		if err := lockRentalFinancialResources(ctx, rentalID); err != nil {
			return err
		}
		if err := validateResourceRentalClaim(ctx, rentalID, claim); err != nil {
			return err
		}
		var accountID, networkID, tokenID id.ID
		var provider, serial, outTradeNo string
		var gross, oldRefund types.CryptoAmount
		var currentStatus string
		var posted bool
		var postedVersion int
		var delegated int64
		if err := q.QueryRow(ctx, `SELECT provider_account_id,network_id,cost_token_id,provider,external_serial,out_trade_no,gross_cost_amount,refund_amount,delegated_amount,status,posted,posted_version FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, rentalID).Scan(&accountID, &networkID, &tokenID, &provider, &serial, &outTradeNo, &gross, &oldRefund, &delegated, &currentStatus, &posted, &postedVersion); err != nil {
			return err
		}
		if !posted || postedVersion != 1 {
			return apperror.NewConflict("resource rental has no complete recorded charge; reconciliation required")
		}
		if refund.Cmp(gross) > 0 {
			return apperror.NewConflict("resource rental refund exceeds its original charge")
		}
		if refund.Cmp(oldRefund) < 0 {
			return apperror.NewConflict("resource rental cumulative refund cannot decrease")
		}
		terminal := currentStatus == "closed" || currentStatus == "failed"
		if terminal && currentStatus != status {
			return apperror.NewConflict("resource rental terminal outcome cannot change")
		}
		f := rentalFinancialIdentity{rentalID, accountID, networkID, tokenID, provider, serial, gross, oldRefund}
		if err := validateRentalFinancials(ctx, q, f); err != nil {
			return err
		}
		if terminal && refund.Equal(oldRefund) {
			if providerBalance != nil {
				return r.RecordProviderBalanceObservation(ctx, accountID, tokenID, outTradeNo, *providerBalance)
			}
			return nil
		}
		delta := types.ZeroCryptoAmount()
		if refund.Cmp(oldRefund) > 0 {
			delta = refund.Sub(oldRefund)
		}
		net := gross.Sub(refund)
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_resource_allocations SET status='released',released_at=NOW(),updated_at=NOW() WHERE rental_id=$1 AND status='reserved'`, rentalID); err != nil {
			return err
		}
		if delegated > 0 {
			if _, err := q.Exec(ctx, `UPDATE reg_crypto_resource_allocations SET actual_amount=LEAST(actual_amount,reserved_amount),allocated_cost_amount=FLOOR(($2::numeric*LEAST(actual_amount,reserved_amount)::numeric)/$3::numeric),updated_at=NOW() WHERE rental_id=$1 AND status='confirmed'`, rentalID, net, delegated); err != nil {
				return err
			}
		}
		var consumed int64
		var allocated types.CryptoAmount
		if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(CASE WHEN status='confirmed' THEN LEAST(actual_amount,reserved_amount) ELSE 0 END),0),COALESCE(SUM(allocated_cost_amount),0) FROM reg_crypto_resource_allocations WHERE rental_id=$1`, rentalID).Scan(&consumed, &allocated); err != nil {
			return err
		}
		costStatus := "recorded"
		if refund.IsPositive() && refund.Cmp(gross) < 0 {
			costStatus = "partially_refunded"
		}
		if refund.Equal(gross) && gross.IsPositive() {
			costStatus = "refunded"
		}
		if _, err := q.Exec(ctx, `UPDATE doc_crypto_resource_rentals SET status=$2,cost_status=$3,refund_amount=$4,consumed_amount=$5,allocated_cost_amount=$6,unused_cost_amount=GREATEST($7::numeric-$6::numeric,0),provider_balance_after=$8,response_json=$9::jsonb,closed_at=COALESCE(closed_at,NOW()),claimed_by='',claimed_at=NULL,lease_expires_at=NULL,maintenance_claim_token=NULL,updated_at=NOW(),version=version+1 WHERE id=$1`, rentalID, status, costStatus, refund, consumed, allocated, net, providerBalance, string(responseJSON)); err != nil {
			return err
		}
		if delta.IsPositive() {
			externalRef := fmt.Sprintf("%s:refund:%s", serial, refund)
			providerLine := deterministicMovementID(rentalID, "provider:"+externalRef)
			platformLine := deterministicMovementID(rentalID, "platform:"+externalRef)
			if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_provider_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,provider_account_id,token_id,operation_type,external_ref,amount) VALUES($1,$2,'CryptoResourceRental',2,NOW(),'receipt',$3,$4,'refund',$5,$6) ON CONFLICT DO NOTHING`, providerLine, rentalID, accountID, tokenID, externalRef, delta); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount) VALUES($1,$2,'CryptoResourceRental',2,NOW(),'receipt',$3,NULL,$4,'chain_resource_rent','resource_rental',$5,$6,$7) ON CONFLICT DO NOTHING`, platformLine, rentalID, networkID, tokenID, provider, externalRef, delta); err != nil {
				return err
			}
		}
		f.refund = refund
		if err := validateRentalFinancials(ctx, q, f); err != nil {
			return err
		}
		if providerBalance != nil {
			return r.RecordProviderBalanceObservation(ctx, accountID, tokenID, outTradeNo, *providerBalance)
		}
		return nil
	})
}

func (r *OutboundRepo) MarkResourceAllocationBroadcast(ctx context.Context, command ResourceBroadcastCommand) error {
	if strings.TrimSpace(command.TxHash) == "" {
		return fmt.Errorf("resource allocation broadcast tx hash is required")
	}
	if id.IsNil(command.AllocationID) && (strings.TrimSpace(command.BasisType) == "" || id.IsNil(command.BasisID)) {
		return fmt.Errorf("resource allocation id or immutable basis is required")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var allocationID id.ID
		var oldStatus, existingTx string
		var basisType string
		var basisID id.ID
		err := q.QueryRow(ctx, `
			SELECT id,status,tx_hash,basis_type,basis_id
			FROM reg_crypto_resource_allocations
			WHERE (($1::uuid <> '00000000-0000-0000-0000-000000000000'::uuid AND id=$1)
			   OR ($1::uuid = '00000000-0000-0000-0000-000000000000'::uuid
			       AND basis_type=$2 AND basis_id=$3 AND resource_type='energy'))
			ORDER BY generation DESC
			LIMIT 1
			FOR UPDATE
		`, command.AllocationID, command.BasisType, command.BasisID).Scan(&allocationID, &oldStatus, &existingTx, &basisType, &basisID)
		if errors.Is(err, pgx.ErrNoRows) && id.IsNil(command.AllocationID) {
			// A fee strategy without an external resource has no allocation.
			return nil
		}
		if err != nil {
			return fmt.Errorf("load resource allocation for broadcast: %w", err)
		}
		if (command.BasisType != "" || !id.IsNil(command.BasisID)) && (command.BasisType != basisType || command.BasisID != basisID) {
			return apperror.NewConflict("resource broadcast command differs from its allocation basis")
		}

		var exactAuthorization bool
		if err := q.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM reg_crypto_resource_allocations a
				JOIN doc_crypto_resource_rentals rental ON rental.id=a.rental_id
				JOIN cat_wallets w ON w.id=a.source_wallet_id
				JOIN reg_crypto_signing_authorizations s
				  ON s.tx_hash=$2 AND s.status IN ('signed','broadcast','held')
				JOIN reg_crypto_expected_chain_effects e
				  ON e.signing_authorization_id=s.id
				 AND e.tx_hash=s.tx_hash
				 AND e.token_id=a.operation_token_id
				WHERE a.id=$1
				  AND s.network_id=rental.network_id AND w.network_id=rental.network_id AND e.network_id=rental.network_id
				  AND e.source_address_key=CASE WHEN s.chain_family='evm' THEN LOWER(w.address) ELSE w.address END
				  AND (
				       (a.basis_type=s.basis_type AND a.basis_id=s.basis_id)
				    OR (a.basis_type='CryptoSweepLine' AND s.basis_type='CryptoSweepAttempt' AND a.basis_id=s.basis_id)
				    OR (a.basis_type='CryptoWithdrawal' AND EXISTS (
				          SELECT 1 FROM doc_crypto_withdrawals d
				          WHERE d.id=a.basis_id AND d.basis_type=s.basis_type AND d.basis_id=s.basis_id
				       ))
				  )
			)
		`, allocationID, command.TxHash).Scan(&exactAuthorization); err != nil {
			return fmt.Errorf("validate resource allocation signing authorization: %w", err)
		}
		if !exactAuthorization {
			return fmt.Errorf("resource allocation has no exact signed chain-effect authorization")
		}
		if oldStatus == "confirmed" {
			if existingTx == command.TxHash {
				return validateResourceBroadcastEvent(ctx, allocationID, command.TxHash)
			}
			return fmt.Errorf("confirmed resource allocation belongs to another transaction")
		}
		if oldStatus != "reserved" && oldStatus != "broadcast" && oldStatus != "released" {
			return fmt.Errorf("resource allocation in status %s cannot be marked broadcast", oldStatus)
		}
		if existingTx != "" && existingTx != command.TxHash {
			return fmt.Errorf("resource allocation belongs to another transaction")
		}
		if oldStatus == "broadcast" {
			return validateResourceBroadcastEvent(ctx, allocationID, command.TxHash)
		}
		tag, err := q.Exec(ctx, `
			UPDATE reg_crypto_resource_allocations
			SET status='broadcast',tx_hash=$2,
			    broadcast_at=COALESCE(broadcast_at,NOW()),released_at=NULL,updated_at=NOW()
			WHERE id=$1 AND status=$3 AND actual_amount=0 AND (tx_hash='' OR tx_hash=$2)
		`, allocationID, command.TxHash, oldStatus)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("resource allocation cannot be marked broadcast")
		}
		eventType := "broadcast"
		if oldStatus == "released" {
			eventType = "broadcast_recovered"
		}
		_, err = q.Exec(ctx, `
			INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details)
			VALUES($1,$2,$3,'broadcast',jsonb_build_object('txHash',$4::text,'exactAuthorization',true))
			ON CONFLICT DO NOTHING
		`, allocationID, eventType, oldStatus, command.TxHash)
		if err != nil {
			return err
		}
		return validateResourceBroadcastEvent(ctx, allocationID, command.TxHash)
	})
}

func (r *OutboundRepo) SettleResourceAllocation(ctx context.Context, txHash string, receipt crypto.ChainReceipt) error {
	if receipt.TxHash != txHash || !receipt.FinanciallyFinal() {
		return fmt.Errorf("final chain receipt is required")
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var allocationID, rentalID id.ID
		if err := q.QueryRow(ctx, `SELECT rental_id FROM reg_crypto_resource_allocations WHERE tx_hash=$1`, txHash).Scan(&rentalID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		var network id.ID
		if err := q.QueryRow(ctx, `SELECT network_id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, rentalID).Scan(&network); err != nil {
			return err
		}
		if network != receipt.NetworkID {
			return apperror.NewConflict("resource allocation receipt belongs to another network")
		}
		var reserved int64
		var currentStatus, currentResult string
		var currentActual int64
		err := q.QueryRow(ctx, `SELECT id,reserved_amount,status,actual_amount,chain_result FROM reg_crypto_resource_allocations WHERE tx_hash=$1 AND rental_id=$2 FOR UPDATE`, txHash, rentalID).Scan(&allocationID, &reserved, &currentStatus, &currentActual, &currentResult)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewConflict("resource allocation identity changed during settlement")
		}
		if err != nil {
			return err
		}
		energyUsed := int64(0)
		for _, component := range receipt.Fee.Components {
			if component.Name == "energy_fee" {
				energyUsed = component.Quantity
				break
			}
		}
		actual := min(max(energyUsed, 0), reserved)
		result := "SUCCESS"
		if receipt.Status == crypto.ReceiptReverted {
			result = "REVERTED"
		}
		if currentStatus == "confirmed" {
			if currentActual != actual || currentResult != result {
				return apperror.NewConflict("resource allocation settlement replay differs from its original usage or outcome")
			}
			return nil
		}
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_resource_allocations SET status='confirmed',actual_amount=$2,chain_result=$3,confirmed_at=COALESCE(confirmed_at,NOW()),updated_at=NOW() WHERE id=$1`, allocationID, actual, result); err != nil {
			return err
		}
		var net types.CryptoAmount
		var delegated int64
		if err := q.QueryRow(ctx, `SELECT net_cost_amount,delegated_amount FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, rentalID).Scan(&net, &delegated); err != nil {
			return err
		}
		allocated := types.ZeroCryptoAmount()
		if delegated > 0 {
			allocated = net.MulDiv(actual, delegated)
		}
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_resource_allocations SET allocated_cost_amount=$2 WHERE id=$1`, allocationID, allocated); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `UPDATE doc_crypto_resource_rentals r SET consumed_amount=s.consumed,allocated_cost_amount=s.allocated,unused_cost_amount=GREATEST(r.net_cost_amount-s.allocated,0),updated_at=NOW(),version=version+1 FROM(SELECT COALESCE(SUM(LEAST(actual_amount,reserved_amount)),0) consumed,COALESCE(SUM(allocated_cost_amount),0) allocated FROM reg_crypto_resource_allocations WHERE rental_id=$1 AND status='confirmed')s WHERE r.id=$1`, rentalID); err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'confirmed','broadcast','confirmed',jsonb_build_object('txHash',$2::text,'actualEnergy',$3::bigint,'allocatedCost',$4::text,'chainResult',$5::text)) ON CONFLICT DO NOTHING`, allocationID, txHash, actual, allocated, result)
		return err
	})
}

// ListOrphanedResourceAllocations returns reservations whose exact signed
// transaction is already recorded as broadcast but whose allocation was not
// attached to it (for example, after a crash between the two durable writes).
func (r *OutboundRepo) ListOrphanedResourceAllocations(ctx context.Context, limit int) ([]OrphanedResourceAllocation, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (a.id)
		       a.id,s.network_id,a.basis_type,a.basis_id,s.tx_hash
		FROM reg_crypto_resource_allocations a
		JOIN cat_wallets w ON w.id=a.source_wallet_id
		JOIN reg_crypto_signing_authorizations s
		  ON s.status='broadcast' AND s.tx_hash<>''
		 AND (
		      (a.basis_type=s.basis_type AND a.basis_id=s.basis_id)
		   OR (a.basis_type='CryptoSweepLine' AND s.basis_type='CryptoSweepAttempt' AND a.basis_id=s.basis_id)
		   OR (a.basis_type='CryptoWithdrawal' AND EXISTS (
		         SELECT 1 FROM doc_crypto_withdrawals d
		         WHERE d.id=a.basis_id AND d.basis_type=s.basis_type AND d.basis_id=s.basis_id
		      ))
		 )
		JOIN reg_crypto_expected_chain_effects e
		  ON e.signing_authorization_id=s.id
		 AND e.tx_hash=s.tx_hash
		 AND e.token_id=a.operation_token_id
		 AND e.source_address_key=CASE WHEN s.chain_family='evm' THEN LOWER(w.address) ELSE w.address END
		WHERE a.status IN ('reserved','released') AND a.tx_hash=''
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_resource_allocations active
		      WHERE active.basis_type=a.basis_type
		        AND active.basis_id=a.basis_id
		        AND active.resource_type=a.resource_type
		        AND active.status IN ('broadcast','confirmed')
		  )
		ORDER BY a.id,s.generation DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list orphaned resource allocations: %w", err)
	}
	defer rows.Close()
	result := make([]OrphanedResourceAllocation, 0, limit)
	for rows.Next() {
		var item OrphanedResourceAllocation
		if err := rows.Scan(&item.AllocationID, &item.NetworkID, &item.BasisType, &item.BasisID, &item.TxHash); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OutboundRepo) ReleaseResourceAllocation(ctx context.Context, allocationID id.ID, reason string) error {
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var rental id.ID
		if err := q.QueryRow(ctx, `SELECT rental_id FROM reg_crypto_resource_allocations WHERE id=$1`, allocationID).Scan(&rental); err != nil {
			return err
		}
		if err := lockRentalFinancialResources(ctx, rental); err != nil {
			return err
		}
		var status string
		var current id.ID
		if err := q.QueryRow(ctx, `SELECT rental_id,status FROM reg_crypto_resource_allocations WHERE id=$1 FOR UPDATE`, allocationID).Scan(&current, &status); err != nil {
			return err
		}
		if current != rental {
			return apperror.NewConflict("resource allocation rental identity changed")
		}
		normalized := statusFailedReason(errors.New(reason))
		if status == "released" {
			return validateResourceReleaseReplay(ctx, allocationID, rental, normalized)
		}
		if status != "reserved" {
			return nil
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'released','reserved','released',jsonb_build_object('reason',$2::text)) ON CONFLICT DO NOTHING`, allocationID, normalized); err != nil {
			return err
		}
		if err := validateResourceReleaseReplay(ctx, allocationID, rental, normalized); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE reg_crypto_resource_allocations SET status='released',released_at=NOW(),updated_at=NOW() WHERE id=$1 AND status='reserved'`, allocationID)
		return err
	})
}

func (r *OutboundRepo) ClaimResourceRentalsForMaintenance(ctx context.Context, workerID string, limit int) ([]*ResourceRentalRecord, error) {
	if workerID == "" {
		return nil, apperror.NewValidation("resource rental maintenance requires a worker")
	}
	var result []*ResourceRentalRecord
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var isolation string
		if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("resource rental maintenance requires read committed isolation")
		}
		var err error
		result, err = r.claimResourceRentalsForMaintenance(ctx, workerID, limit)
		return err
	})
	return result, err
}
func (r *OutboundRepo) claimResourceRentalsForMaintenance(ctx context.Context, workerID string, limit int) ([]*ResourceRentalRecord, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH picked AS (
			SELECT id FROM doc_crypto_resource_rentals
			WHERE external_serial<>'' AND deletion_mark=FALSE
			  AND ((status='active' AND expires_at<=clock_timestamp()) OR status IN ('reclaiming','held'))
			  AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp())
			ORDER BY COALESCE(expires_at,updated_at),id
			FOR UPDATE SKIP LOCKED LIMIT $1
		), claimed AS (
			UPDATE doc_crypto_resource_rentals r
			SET claimed_by=$2,claimed_at=clock_timestamp(),lease_expires_at=clock_timestamp()+($3*interval '1 second'),maintenance_claim_token=gen_random_uuid_v7(),updated_at=NOW()
			FROM picked p WHERE r.id=p.id
			RETURNING r.id,r.provider_account_id,r.network_id,r.receive_wallet_id,r.cost_token_id,
			          r.requested_amount,r.delegated_amount,r.gross_cost_amount,r.refund_amount,
			          r.provider,r.out_trade_no,r.external_serial,r.status,r.expires_at,
			          (SELECT address FROM cat_wallets WHERE id=r.receive_wallet_id),r.maintenance_claim_token
		)
		SELECT * FROM claimed
	`, limit, workerID, int64(outboundSettlementLease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*ResourceRentalRecord, 0, limit)
	for rows.Next() {
		rental, err := scanResourceRental(rows, true)
		if err != nil {
			return nil, err
		}
		result = append(result, rental)
	}
	return result, rows.Err()
}

// RecordProviderBalanceObservation records provider evidence in the caller's transaction.
func (r *OutboundRepo) RecordProviderBalanceObservation(ctx context.Context, accountID, tokenID id.ID, externalRef string, providerAmount types.CryptoAmount) error {
	return recordProviderBalanceObservation(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), accountID, tokenID, externalRef, providerAmount)
}
