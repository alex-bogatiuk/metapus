package cryptodto

import (
	"time"

	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoNetworkOperationResponse is a wallet activation transaction and the
// network fee it was recognized with. The activation workflow owns it.
type CryptoNetworkOperationResponse struct {
	dto.DocumentResponse
	OperationType  string     `json:"operationType"`
	Status         string     `json:"status"`
	NetworkID      string     `json:"networkId"`
	TargetWalletID string     `json:"targetWalletId"`
	TargetAddress  string     `json:"targetAddress"`
	SourceWalletID string     `json:"sourceWalletId"`
	CostTokenID    string     `json:"costTokenId"`
	CostAmount     string     `json:"costAmount"`
	TxHash         string     `json:"txHash"`
	FinalizedAt    *time.Time `json:"finalizedAt,omitempty"`

	Network      *postgres.RefDisplay `json:"network,omitempty"`
	TargetWallet *postgres.RefDisplay `json:"targetWallet,omitempty"`
	SourceWallet *postgres.RefDisplay `json:"sourceWallet,omitempty"`
	CostToken    *postgres.RefDisplay `json:"costToken,omitempty"`
}

func CollectCryptoNetworkOperationRefs(resolver *postgres.ReferenceResolver, d *crypto_network_operation.CryptoNetworkOperation) {
	resolver.Add(TableBlockchainNetworks, d.NetworkID)
	resolver.Add(TableWallets, d.TargetWalletID)
	resolver.Add(TableWallets, d.SourceWalletID)
	resolver.Add(TableTokens, d.CostTokenID)
}

func FromCryptoNetworkOperation(d *crypto_network_operation.CryptoNetworkOperation, refs postgres.ResolvedRefs) CryptoNetworkOperationResponse {
	resp := CryptoNetworkOperationResponse{
		DocumentResponse: documentResponse(d.Document),
		OperationType:    string(d.OperationType), Status: string(d.Status), NetworkID: d.NetworkID.String(),
		TargetWalletID: d.TargetWalletID.String(), TargetAddress: d.TargetAddress, SourceWalletID: d.SourceWalletID.String(),
		CostTokenID: d.CostTokenID.String(), CostAmount: d.CostAmount.String(), TxHash: d.TxHash, FinalizedAt: d.FinalizedAt,
	}
	if refs != nil {
		network, target := refs.Get(TableBlockchainNetworks, d.NetworkID), refs.Get(TableWallets, d.TargetWalletID)
		source, token := refs.Get(TableWallets, d.SourceWalletID), refs.Get(TableTokens, d.CostTokenID)
		resp.Network, resp.TargetWallet, resp.SourceWallet, resp.CostToken = &network, &target, &source, &token
	}
	return resp
}
