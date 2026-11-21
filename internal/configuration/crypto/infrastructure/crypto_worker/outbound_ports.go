package crypto_worker

import (
	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
)

// Convert optional concrete dependencies without creating non-nil interfaces
// containing nil pointers. Application configuration checks stay fail-closed.
func feeBudgetPort(value *FeeBudgetManager) outboundapp.FeeBudget {
	if value == nil {
		return nil
	}
	return value
}
func approvalBundlePort(value *money_movement_repo.Repo) outboundapp.ApprovalBundles {
	if value == nil {
		return nil
	}
	return value
}
func authorizationPort(value *SigningAuthorizationService) outboundapp.Authorizations {
	if value == nil {
		return nil
	}
	return value
}
