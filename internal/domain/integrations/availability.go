package integrations

import (
	"time"

	"metapus/internal/core/apperror"
)

// PoolUnavailableError describes account availability without disclosing credentials.
// KeyMissing means accounts were skipped because they lack the requested key
// name; an empty Reason means no other account state explains the failure.
type PoolUnavailableError struct {
	Reason  Failure
	RetryAt time.Time
}

func (*PoolUnavailableError) Error() string { return "integration pool has no available accounts" }
func (*PoolUnavailableError) Unwrap() error {
	return apperror.NewBusinessRule("INTEGRATION_POOL_UNAVAILABLE", "Нет доступных аккаунтов интеграции")
}
