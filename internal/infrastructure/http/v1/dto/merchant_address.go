package dto

// CreateMerchantAddressRequest is POSTed by merchants to assign a persistent wallet to a customer.
type CreateMerchantAddressRequest struct {
	// Currency code matching a configured token, e.g. "USDT_TRC20". Required.
	Currency string `json:"currency" binding:"required"`

	// CustomerRef is the merchant's internal ID for the customer. Required.
	// Used to ensure idempotency (one address per currency network per customer).
	CustomerRef string `json:"customerRef" binding:"required"`
}

// MerchantAddressResponse is the response for address creation.
type MerchantAddressResponse struct {
	Address     string `json:"address"`
	Currency    string `json:"currency"`
	Network     string `json:"network"`
	CustomerRef string `json:"customerRef"`
}
