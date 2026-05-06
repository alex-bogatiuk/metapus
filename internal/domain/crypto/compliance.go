package crypto

import "context"

// ComplianceEngine provides AML/CFT screening for addresses and transactions.
// v1: NoopComplianceEngine (always returns low risk).
// Production: Chainalysis / Elliptic / Crystal adapter.
type ComplianceEngine interface {
	// ScreenAddress checks an address against sanctions/watchlists.
	ScreenAddress(ctx context.Context, address string) (RiskScore, error)

	// ScreenTransaction checks a transaction for suspicious patterns.
	ScreenTransaction(ctx context.Context, txHash string) (RiskScore, error)
}

// RiskScore represents the compliance risk assessment result.
type RiskScore struct {
	Score   int    `json:"score"`   // 0–100 (0 = no risk, 100 = maximum risk)
	Level   string `json:"level"`   // "low", "medium", "high", "critical"
	Details string `json:"details"` // human-readable explanation
}

// RiskLevel constants.
const (
	RiskLevelLow      = "low"
	RiskLevelMedium   = "medium"
	RiskLevelHigh     = "high"
	RiskLevelCritical = "critical"
)

// NoopComplianceEngine is a development-only stub that always returns low risk.
// MUST be replaced with a real AML provider before production deployment.
type NoopComplianceEngine struct{}

// NewNoopComplianceEngine creates a new noop compliance engine.
func NewNoopComplianceEngine() *NoopComplianceEngine {
	return &NoopComplianceEngine{}
}

// ScreenAddress implements ComplianceEngine — always returns low risk.
func (e *NoopComplianceEngine) ScreenAddress(_ context.Context, _ string) (RiskScore, error) {
	return RiskScore{Score: 0, Level: RiskLevelLow, Details: "noop: no screening configured"}, nil
}

// ScreenTransaction implements ComplianceEngine — always returns low risk.
func (e *NoopComplianceEngine) ScreenTransaction(_ context.Context, _ string) (RiskScore, error) {
	return RiskScore{Score: 0, Level: RiskLevelLow, Details: "noop: no screening configured"}, nil
}

// Compile-time interface check.
var _ ComplianceEngine = (*NoopComplianceEngine)(nil)
