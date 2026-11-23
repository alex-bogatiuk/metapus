package ingestionmonitor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

func TestHistoryKeepsRPCFailureEvidenceWithoutTransportSecrets(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenHistory(directory, "tenant")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	snapshot := crypto_worker.IngestionMonitorSnapshot{IngestionPreflightResult: crypto_worker.IngestionPreflightResult{ObservedAt: time.Now().UTC(), Networks: []crypto_worker.IngestionNetworkReport{{
		NetworkCode: "SEPOLIA", State: "degraded", Reason: "ingestion RPC coverage unavailable", RPCErrorCode: "timeout", ElapsedMS: 25001,
		RPCOperations: []crypto.IngestionOperationStatus{{ProviderID: "one", Operation: "full_block", LastError: "timeout"}, {ProviderID: "two", Operation: "receipt", LastError: "https://rpc.example/secret-key"}},
	}}}}
	require.NoError(t, store.Append(snapshot, 1))
	files, err := filepath.Glob(filepath.Join(directory, "samples-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret-key")
	var recorded struct {
		Networks []struct {
			RPCErrorCode  string                            `json:"rpcErrorCode"`
			ElapsedMS     int64                             `json:"elapsedMs"`
			RPCOperations []crypto.IngestionOperationStatus `json:"rpcOperations"`
		} `json:"networks"`
	}
	require.NoError(t, json.Unmarshal(data, &recorded))
	require.Equal(t, "timeout", recorded.Networks[0].RPCErrorCode)
	require.Equal(t, int64(25001), recorded.Networks[0].ElapsedMS)
	require.Equal(t, "timeout", recorded.Networks[0].RPCOperations[0].LastError)
	require.Equal(t, "unknown", recorded.Networks[0].RPCOperations[1].LastError)
	require.Equal(t, "https://rpc.example/secret-key", snapshot.Networks[0].RPCOperations[1].LastError, "persistence must not mutate its input")
}

func TestHistoryPreservesPendingTransitionsAndRejectsConcurrentOwnerOrWrongTenant(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenHistory(dir, "tenant-a")
	require.NoError(t, err)
	_, err = OpenHistory(dir, "tenant-a")
	require.Error(t, err)
	state := cryptoapp.IngestionAlertState{StartedAt: time.Now().UTC(), Pending: []crypto.IngestionAlertTransition{{ID: id.New().String(), At: time.Now().UTC(), Condition: crypto.IngestionCondition{Scope: "network", Severity: "error", Reason: "rpc_coverage"}}}}
	require.NoError(t, store.Save(state))
	require.NoError(t, store.Close())
	store, err = OpenHistory(dir, "tenant-a")
	require.NoError(t, err)
	loaded, err := store.Load()
	require.NoError(t, err)
	require.Equal(t, state.Pending[0].ID, loaded.Pending[0].ID)
	require.NoError(t, store.Close())
	store, err = OpenHistory(dir, "tenant-b")
	require.NoError(t, err)
	_, err = store.Load()
	require.ErrorContains(t, err, "tenant mismatch")
	require.NoError(t, store.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"broken":`), 0600))
	store, err = OpenHistory(dir, "tenant-a")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Load()
	require.Error(t, err, "corrupt state must not silently reset deduplication")
}

func TestHistoryRetentionNeverDeletesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenHistory(dir, "tenant")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, "samples-2020010"+string(rune('0'+i))+"T000000Z.jsonl")
		require.NoError(t, os.WriteFile(path, []byte("expired"), 0600))
		old := time.Now().Add(-8 * 24 * time.Hour)
		require.NoError(t, os.Chtimes(path, old, old))
	}
	unrelated := filepath.Join(dir, "keep.txt")
	require.NoError(t, os.WriteFile(unrelated, []byte("keep"), 0600))
	snapshot := crypto_worker.IngestionMonitorSnapshot{IngestionPreflightResult: crypto_worker.IngestionPreflightResult{ObservedAt: time.Now().UTC(), Ready: true}}
	require.NoError(t, store.Append(snapshot, 0))
	files, err := filepath.Glob(filepath.Join(dir, "samples-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	_, err = os.Stat(unrelated)
	require.NoError(t, err)
}

func TestHistoryRotationCapsSegmentsAndBytes(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenHistory(dir, "tenant")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	for i := 0; i < historyFileCount+2; i++ {
		path := filepath.Join(dir, fmt.Sprintf("samples-%sT%09d.jsonl", now.Format("20060102"), i))
		file, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, file.Truncate(historyFileBytes))
		require.NoError(t, file.Close())
	}
	snapshot := crypto_worker.IngestionMonitorSnapshot{IngestionPreflightResult: crypto_worker.IngestionPreflightResult{ObservedAt: now, Ready: true}}
	require.NoError(t, store.Append(snapshot, 0))
	files, err := filepath.Glob(filepath.Join(dir, "samples-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, historyFileCount)
	var total int64
	for _, path := range files {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.LessOrEqual(t, info.Size(), int64(historyFileBytes))
		total += info.Size()
	}
	require.LessOrEqual(t, total, int64(historyFileCount*historyFileBytes))
}

type historyTestObserver struct{}

func (historyTestObserver) Read(context.Context) (crypto_worker.IngestionMonitorSnapshot, error) {
	return crypto_worker.IngestionMonitorSnapshot{IngestionPreflightResult: crypto_worker.IngestionPreflightResult{ObservedAt: time.Now().UTC(), Ready: true, Networks: []crypto_worker.IngestionNetworkReport{{NetworkCode: "SEPOLIA", State: "ready", Head: &crypto.IngestionHead{ObservedAt: time.Now().UTC()}}}}}, nil
}

func TestRuntimePersistsBeforeDeliveryAndRetriesSameEventID(t *testing.T) {
	store, err := OpenHistory(t.TempDir(), "tenant")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	event := crypto.IngestionAlertTransition{ID: id.New().String(), At: time.Now().UTC(), Condition: crypto.IngestionCondition{Scope: "network", Severity: "error", Reason: "rpc_coverage"}}
	state := cryptoapp.IngestionAlertState{StartedAt: time.Now().UTC(), Pending: []crypto.IngestionAlertTransition{event}}
	failed := true
	calls := 0
	runtime := Runtime{Observer: historyTestObserver{}, History: store, Policy: cryptoapp.DefaultIngestionAlertPolicy(), SampleTimeout: time.Second, Metrics: crypto_worker.NewIngestionMonitorMetrics()}
	runtime.Deliver = func(_ context.Context, got crypto.IngestionAlertTransition) error {
		calls++
		saved, err := store.Load()
		require.NoError(t, err)
		require.Equal(t, event.ID, saved.Pending[0].ID)
		require.Equal(t, event.ID, got.ID)
		if failed {
			return errors.New("delivery unavailable")
		}
		return nil
	}
	require.NoError(t, runtime.sample(t.Context(), &state))
	require.Len(t, state.Pending, 1)
	failed = false
	loaded, err := store.Load()
	require.NoError(t, err)
	require.NoError(t, runtime.sample(t.Context(), &loaded))
	require.Empty(t, loaded.Pending)
	require.Equal(t, 2, calls)
	saved, err := store.Load()
	require.NoError(t, err)
	require.Empty(t, saved.Pending)
}
