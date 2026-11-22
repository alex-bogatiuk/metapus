package cryptodto

import (
	"time"

	"metapus/internal/configuration/crypto/domain/documents/crypto_resource_rental"
	"metapus/pkg/sdk/http/dto"
	"metapus/pkg/sdk/postgres"
)

// CryptoResourceRentalResponse is a TRON resource rental the rental workflow
// owns; provider payloads and worker leases stay internal.
type CryptoResourceRentalResponse struct {
	dto.DocumentResponse
	ProviderAccountID    string     `json:"providerAccountId"`
	NetworkID            string     `json:"networkId"`
	ReceiveWalletID      string     `json:"receiveWalletId"`
	CostTokenID          string     `json:"costTokenId"`
	ResourceType         string     `json:"resourceType"`
	RentalMode           string     `json:"rentalMode"`
	RentalPeriod         string     `json:"rentalPeriod"`
	RequestedAmount      int64      `json:"requestedAmount"`
	DelegatedAmount      int64      `json:"delegatedAmount"`
	ReservedAmount       int64      `json:"reservedAmount"`
	ConsumedAmount       int64      `json:"consumedAmount"`
	GrossCostAmount      string     `json:"grossCostAmount"`
	RefundAmount         string     `json:"refundAmount"`
	NetCostAmount        string     `json:"netCostAmount"`
	AllocatedCostAmount  string     `json:"allocatedCostAmount"`
	UnusedCostAmount     string     `json:"unusedCostAmount"`
	Provider             string     `json:"provider"`
	OutTradeNo           string     `json:"outTradeNo"`
	ExternalSerial       string     `json:"externalSerial,omitempty"`
	Status               string     `json:"status"`
	CostStatus           string     `json:"costStatus"`
	ProviderBalanceAfter *string    `json:"providerBalanceAfter,omitempty"`
	ErrorMessage         string     `json:"errorMessage,omitempty"`
	DelegatedAt          *time.Time `json:"delegatedAt,omitempty"`
	ActivatedAt          *time.Time `json:"activatedAt,omitempty"`
	ReclaimRequestedAt   *time.Time `json:"reclaimRequestedAt,omitempty"`
	ExpiresAt            *time.Time `json:"expiresAt,omitempty"`
	ClosedAt             *time.Time `json:"closedAt,omitempty"`

	Network       *postgres.RefDisplay `json:"network,omitempty"`
	ReceiveWallet *postgres.RefDisplay `json:"receiveWallet,omitempty"`
	CostToken     *postgres.RefDisplay `json:"costToken,omitempty"`
}

func CollectCryptoResourceRentalRefs(resolver *postgres.ReferenceResolver, r *crypto_resource_rental.CryptoResourceRental) {
	resolver.Add(TableBlockchainNetworks, r.NetworkID)
	resolver.Add(TableWallets, r.ReceiveWalletID)
	resolver.Add(TableTokens, r.CostTokenID)
}

func FromCryptoResourceRental(r *crypto_resource_rental.CryptoResourceRental, refs postgres.ResolvedRefs) CryptoResourceRentalResponse {
	resp := CryptoResourceRentalResponse{
		DocumentResponse:  documentResponse(r.Document),
		ProviderAccountID: r.ProviderAccountID.String(), NetworkID: r.NetworkID.String(), ReceiveWalletID: r.ReceiveWalletID.String(),
		CostTokenID: r.CostTokenID.String(), ResourceType: r.ResourceType, RentalMode: r.RentalMode, RentalPeriod: r.RentalPeriod,
		RequestedAmount: r.RequestedAmount, DelegatedAmount: r.DelegatedAmount, ReservedAmount: r.ReservedAmount, ConsumedAmount: r.ConsumedAmount,
		GrossCostAmount: r.GrossCostAmount.String(), RefundAmount: r.RefundAmount.String(), NetCostAmount: r.NetCostAmount.String(),
		AllocatedCostAmount: r.AllocatedCostAmount.String(), UnusedCostAmount: r.UnusedCostAmount.String(),
		Provider: r.Provider, OutTradeNo: r.OutTradeNo, ExternalSerial: r.ExternalSerial,
		Status: string(r.Status), CostStatus: string(r.CostStatus), ErrorMessage: r.ErrorMessage,
		DelegatedAt: r.DelegatedAt, ActivatedAt: r.ActivatedAt, ReclaimRequestedAt: r.ReclaimRequestedAt, ExpiresAt: r.ExpiresAt, ClosedAt: r.ClosedAt,
	}
	if r.ProviderBalanceAfter != nil {
		balance := r.ProviderBalanceAfter.String()
		resp.ProviderBalanceAfter = &balance
	}
	if refs != nil {
		network, wallet, token := refs.Get(TableBlockchainNetworks, r.NetworkID), refs.Get(TableWallets, r.ReceiveWalletID), refs.Get(TableTokens, r.CostTokenID)
		resp.Network, resp.ReceiveWallet, resp.CostToken = &network, &wallet, &token
	}
	return resp
}
