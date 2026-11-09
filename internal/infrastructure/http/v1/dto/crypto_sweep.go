package dto

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

// CryptoSweep is system-only — no Create/Update DTOs needed.

// CryptoSweepResponse is the response body for a sweep.
type CryptoSweepResponse struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	Date         string `json:"date"`
	TokenID      string `json:"tokenId"`
	HotWalletID  string `json:"hotWalletId"`
	TotalAmount  string `json:"totalAmount"`
	TotalFee     string `json:"totalFee"`
	Status       string `json:"status"`
	StatusName   string `json:"statusName"`
	LineCount    int    `json:"lineCount"`
	Posted       bool   `json:"posted"`
	DeletionMark bool   `json:"deletionMark"`
	Version      int    `json:"version"`

	// Resolved references
	Token *postgres.RefDisplay `json:"token,omitempty"`
}

// FromCryptoSweep creates response DTO from domain entity.
func FromCryptoSweep(s *crypto_sweep.CryptoSweep, refs ...postgres.ResolvedRefs) *CryptoSweepResponse {
	resp := &CryptoSweepResponse{
		ID:           s.ID.String(),
		Number:       s.Number,
		Date:         s.Date.Format(time.RFC3339),
		TokenID:      s.TokenID.String(),
		HotWalletID:  s.HotWalletID.String(),
		TotalAmount:  s.TotalAmount.String(),
		TotalFee:     s.TotalFee.String(),
		Status:       string(s.Status),
		StatusName:   string(s.Status),
		LineCount:    len(s.Lines),
		Posted:       s.Posted,
		DeletionMark: s.DeletionMark,
		Version:      s.Version,
	}

	if len(refs) > 0 {
		tok := refs[0].Get(TableTokens, s.TokenID)
		resp.Token = &tok
	}

	return resp
}

// CollectCryptoSweepRefs collects FK references for batch resolution.
func CollectCryptoSweepRefs(resolver *postgres.ReferenceResolver, s *crypto_sweep.CryptoSweep) {
	resolver.Add(TableTokens, s.TokenID)
}

type CryptoSweepTokenDisplay struct {
	ID            string `json:"id"`
	Symbol        string `json:"symbol"`
	DecimalPlaces int    `json:"decimalPlaces"`
	NetworkName   string `json:"networkName,omitempty"`
	ExplorerURL   string `json:"explorerUrl,omitempty"`
}

type CryptoSweepWalletDisplay struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

type CryptoSweepLineResponse struct {
	LineID              string                    `json:"lineId,omitempty"`
	LineNo              int                       `json:"lineNo"`
	WalletID            string                    `json:"walletId,omitempty"`
	Wallet              *CryptoSweepWalletDisplay `json:"wallet,omitempty"`
	Amount              string                    `json:"amount,omitempty"`
	TxHash              string                    `json:"txHash,omitempty"`
	Confirmed           *bool                     `json:"confirmed,omitempty"`
	SettlementStatus    string                    `json:"settlementStatus,omitempty"`
	ChainResult         string                    `json:"chainResult,omitempty"`
	SettlementCheckedAt *time.Time                `json:"settlementCheckedAt,omitempty"`
	SettlementError     string                    `json:"settlementError,omitempty"`
	EnergyUsed          *int64                    `json:"energyUsed,omitempty"`
	NetUsage            *int64                    `json:"netUsage,omitempty"`
}

type CryptoSweepNetworkFeeResponse struct {
	LineID       string                   `json:"lineId"`
	SettlementID string                   `json:"settlementId"`
	TxHash       string                   `json:"txHash"`
	Amount       string                   `json:"amount"`
	Token        *CryptoSweepTokenDisplay `json:"token,omitempty"`
}

type CryptoSweepResourceCostResponse struct {
	LineID       string                   `json:"lineId"`
	RentalID     string                   `json:"rentalId"`
	RentalNumber string                   `json:"rentalNumber"`
	Status       string                   `json:"status"`
	Amount       string                   `json:"amount"`
	Token        *CryptoSweepTokenDisplay `json:"token,omitempty"`
}

// CryptoSweepDetailResponse is used only by GET /document/crypto-sweep/:id.
// The list response stays small and does not read linked ledgers.
type CryptoSweepDetailResponse struct {
	*CryptoSweepResponse
	CanRetry      bool                              `json:"canRetry,omitempty"`
	Asset         *CryptoSweepTokenDisplay          `json:"asset,omitempty"`
	HotWallet     *CryptoSweepWalletDisplay         `json:"hotWallet,omitempty"`
	Lines         []CryptoSweepLineResponse         `json:"lines"`
	NetworkFees   []CryptoSweepNetworkFeeResponse   `json:"networkFees"`
	ResourceCosts []CryptoSweepResourceCostResponse `json:"resourceCosts"`
}

func FromCryptoSweepDetail(ctx context.Context, doc *crypto_sweep.CryptoSweep, summary *CryptoSweepResponse, data document_repo.CryptoSweepCardData) CryptoSweepDetailResponse {
	result := CryptoSweepDetailResponse{
		CryptoSweepResponse: summary,
		Lines:               []CryptoSweepLineResponse{}, NetworkFees: []CryptoSweepNetworkFeeResponse{},
		ResourceCosts: []CryptoSweepResourceCostResponse{},
	}
	source := security.GetFieldPolicy(ctx, "crypto_sweep", "read")
	allowSource := func(field string) bool { return sweepFieldAllowed(source, field) }
	tokenPolicy := security.GetFieldPolicy(ctx, "token", "read")
	networkPolicy := security.GetFieldPolicy(ctx, "blockchain_network", "read")
	walletPolicy := security.GetFieldPolicy(ctx, "wallet", "read")
	tokens := make(map[id.ID]*CryptoSweepTokenDisplay, len(data.Tokens))
	if sweepFieldAllowed(tokenPolicy, "symbol") && sweepFieldAllowed(tokenPolicy, "decimal_places") {
		for _, token := range data.Tokens {
			display := &CryptoSweepTokenDisplay{ID: token.ID.String(), Symbol: token.Symbol, DecimalPlaces: token.DecimalPlaces}
			if sweepFieldAllowed(networkPolicy, "name") {
				display.NetworkName = token.NetworkName
			}
			if sweepFieldAllowed(networkPolicy, "explorer_url") {
				display.ExplorerURL = token.ExplorerURL
			}
			tokens[token.ID] = display
		}
	}
	wallets := make(map[id.ID]*CryptoSweepWalletDisplay, len(data.Wallets))
	for _, wallet := range data.Wallets {
		display := &CryptoSweepWalletDisplay{ID: wallet.ID.String()}
		if sweepFieldAllowed(walletPolicy, "name") {
			display.Name = wallet.Name
		}
		if sweepFieldAllowed(walletPolicy, "address") {
			display.Address = wallet.Address
		}
		wallets[wallet.ID] = display
	}
	if allowSource("token_id") {
		result.Asset = tokens[doc.TokenID]
	}
	if allowSource("hot_wallet_id") {
		result.HotWallet = wallets[doc.HotWalletID]
	}
	if !allowSource("lines") {
		return result
	}
	attempts := make(map[id.ID]document_repo.CryptoSweepCardAttempt, len(data.Attempts))
	for _, attempt := range data.Attempts {
		attempts[attempt.LineID] = attempt
		if doc.Status == crypto_sweep.SweepStatusHeld && !doc.Posted && attempt.Retryable {
			result.CanRetry = true
		}
	}
	for _, line := range doc.Lines {
		row := CryptoSweepLineResponse{LineNo: line.LineNo}
		if allowSource("lines.line_id") {
			row.LineID = line.LineID.String()
		}
		if allowSource("lines.wallet_id") {
			row.WalletID = line.WalletID.String()
			row.Wallet = wallets[line.WalletID]
		}
		if allowSource("lines.amount") {
			row.Amount = line.Amount.String()
		}
		if allowSource("lines.tx_hash") {
			row.TxHash = line.TxHash
		}
		if allowSource("lines.confirmed") {
			row.Confirmed = &line.Confirmed
		}
		if allowSource("lines.settlement_status") {
			row.SettlementStatus = line.SettlementStatus.String()
		}
		if allowSource("lines.chain_result") {
			row.ChainResult = line.ChainResult.String()
		}
		if allowSource("lines.settlement_checked_at") {
			row.SettlementCheckedAt = line.SettlementCheckedAt
		}
		if allowSource("lines.settlement_error") {
			row.SettlementError = line.SettlementError
			if row.SettlementError == "" {
				row.SettlementError = attempts[line.LineID].ErrorMessage
			}
		}
		if allowSource("lines.energy_used") {
			row.EnergyUsed = &line.EnergyUsed
		}
		if allowSource("lines.net_usage") {
			row.NetUsage = &line.NetUsage
		}
		result.Lines = append(result.Lines, row)
	}
	// Linked financial effects require both source visibility and register FLS.
	if !allowSource("posted") || !allowSource("total_fee") || !allowSource("lines.line_id") || !allowSource("lines.network_fee") {
		return result
	}
	platform := security.GetFieldPolicy(ctx, "crypto_platform_cost", "read")
	if sweepFieldsAllowed(platform, "recorder_id", "amount", "cost_token_id", "cost_type", "purpose") {
		for _, fee := range data.NetworkFees {
			result.NetworkFees = append(result.NetworkFees, CryptoSweepNetworkFeeResponse{
				LineID: fee.LineID.String(), SettlementID: fee.SettlementID.String(), TxHash: fee.TxHash,
				Amount: fee.Amount, Token: tokens[fee.CostTokenID],
			})
		}
	}
	rental := security.GetFieldPolicy(ctx, "crypto_resource_rental", "read")
	if sweepFieldsAllowed(rental, "id", "number", "status", "cost_token_id", "allocated_cost_amount") {
		for _, cost := range data.ResourceCosts {
			result.ResourceCosts = append(result.ResourceCosts, CryptoSweepResourceCostResponse{
				LineID: cost.LineID.String(), RentalID: cost.RentalID.String(), RentalNumber: cost.RentalNumber,
				Status: cost.Status, Amount: cost.Amount, Token: tokens[cost.CostTokenID],
			})
		}
	}
	return result
}

func sweepFieldAllowed(policy *security.FieldPolicy, path string) bool {
	if policy == nil {
		return true
	}
	if len(path) > 6 && path[:6] == "lines." {
		return policy.IsFieldAllowed("lines") && policy.IsFieldAllowed(path) && policy.IsTablePartFieldAllowed("lines", path[6:])
	}
	return policy.IsFieldAllowed(path)
}

func sweepFieldsAllowed(policy *security.FieldPolicy, fields ...string) bool {
	for _, field := range fields {
		if !sweepFieldAllowed(policy, field) {
			return false
		}
	}
	return true
}

// Stub DTOs for BaseDocumentHandler type compliance.

type CreateCryptoSweepRequest struct{}

func (r *CreateCryptoSweepRequest) ToEntity() *crypto_sweep.CryptoSweep {
	return &crypto_sweep.CryptoSweep{}
}

type UpdateCryptoSweepRequest struct {
	Version int `json:"version" binding:"required"`
}

func (r *UpdateCryptoSweepRequest) ApplyTo(s *crypto_sweep.CryptoSweep) {
	s.Version = r.Version
}
