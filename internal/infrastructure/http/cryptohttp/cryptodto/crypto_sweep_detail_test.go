package cryptodto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
)

func TestCryptoSweepDetailSeparatesLinkedCostsAndRespectsFieldPolicies(t *testing.T) {
	lineID, tokenID, feeTokenID, walletID, rentalID := id.New(), id.New(), id.New(), id.New(), id.New()
	doc := &crypto_sweep.CryptoSweep{TokenID: tokenID, HotWalletID: walletID, Lines: []crypto_sweep.CryptoSweepLine{{
		LineID: lineID, LineNo: 1, WalletID: walletID, TxHash: "0xabc", Confirmed: true,
		SettlementStatus: outbound_settlement.StatusRecorded,
	}}}
	facts := crypto_document_repo.CryptoSweepCardData{
		Tokens: []crypto_document_repo.CryptoSweepCardToken{
			{ID: tokenID, Symbol: "USDT", DecimalPlaces: 6},
			{ID: feeTokenID, Symbol: "ETH", DecimalPlaces: 18},
		},
		Wallets:       []crypto_document_repo.CryptoSweepCardWallet{{ID: walletID, Name: "Pool", Address: "0xwallet"}},
		NetworkFees:   []crypto_document_repo.CryptoSweepCardNetworkFee{{LineID: lineID, SettlementID: id.New(), CostTokenID: feeTokenID, Amount: "23620165128000", TxHash: "0xabc"}},
		ResourceCosts: []crypto_document_repo.CryptoSweepCardResourceCost{{LineID: lineID, RentalID: rentalID, RentalNumber: "R-1", Status: "confirmed", CostTokenID: feeTokenID, Amount: "340"}},
	}
	result := FromCryptoSweepDetail(context.Background(), doc, &CryptoSweepResponse{}, facts)
	require.Equal(t, "USDT", result.Asset.Symbol)
	require.Equal(t, "ETH", result.NetworkFees[0].Token.Symbol)
	require.Equal(t, "23620165128000", result.NetworkFees[0].Amount)
	require.Equal(t, "340", result.ResourceCosts[0].Amount, "only the line allocation is shown, never the full rental cost")

	ctx := security.WithFieldPolicies(context.Background(), map[string]*security.FieldPolicy{
		"crypto_sweep:read": {AllowedFields: []string{"*", "-total_fee"}},
		"wallet:read":       {AllowedFields: []string{"*", "-address"}},
	})
	restricted := FromCryptoSweepDetail(ctx, doc, &CryptoSweepResponse{}, facts)
	require.Empty(t, restricted.NetworkFees)
	require.Empty(t, restricted.ResourceCosts)
	require.Equal(t, "", restricted.HotWallet.Address)

	ctx = security.WithFieldPolicies(context.Background(), map[string]*security.FieldPolicy{
		"crypto_platform_cost:read": {AllowedFields: []string{"*", "-amount"}},
	})
	restricted = FromCryptoSweepDetail(ctx, doc, &CryptoSweepResponse{}, facts)
	require.Empty(t, restricted.NetworkFees)
	require.Len(t, restricted.ResourceCosts, 1)
}

func TestCryptoSweepDetailExposesPreBroadcastAttemptErrorAndRetryability(t *testing.T) {
	lineID, tokenID, walletID := id.New(), id.New(), id.New()
	doc := &crypto_sweep.CryptoSweep{
		TokenID:     tokenID,
		HotWalletID: walletID,
		Status:      crypto_sweep.SweepStatusHeld,
		Lines: []crypto_sweep.CryptoSweepLine{{
			LineID: lineID, LineNo: 1, WalletID: walletID,
			SettlementStatus: outbound_settlement.StatusUnknown,
		}},
	}
	facts := crypto_document_repo.CryptoSweepCardData{
		Attempts: []crypto_document_repo.CryptoSweepCardAttempt{{
			LineID:       lineID,
			Status:       "held",
			ErrorMessage: "estimated network fee exceeds per-transaction policy cap",
			Retryable:    true,
		}},
	}

	result := FromCryptoSweepDetail(context.Background(), doc, &CryptoSweepResponse{}, facts)
	require.True(t, result.CanRetry)
	require.Len(t, result.Lines, 1)
	require.Equal(t, "estimated network fee exceeds per-transaction policy cap", result.Lines[0].SettlementError)
}
