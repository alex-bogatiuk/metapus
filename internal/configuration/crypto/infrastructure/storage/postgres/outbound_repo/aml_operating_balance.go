package outbound_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

// The caller already accounts for active fee reservations. This balance is
// operating capital alone; a funded principal wallet can still need JIT gas.
func amlAvailableOperatingBalance(ctx context.Context, wallet, native id.ID) (types.RawAmount, bool, error) {
	var balance types.RawAmount
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE(resource.amount,0)
 FROM cat_wallets wallet JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=wallet.custody_merchant_id AND scope.network_id=wallet.network_id AND scope.token_id=$2 AND scope.enabled
 LEFT JOIN reg_crypto_aml_resource_positions resource ON resource.wallet_id=wallet.id AND resource.token_id=scope.token_id
 WHERE wallet.id=$1`, wallet, native).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ZeroRawAmount(), false, nil
	}
	return balance, err == nil, err
}
