package crypto

import (
	"context"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// mockFeeScheduleRepo is a minimal in-memory implementation for testing.
type mockFeeScheduleRepo struct {
	// entries keyed by "merchantID|tokenID|direction"
	entries map[string]*FeeSchedule
}

func newMockFeeScheduleRepo() *mockFeeScheduleRepo {
	return &mockFeeScheduleRepo{entries: make(map[string]*FeeSchedule)}
}

func (m *mockFeeScheduleRepo) key(merchantID *id.ID, tokenID id.ID, direction FeeDirection) string {
	mid := "global"
	if merchantID != nil {
		mid = merchantID.String()
	}
	return mid + "|" + tokenID.String() + "|" + string(direction)
}

func (m *mockFeeScheduleRepo) Get(ctx context.Context, merchantID *id.ID, tokenID id.ID, direction FeeDirection) (*FeeSchedule, error) {
	s, ok := m.entries[m.key(merchantID, tokenID, direction)]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (m *mockFeeScheduleRepo) Upsert(_ context.Context, s *FeeSchedule) error {
	m.entries[m.key(s.MerchantID, s.TokenID, s.Direction)] = s
	return nil
}

func (m *mockFeeScheduleRepo) ListByMerchant(_ context.Context, _ id.ID) ([]FeeSchedule, error) {
	return nil, nil
}

func (m *mockFeeScheduleRepo) ListGlobal(_ context.Context) ([]FeeSchedule, error) {
	return nil, nil
}

func (m *mockFeeScheduleRepo) Delete(_ context.Context, merchantID *id.ID, tokenID id.ID, direction FeeDirection) error {
	delete(m.entries, m.key(merchantID, tokenID, direction))
	return nil
}

func TestFeeConfigResolver_Resolve(t *testing.T) {
	ctx := context.Background()

	merchantID := id.New()
	tokenID := id.New()
	direction := FeeDirectionProcessing

	tests := []struct {
		give          string
		merchantEntry *FeeSchedule // nil = no merchant-specific entry
		globalEntry   *FeeSchedule // nil = no global default
		wantPercentBP int
		wantFixed     int64
		wantMinFee    int64
		wantMaxFee    int64
	}{
		{
			give:          "merchant-specific takes priority",
			merchantEntry: &FeeSchedule{MerchantID: &merchantID, TokenID: tokenID, Direction: direction, PercentBP: 50, FixedFee: types.NewCryptoAmountFromInt64(500_000)},
			globalEntry:   &FeeSchedule{TokenID: tokenID, Direction: direction, PercentBP: 100, FixedFee: types.NewCryptoAmountFromInt64(1_000_000)},
			wantPercentBP: 50,
			wantFixed:     500_000,
		},
		{
			give:          "fallback to global when no merchant entry",
			merchantEntry: nil,
			globalEntry:   &FeeSchedule{TokenID: tokenID, Direction: direction, PercentBP: 100, FixedFee: types.NewCryptoAmountFromInt64(1_000_000)},
			wantPercentBP: 100,
			wantFixed:     1_000_000,
		},
		{
			give:          "zero fee when no entries at all",
			merchantEntry: nil,
			globalEntry:   nil,
			wantPercentBP: 0,
			wantFixed:     0,
		},
		{
			give: "merchant override with min/max",
			merchantEntry: &FeeSchedule{
				MerchantID: &merchantID, TokenID: tokenID, Direction: direction,
				PercentBP: 200,
				MinFee:    types.NewCryptoAmountFromInt64(3_000_000),
				MaxFee:    types.NewCryptoAmountFromInt64(50_000_000),
			},
			globalEntry:   &FeeSchedule{TokenID: tokenID, Direction: direction, PercentBP: 100},
			wantPercentBP: 200,
			wantMinFee:    3_000_000,
			wantMaxFee:    50_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			repo := newMockFeeScheduleRepo()
			if tt.merchantEntry != nil {
				_ = repo.Upsert(ctx, tt.merchantEntry)
			}
			if tt.globalEntry != nil {
				_ = repo.Upsert(ctx, tt.globalEntry)
			}

			resolver := NewFeeConfigResolver(repo)
			got, err := resolver.Resolve(ctx, merchantID, tokenID, direction)
			if err != nil {
				t.Fatalf("Resolve() error: %v", err)
			}

			if got.PercentBP != tt.wantPercentBP {
				t.Errorf("PercentBP = %d, want %d", got.PercentBP, tt.wantPercentBP)
			}

			wantFixed := types.NewCryptoAmountFromInt64(tt.wantFixed)
			if got.FixedFee.Cmp(wantFixed) != 0 {
				t.Errorf("FixedFee = %s, want %s", got.FixedFee.String(), wantFixed.String())
			}

			wantMinFee := types.NewCryptoAmountFromInt64(tt.wantMinFee)
			if got.MinFee.Cmp(wantMinFee) != 0 {
				t.Errorf("MinFee = %s, want %s", got.MinFee.String(), wantMinFee.String())
			}

			wantMaxFee := types.NewCryptoAmountFromInt64(tt.wantMaxFee)
			if got.MaxFee.Cmp(wantMaxFee) != 0 {
				t.Errorf("MaxFee = %s, want %s", got.MaxFee.String(), wantMaxFee.String())
			}
		})
	}
}
