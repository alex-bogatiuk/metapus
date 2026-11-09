//go:build integration

package crypto_repo

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAMLCancellationConcurrentReplayRetainsFirstAuditFact(t *testing.T) {
	for _, resource := range []bool{false, true} {
		name := "allocation"
		if resource {
			name = "resource"
		}
		t.Run(name, func(t *testing.T) {
			f, destination, _ := readyAMLInventory(t)
			table, key := "reg_crypto_aml_allocation_cancellations", "allocation_id"
			allocation, err := f.repo.ReserveAMLInternalAllocation(f.ctx, f.allocationRequest(t, destination, "50"))
			require.NoError(t, err)
			cancel := func(reason, actor string) error {
				return f.repo.CancelAMLInternalAllocation(f.ctx, allocation.ID, reason, actor)
			}
			factID := allocation.ID
			if resource {
				f, destination, _ = nativeResourceFixture(t)
				fee, _ := resourceFee(t, f, destination, "20", "10")
				budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
				require.NoError(t, err)
				factID = budget.ID
				table, key = "reg_crypto_aml_resource_cancellations", "budget_id"
				cancel = func(reason, actor string) error {
					return f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, reason, actor)
				}
			}
			// Existing audit evidence is never silently consumed with a new reason.
			_, err = f.pool.Exec(f.ctx, `INSERT INTO `+table+`(`+key+`,reason,cancelled_by) VALUES($1,'original reason','operator')`, factID)
			require.NoError(t, err)
			var before string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(c)::text FROM `+table+` c WHERE `+key+`=$1`, factID).Scan(&before))
			require.ErrorContains(t, cancel("different reason", "operator"), "original reason or actor")
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Go(func() { errs <- cancel("original reason", "operator") })
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.ErrorContains(t, cancel("original reason", "different actor"), "original reason or actor")
			var after string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(c)::text FROM `+table+` c WHERE `+key+`=$1`, factID).Scan(&after))
			require.Equal(t, before, after)
			var events int
			if resource {
				require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_fee_reservation_events WHERE reservation_id=(SELECT fee_reservation_id FROM reg_crypto_aml_resource_budgets WHERE id=$1) AND to_status='released'`, factID).Scan(&events))
				require.Equal(t, 1, events, "replay must not repeat the fee release event")
			}
		})
	}
}
