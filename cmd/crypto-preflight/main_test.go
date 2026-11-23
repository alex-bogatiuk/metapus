package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto_worker"
)

func TestValidatePreflightResult(t *testing.T) {
	t.Parallel()

	require.NoError(t, validatePreflightResult(crypto_worker.ReconciliationResult{Acquired: true}))
	require.ErrorContains(t, validatePreflightResult(crypto_worker.ReconciliationResult{}), "owns the lease")
	require.ErrorContains(t, validatePreflightResult(crypto_worker.ReconciliationResult{
		Acquired:   true,
		ReadErrors: 2,
	}), "2 chain read errors")
	require.ErrorContains(t, validatePreflightResult(crypto_worker.ReconciliationResult{
		Acquired: true, EligiblePairs: 3000, AttemptedPairs: 2500, WalletsChecked: 2500,
	}), "coverage incomplete")
	require.ErrorContains(t, validatePreflightResult(crypto_worker.ReconciliationResult{
		Acquired: true, EligiblePairs: 2, AttemptedPairs: 2, WalletsChecked: 1,
	}), "successful=1")
	require.NoError(t, validatePreflightResult(crypto_worker.ReconciliationResult{
		Acquired: true, EligiblePairs: 2, AttemptedPairs: 2, WalletsChecked: 2,
	}))
}
