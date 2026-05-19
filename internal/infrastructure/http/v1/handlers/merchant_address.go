package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/http/v1/dto"
	mw "metapus/internal/infrastructure/http/v1/middleware"
)

// MerchantAddressHandler handles /merchant/v1/addresses endpoints.
// Requires MerchantAPIKey middleware.
type MerchantAddressHandler struct {
	walletService interface {
		AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerRef string) (*wallet.Wallet, error)
	}
}

// NewMerchantAddressHandler creates the handler.
func NewMerchantAddressHandler(
	walletService interface {
		AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerRef string) (*wallet.Wallet, error)
	},
) *MerchantAddressHandler {
	return &MerchantAddressHandler{
		walletService: walletService,
	}
}

// CreateAddress handles POST /merchant/v1/addresses.
// Assigns a persistent wallet to a customer. Idempotent by customerRef + currency.
func (h *MerchantAddressHandler) CreateAddress(c *gin.Context) {
	mc := mw.GetMerchant(c.Request.Context())
	if mc == nil {
		_ = c.Error(apperror.NewUnauthorized("merchant context missing"))
		c.Abort()
		return
	}

	if !mc.HasScope(merchant.ScopeAddressCreate) {
		_ = c.Error(apperror.NewForbidden("missing scope: address:create"))
		c.Abort()
		return
	}

	var req dto.CreateMerchantAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation(err.Error()))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	pool := tenant.MustGetPool(ctx)

	// Resolve token by currency code (we need the NetworkID from it)
	tokenID, err := resolveTokenByCode(ctx, pool, req.Currency)
	if err != nil {
		_ = c.Error(apperror.NewValidation(fmt.Sprintf("unknown currency: %s", req.Currency)).
			WithDetail("field", "currency"))
		c.Abort()
		return
	}

	// We need to resolve the network from the token
	// This relies on cat_tokens joining cat_blockchain_networks.
	// Since resolveTokenByCode only gives tokenID, we do a direct query for the network ID.
	var networkID id.ID
	var networkCode string
	err = pool.QueryRow(ctx, "SELECT t.network_id, n.code FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id = t.network_id WHERE t.id = $1", tokenID).Scan(&networkID, &networkCode)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to get network for token: %w", err))
		c.Abort()
		return
	}

	w, err := h.walletService.AssignPersistentAddress(ctx, mc.MerchantID, networkID, req.CustomerRef)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.MerchantAddressResponse{
		Address:     w.Address,
		Currency:    req.Currency,
		Network:     networkCode,
		CustomerRef: w.CustomerRef,
	})
}
