package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/http/v1/dto"
)

func (h *CryptoNetworkHandler) ShadowStatus(c *gin.Context) {
	network, ok := h.networkID(c)
	if !ok {
		return
	}
	state, err := h.shadow.Get(c.Request.Context(), network)
	if err != nil {
		h.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *CryptoNetworkHandler) SetShadowPaused(c *gin.Context) {
	network, ok := h.networkID(c)
	if !ok {
		return
	}
	var request dto.SetCryptoShadowControlRequest
	if !h.BindJSON(c, &request) {
		return
	}
	state, err := h.shadow.SetPaused(c.Request.Context(), network, request.Version, *request.Paused, request.Reason)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.CompleteIdempotency(c, http.StatusAccepted, "application/json", state)
	c.JSON(http.StatusAccepted, state)
}
