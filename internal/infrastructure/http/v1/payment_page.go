package v1

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed static/payment.html
var paymentPageFS embed.FS

// RegisterPaymentPage registers the GET /pay/:invoiceId route that serves
// the embedded payment page HTML. The HTML page fetches data from
// GET /api/v1/pay/:invoiceId (JSON API) for rendering.
func RegisterPaymentPage(router *gin.Engine) {
	// Read the embedded file once at startup.
	content, err := fs.ReadFile(paymentPageFS, "static/payment.html")
	if err != nil {
		panic("payment page HTML not embedded: " + err.Error())
	}

	// Serve the same HTML for any /pay/:invoiceId path.
	// The JS inside extracts invoiceId from the URL path.
	router.GET("/pay/:invoiceId", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})
}
