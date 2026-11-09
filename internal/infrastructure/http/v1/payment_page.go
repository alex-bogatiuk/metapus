package v1

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed static/payment.html
var paymentPageFS embed.FS

// RegisterPaymentPage registers the embedded payment page HTML for tenantless
// public checkout URLs.
func RegisterPaymentPage(router *gin.Engine) {
	// Read the embedded file once at startup.
	content, err := fs.ReadFile(paymentPageFS, "static/payment.html")
	if err != nil {
		panic("payment page HTML not embedded: " + err.Error())
	}

	serve := func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	}
	router.GET("/pay/i/:publicRef", serve)
	router.GET("/pay/l/:publicRef", serve)
}
