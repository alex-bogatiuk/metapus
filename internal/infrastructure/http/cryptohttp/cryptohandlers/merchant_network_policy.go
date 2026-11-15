package cryptohandlers

import (
	"net/netip"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/http/cryptohttp/cryptomiddleware"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
)

type MerchantNetworkPolicyHandler struct {
	base     *handlers.BaseHandler
	service  *cryptoapp.MerchantNetworkPolicyService
	stepUp   middleware.StepUpChecker
	recovery bool
}

func NewMerchantNetworkPolicyHandler(repo merchant.NetworkPolicyRepository, stepUp middleware.StepUpChecker, recovery bool) *MerchantNetworkPolicyHandler {
	return &MerchantNetworkPolicyHandler{
		base:     handlers.NewBaseHandler(),
		service:  cryptoapp.NewMerchantNetworkPolicyService(postgres.NewContextTxManager(), repo),
		stepUp:   stepUp,
		recovery: recovery,
	}
}

func (h *MerchantNetworkPolicyHandler) Get(c *gin.Context) {
	merchantID, surface, keyID, err := h.scope(c)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	policy, err := h.service.Get(c.Request.Context(), merchantID, surface, keyID)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	ip, err := netip.ParseAddr(c.ClientIP())
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid client IP"))
		return
	}
	h.base.OK(c, dto.MerchantNetworkPolicyResponse{Policy: policy, DetectedIP: ip.Unmap().String(), NetworkAccess: merchant.EvaluateNetworkPolicies([]merchant.NetworkPolicy{*policy}, ip.Unmap())})
}

func (h *MerchantNetworkPolicyHandler) SetMode(c *gin.Context) {
	merchantID, surface, keyID, err := h.scope(c)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	actorID, err := h.requireStepUp(c, merchantID, surface, keyID, "set_mode")
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.SetMerchantNetworkPolicyRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	ip, err := netip.ParseAddr(c.ClientIP())
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid client IP"))
		return
	}
	policy, err := h.service.SetMode(c.Request.Context(), merchantID, surface, keyID, req.Mode, actorID, ip.Unmap(), req.Reason, h.recovery)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	cryptomiddleware.InvalidateMerchantNetworkPolicyCache(merchantID)
	h.base.OK(c, policy)
}

func (h *MerchantNetworkPolicyHandler) AddNetwork(c *gin.Context) {
	merchantID, surface, keyID, err := h.scope(c)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	actorID, err := h.requireStepUp(c, merchantID, surface, keyID, "add_network")
	if err != nil {
		h.base.Error(c, err)
		return
	}
	var req dto.AddMerchantAllowedNetworkRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	policy, err := h.service.AddNetwork(c.Request.Context(), merchantID, surface, keyID, req.CIDR, req.Label, actorID, req.Reason, h.recovery)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	cryptomiddleware.InvalidateMerchantNetworkPolicyCache(merchantID)
	h.base.OK(c, policy)
}

func (h *MerchantNetworkPolicyHandler) RevokeNetwork(c *gin.Context) {
	merchantID, surface, keyID, err := h.scope(c)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	actorID, err := h.requireStepUp(c, merchantID, surface, keyID, "revoke_network")
	if err != nil {
		h.base.Error(c, err)
		return
	}
	networkID, err := id.Parse(c.Param("networkId"))
	if err != nil {
		h.base.Error(c, apperror.NewValidation("invalid allowed network id"))
		return
	}
	var req dto.RequiredReasonRequest
	if !h.base.BindJSON(c, &req) {
		return
	}
	policy, err := h.service.RevokeNetwork(c.Request.Context(), merchantID, surface, keyID, networkID, actorID, req.Reason, h.recovery)
	if err != nil {
		h.base.Error(c, err)
		return
	}
	cryptomiddleware.InvalidateMerchantNetworkPolicyCache(merchantID)
	h.base.OK(c, policy)
}

func (h *MerchantNetworkPolicyHandler) scope(c *gin.Context) (id.ID, merchant.NetworkSurface, *id.ID, error) {
	merchantRaw := c.Query("merchant_id")
	if h.recovery {
		merchantRaw = c.Param("merchantId")
		if merchantRaw == "" {
			merchantRaw = c.Param("id")
		}
	}
	merchantID, err := id.Parse(merchantRaw)
	if err != nil {
		return id.Nil(), "", nil, apperror.NewValidation("invalid merchant id")
	}
	surface := merchant.NetworkSurface(c.Param("surface"))
	if surface == "" {
		surface = merchant.NetworkSurfaceAPI
	}
	if !surface.Valid() {
		return id.Nil(), "", nil, apperror.NewValidation("surface must be portal or api")
	}
	keyRaw := c.Param("keyId")
	var keyID *id.ID
	if keyRaw != "" {
		parsed, err := id.Parse(keyRaw)
		if err != nil {
			return id.Nil(), "", nil, apperror.NewValidation("invalid API key id")
		}
		keyID = &parsed
		surface = merchant.NetworkSurfaceAPI
	}
	return merchantID, surface, keyID, nil
}

func (h *MerchantNetworkPolicyHandler) requireStepUp(c *gin.Context, merchantID id.ID, surface merchant.NetworkSurface, keyID *id.ID, operation string) (id.ID, error) {
	object := merchantID.String() + ":" + string(surface)
	if keyID != nil {
		object += ":" + keyID.String()
	}
	action := "merchant.network_policy." + operation + ":" + object
	if h.recovery {
		action = "admin.recovery." + action
	}
	actorID, _, _, err := requireObjectBoundMoneyMovementStepUp(c.Request.Context(), h.stepUp, action)
	return actorID, err
}
