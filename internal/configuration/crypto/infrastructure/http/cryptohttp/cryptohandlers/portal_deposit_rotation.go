package cryptohandlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

func (h *PortalHandler) RotateDepositAddress(c *gin.Context) {
	merchantID, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	walletID, err := id.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(apperror.NewValidation("invalid deposit address id"))
		c.Abort()
		return
	}
	var req cryptodto.PortalRotateDepositAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}
	user := appctx.GetUser(c.Request.Context())
	if user == nil {
		_ = c.Error(apperror.NewUnauthorized("authentication required"))
		c.Abort()
		return
	}
	actorID, err := id.Parse(user.UserID)
	if err != nil {
		_ = c.Error(apperror.NewUnauthorized("invalid user context"))
		c.Abort()
		return
	}
	result, err := h.depositRotationService.Rotate(c.Request.Context(), wallet.DepositRotationCommand{
		MerchantID: merchantID, WalletID: walletID, ActorID: actorID, Reason: req.Reason, IdempotencyKey: c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, cryptodto.PortalRotateDepositAddressResponse{
		PortalCreateDepositAddressResponse: cryptodto.PortalCreateDepositAddressResponse{
			Object: "deposit_address", ID: result.NewWalletID.String(), CustomerID: result.CustomerID, Address: result.NewAddress,
			NetworkID: result.NetworkID.String(), NetworkCode: result.NetworkCode, NetworkName: result.NetworkName, Tokens: result.Tokens, Reused: result.Replayed,
			CreatedAt: result.CreatedAt.UTC().Format(time.RFC3339),
			QRCode:    generateQRDataURL(result.NewAddress),
		},
		RotationID: result.ID.String(), OldWalletID: result.OldWalletID.String(), OldAddress: result.OldAddress, ResourceVersion: result.Version,
		NextRotationAt: result.CreatedAt.Add(wallet.DepositRotationCooldown),
	})
}
