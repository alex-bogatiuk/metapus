package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetRiskBalance handles one explicitly authorized merchant; balances from
// different merchants are never presented as one interchangeable principal.
func (h *PortalHandler) GetRiskBalance(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	merchantID, err := parseRequiredMerchant(c)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	result, err := h.amlBalanceService.Get(c.Request.Context(), merchantID)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, result)
}
