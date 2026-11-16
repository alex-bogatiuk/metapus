package rpcconfig

import (
	"fmt"
	"time"
)

// TRONSettings bounds block and receipt reads independently of EVM prefetch.
type TRONSettings struct {
	IngestionMode       string `json:"ingestionMode,omitempty"`
	AttemptTimeoutMS    int    `json:"attemptTimeoutMs,omitempty"`
	ReadBudgetMS        int    `json:"readBudgetMs,omitempty"`
	MaxAttempts         int    `json:"maxAttempts,omitempty"`
	ProviderConcurrency int    `json:"providerConcurrency,omitempty"`
	BackfillConcurrency int    `json:"backfillConcurrency,omitempty"`
	RequestsPerSecond   int    `json:"requestsPerSecond,omitempty"`
	MaxResponseBytes    int64  `json:"maxResponseBytes,omitempty"`
}

func (s TRONSettings) Defaults() TRONSettings {
	if s.IngestionMode == "" {
		s.IngestionMode = "hybrid"
	}
	if s.AttemptTimeoutMS == 0 {
		s.AttemptTimeoutMS = 5000
	}
	if s.ReadBudgetMS == 0 {
		s.ReadBudgetMS = 12000
	}
	if s.MaxAttempts == 0 {
		s.MaxAttempts = 2
	}
	if s.ProviderConcurrency == 0 {
		s.ProviderConcurrency = 8
	}
	if s.BackfillConcurrency == 0 {
		s.BackfillConcurrency = 4
	}
	if s.RequestsPerSecond == 0 {
		s.RequestsPerSecond = 10
	}
	if s.MaxResponseBytes == 0 {
		s.MaxResponseBytes = 10 << 20
	}
	return s
}
func (s TRONSettings) Validate() error {
	s = s.Defaults()
	switch s.IngestionMode {
	case "shadow", "solidified", "hybrid", "paused":
	default:
		return fmt.Errorf("invalid TRON ingestion mode")
	}
	if s.AttemptTimeoutMS < 1 || s.AttemptTimeoutMS > 30000 || s.ReadBudgetMS < s.AttemptTimeoutMS || s.ReadBudgetMS > 60000 {
		return fmt.Errorf("invalid TRON read timeouts/budget")
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 8 || s.ProviderConcurrency < 2 || s.ProviderConcurrency > 64 || s.BackfillConcurrency < 1 || s.BackfillConcurrency >= s.ProviderConcurrency || s.RequestsPerSecond < 1 || s.RequestsPerSecond > 1000 || s.MaxResponseBytes < 1024 || s.MaxResponseBytes > 64<<20 {
		return fmt.Errorf("invalid TRON admission/response limits")
	}
	return nil
}
func (s TRONSettings) AttemptTimeout() time.Duration {
	return time.Duration(s.Defaults().AttemptTimeoutMS) * time.Millisecond
}
func (s TRONSettings) ReadBudget() time.Duration {
	return time.Duration(s.Defaults().ReadBudgetMS) * time.Millisecond
}
