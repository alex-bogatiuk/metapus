// internal/domain/crypto/sweep_resolver_test.go
package crypto

import (
	"context"
	"fmt"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
)

// ── Mock: MerchantTokenConfigRepository ─────────────────────────────────

type mockMerchantConfigRepo struct {
	cfg *MerchantTokenConfig
	err error
}

func (m *mockMerchantConfigRepo) Get(_ context.Context, _, _ id.ID) (*MerchantTokenConfig, error) {
	return m.cfg, m.err
}

func (m *mockMerchantConfigRepo) Upsert(_ context.Context, _ *MerchantTokenConfig) error {
	return nil
}

// ── Mock: tokenGetter (minimal interface) ───────────────────────────────
// Only GetByID — no more 12 stub methods thanks to minimal consumer interface.

type mockTokenRepo struct {
	tok *token.Token
	err error
}

func (m *mockTokenRepo) GetByID(_ context.Context, _ id.ID) (*token.Token, error) {
	return m.tok, m.err
}

// ── Helpers ─────────────────────────────────────────────────────────────

func makeToken(threshold int64, maxAgeHours int) *token.Token {
	return &token.Token{
		SweepThreshold:   types.NewCryptoAmountFromInt64(threshold),
		SweepMaxAgeHours: maxAgeHours,
	}
}

func cryptoAmountPtr(v int64) *types.CryptoAmount {
	a := types.NewCryptoAmountFromInt64(v)
	return &a
}

func intPtr(v int) *int {
	return &v
}

// ── Tests ───────────────────────────────────────────────────────────────

func TestSweepConfigResolver_Resolve(t *testing.T) {
	ctx := context.Background()
	merchantID := id.New()
	tokenID := id.New()

	tests := []struct {
		give          string
		merchantID    id.ID
		tokenRepo     *mockTokenRepo
		overrideRepo  *mockMerchantConfigRepo
		wantThreshold int64
		wantMaxAge    int
		wantErr       bool
	}{
		{
			give:       "token defaults only (no override exists)",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: nil,
				err: nil,
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    24,
		},
		{
			give:       "full merchant override (both fields)",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: &MerchantTokenConfig{
					MerchantID:       merchantID,
					TokenID:          tokenID,
					SweepThreshold:   cryptoAmountPtr(5_000_000),
					SweepMaxAgeHours: intPtr(1),
				},
			},
			wantThreshold: 5_000_000,
			wantMaxAge:    1,
		},
		{
			give:       "partial override — threshold only",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: &MerchantTokenConfig{
					MerchantID:       merchantID,
					TokenID:          tokenID,
					SweepThreshold:   cryptoAmountPtr(20_000_000),
					SweepMaxAgeHours: nil,
				},
			},
			wantThreshold: 20_000_000,
			wantMaxAge:    24,
		},
		{
			give:       "partial override — maxAge only",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: &MerchantTokenConfig{
					MerchantID:       merchantID,
					TokenID:          tokenID,
					SweepThreshold:   nil,
					SweepMaxAgeHours: intPtr(48),
				},
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    48,
		},
		{
			give:       "nil merchantID → skip override lookup",
			merchantID: id.Nil(),
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				// This should never be called for nil merchantID
				cfg: &MerchantTokenConfig{
					SweepThreshold:   cryptoAmountPtr(999),
					SweepMaxAgeHours: intPtr(999),
				},
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    24,
		},
		{
			give:       "override repo error → graceful fallback to token defaults",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				err: fmt.Errorf("db connection failed"),
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    24,
		},
		{
			give:       "token repo error → propagate error",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{err: fmt.Errorf("token not found")},
			overrideRepo: &mockMerchantConfigRepo{},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			resolver := NewSweepConfigResolver(tt.overrideRepo, tt.tokenRepo)
			cfg, err := resolver.Resolve(ctx, tt.merchantID, tokenID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotThreshold := cfg.Threshold.Int64()
			if gotThreshold != tt.wantThreshold {
				t.Errorf("Threshold = %d, want %d", gotThreshold, tt.wantThreshold)
			}
			if cfg.MaxAgeHours != tt.wantMaxAge {
				t.Errorf("MaxAgeHours = %d, want %d", cfg.MaxAgeHours, tt.wantMaxAge)
			}
		})
	}
}
