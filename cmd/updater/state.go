// cmd/updater/state.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Phase represents the current step in the update lifecycle.
type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseChecking   Phase = "checking"
	PhasePulling    Phase = "pulling"
	PhaseStarting   Phase = "starting"
	PhaseHealthWait Phase = "health_wait"
	PhaseSwitching  Phase = "switching"
	PhaseMigrating  Phase = "migrating"
	PhaseDone       Phase = "done"
	PhaseRollback   Phase = "rollback"
	PhaseFailed     Phase = "failed"
)

// UpdateState is the persistent state of the current update operation.
// Written to disk as WAL *before* each phase transition.
type UpdateState struct {
	Phase          Phase     `json:"phase"`
	TargetImage    string    `json:"targetImage,omitempty"`
	TargetTag      string    `json:"targetTag,omitempty"`
	OldContainerID string    `json:"oldContainerId,omitempty"`
	NewContainerID string    `json:"newContainerId,omitempty"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	CompletedAt    time.Time `json:"completedAt,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	Log            []LogEntry `json:"log,omitempty"`

	// PhaseDetail provides a human-readable description of what's happening
	// inside the current phase (e.g. "Скачано 45 MB / 120 MB").
	PhaseDetail string `json:"phaseDetail,omitempty"`

	// PullProgress tracks image download progress (bytes).
	PullCurrent int64 `json:"pullCurrent,omitempty"`
	PullTotal   int64 `json:"pullTotal,omitempty"`
}

// LogEntry is a timestamped log message for the update process.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// StateStore manages persistent state with WAL semantics.
// State is written to disk BEFORE the corresponding action is performed.
type StateStore struct {
	mu       sync.RWMutex
	state    UpdateState
	filePath string
}

// NewStateStore creates a store and loads existing state from disk if present.
func NewStateStore(filePath string) (*StateStore, error) {
	s := &StateStore{
		filePath: filePath,
		state:    UpdateState{Phase: PhaseIdle},
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	// Load existing state
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // Fresh start
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}

	return s, nil
}

// Get returns a copy of the current state (thread-safe).
func (s *StateStore) Get() UpdateState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy log slice
	cp := s.state
	if s.state.Log != nil {
		cp.Log = make([]LogEntry, len(s.state.Log))
		copy(cp.Log, s.state.Log)
	}
	return cp
}

// Transition atomically updates the phase and persists to disk (WAL).
// The state is written BEFORE the caller performs the corresponding action.
func (s *StateStore) Transition(phase Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Phase = phase
	s.appendLogLocked(fmt.Sprintf("→ %s", phase))
	return s.persistLocked()
}

// Update applies a mutation function to the state and persists.
func (s *StateStore) Update(fn func(st *UpdateState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn(&s.state)
	return s.persistLocked()
}

// SetError records an error and transitions to failed phase.
func (s *StateStore) SetError(phase Phase, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Phase = phase
	s.state.LastError = err.Error()
	s.appendLogLocked(fmt.Sprintf("ERROR [%s]: %s", phase, err.Error()))
	return s.persistLocked()
}

// AppendLog adds a timestamped log entry.
func (s *StateStore) AppendLog(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.appendLogLocked(msg)
	// Best-effort persist for log entries (non-critical)
	_ = s.persistLocked()
}

// Reset returns state to idle (after successful completion or manual reset).
func (s *StateStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = UpdateState{Phase: PhaseIdle}
	return s.persistLocked()
}

// NeedsRecovery returns true if the state indicates an interrupted update.
func (s *StateStore) NeedsRecovery() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch s.state.Phase {
	case PhaseIdle, PhaseDone, PhaseFailed:
		return false
	default:
		return true
	}
}

func (s *StateStore) appendLogLocked(msg string) {
	s.state.Log = append(s.state.Log, LogEntry{
		Time:    time.Now(),
		Message: msg,
	})
	// Cap log at 200 entries to prevent unbounded growth
	if len(s.state.Log) > 200 {
		s.state.Log = s.state.Log[len(s.state.Log)-200:]
	}
}

func (s *StateStore) persistLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		return fmt.Errorf("rename state file: %w", err)
	}
	return nil
}
