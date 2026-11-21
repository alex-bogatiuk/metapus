// Package token provides the Token catalog.
// Tokens represent cryptocurrency assets on blockchain networks (USDT-TRC20, ETH, BTC, etc.).
package token

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// TokenStandard defines the token protocol standard.
type TokenStandard string

const (
	TokenStandardNative TokenStandard = "native"
	TokenStandardTRC20  TokenStandard = "TRC-20"
	TokenStandardERC20  TokenStandard = "ERC-20"
	TokenStandardBEP20  TokenStandard = "BEP-20"
	TokenStandardSPL    TokenStandard = "SPL"
	TokenStandardJetton TokenStandard = "Jetton"
)

// Token represents a cryptocurrency asset on a specific blockchain network.
type Token struct {
	entity.Catalog

	// NetworkID references the blockchain network (FK → cat_blockchain_networks)
	NetworkID id.ID `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network" binding:"required"`

	// ContractAddress is the smart contract address. Empty string for native tokens.
	ContractAddress string `db:"contract_address" json:"contractAddress" meta:"label:Адрес контракта"`

	// Symbol is the ticker symbol (e.g., "USDT", "ETH", "BTC")
	Symbol string `db:"symbol" json:"symbol" meta:"label:Символ" binding:"required"`

	// DecimalPlaces defines the token precision.
	// NEVER hardcode this value — always read from this field.
	DecimalPlaces int `db:"decimal_places" json:"decimalPlaces" meta:"label:Десятичные знаки"`

	// TokenStandard identifies the token protocol (native, TRC-20, ERC-20, etc.)
	Standard TokenStandard `db:"token_standard" json:"tokenStandard" meta:"label:Стандарт" binding:"required"`

	// IsActive enables/disables the token for processing
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активен"`

	// CurrencyID links the token to its currency for exchange rate lookups.
	// Multiple tokens can reference the same currency (e.g., USDT-TRC20 + USDT-ERC20 → USDT).
	CurrencyID *id.ID `db:"currency_id" json:"currencyId,omitempty" meta:"label:Валюта,ref:currency"`

	// SweepThreshold is the minimum accumulated balance on a pool wallet
	// before a sweep is triggered (in minor units). 0 = sweep after every payment.
	// Merchant can override via reg_merchant_token_config.
	SweepThreshold types.RawAmount `db:"sweep_threshold" json:"sweepThreshold" meta:"label:Порог свипа"`

	// SweepMaxAgeHours is the maximum time (hours) before a forced sweep
	// regardless of threshold. 0 = disabled (only threshold-based sweep).
	SweepMaxAgeHours int `db:"sweep_max_age_hours" json:"sweepMaxAgeHours" meta:"label:Макс. возраст свипа (ч)"`

	// SweepRetainAmount is the minimal token dust, in raw integer units of this
	// token contract, left on a reusable pool wallet after sweep. Merchant can
	// override it via reg_merchant_token_config.
	SweepRetainAmount types.RawAmount `db:"sweep_retain_amount" json:"sweepRetainAmount" meta:"label:Остаток после свипа"`
}

// NewToken creates a new Token with required fields.
func NewToken(code, name string, networkID id.ID, symbol string, decimalPlaces int, standard TokenStandard) *Token {
	return &Token{
		Catalog:       entity.NewCatalog(code, name),
		NetworkID:     networkID,
		Symbol:        symbol,
		DecimalPlaces: decimalPlaces,
		Standard:      standard,
		IsActive:      true,
	}
}

// Validate implements entity.Validatable.
func (t *Token) Validate(ctx context.Context) error {
	if err := t.Catalog.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(t.NetworkID) {
		return apperror.NewValidation("network is required").
			WithDetail("field", "networkId")
	}

	if t.Symbol == "" {
		return apperror.NewValidation("symbol is required").
			WithDetail("field", "symbol")
	}

	if t.DecimalPlaces < 0 || t.DecimalPlaces > 18 {
		return apperror.NewValidation("decimal places must be between 0 and 18").
			WithDetail("field", "decimalPlaces")
	}

	if t.Standard == "" {
		return apperror.NewValidation("token standard is required").
			WithDetail("field", "tokenStandard")
	}

	// Non-native tokens MUST have a contract address
	if t.Standard != TokenStandardNative && t.ContractAddress == "" {
		return apperror.NewValidation("contract address is required for non-native tokens").
			WithDetail("field", "contractAddress")
	}

	if t.SweepMaxAgeHours < 0 {
		return apperror.NewValidation("sweep max age must be non-negative").
			WithDetail("field", "sweepMaxAgeHours")
	}
	if _, _, err := t.DustThreshold(); err != nil {
		return apperror.NewValidation(err.Error()).WithDetail("field", "attributes."+DustThresholdAttribute)
	}

	return nil
}

// DustThresholdAttribute overrides the incoming dust threshold of a token in
// raw units. The network default is one value in token units (0.001 token),
// which is $0.001 for a stablecoin but about $2 for ETH, so valuable natives
// need their own threshold.
const DustThresholdAttribute = "dustThreshold"

// DustThreshold returns the configured raw dust threshold, if any. The value
// must be a canonical non-negative integer string, like every money amount.
func (t *Token) DustThreshold() (types.RawAmount, bool, error) {
	raw, exists := t.Attributes[DustThresholdAttribute]
	if !exists || raw == nil {
		return types.ZeroRawAmount(), false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return types.ZeroRawAmount(), false, fmt.Errorf("%s must be a decimal string in raw units", DustThresholdAttribute)
	}
	amount, err := types.NewRawAmountFromString(value)
	if err != nil {
		return types.ZeroRawAmount(), false, fmt.Errorf("%s must be a canonical non-negative integer in raw units", DustThresholdAttribute)
	}
	return amount, true, nil
}

// IsNative returns true if this is the network's native token.
func (t *Token) IsNative() bool {
	return t.Standard == TokenStandardNative
}

// IsEVMContract reports ERC-20 and BEP-20. BNB Smart Chain is an EVM family;
// the standard name is catalog metadata, not a separate driver family.
func (s TokenStandard) IsEVMContract() bool {
	return s == TokenStandardERC20 || s == TokenStandardBEP20
}
