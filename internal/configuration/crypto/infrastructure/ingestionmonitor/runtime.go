package ingestionmonitor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"metapus/internal/core/json"
	"metapus/pkg/logger"
)

type Observer interface {
	Read(context.Context) (crypto_worker.IngestionMonitorSnapshot, error)
}
type Runtime struct {
	Observer                Observer
	History                 *HistoryStore
	Deliver                 func(context.Context, crypto.IngestionAlertTransition) error
	Policy                  cryptoapp.IngestionAlertPolicy
	Interval, SampleTimeout time.Duration
	Address                 string
	Metrics                 crypto_worker.IngestionMonitorMetrics
	mu                      sync.RWMutex
	latest                  crypto_worker.IngestionMonitorSnapshot
	completedAt             time.Time
	deliveryOK              bool
}

func (r *Runtime) Run(ctx context.Context) error {
	state, err := r.History.Load()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", r.Address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: r.handler(), ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = server.Close()
		<-serverDone
	}()
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		if err := r.sample(ctx, &state); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-serverDone:
			serverDone <- err
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runtime) sample(ctx context.Context, state *cryptoapp.IngestionAlertState) error {
	sampleCtx, cancel := context.WithTimeout(ctx, r.SampleTimeout)
	snapshot, readErr := r.Observer.Read(sampleCtx)
	cancel()
	if ctx.Err() != nil {
		return nil
	}
	if snapshot.ObservedAt.IsZero() {
		snapshot.ObservedAt = time.Now().UTC()
	}
	if readErr != nil {
		logger.Warn(ctx, "ingestion monitor read incomplete")
	}
	policy := r.Policy
	policy.NetworkSettings = snapshot.AlertSettings
	cryptoapp.AdvanceIngestionIncidents(state, time.Now().UTC(), crypto_worker.IngestionConditions(snapshot), policy)
	// Persist event IDs before delivery. A committed DB notification can then be
	// replayed safely if the process dies before saving its acknowledgement.
	if err := r.History.Save(*state); err != nil {
		return err
	}
	active := 0
	for _, scope := range state.Scopes {
		if scope.Active != nil {
			active++
		}
	}
	if err := r.History.Append(snapshot, active); err != nil {
		return err
	}
	r.Metrics.Record(ctx, snapshot, active)
	for len(state.Pending) > 0 {
		event := state.Pending[0]
		deliveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := r.Deliver(deliveryCtx, event)
		cancel()
		if err != nil {
			logger.Warn(ctx, "ingestion alert delivery pending")
			break
		}
		state.Pending = state.Pending[1:]
		if err := r.History.Save(*state); err != nil {
			return err
		}
		logger.Info(ctx, "ingestion alert transition delivered", "network_code", event.Condition.NetworkCode, "reason", event.Condition.Reason, "recovered", event.Recovered)
	}
	r.mu.Lock()
	r.latest = snapshot
	r.completedAt = time.Now().UTC()
	r.deliveryOK = len(state.Pending) == 0
	r.mu.Unlock()
	return nil
}

func (r *Runtime) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.RLock()
		ready := !r.completedAt.IsZero() && time.Since(r.completedAt) <= r.Interval+r.SampleTimeout && r.deliveryOK
		r.mu.RUnlock()
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.latest)
	})
	return mux
}
