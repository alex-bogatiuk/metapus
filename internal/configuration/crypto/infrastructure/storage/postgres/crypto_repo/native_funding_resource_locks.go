package crypto_repo

import (
	"context"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

// LockNativeFundingResources must precede funding row locks even for lease or
// retry metadata: the SQL resource queue hook locks its custody scope too.
func LockNativeFundingResources(ctx context.Context, funding id.ID, extra ...id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("native funding resources require a transaction")
	}
	_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE id=$1
		UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE id=$1
		UNION SELECT fee.payer_wallet_id FROM doc_crypto_native_fundings f
		JOIN reg_crypto_fee_reservations fee ON fee.id=f.fee_reservation_id WHERE f.id=$1
		UNION SELECT unnest($2::uuid[])))`, funding, extra)
	return err
}
