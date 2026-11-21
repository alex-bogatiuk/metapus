//go:build integration

package crypto_repo_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type rentalReplayFixture struct {
	crypto_repo.OutboundPublicationFixture
	rental, account, native, asset id.ID
	expiry                         time.Time
}

func newRentalReplayFixture(t *testing.T) rentalReplayFixture {
	t.Helper()
	f := rentalReplayFixture{OutboundPublicationFixture: crypto_repo.NewOutboundPublicationFixture(t), rental: id.New(), account: id.New(), native: id.New(), expiry: time.Now().UTC().Add(time.Hour)}
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT token_id FROM doc_withdrawal_requests WHERE id=$1`, f.Parent).Scan(&f.asset))
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard)
 VALUES($1,$1::uuid::text,'Rental cost asset',$3,'TRX',6,'native');
 INSERT INTO cat_crypto_provider_accounts(id,code,name,provider,network_id,balance_token_id)
 VALUES($2,$2::uuid::text,'Synthetic rental provider','itrx',$3,$1)`, pgx.QueryExecModeSimpleProtocol, f.native, f.account, f.Auth.NetworkID)
	require.NoError(t, err)
	rental := outbound_repo.ResourceRentalRecord{ID: f.rental, ProviderAccountID: f.account, NetworkID: f.Auth.NetworkID, ReceiveWalletID: f.Source, CostTokenID: f.native, RequestedAmount: 100, Provider: "itrx", OutTradeNo: "rental-" + f.rental.String(), ExpiresAt: &f.expiry}
	require.NoError(t, outbound_repo.NewOutboundRepo().CreateResourceRental(f.Ctx, &rental, outbound_repo.ResourceRequest{BasisType: "CryptoAMLWithdrawalLine", BasisID: f.Auth.BasisID}, []byte(`{"synthetic":true}`)))
	return f
}

func (f rentalReplayFixture) activate(cost string) error {
	return outbound_repo.NewOutboundRepo().ActivateResourceRental(f.Ctx, f.rental, "serial", 100, types.MustRawAmount(cost), nil, nil, f.expiry)
}
func (f rentalReplayFixture) close(refund string) error {
	return outbound_repo.NewOutboundRepo().CloseResourceRental(f.Ctx, f.rental, types.MustRawAmount(refund), nil, nil, "closed")
}
func (f rentalReplayFixture) snapshot(t *testing.T) string {
	t.Helper()
	var value string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(r)::text FROM doc_crypto_resource_rentals r WHERE id=$1`, f.rental).Scan(&value))
	return value
}
func (f rentalReplayFixture) amounts(t *testing.T) []string {
	t.Helper()
	var refund, provider, cost string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT refund_amount::text,
 (SELECT COALESCE(sum(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0)::text FROM reg_crypto_provider_balance_movements WHERE recorder_id=$1),
 (SELECT COALESCE(sum(CASE WHEN record_type='expense' THEN amount ELSE -amount END),0)::text FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1)
 FROM doc_crypto_resource_rentals WHERE id=$1`, f.rental).Scan(&refund, &provider, &cost))
	return []string{refund, provider, cost}
}

func TestRentalChargeConcurrentReplayPreservesFirstFinancialSet(t *testing.T) {
	f := newRentalReplayFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Go(func() { errs <- f.activate("1000") })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	before := f.snapshot(t)
	require.NoError(t, outbound_repo.NewOutboundRepo().ActivateResourceRental(f.Ctx, f.rental, "serial", 100, types.MustRawAmount("1000"), nil, []byte(`{"later":true}`), f.expiry.Add(time.Hour)))
	require.Equal(t, before, f.snapshot(t))
	require.ErrorContains(t, f.activate("999"), "activation replay differs")
	require.ErrorContains(t, outbound_repo.NewOutboundRepo().ActivateResourceRental(f.Ctx, f.rental, "another", 100, types.MustRawAmount("1000"), nil, nil, f.expiry), "activation replay differs")
	require.ErrorContains(t, outbound_repo.NewOutboundRepo().ActivateResourceRental(f.Ctx, f.rental, "serial", 101, types.MustRawAmount("1000"), nil, nil, f.expiry), "activation replay differs")
	require.Equal(t, []string{"0", "-1000", "1000"}, f.amounts(t))
}

func TestRentalCumulativeRefundNeverRegressesOrDuplicatesCredit(t *testing.T) {
	f := newRentalReplayFixture(t)
	require.NoError(t, f.activate("1000"))
	require.NoError(t, f.close("400"))
	first := f.snapshot(t)
	require.ErrorContains(t, f.close("200"), "cannot decrease")
	require.Equal(t, first, f.snapshot(t))
	require.NoError(t, f.close("700"))
	require.NoError(t, f.close("1000"))
	before := f.snapshot(t)
	require.NoError(t, f.close("1000"))
	require.NoError(t, f.activate("1000"))
	require.NoError(t, outbound_repo.NewOutboundRepo().MarkResourceRentalHeld(f.Ctx, f.rental, "stale provider reply", nil))
	require.Equal(t, before, f.snapshot(t))
	require.ErrorContains(t, f.close("1001"), "exceeds its original charge")
	require.ErrorContains(t, outbound_repo.NewOutboundRepo().CloseResourceRental(f.Ctx, f.rental, types.MustRawAmount("1000"), nil, nil, "failed"), "terminal outcome")
	require.Equal(t, []string{"1000", "0", "0"}, f.amounts(t))
	var preserved bool
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT closed_at=($2::jsonb->>'closed_at')::timestamptz FROM doc_crypto_resource_rentals WHERE id=$1`, f.rental, first).Scan(&preserved))
	require.True(t, preserved)
}

func TestRentalConcurrentCumulativeRefundUsesOnlyTheIncrease(t *testing.T) {
	f := newRentalReplayFixture(t)
	require.NoError(t, f.activate("1000"))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, amount := range []string{"400", "700"} {
		wg.Go(func() { errs <- f.close(amount) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			require.ErrorContains(t, err, "cannot decrease")
		}
	}
	require.Equal(t, []string{"700", "-300", "300"}, f.amounts(t))
	require.NoError(t, f.close("700"))
}

func TestRentalMovementCollisionRollsBackChargeAndRefund(t *testing.T) {
	for _, kind := range []string{"provider", "platform", "pending"} {
		for _, refund := range []bool{false, true} {
			if kind == "pending" && refund {
				continue
			}
			t.Run(kind+map[bool]string{false: "/charge", true: "/refund"}[refund], func(t *testing.T) {
				f := newRentalReplayFixture(t)
				line, recorder, version, record, ref, amount := f.rental, id.New(), 1, "expense", "serial", "1000"
				if kind == "pending" {
					recorder = f.rental
				}
				if refund {
					require.NoError(t, f.activate("1000"))
					require.NoError(t, f.close("400"))
					line = uuid.NewSHA1(f.rental, []byte(kind+":serial:refund:700"))
					version = 2
					record = "receipt"
					ref = "serial:refund:700"
					amount = "300"
				}
				before := f.snapshot(t)
				if kind == "platform" {
					_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount)
 VALUES($1,$2,'CryptoResourceRental',$3,now(),$4,$5,$6,'chain_resource_rent','resource_rental','itrx',$7,$8)`, line, recorder, version, record, f.Auth.NetworkID, f.native, ref, amount)
					require.NoError(t, err)
				} else {
					operation := "rental_charge"
					if refund {
						operation = "refund"
					}
					_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_provider_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,provider_account_id,token_id,operation_type,external_ref,amount)
 VALUES($1,$2,'CryptoResourceRental',$3,now(),$4,$5,$6,$7,$8,$9)`, line, recorder, version, record, f.account, f.native, operation, ref, amount)
					require.NoError(t, err)
				}
				if refund {
					require.ErrorContains(t, f.close("700"), "reconciliation required")
					require.Equal(t, []string{"400", "-600", "600"}, f.amounts(t))
				} else {
					require.ErrorContains(t, f.activate("1000"), "reconciliation required")
				}
				require.Equal(t, before, f.snapshot(t))
			})
		}
	}
}

func TestRentalFinancialLocksAccountBeforeRental(t *testing.T) {
	f := newRentalReplayFixture(t)
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_crypto_provider_accounts WHERE id=$1 FOR UPDATE`, f.account)
		return err
	},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, f.rental)
			return err
		},
		func(ctx context.Context) error {
			return outbound_repo.NewOutboundRepo().ActivateResourceRental(ctx, f.rental, "serial", 100, types.MustRawAmount("1000"), nil, nil, f.expiry)
		})
}

func TestRentalAllocationSettlementLocksParentAndPreservesRefundCost(t *testing.T) {
	f := newRentalReplayFixture(t)
	repo := outbound_repo.NewOutboundRepo()
	require.NoError(t, f.activate("1000"))
	allocation, _, err := repo.TryReserveResource(f.Ctx, outbound_repo.ResourceRequest{BasisType: "CryptoAMLWithdrawalLine", BasisID: f.Auth.BasisID, Purpose: "withdrawal", NetworkID: f.Auth.NetworkID, SourceWalletID: f.Source, OperationTokenID: f.asset}, 100, 0)
	require.NoError(t, err)
	require.NotNil(t, allocation)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_resource_allocations SET status='broadcast',tx_hash='synthetic-rental-tx' WHERE id=$1`, allocation.ID)
	require.NoError(t, err)
	receipt := crypto.ChainReceipt{NetworkID: f.Auth.NetworkID, TxHash: "synthetic-rental-tx", Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, Fee: crypto.FeeSettlementReport{Components: []crypto.FeeComponent{{Name: "energy_fee", Quantity: 50}}}}
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, f.rental)
		return err
	},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_resource_allocations WHERE id=$1 FOR UPDATE`, allocation.ID)
			return err
		},
		func(ctx context.Context) error { return repo.SettleResourceAllocation(ctx, receipt.TxHash, receipt) })
	require.NoError(t, f.close("400"))
	before := f.snapshot(t)
	require.NoError(t, repo.SettleResourceAllocation(f.Ctx, receipt.TxHash, receipt))
	require.Equal(t, before, f.snapshot(t))
	var allocated string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT allocated_cost_amount::text FROM reg_crypto_resource_allocations WHERE id=$1`, allocation.ID).Scan(&allocated))
	require.Equal(t, "300", allocated)
	receipt.Fee.Components[0].Quantity = 75
	require.ErrorContains(t, repo.SettleResourceAllocation(f.Ctx, receipt.TxHash, receipt), "original usage or outcome")
	receipt.NetworkID = id.New()
	require.ErrorContains(t, repo.SettleResourceAllocation(f.Ctx, receipt.TxHash, receipt), "another network")
}

func TestRentalFreeChargeAndIsolationContract(t *testing.T) {
	f := newRentalReplayFixture(t)
	tm := postgres.MustGetTxManager(f.Ctx)
	opts := postgres.DefaultTxOptions()
	opts.IsolationLevel = pgx.RepeatableRead
	err := tm.RunInTransactionWithOptions(f.Ctx, opts, func(ctx context.Context) error {
		return outbound_repo.NewOutboundRepo().ActivateResourceRental(ctx, f.rental, "serial", 100, types.ZeroRawAmount(), nil, nil, f.expiry)
	})
	require.ErrorContains(t, err, "read committed")
	require.NoError(t, f.activate("0"))
	require.NoError(t, f.close("0"))
	require.NoError(t, f.close("0"))
	require.Equal(t, []string{"0", "0", "0"}, f.amounts(t))
	require.ErrorContains(t, f.close("1"), "exceeds its original charge")
}
