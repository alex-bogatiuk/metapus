//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

func TestPortalMerchantSettingsPatch(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "METAPUS_TEST_DATABASE_DSN is required for integration tests")
	ctx := context.Background()
	pool, err := sdktest.OpenPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	repo := NewDashboardRepo()
	ttl, minimumTTL, maximumTTL, invalidTTL := 120, 5, 10080, 4
	enabled, disabled := true, false
	tests := []struct {
		name          string
		give          cryptodto.UpdatePortalSettingsRequest
		initialSingle bool
		outOfScope    bool
		deleted       bool
		wantTTL       int
		wantSingle    bool
		wantTTLType   string
		wantError     string
		wantUnchanged bool
	}{
		{name: "both settings", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &ttl, SinglePaymentPerInvoice: &enabled}, wantTTL: 120, wantSingle: true, wantTTLType: "number"},
		{name: "only TTL", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &ttl}, initialSingle: true, wantTTL: 120, wantSingle: true, wantTTLType: "number"},
		{name: "only enable single payment", give: cryptodto.UpdatePortalSettingsRequest{SinglePaymentPerInvoice: &enabled}, wantTTL: 60, wantSingle: true, wantTTLType: "string"},
		{name: "disable single payment", give: cryptodto.UpdatePortalSettingsRequest{SinglePaymentPerInvoice: &disabled}, initialSingle: true, wantTTL: 60, wantTTLType: "string"},
		{name: "minimum TTL", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &minimumTTL}, wantTTL: 5, wantTTLType: "number"},
		{name: "maximum TTL", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &maximumTTL}, wantTTL: 10080, wantTTLType: "number"},
		{name: "empty patch", initialSingle: true, wantTTL: 60, wantSingle: true, wantTTLType: "string", wantUnchanged: true},
		{name: "invalid TTL", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &invalidTTL, SinglePaymentPerInvoice: &enabled}, wantTTL: 60, wantTTLType: "string", wantError: "defaultTtlMinutes must be between 5 and 10080", wantUnchanged: true},
		{name: "out of scope", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &ttl, SinglePaymentPerInvoice: &enabled}, outOfScope: true, wantTTL: 60, wantTTLType: "string", wantError: "merchant_id not in scope", wantUnchanged: true},
		{name: "deleted merchant", give: cryptodto.UpdatePortalSettingsRequest{DefaultTTLMinutes: &ttl, SinglePaymentPerInvoice: &enabled}, deleted: true, wantTTL: 60, wantTTLType: "string", wantUnchanged: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rollback := errors.New("rollback portal settings fixture")
			err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
				q := txm.GetQuerier(txCtx)
				merchantID := id.New()
				var deletedAt *time.Time
				if tt.deleted {
					deletedAt = new(time.Now())
				}
				_, err := q.Exec(txCtx, `
					INSERT INTO cat_merchants (id, code, name, attributes, updated_at, _deleted_at)
					VALUES ($1, $2, 'Portal settings regression', $3::jsonb, NOW() - INTERVAL '1 day', $4)
				`, merchantID, "SETTINGS-"+id.Compact(merchantID), map[string]any{
					"defaultTtlMinutes": "60", "singlePaymentPerInvoice": tt.initialSingle,
					"integrationMarker": map[string]bool{"enabled": true},
				}, deletedAt)
				require.NoError(t, err)
				var beforeAttributes string
				var beforeUpdatedAt time.Time
				require.NoError(t, q.QueryRow(txCtx, `SELECT attributes::text, updated_at FROM cat_merchants WHERE id=$1`, merchantID).Scan(&beforeAttributes, &beforeUpdatedAt))
				scopeID := merchantID
				if tt.outOfScope {
					scopeID = id.New()
				}
				scopedCtx := merchant.WithPortalScope(txCtx, merchant.PortalScope{MerchantIDs: []id.ID{scopeID}})
				err = repo.UpdateMerchantSettings(scopedCtx, merchantID, tt.give)
				if tt.wantError != "" {
					require.EqualError(t, err, tt.wantError)
				} else {
					require.NoError(t, err)
				}
				var afterAttributes, ttlType, singleType string
				var afterUpdatedAt time.Time
				var actualTTL int
				var actualSingle, marker bool
				require.NoError(t, q.QueryRow(txCtx, `
					SELECT attributes::text, updated_at,
					       (attributes->>'defaultTtlMinutes')::int,
					       (attributes->>'singlePaymentPerInvoice')::boolean,
					       jsonb_typeof(attributes->'defaultTtlMinutes'),
					       jsonb_typeof(attributes->'singlePaymentPerInvoice'),
					       (attributes->'integrationMarker'->>'enabled')::boolean
					FROM cat_merchants WHERE id=$1
				`, merchantID).Scan(&afterAttributes, &afterUpdatedAt, &actualTTL, &actualSingle, &ttlType, &singleType, &marker))
				require.Equal(t, tt.wantTTL, actualTTL)
				require.Equal(t, tt.wantSingle, actualSingle)
				require.Equal(t, tt.wantTTLType, ttlType)
				require.Equal(t, "boolean", singleType)
				require.True(t, marker, "other merchant attributes must survive the patch")
				if tt.wantUnchanged {
					require.Equal(t, beforeAttributes, afterAttributes)
					require.Equal(t, beforeUpdatedAt, afterUpdatedAt)
				} else {
					require.True(t, afterUpdatedAt.After(beforeUpdatedAt))
				}
				if !tt.outOfScope && !tt.deleted {
					settings, err := repo.GetMerchantSettings(scopedCtx, merchantID)
					require.NoError(t, err)
					require.Equal(t, tt.wantTTL, settings.DefaultTTLMinutes)
					require.Equal(t, tt.wantSingle, settings.SinglePaymentPerInvoice)
				}
				return rollback
			})
			require.ErrorIs(t, err, rollback)
		})
	}
}
