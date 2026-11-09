package catalog_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
)

var _ wallet.DepositRotationRepository = (*WalletRepo)(nil)

// RotatePersistentAddress shares the allocator's admission lock and reserve.
// Lock order is idempotency key -> network policy -> customer -> wallet.
// Intake also locks the wallet before creating any financial document.
func (r *WalletRepo) RotatePersistentAddress(ctx context.Context, command wallet.DepositRotationCommand) (*wallet.DepositRotation, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	txm := r.getTxManager(ctx)
	if txm.GetTx(ctx) == nil {
		return nil, apperror.NewValidation("deposit rotation requires a transaction")
	}
	q := txm.GetQuerier(ctx)
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"deposit-rotation-key:"+command.MerchantID.String()+":"+command.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("lock deposit rotation retry: %w", err)
	}
	var previous wallet.DepositRotation
	err := pgxscan.Get(ctx, q, &previous, `SELECT id,merchant_id,network_id,network_code,network_name,tokens,customer_id,old_wallet_id,new_wallet_id,
 old_address,new_address,reason,actor_id,idempotency_key,version,created_at
 FROM reg_crypto_deposit_address_rotations WHERE merchant_id=$1 AND idempotency_key=$2`, command.MerchantID, command.IdempotencyKey)
	if err == nil {
		if previous.OldWalletID != command.WalletID || previous.Reason != command.Reason {
			return nil, apperror.NewConflict("Idempotency-Key was already used for another rotation")
		}
		previous.Replayed = true
		return &previous, nil
	}
	if !pgxscan.NotFound(err) {
		return nil, fmt.Errorf("read deposit rotation retry: %w", err)
	}
	var networkID id.ID
	var customerID string
	err = q.QueryRow(ctx, `SELECT network_id,customer_id FROM cat_wallets
 WHERE id=$1 AND merchant_id=$2 AND allocation_mode='persistent' AND NOT deletion_mark AND _deleted_at IS NULL`, command.WalletID, command.MerchantID).Scan(&networkID, &customerID)
	if pgxscan.NotFound(err) {
		return nil, apperror.NewNotFound("deposit_address", command.WalletID)
	}
	if err != nil {
		return nil, fmt.Errorf("find address to rotate: %w", err)
	}
	policy, err := r.lockEffectiveAllocationPolicy(ctx, command.MerchantID, networkID)
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"deposit-rotation-customer:"+command.MerchantID.String()+":"+customerID); err != nil {
		return nil, fmt.Errorf("lock customer rotation: %w", err)
	}
	var oldAddress, status string
	var retiredAt *time.Time
	err = q.QueryRow(ctx, `SELECT address,status,deposit_retired_at FROM cat_wallets
 WHERE id=$1 AND merchant_id=$2 AND network_id=$3 AND customer_id=$4 AND allocation_mode='persistent'
 AND is_active AND NOT deletion_mark AND _deleted_at IS NULL FOR UPDATE`,
		command.WalletID, command.MerchantID, networkID, customerID).Scan(&oldAddress, &status, &retiredAt)
	if pgxscan.NotFound(err) {
		return nil, apperror.NewNotFound("deposit_address", command.WalletID)
	}
	if err != nil {
		return nil, fmt.Errorf("lock deposit address: %w", err)
	}
	if retiredAt != nil {
		return nil, apperror.NewConflict("deposit address has already been retired").WithDetail("reason", "retired")
	}
	if status == string(wallet.WalletStatusSweeping) {
		return nil, apperror.NewConflict("wait for the current sweep to finish").WithDetail("reason", "sweep_in_progress")
	}
	var used, pending bool
	err = q.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM doc_crypto_payments p WHERE p.wallet_id=$1 AND p.merchant_id=$2
   AND p.canonicality='canonical' AND p.status IN ('confirmed','settled') AND p._deleted_at IS NULL),
 EXISTS(SELECT 1 FROM doc_crypto_payments p WHERE p.wallet_id=$1 AND p.status IN ('detected','confirming') AND p._deleted_at IS NULL)
 OR EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.wallet_id=$1 AND a.status IN ('claimed','resource_ready','signing','broadcast','held'))`,
		command.WalletID, command.MerchantID).Scan(&used, &pending)
	if err != nil {
		return nil, fmt.Errorf("check deposit address history: %w", err)
	}
	if !used {
		return nil, apperror.NewConflict("unused deposit addresses cannot be replaced").WithDetail("reason", "unused_address")
	}
	if pending {
		return nil, apperror.NewConflict("address has an unfinished payment or sweep").WithDetail("reason", "operation_in_progress")
	}
	var now time.Time
	var customerRetry, merchantCount, revision int
	err = q.QueryRow(ctx, `SELECT NOW(),
 COALESCE((SELECT GREATEST(0,CEIL(EXTRACT(EPOCH FROM MAX(created_at)+INTERVAL '24 hours'-NOW())))::INT
 FROM reg_crypto_deposit_address_rotations WHERE merchant_id=$1 AND customer_id=$2),0),
 (SELECT COUNT(*)::INT FROM reg_crypto_deposit_address_rotations WHERE merchant_id=$1 AND network_id=$3 AND created_at>NOW()-INTERVAL '24 hours'),
 (SELECT COALESCE(MAX(version),0)+1 FROM reg_crypto_deposit_address_rotations WHERE merchant_id=$1 AND customer_id=$2 AND network_id=$3)`,
		command.MerchantID, customerID, networkID).Scan(&now, &customerRetry, &merchantCount, &revision)
	if err != nil {
		return nil, fmt.Errorf("check deposit rotation limits: %w", err)
	}
	if customerRetry > 0 {
		return nil, apperror.NewConflict("customer address can be replaced once every 24 hours").WithDetail("reason", "rotation_cooldown").WithDetail("retryAfterSeconds", customerRetry)
	}
	if merchantCount >= policy.MaxPersistentAddresses {
		return nil, apperror.NewConflict("merchant daily address replacement limit reached").WithDetail("reason", "merchant_rotation_limit")
	}
	ready, err := r.countReadyForAllocation(ctx, command.MerchantID, networkID, policy.CustodyEnforced)
	if err != nil {
		return nil, err
	}
	if ready == 0 || (!policy.CustodyEnforced && ready <= policy.MinGlobalReadyReserve) {
		return nil, apperror.NewWalletPoolExhausted(networkID.String(), policy.RetryAfterSeconds)
	}
	rateRetryAfter, err := r.consumeLeaseRateWindow(ctx, command.MerchantID, networkID, policy.MaxLeaseStartsPerMinute)
	if err != nil {
		return nil, err
	}
	if rateRetryAfter > 0 {
		return nil, apperror.NewMerchantWalletLeaseLimitExceeded(networkID.String(), rateRetryAfter)
	}
	// Preserve the customer and custody association. Retired addresses remain
	// active watch targets; retirement independently forbids allocation/sweep.
	if _, err := q.Exec(ctx, `UPDATE cat_wallets SET deposit_retired_at=$2,status='assigned',version=version+1,updated_at=$2 WHERE id=$1`, command.WalletID, now); err != nil {
		return nil, fmt.Errorf("retire deposit address: %w", err)
	}
	assigned, err := r.assignReadyPersistentWallet(ctx, command.MerchantID, networkID, customerID, policy.RetryAfterSeconds, policy.CustodyEnforced, true)
	if err != nil {
		return nil, err // outer application transaction also restores the old address
	}
	rotation := &wallet.DepositRotation{
		ID: id.New(), MerchantID: command.MerchantID, NetworkID: networkID, CustomerID: customerID,
		OldWalletID: command.WalletID, NewWalletID: assigned.ID, OldAddress: oldAddress, NewAddress: assigned.Address,
		Reason: command.Reason, ActorID: command.ActorID, IdempotencyKey: command.IdempotencyKey,
		Version: revision, CreatedAt: now,
	}
	if err := q.QueryRow(ctx, `SELECT n.code,n.name,COALESCE(ARRAY_AGG(DISTINCT t.symbol ORDER BY t.symbol) FILTER(WHERE t.id IS NOT NULL),ARRAY[]::TEXT[])
 FROM cat_blockchain_networks n LEFT JOIN cat_tokens t ON t.network_id=n.id AND t.is_active AND NOT t.deletion_mark AND t._deleted_at IS NULL
 WHERE n.id=$1 AND n.is_active AND NOT n.deletion_mark GROUP BY n.id,n.code,n.name`, networkID).Scan(&rotation.NetworkCode, &rotation.NetworkName, &rotation.Tokens); err != nil {
		return nil, fmt.Errorf("snapshot rotated address network: %w", err)
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_deposit_address_rotations
 (id,merchant_id,network_id,customer_id,old_wallet_id,new_wallet_id,old_address,new_address,reason,actor_id,idempotency_key,version,created_at,updated_at,network_code,network_name,tokens)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$14,$15,$16)`,
		rotation.ID, rotation.MerchantID, rotation.NetworkID, rotation.CustomerID, rotation.OldWalletID, rotation.NewWalletID,
		rotation.OldAddress, rotation.NewAddress, rotation.Reason, rotation.ActorID, rotation.IdempotencyKey, rotation.Version, rotation.CreatedAt, rotation.NetworkCode, rotation.NetworkName, rotation.Tokens)
	if err != nil {
		return nil, fmt.Errorf("save deposit address rotation: %w", err)
	}
	return rotation, nil
}
