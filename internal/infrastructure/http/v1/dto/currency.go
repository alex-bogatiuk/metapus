package dto

import (
	"time"

	"github.com/shopspring/decimal"
)

// UpdateExchangeRateRequest is the request body for updating exchange rate.
// The currency catalog itself is declared from its model (content.currencyRegistration).
type UpdateExchangeRateRequest struct {
	Rate decimal.Decimal `json:"rate" binding:"required"`
	Date time.Time       `json:"date"`
}
