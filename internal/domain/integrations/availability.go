package integrations

import (
	"time"

	"metapus/internal/core/apperror"
)

// PoolUnavailableError describes account availability without disclosing credentials.
// An empty Reason means no configured account can serve the requested slot.
type PoolUnavailableError struct {
	Reason  Failure
	RetryAt time.Time
}

func (*PoolUnavailableError) Error() string { return "integration pool has no available accounts" }
func (*PoolUnavailableError) Unwrap() error {
	return apperror.NewBusinessRule("INTEGRATION_POOL_UNAVAILABLE", "Нет доступных аккаунтов интеграции")
}
