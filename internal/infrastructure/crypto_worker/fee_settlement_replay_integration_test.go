//go:build integration

package crypto_worker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func seedLegacyFeeReplay(t *testing.T, ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) (id.ID, id.ID, crypto.ChainReceipt) {
	t.Helper()
	reservation, basis := id.New(), id.New()
	hash := id.Compact(reservation) + id.Compact(reservation)
	_, err := tm.GetQuerier(ctx).Exec(ctx, "INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100)", f.hot, f.native)
	require.NoError(t, err)
	_, err = tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status)
        VALUES(1,$1,$2,$3,$4,$4,'FeeReplayTest',$5,'sweep',1,'native_burn',10,$6,'broadcast')`, reservation, f.network, f.hot, f.native, basis, hash)
	require.NoError(t, err)
	receipt := crypto.ChainReceipt{NetworkID: f.network, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHash: strings.Repeat("a", 64), BlockHeight: 100, Confirmations: 20, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, ObservedAt: time.Now().UTC().Truncate(time.Microsecond), RawReceipt: []byte(`{"first":true}`),
		Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.native}, TotalFee: types.MustCryptoAmount("2"), Components: []crypto.FeeComponent{{Name: "energy_fee", Amount: types.MustCryptoAmount("1"), Quantity: 1}, {Name: "bandwidth_fee", Amount: types.MustCryptoAmount("1"), Quantity: 2}}}}
	return reservation, basis, receipt
}

func TestLegacyFeeReplayRequiresExactNormalizedOutcomeAndComponents(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		reservation, _, receipt := seedLegacyFeeReplay(t, ctx, tm, f)
		manager := NewFeeBudgetManager()
		var workers sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			workers.Go(func() { results <- manager.Settle(ctx, reservation, receipt) })
		}
		workers.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		for _, changed := range []string{"outcome", "block", "height", "fee", "quantity", "missing_component", "extra_component", "duplicate_component", "owner"} {
			t.Run(changed, func(t *testing.T) {
				replay := receipt
				replay.Fee.Components = append([]crypto.FeeComponent(nil), receipt.Fee.Components...)
				switch changed {
				case "outcome":
					replay.Status = crypto.ReceiptReverted
				case "block":
					replay.BlockHash = strings.Repeat("b", 64)
				case "height":
					replay.BlockHeight++
				case "fee":
					replay.Fee.TotalFee = types.MustCryptoAmount("3")
					replay.Fee.Components[0].Amount = types.MustCryptoAmount("2")
				case "quantity":
					replay.Fee.Components[0].Quantity++
				case "missing_component":
					replay.Fee.Components = nil
				case "extra_component":
					replay.Fee.Components = append(replay.Fee.Components, crypto.FeeComponent{Name: "other", Amount: types.ZeroCryptoAmount()})
				case "duplicate_component":
					replay.Fee.Components[1].Name = replay.Fee.Components[0].Name
				}
				var err error
				if changed == "owner" {
					_, err = manager.SettleForDocument(ctx, reservation, replay, "CryptoNetworkOperation", id.New())
				} else {
					err = manager.Settle(ctx, reservation, replay)
				}
				require.Error(t, err)
			})
		}
		later := receipt
		later.ObservedAt = receipt.ObservedAt.Add(time.Hour)
		later.Finality = crypto.FinalityFinalized
		later.Confirmations++
		later.RawReceipt = []byte(`{"laterProvider":true}`)
		require.NoError(t, manager.Settle(ctx, reservation, later))
		var balance string
		var debits, costs, settlements, events int
		var period time.Time
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT amount::text,
            (SELECT count(*) FROM reg_crypto_balance_movements WHERE line_id=$3),(SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE line_id=$3),
            (SELECT count(*) FROM reg_crypto_chain_settlements WHERE network_id=$4 AND tx_hash=$5),
            (SELECT count(*) FROM reg_crypto_fee_reservation_events WHERE reservation_id=$3 AND to_status='settled'),
            (SELECT observed_at FROM reg_crypto_chain_settlements WHERE network_id=$4 AND tx_hash=$5)
            FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.hot, f.native, reservation, f.network, receipt.TxHash).Scan(&balance, &debits, &costs, &settlements, &events, &period))
		require.Equal(t, "98", balance)
		require.Equal(t, []int{1, 1, 1, 1}, []int{debits, costs, settlements, events})
		require.Equal(t, receipt.ObservedAt, period.UTC())
	})
}

func TestPendingFeeCannotAdoptExistingSettlementOrForeignMovement(t *testing.T) {
	for _, collision := range []string{"settlement", "wallet_movement", "cost_movement"} {
		t.Run(collision, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
				reservation, basis, receipt := seedLegacyFeeReplay(t, ctx, tm, f)
				q := tm.GetQuerier(ctx)
				switch collision {
				case "settlement":
					_, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at)
                        VALUES($1,'FeeReplayTest',$2,$3,'succeeded',$4,100,'solidified',$5,2,'{}',$6)`, f.network, basis, receipt.TxHash, receipt.BlockHash, f.native, receipt.ObservedAt)
					require.NoError(t, err)
				case "wallet_movement":
					_, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
                        VALUES($1,$2,'LegacyCollision',1,now(),'receipt',$3,$4,1)`, reservation, id.New(), f.hot, f.native)
					require.NoError(t, err)
				case "cost_movement":
					_, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,amount)
                        VALUES($1,$2,'LegacyCollision',1,now(),'expense',$3,$4,$4,'custody_loss','custody_loss','test',1)`, reservation, id.New(), f.network, f.native)
					require.NoError(t, err)
				}
				require.Error(t, NewFeeBudgetManager().Settle(ctx, reservation, receipt))
				var status, balance string
				var settledEvents int
				require.NoError(t, q.QueryRow(ctx, `SELECT f.status,b.amount::text,(SELECT count(*) FROM reg_crypto_fee_reservation_events WHERE reservation_id=f.id AND to_status='settled')
                    FROM reg_crypto_fee_reservations f JOIN reg_crypto_balance_balances b ON b.wallet_id=f.payer_wallet_id AND b.token_id=f.native_token_id WHERE f.id=$1`, reservation).Scan(&status, &balance, &settledEvents))
				require.Equal(t, "broadcast", status)
				expected := "100"
				if collision == "wallet_movement" {
					expected = "101"
				}
				require.Equal(t, expected, balance)
				require.Zero(t, settledEvents)
			})
		})
	}
}
