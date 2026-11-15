package cryptodto

import "metapus/internal/application/cryptoapp"

type ResourceRentalMetricsResponse = cryptoapp.ResourceRentalMetrics

type ProviderBalanceIncidentsResponse struct {
	Items []cryptoapp.ProviderBalanceIncident `json:"items"`
}

type ResourceRentalAllocationsResponse struct {
	Items []cryptoapp.ResourceAllocation `json:"items"`
}
