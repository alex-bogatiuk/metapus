// internal/configuration/crypto/domain/crypto/sweep_resolver_test.go
package crypto

import (
	"context"
	"fmt"
	"testing"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
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

func (m *mockTokenRepo) FindByContractAndNetwork(_ context.Context, _ string, _ id.ID) (*token.Token, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tok, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func makeToken(threshold int64, maxAgeHours int) *token.Token {
	return &token.Token{
		SweepThreshold:    types.NewRawAmountFromInt64(threshold),
		SweepMaxAgeHours:  maxAgeHours,
		SweepRetainAmount: types.NewRawAmountFromInt64(1),
	}
}

func rawAmountPtr(v int64) *types.RawAmount {
	a := types.NewRawAmountFromInt64(v)
	return &a
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
		wantRetain    int64
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
			wantRetain:    1,
		},
		{
			give:       "full merchant override",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: &MerchantTokenConfig{
					MerchantID:        merchantID,
					TokenID:           tokenID,
					SweepThreshold:    rawAmountPtr(5_000_000),
					SweepMaxAgeHours:  new(1),
					SweepRetainAmount: rawAmountPtr(100),
				},
			},
			wantThreshold: 5_000_000,
			wantMaxAge:    1,
			wantRetain:    100,
		},
		{
			give:       "partial override — threshold only",
			merchantID: merchantID,
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				cfg: &MerchantTokenConfig{
					MerchantID:       merchantID,
					TokenID:          tokenID,
					SweepThreshold:   rawAmountPtr(20_000_000),
					SweepMaxAgeHours: nil,
				},
			},
			wantThreshold: 20_000_000,
			wantMaxAge:    24,
			wantRetain:    1,
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
					SweepMaxAgeHours: new(48),
				},
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    48,
			wantRetain:    1,
		},
		{
			give:       "nil merchantID → skip override lookup",
			merchantID: id.Nil(),
			tokenRepo:  &mockTokenRepo{tok: makeToken(10_000_000, 24)},
			overrideRepo: &mockMerchantConfigRepo{
				// This should never be called for nil merchantID
				cfg: &MerchantTokenConfig{
					SweepThreshold:   rawAmountPtr(999),
					SweepMaxAgeHours: new(999),
				},
			},
			wantThreshold: 10_000_000,
			wantMaxAge:    24,
			wantRetain:    1,
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
			wantRetain:    1,
		},
		{
			give:         "token repo error → propagate error",
			merchantID:   merchantID,
			tokenRepo:    &mockTokenRepo{err: fmt.Errorf("token not found")},
			overrideRepo: &mockMerchantConfigRepo{},
			wantErr:      true,
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

			gotThreshold := cfg.Threshold.String()
			if gotThreshold != types.NewRawAmountFromInt64(tt.wantThreshold).String() {
				t.Errorf("Threshold = %s, want %d", gotThreshold, tt.wantThreshold)
			}
			if cfg.MaxAgeHours != tt.wantMaxAge {
				t.Errorf("MaxAgeHours = %d, want %d", cfg.MaxAgeHours, tt.wantMaxAge)
			}
			if cfg.RetainAmount.String() != types.NewRawAmountFromInt64(tt.wantRetain).String() {
				t.Errorf("RetainAmount = %s, want %d", cfg.RetainAmount.String(), tt.wantRetain)
			}
		})
	}
}
