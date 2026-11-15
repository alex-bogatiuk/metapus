package cryptohandlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/http/cryptohttp/cryptomiddleware"
	"metapus/internal/infrastructure/http/v1/dto"
)

// MerchantAddressHandler handles /merchant/v1/addresses endpoints.
// Requires MerchantAPIKey middleware.
type MerchantAddressHandler struct {
	walletService interface {
		AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error)
	}
	queries *cryptoapp.MerchantCryptoQueryService
}

// NewMerchantAddressHandler creates the handler.
func NewMerchantAddressHandler(
	walletService interface {
		AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerID string) (*wallet.Wallet, error)
	},
	queries *cryptoapp.MerchantCryptoQueryService,
) *MerchantAddressHandler {
	return &MerchantAddressHandler{
		walletService: walletService,
		queries:       queries,
	}
}

// CreateAddress handles POST /merchant/v1/addresses.
// Assigns a persistent wallet to a customer. Idempotent by customerId + currency.
func (h *MerchantAddressHandler) CreateAddress(c *gin.Context) {
	mc := cryptomiddleware.GetMerchant(c.Request.Context())
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
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	ctx := c.Request.Context()
	token, err := h.queries.ResolveToken(ctx, req.Currency)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	w, err := h.walletService.AssignPersistentAddress(ctx, mc.MerchantID, token.NetworkID, req.CustomerID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, dto.MerchantAddressResponse{
		Address:    w.Address,
		Currency:   req.Currency,
		Network:    token.NetworkCode,
		CustomerID: w.CustomerID,
	})
}
