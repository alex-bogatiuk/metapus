// Package blockchain_network provides the BlockchainNetwork catalog.
// BlockchainNetworks represent supported blockchain platforms (Bitcoin, Ethereum, TRON, etc.).
package blockchain_network

import (
	"context"
	"net/url"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const ExplorerTxHashPlaceholder = "{txHash}"

// BlockchainNetwork represents a supported blockchain platform.
type BlockchainNetwork struct {
	RPCConfigSource string `db:"rpc_config_source" json:"rpcConfigSource" meta:"label:Источник RPC (file/database)"`
	entity.Catalog

	// ChainFamily selects a driver factory (for example tron or evm).
	ChainFamily crypto.ChainFamily `db:"chain_family" json:"chainFamily" meta:"label:Семейство сети"`

	// NetworkIdentifier selects a concrete chain within the family (for example nile or 1).
	NetworkIdentifier string `db:"network_identifier" json:"networkIdentifier" meta:"label:Идентификатор сети"`

	// NativeTokenSymbol is the native currency symbol (e.g., "TRX", "ETH", "BTC")
	NativeTokenSymbol string `db:"native_token_symbol" json:"nativeTokenSymbol" meta:"label:Нативный токен"`

	// NativeDecimals is the native token's decimal places (e.g., 6 for TRX, 18 for ETH, 8 for BTC)
	NativeDecimals int `db:"native_decimals" json:"nativeDecimals" meta:"label:Десятичные знаки"`

	// ConfirmationsNeeded is the required number of block confirmations for finality.
	// NEVER hardcode — always from this metadata field.
	ConfirmationsNeeded int `db:"confirmations_needed" json:"confirmationsNeeded" meta:"label:Подтверждения"`

	FinalityMode crypto.FinalityMode `db:"finality_mode" json:"finalityMode" meta:"label:Режим финальности"`

	// BlockTimeSeconds is the average block time in seconds
	BlockTimeSeconds int `db:"block_time_seconds" json:"blockTimeSeconds" meta:"label:Время блока (сек)"`

	// ExplorerURL is an HTTPS transaction URL template containing exactly one {txHash} placeholder.
	ExplorerURL string `db:"explorer_url" json:"explorerUrl,omitempty" meta:"label:Explorer URL (шаблон)"`

	// Color marks the network and its tokens in charts and lists. Empty lets the interface choose.
	Color types.PaletteColor `db:"color" json:"color" meta:"label:Цвет"`

	// IsActive is network watching: the worker reads only active networks.
	// It changes only through the guarded command on the networks page.
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Наблюдение за сетью,readOnly:true"`
}

// NewBlockchainNetwork creates a new BlockchainNetwork with required fields.
func NewBlockchainNetwork(code, name string, family crypto.ChainFamily, networkIdentifier, nativeSymbol string, nativeDecimals int) *BlockchainNetwork {
	return &BlockchainNetwork{
		Catalog:             entity.NewCatalog(code, name),
		RPCConfigSource:     "file",
		ChainFamily:         family,
		NetworkIdentifier:   networkIdentifier,
		NativeTokenSymbol:   nativeSymbol,
		NativeDecimals:      nativeDecimals,
		ConfirmationsNeeded: 1,
		FinalityMode:        crypto.FinalityByConfirmations,
		BlockTimeSeconds:    10,
		IsActive:            true,
	}
}

// Validate implements entity.Validatable.
func (n *BlockchainNetwork) Validate(ctx context.Context) error {
	if err := n.Catalog.Validate(ctx); err != nil {
		return err
	}

	if n.RPCConfigSource != "" && n.RPCConfigSource != "file" && n.RPCConfigSource != "database" {
		return apperror.NewValidation("Источник RPC: file или database")
	}
	if !n.ChainFamily.Valid() {
		return apperror.NewValidation("valid chain family is required").
			WithDetail("field", "chainFamily")
	}

	if n.NetworkIdentifier == "" {
		return apperror.NewValidation("network identifier is required").
			WithDetail("field", "networkIdentifier")
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
	if !n.FinalityMode.Valid() {
		return apperror.NewValidation("valid finality mode is required").WithDetail("field", "finalityMode")
	}

	if n.BlockTimeSeconds < 1 {
		return apperror.NewValidation("block time must be at least 1 second").
			WithDetail("field", "blockTimeSeconds")
	}

	if err := validateExplorerURLTemplate(n.ExplorerURL); err != nil {
		return err
	}

	if !n.Color.Valid() {
		return apperror.NewValidation("unknown palette color").WithDetail("field", "color")
	}

	return nil
}

func validateExplorerURLTemplate(template string) error {
	if template == "" {
		return nil
	}

	if template != strings.TrimSpace(template) || strings.Count(template, ExplorerTxHashPlaceholder) != 1 {
		return apperror.NewValidation("explorer URL must contain exactly one {txHash} placeholder").
			WithDetail("field", "explorerUrl")
	}

	rendered := strings.Replace(template, ExplorerTxHashPlaceholder, strings.Repeat("a", 64), 1)
	if strings.ContainsAny(rendered, "{}") {
		return apperror.NewValidation("explorer URL contains an unknown placeholder").
			WithDetail("field", "explorerUrl")
	}

	parsed, err := url.Parse(rendered)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return apperror.NewValidation("explorer URL must be an absolute HTTPS URL").
			WithDetail("field", "explorerUrl")
	}

	return nil
}
