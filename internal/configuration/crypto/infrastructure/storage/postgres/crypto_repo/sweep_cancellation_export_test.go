//go:build integration

package crypto_repo

import (
	"context"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type SweepCancellationFixture struct {
	Ctx                               context.Context
	Pool                              *pgxpool.Pool
	Key, Auth                         id.ID
	Table, Column, StateTable, Reason string
}

func NewSweepCancellationFixture(t *testing.T, resource bool) SweepCancellationFixture {
	t.Helper()
	var result SweepCancellationFixture
	if !resource {
		f, destination, _ := readyAMLInventory(t)
		r := f.allocationRequest(t, destination, "50")
		allocation, err := f.repo.ReserveAMLInternalAllocation(f.ctx, r)
		require.NoError(t, err)
		auth, unsigned := f.allocationAuthorization(t, r)
		auth.ExpiresAt = time.Now().Add(-time.Hour)
		authorization, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
		require.NoError(t, err)
		_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET status='expired' WHERE id=$1`, r.SweepAttemptID)
		require.NoError(t, err)
		result = SweepCancellationFixture{f.ctx, f.pool, allocation.ID, authorization, "reg_crypto_aml_allocation_cancellations", "allocation_id", "reg_crypto_aml_internal_allocations", "sweep attempt expired before broadcast"}
	} else {
		f, target, _ := nativeResourceFixture(t)
		fee, funding := resourceFee(t, f, target, "20", "10")
		budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
		require.NoError(t, err)
		var destination string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&destination))
		auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.input.Request.MerchantID,
			Purpose: crypto.SigningPurposeNativeFunding, BasisType: "CryptoNativeFunding", BasisID: funding, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(-time.Hour),
			NetworkID: f.input.Request.NetworkID, NetworkCode: "RESOURCE-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: f.input.Request.Address, FeePayerAddress: f.input.Request.Address,
			VaultKeyID: "resource-key", DestinationAddress: destination, AssetStandard: "native", AmountMinorUnits: types.MustRawAmount("20"), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "expiry-" + funding.String(), PolicyVersion: "tron-v1"}
		unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{"synthetic":true}`)}
		authorization, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
		require.NoError(t, err)
		result = SweepCancellationFixture{f.ctx, f.pool, budget.ID, authorization, "reg_crypto_aml_resource_cancellations", "budget_id", "reg_crypto_aml_resource_budgets", "signing authorization expired before transaction signing"}
	}
	return result
}
