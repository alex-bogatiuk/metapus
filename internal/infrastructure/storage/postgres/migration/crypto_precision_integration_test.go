//go:build integration

package migration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/types"
)

func TestCryptoPrecisionIntegration(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	_, err := RunAll(dsn)
	require.NoError(t, err)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	expectedNumericColumns := [][2]string{
		{"cat_tokens", "sweep_threshold"},
		{"cat_tokens", "sweep_retain_amount"},
		{"doc_crypto_invoices", "expected_amount"},
		{"doc_crypto_invoices", "received_amount"},
		{"doc_crypto_payments", "amount"},
		{"doc_crypto_withdrawals", "amount"},
		{"reg_crypto_balance_movements", "amount"},
		{"reg_crypto_balance_balances", "amount"},
		{"reg_crypto_merchant_balance_movements", "amount"},
		{"reg_crypto_fee_movements", "amount"},
		{"reg_crypto_signing_authorizations", "amount_minor_units"},
		{"reg_crypto_signing_authorizations", "max_network_fee_minor"},
		{"reg_evm_nonce_cursors", "next_nonce"},
		{"reg_evm_nonce_leases", "nonce"},
		{"reg_evm_nonce_lease_events", "max_fee_minor"},
		{"reg_crypto_reconciliation_items", "delta_amount"},
		{"sys_crypto_network_policies", "per_transaction_max_fee_minor"},
		{"sys_crypto_network_policies", "daily_network_fee_budget_minor"},
		{"sys_crypto_network_policies", "jit_funding_max_amount_minor"},
		{"reg_crypto_resource_allocations", "allocated_cost_amount"},
		{"sys_crypto_wallet_pool_policies", "daily_activation_budget_minor"},
	}
	for _, column := range expectedNumericColumns {
		var dataType string
		var precision, scale int
		err := pool.QueryRow(ctx, `
			SELECT data_type, numeric_precision, numeric_scale
			FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2
		`, column[0], column[1]).Scan(&dataType, &precision, &scale)
		require.NoError(t, err, "%s.%s", column[0], column[1])
		require.Equal(t, "numeric", dataType, "%s.%s", column[0], column[1])
		require.Equal(t, 78, precision, "%s.%s", column[0], column[1])
		require.Zero(t, scale, "%s.%s", column[0], column[1])
	}

	const maximumUint256 = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	input := types.MustCryptoAmount(maximumUint256)
	var output types.CryptoAmount
	require.NoError(t, pool.QueryRow(ctx, `SELECT $1::numeric(78,0)`, input).Scan(&output))
	require.True(t, input.Equal(output))

	inputDelta, err := types.NewCryptoDeltaFromString("-115792089237316195423570985008687907853269984665640564039457584007913129639935")
	require.NoError(t, err)
	var outputDelta types.CryptoDelta
	require.NoError(t, pool.QueryRow(ctx, `SELECT $1::numeric(78,0)`, inputDelta).Scan(&outputDelta))
	require.Zero(t, inputDelta.Cmp(outputDelta))
}
