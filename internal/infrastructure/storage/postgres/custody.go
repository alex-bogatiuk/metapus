package postgres

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
)

// MerchantCustodyEnforced is independent of AML enablement. Read failures
// propagate; they never fall back to shared merchant liquidity.
func MerchantCustodyEnforced(ctx context.Context, networkID id.ID) (bool, error) {
	var enforced bool
	err := MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=$1 AND enforced)`, networkID).Scan(&enforced)
	if err != nil {
		return false, fmt.Errorf("read merchant custody policy: %w", err)
	}
	return enforced, nil
}
