package crypto_repo

// WorkerRuntimeRepo persists worker discovery, reconciliation and queue state.
// Transactions and tenant routing are resolved exclusively from context.
type WorkerRuntimeRepo struct{}

func NewWorkerRuntimeRepo() *WorkerRuntimeRepo { return &WorkerRuntimeRepo{} }
