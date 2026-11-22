package crypto_worker

import (
	"context"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/pkg/sdk/id"
)

var ErrMerchantCustody = outbound_repo.ErrMerchantCustody

func custodyService() *outboundapp.CustodyService {
	return outboundapp.NewCustodyService(custodyStore{})
}
func (s *SigningAuthorizationService) PrepareTransferCustody(ctx context.Context, intent crypto.TransferIntent, expected *id.ID) (*id.ID, error) {
	return custodyService().PrepareTransferCustody(ctx, intent, expected)
}
func (s *SigningAuthorizationService) CheckCustody(ctx context.Context, auth crypto.SigningAuthorization) error {
	return custodyService().CheckCustody(ctx, auth)
}
func (s *SigningAuthorizationService) CheckCustodySigner(ctx context.Context, network crypto.NetworkRef, signer crypto.PolicyAwareTransactionSigner) error {
	return custodyService().CheckCustodySigner(ctx, network, signer)
}
func (s *SigningAuthorizationService) CheckCustodyDispatch(ctx context.Context, auth crypto.SigningAuthorization, registry *crypto.FamilySignerRegistry) error {
	return custodyService().CheckCustodyDispatch(ctx, auth, registry)
}

// A mined effect belongs to history even when today's custody policy prohibits
// another send. Its immutable hash/network and financial finality are required.
func observedSavedOutflow(ctx context.Context, driver crypto.NetworkDriver, record SigningAuthorizationRecord) bool {
	if record.SignedTx == nil || record.SignedTx.TxHash == "" || record.TxID != record.SignedTx.TxHash || driver.Finality() == nil {
		return false
	}
	receipt, err := driver.Finality().Receipt(ctx, record.SignedTx.TxHash)
	return err == nil && receipt.NetworkID == record.Authorization.NetworkID && receipt.TxHash == record.SignedTx.TxHash && receipt.FinanciallyFinal()
}
