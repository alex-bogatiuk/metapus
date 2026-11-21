package ingestionmonitor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/rpcpolicy"
	"metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"metapus/internal/core/json"
)

const historyFileBytes = 8 << 20
const historyFileCount = 8

type HistoryStore struct {
	dir, tenant string
	lock        *os.File
}

type persistedState struct {
	Version int                           `json:"version"`
	Tenant  string                        `json:"tenant"`
	Alerts  cryptoapp.IngestionAlertState `json:"alerts"`
}

func OpenHistory(dir, tenant string) (*HistoryStore, error) {
	if dir == "" || tenant == "" {
		return nil, errors.New("monitor history directory and tenant are required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := lockHistory(filepath.Join(dir, "monitor.lock"))
	if err != nil {
		return nil, err
	}
	return &HistoryStore{dir: dir, tenant: tenant, lock: lock}, nil
}

func (s *HistoryStore) Close() error { return s.lock.Close() }

func (s *HistoryStore) Load() (cryptoapp.IngestionAlertState, error) {
	var state persistedState
	data, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state.Alerts, nil
	}
	if err != nil {
		return state.Alerts, err
	}
	if len(data) > 1<<20 {
		return state.Alerts, errors.New("monitor state exceeds size limit")
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return state.Alerts, fmt.Errorf("decode monitor state: %w", err)
	}
	if state.Version != 1 || state.Tenant != s.tenant {
		return state.Alerts, errors.New("monitor state version or tenant mismatch")
	}
	return state.Alerts, nil
}

func (s *HistoryStore) Save(state cryptoapp.IngestionAlertState) error {
	data, err := json.Marshal(persistedState{Version: 1, Tenant: s.tenant, Alerts: state})
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("monitor state exceeds size limit")
	}
	tmp, err := os.CreateTemp(s.dir, ".state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Sync(), tmp.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(s.dir, "state.json")); err != nil {
		return err
	}
	return syncHistoryDir(s.dir)
}

// Append stores operational evidence only, not RPC payloads, credentials,
// payment identifiers or wallet addresses. Retention is seven days subject to
// the hard cap of eight 8-MiB segments (64 MiB per tenant monitor).
func (s *HistoryStore) Append(snapshot crypto_worker.IngestionMonitorSnapshot, activeAlerts int) error {
	type stream struct {
		Key                string `json:"key"`
		State              string `json:"state"`
		Reason             string `json:"reason,omitempty"`
		Height             int64  `json:"height"`
		LagBlocks          int64  `json:"lagBlocks"`
		LagSeconds         int64  `json:"lagSeconds"`
		ProgressAgeSeconds int64  `json:"progressAgeSeconds"`
		LeaseActive        bool   `json:"leaseActive"`
	}
	type network struct {
		Code          string                            `json:"code"`
		State         string                            `json:"state"`
		HeadHeight    int64                             `json:"headHeight"`
		RPCCoverage   bool                              `json:"rpcCoverage"`
		Streams       []stream                          `json:"streams"`
		Reason        string                            `json:"reason,omitempty"`
		RPCErrorCode  string                            `json:"rpcErrorCode,omitempty"`
		ElapsedMS     int64                             `json:"elapsedMs"`
		RPCOperations []crypto.IngestionOperationStatus `json:"rpcOperations,omitempty"`
	}
	compact := struct {
		AlertSettings map[string]rpcconfig.IngestionAlertSettings `json:"alertSettings,omitempty"`
		At            time.Time                                   `json:"at"`
		Ready         bool                                        `json:"ready"`
		ReadError     bool                                        `json:"readError"`
		ActiveAlerts  int                                         `json:"activeAlerts"`
		Networks      []network                                   `json:"networks"`
		Inbox         map[string]crypto.IngestionInboxBacklog     `json:"inbox"`
	}{AlertSettings: snapshot.AlertSettings, At: snapshot.ObservedAt, Ready: snapshot.Ready, ReadError: snapshot.ReadError, ActiveAlerts: activeAlerts, Inbox: snapshot.Inbox}
	for _, n := range snapshot.Networks {
		operations := make([]crypto.IngestionOperationStatus, len(n.RPCOperations))
		copy(operations, n.RPCOperations)
		for i := range operations {
			operations[i].LastError = rpcpolicy.SafeReason(operations[i].LastError)
		}
		item := network{Code: n.NetworkCode, State: n.State, HeadHeight: -1, RPCCoverage: n.Head != nil,
			Reason: n.Reason, RPCErrorCode: rpcpolicy.SafeReason(n.RPCErrorCode), ElapsedMS: n.ElapsedMS,
			RPCOperations: operations}
		if n.Head != nil {
			item.HeadHeight = n.Head.Height
		}
		for _, v := range n.Streams {
			height := int64(-1)
			if v.Checkpoint != nil {
				height = v.Checkpoint.LastBlock
			}
			item.Streams = append(item.Streams, stream{Key: v.WatcherKey, State: v.State, Reason: v.Reason, Height: height, LagBlocks: v.LagBlocks, LagSeconds: v.LagSeconds, ProgressAgeSeconds: v.ProgressAgeSeconds, LeaseActive: v.LeaseActive})
		}
		compact.Networks = append(compact.Networks, item)
	}
	data, err := json.Marshal(compact)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return errors.New("monitor sample exceeds size limit")
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "samples-*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	path := ""
	if len(files) > 0 {
		latest := files[len(files)-1]
		info, err := os.Stat(latest)
		if err != nil {
			return err
		}
		if info.Size()+int64(len(data)+1) <= historyFileBytes && strings.HasPrefix(filepath.Base(latest), "samples-"+snapshot.ObservedAt.UTC().Format("20060102")) {
			path = latest
		}
	}
	if path == "" {
		path = filepath.Join(s.dir, "samples-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".jsonl")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	err = errors.Join(err, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	files, err = filepath.Glob(filepath.Join(s.dir, "samples-*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for index, p := range files {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if index < len(files)-historyFileCount || snapshot.ObservedAt.Sub(info.ModTime()) > 7*24*time.Hour {
			if err = os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}
