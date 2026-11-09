package dto

// CreateMerchantAddressRequest is POSTed by merchants to assign a persistent wallet to a customer.
type CreateMerchantAddressRequest struct {
	// Currency code matching a configured token, e.g. "USDT-TRC20". Required.
	Currency string `json:"currency" binding:"required"`

	// CustomerID is the merchant's internal ID for the customer. Required.
	// Used to ensure idempotency (one address per currency network per customer).
	CustomerID string `json:"customerId" binding:"required"`
}

// MerchantAddressResponse is the response for address creation.
type MerchantAddressResponse struct {
	Address    string `json:"address"`
	Currency   string `json:"currency"`
	Network    string `json:"network"`
	CustomerID string `json:"customerId"`
}
