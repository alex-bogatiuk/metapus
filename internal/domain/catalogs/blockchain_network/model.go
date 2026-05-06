// Package blockchain_network provides the BlockchainNetwork catalog.
// BlockchainNetworks represent supported blockchain platforms (Bitcoin, Ethereum, TRON, etc.).
package blockchain_network

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
)

// BlockchainNetwork represents a supported blockchain platform.
type BlockchainNetwork struct {
	entity.Catalog

	// ChainID is the unique chain identifier (e.g., "tron", "ethereum", "bitcoin")
	ChainID string `db:"chain_id" json:"chainId" meta:"label:Chain ID"`

	// NativeTokenSymbol is the native currency symbol (e.g., "TRX", "ETH", "BTC")
	NativeTokenSymbol string `db:"native_token_symbol" json:"nativeTokenSymbol" meta:"label:Нативный токен"`

	// NativeDecimals is the native token's decimal places (e.g., 6 for TRX, 18 for ETH, 8 for BTC)
	NativeDecimals int `db:"native_decimals" json:"nativeDecimals" meta:"label:Десятичные знаки"`

	// ConfirmationsNeeded is the required number of block confirmations for finality.
	// NEVER hardcode — always from this metadata field.
	ConfirmationsNeeded int `db:"confirmations_needed" json:"confirmationsNeeded" meta:"label:Подтверждения"`

	// BlockTimeSeconds is the average block time in seconds
	BlockTimeSeconds int `db:"block_time_seconds" json:"blockTimeSeconds" meta:"label:Время блока (сек)"`

	// ExplorerURL is the block explorer base URL (e.g., "https://tronscan.org")
	ExplorerURL string `db:"explorer_url" json:"explorerUrl,omitempty" meta:"label:Explorer URL"`

	// IsActive enables/disables chain monitoring
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активна"`
}

// NewBlockchainNetwork creates a new BlockchainNetwork with required fields.
func NewBlockchainNetwork(code, name, chainID, nativeSymbol string, nativeDecimals int) *BlockchainNetwork {
	return &BlockchainNetwork{
		Catalog:             entity.NewCatalog(code, name),
		ChainID:             chainID,
		NativeTokenSymbol:   nativeSymbol,
		NativeDecimals:      nativeDecimals,
		ConfirmationsNeeded: 1,
		BlockTimeSeconds:    10,
		IsActive:            true,
	}
}

// Validate implements entity.Validatable.
func (n *BlockchainNetwork) Validate(ctx context.Context) error {
	if err := n.Catalog.Validate(ctx); err != nil {
		return err
	}

	if n.ChainID == "" {
		return apperror.NewValidation("chain ID is required").
			WithDetail("field", "chainId")
	}

	if n.NativeTokenSymbol == "" {
		return apperror.NewValidation("native token symbol is required").
			WithDetail("field", "nativeTokenSymbol")
	}

	if n.NativeDecimals < 0 || n.NativeDecimals > 18 {
		return apperror.NewValidation("native decimals must be between 0 and 18").
			WithDetail("field", "nativeDecimals")
	}

	if n.ConfirmationsNeeded < 1 {
		return apperror.NewValidation("confirmations needed must be at least 1").
			WithDetail("field", "confirmationsNeeded")
	}

	if n.BlockTimeSeconds < 1 {
		return apperror.NewValidation("block time must be at least 1 second").
			WithDetail("field", "blockTimeSeconds")
	}

	return nil
}
