// internal/domain/catalogs/token/model_test.go
package token

import (
	"context"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestToken_Validate_SweepFields(t *testing.T) {
	ctx := context.Background()

	// Helper to create a valid base token
	validToken := func() *Token {
		tok := NewToken("USDT", "Tether USD", id.New(), "USDT", 6, TokenStandardTRC20)
		tok.ContractAddress = "TXYZExampleContract"
		return tok
	}

	tests := []struct {
		give    string
		modify  func(tok *Token)
		wantErr bool
	}{
		{
			give: "negative sweep threshold → error",
			modify: func(tok *Token) {
				tok.SweepThreshold = types.NewCryptoAmountFromInt64(-1)
			},
			wantErr: true,
		},
		{
			give: "zero sweep threshold (legacy mode) → OK",
			modify: func(tok *Token) {
				tok.SweepThreshold = types.NewCryptoAmountFromInt64(0)
			},
			wantErr: false,
		},
		{
			give: "positive sweep threshold 10M → OK",
			modify: func(tok *Token) {
				tok.SweepThreshold = types.NewCryptoAmountFromInt64(10_000_000)
			},
			wantErr: false,
		},
		{
			give: "negative max age → error",
			modify: func(tok *Token) {
				tok.SweepMaxAgeHours = -1
			},
			wantErr: true,
		},
		{
			give: "zero max age (disabled) → OK",
			modify: func(tok *Token) {
				tok.SweepMaxAgeHours = 0
			},
			wantErr: false,
		},
		{
			give: "positive max age → OK",
			modify: func(tok *Token) {
				tok.SweepMaxAgeHours = 24
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			tok := validToken()
			tt.modify(tok)
			err := tok.Validate(ctx)

			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestToken_Validate_General(t *testing.T) {
	ctx := context.Background()

	validToken := func() *Token {
		tok := NewToken("USDT", "Tether USD", id.New(), "USDT", 6, TokenStandardTRC20)
		tok.ContractAddress = "TXYZExampleContract"
		return tok
	}

	tests := []struct {
		give    string
		modify  func(tok *Token)
		wantErr bool
	}{
		{
			give:    "valid TRC-20 token",
			modify:  func(tok *Token) {},
			wantErr: false,
		},
		{
			give: "missing networkID → error",
			modify: func(tok *Token) {
				tok.NetworkID = id.Nil()
			},
			wantErr: true,
		},
		{
			give: "missing symbol → error",
			modify: func(tok *Token) {
				tok.Symbol = ""
			},
			wantErr: true,
		},
		{
			give: "decimal places out of range (-1) → error",
			modify: func(tok *Token) {
				tok.DecimalPlaces = -1
			},
			wantErr: true,
		},
		{
			give: "decimal places out of range (19) → error",
			modify: func(tok *Token) {
				tok.DecimalPlaces = 19
			},
			wantErr: true,
		},
		{
			give: "decimal places max (18 — ETH) → OK",
			modify: func(tok *Token) {
				tok.DecimalPlaces = 18
			},
			wantErr: false,
		},
		{
			give: "missing standard → error",
			modify: func(tok *Token) {
				tok.Standard = ""
			},
			wantErr: true,
		},
		{
			give: "non-native without contract address → error",
			modify: func(tok *Token) {
				tok.Standard = TokenStandardTRC20
				tok.ContractAddress = ""
			},
			wantErr: true,
		},
		{
			give: "native without contract address → OK",
			modify: func(tok *Token) {
				tok.Standard = TokenStandardNative
				tok.ContractAddress = ""
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			tok := validToken()
			tt.modify(tok)
			err := tok.Validate(ctx)

			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestToken_IsNative(t *testing.T) {
	tests := []struct {
		give     string
		standard TokenStandard
		want     bool
	}{
		{"native → true", TokenStandardNative, true},
		{"TRC-20 → false", TokenStandardTRC20, false},
		{"ERC-20 → false", TokenStandardERC20, false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			tok := &Token{Standard: tt.standard}
			if got := tok.IsNative(); got != tt.want {
				t.Errorf("IsNative() = %v, want %v", got, tt.want)
			}
		})
	}
}
