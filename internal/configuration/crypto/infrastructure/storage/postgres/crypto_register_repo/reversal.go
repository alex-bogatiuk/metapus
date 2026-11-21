package crypto_register_repo

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

// appendReversalsByRecorder appends linked opposite movements for the five
// crypto financial ledgers, which are append-only and never delete facts. The table and dimension identifiers are static package data,
// never request input. The ledger-reversal partial unique index makes retries safe.
func appendReversalsByRecorder[T any](
	ctx context.Context, r *register_repo.BaseAccumulationRepo[T], recorderID id.ID, beforeVersion int, dimensions []string,
) error {
	if beforeVersion <= 0 {
		return fmt.Errorf("positive reversal version is required")
	}
	columns := strings.Join(dimensions, ",")
	selected := make([]string, len(dimensions))
	for i, column := range dimensions {
		selected[i] = "m." + column
	}
	sql := fmt.Sprintf(`
		INSERT INTO %[1]s (
			line_id,recorder_id,recorder_type,recorder_version,period,
			record_type,%[2]s,amount,reversal_of_line_id,reversal_reason
		)
		SELECT gen_random_uuid_v7(),m.recorder_id,'CryptoLedgerReversal',$2,NOW(),
		       CASE WHEN m.record_type='receipt' THEN 'expense' ELSE 'receipt' END,
		       %[3]s,m.amount-COALESCE(reversed.amount,0),m.line_id,'posting_reversal'
		FROM %[1]s AS m
		LEFT JOIN LATERAL (SELECT SUM(r.amount) AS amount FROM %[1]s r
		    WHERE r.reversal_of_line_id=m.line_id) reversed ON TRUE
		WHERE m.recorder_id=$1 AND m.recorder_version<$2
		  AND m.reversal_of_line_id IS NULL
		  AND m.amount>COALESCE(reversed.amount,0)
		  AND NOT EXISTS (
		      SELECT 1 FROM %[1]s AS reversal
		      WHERE reversal.reversal_of_line_id=m.line_id
		        AND reversal.recorder_type='CryptoLedgerReversal'
		  )
		ON CONFLICT DO NOTHING
	`, r.MovementsTable(), columns, strings.Join(selected, ","))
	_, err := r.GetTxManager(ctx).GetQuerier(ctx).Exec(ctx, sql, recorderID, beforeVersion)
	if err != nil {
		return fmt.Errorf("append %s reversals: %w", r.MovementsTable(), err)
	}
	return nil
}
