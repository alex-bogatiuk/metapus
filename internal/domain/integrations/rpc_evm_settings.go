package integrations

import (
	"fmt"
	"time"
)

// EVMSettings contains read-only ingestion limits. Circuit thresholds remain
// tenant network policy; endpoint admission is shared by every pool in a process.
type EVMSettings struct {
	MaxHeadAgeSeconds   int   `json:"maxHeadAgeSeconds,omitempty"`
	AttemptTimeoutMS    int   `json:"attemptTimeoutMs,omitempty"`
	ReadBudgetMS        int   `json:"readBudgetMs,omitempty"`
	LogsTimeoutMS       int   `json:"logsTimeoutMs,omitempty"`
	MaxAttempts         int   `json:"maxAttempts,omitempty"`
	ProviderConcurrency int   `json:"providerConcurrency,omitempty"`
	BackfillConcurrency int   `json:"backfillConcurrency,omitempty"`
	RequestsPerSecond   int   `json:"requestsPerSecond,omitempty"`
	NativeConcurrency   int   `json:"nativeConcurrency,omitempty"`
	PrefetchBlocks      int   `json:"prefetchBlocks,omitempty"`
	MaxResponseBytes    int64 `json:"maxResponseBytes,omitempty"`
	MaxBufferedBytes    int64 `json:"maxBufferedBytes,omitempty"`
}

func (s EVMSettings) Defaults() EVMSettings {
	if s.MaxHeadAgeSeconds == 0 {
		s.MaxHeadAgeSeconds = 30
	}
	if s.AttemptTimeoutMS == 0 {
		s.AttemptTimeoutMS = 2000
	}
	if s.ReadBudgetMS == 0 {
		s.ReadBudgetMS = 5000
	}
	if s.LogsTimeoutMS == 0 {
		s.LogsTimeoutMS = 5000
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
		s.RequestsPerSecond = 20
	}
	if s.NativeConcurrency == 0 {
		s.NativeConcurrency = 4
	}
	if s.PrefetchBlocks == 0 {
		s.PrefetchBlocks = 32
	}
	if s.MaxResponseBytes == 0 {
		s.MaxResponseBytes = 8 << 20
	}
	if s.MaxBufferedBytes == 0 {
		s.MaxBufferedBytes = 256 << 20
	}
	return s
}

func (s EVMSettings) Validate() error {
	s = s.Defaults()
	if s.MaxHeadAgeSeconds < 5 || s.MaxHeadAgeSeconds > 600 {
		return fmt.Errorf("invalid EVM head freshness limit")
	}
	if s.AttemptTimeoutMS < 1 || s.AttemptTimeoutMS > 30000 || s.ReadBudgetMS < s.AttemptTimeoutMS || s.ReadBudgetMS > 60000 || s.LogsTimeoutMS < 1 || s.LogsTimeoutMS > 30000 {
		return fmt.Errorf("invalid EVM read timeouts/budget")
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 8 || s.ProviderConcurrency < 2 || s.ProviderConcurrency > 64 || s.BackfillConcurrency < 1 || s.BackfillConcurrency >= s.ProviderConcurrency || s.RequestsPerSecond < 1 || s.RequestsPerSecond > 1000 {
		return fmt.Errorf("invalid EVM provider admission limits")
	}
	if s.NativeConcurrency < 1 || s.NativeConcurrency > 16 || s.PrefetchBlocks < s.NativeConcurrency || s.PrefetchBlocks > 128 || s.MaxResponseBytes < 1024 || s.MaxResponseBytes > 64<<20 || s.MaxBufferedBytes < s.MaxResponseBytes*int64(s.NativeConcurrency) || s.MaxBufferedBytes > 512<<20 {
		return fmt.Errorf("invalid EVM prefetch/memory limits")
	}
	return nil
}

func (s EVMSettings) AttemptTimeout() time.Duration {
	return time.Duration(s.Defaults().AttemptTimeoutMS) * time.Millisecond
}
func (s EVMSettings) ReadBudget() time.Duration {
	return time.Duration(s.Defaults().ReadBudgetMS) * time.Millisecond
}

// Window bounds retained results by the maximum accepted response size, even
// when earlier heights are slow. It never waits for a later height's memory.
func (s EVMSettings) Window() int {
	s = s.Defaults()
	return min(s.PrefetchBlocks, int(s.MaxBufferedBytes/s.MaxResponseBytes))
}
