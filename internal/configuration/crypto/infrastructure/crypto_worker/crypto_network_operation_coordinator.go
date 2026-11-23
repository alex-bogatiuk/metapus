package crypto_worker

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

// Compatibility facade used by runtime composition; application owns the transaction.
type CryptoNetworkOperationCoordinator struct {
	*outboundapp.NetworkOperationCoordinator
}

func NewCryptoNetworkOperationCoordinator(repo crypto_network_operation.Repository, service *crypto_network_operation.Service, fees *FeeBudgetManager) *CryptoNetworkOperationCoordinator {
	var commands outboundapp.NetworkOperationCommands
	if service != nil {
		commands = service
	}
	var settlement outboundapp.DocumentFeeSettlement
	if fees != nil {
		settlement = fees
	}
	return &CryptoNetworkOperationCoordinator{outboundapp.NewNetworkOperationCoordinator(repo, commands, settlement, activationWorkflowStore{}, tenant.ContextTxManager{}, time.Now)}
}

func (c *CryptoNetworkOperationCoordinator) EnsureActivationBroadcast(ctx context.Context, task AccountActivationTask, hash string) (id.ID, error) {
	var service *outboundapp.NetworkOperationCoordinator
	if c != nil {
		service = c.NetworkOperationCoordinator
	}
	return service.EnsureActivationBroadcast(ctx, task, hash)
}
func (c *CryptoNetworkOperationCoordinator) FinalizeActivation(ctx context.Context, activation, reservation id.ID, receipt crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error) {
	var service *outboundapp.NetworkOperationCoordinator
	if c != nil {
		service = c.NetworkOperationCoordinator
	}
	return service.FinalizeActivation(ctx, activation, reservation, receipt)
}

func (c *CryptoNetworkOperationCoordinator) FinalizeActivationRepair(ctx context.Context, task outboundapp.ActivationCostRepairTask, reservation id.ID, receipt crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error) {
	var service *outboundapp.NetworkOperationCoordinator
	if c != nil {
		service = c.NetworkOperationCoordinator
	}
	return service.FinalizeActivationRepair(ctx, task, reservation, receipt)
}

func (c *CryptoNetworkOperationCoordinator) RecoverActivationBroadcast(ctx context.Context, task AccountActivationTask, hash string) (id.ID, error) {
	var service *outboundapp.NetworkOperationCoordinator
	if c != nil {
		service = c.NetworkOperationCoordinator
	}
	return service.RecoverActivationBroadcast(ctx, task, hash)
}
