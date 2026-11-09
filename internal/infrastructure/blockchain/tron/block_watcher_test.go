package tron

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

type captureRepoStub struct {
	blocks []crypto.CapturedBlock
	fail   error
}

func (r *captureRepoStub) Cutover(context.Context, id.ID) (crypto.IngestionCutover, error) {
	return crypto.IngestionCutover{}, nil
}
func (r *captureRepoStub) EnsureCutover(context.Context, crypto.NetworkRef, crypto.WatcherBlockFingerprint, string, []crypto.AssetRef) (crypto.IngestionCutover, error) {
	return crypto.IngestionCutover{}, nil
}
func (r *captureRepoStub) SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error) {
	return id.New(), nil
}
func (r *captureRepoStub) CaptureBlock(_ context.Context, b crypto.CapturedBlock) error {
	if r.fail != nil {
		return r.fail
	}
	r.blocks = append(r.blocks, b)
	return nil
}

func TestSequentialScannerStopsAtMissingReceiptWhileLatestAdvances(t *testing.T) {
	blocks := map[int64]*BlockInfo{}
	receipts := map[int64][]TransactionInfo{}
	for height := int64(9); height <= 13; height++ {
		blocks[height], receipts[height] = blockFixture(t, height, false)
	}
	// Hex fingerprints represent bytes; casing cannot create a false hold.
	blocks[9].BlockID = "a" + blocks[9].BlockID[1:]
	blocks[10].BlockHeader.RawData.ParentHash = blocks[9].BlockID
	var missing atomic.Bool
	var headCalls atomic.Int32
	var receiptCalls atomic.Int32
	missing.Store(true)
	server := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Number int64 `json:"num"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&request)
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "getnowblock"):
				headCalls.Add(1)
				_ = json.NewEncoder(w).Encode(blocks[13])
			case strings.HasSuffix(r.URL.Path, "getblockbynum"):
				_ = json.NewEncoder(w).Encode(blocks[request.Number])
			case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
				receiptCalls.Add(1)
				if request.Number == 10 && missing.Load() {
					_ = json.NewEncoder(w).Encode([]TransactionInfo{})
					return
				}
				_ = json.NewEncoder(w).Encode(receipts[request.Number])
			default:
				http.NotFound(w, r)
			}
		}))
	}
	first, second := server(), server()
	defer first.Close()
	defer second.Close()
	pool := newTestRPCPool(t, []string{first.URL, second.URL}, []string{first.URL, second.URL})
	pool.settings.IngestionMode = "hybrid"
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for i, e := range pool.byRole[role] {
			e.trustGroup = fmt.Sprint(i)
		}
	}
	repo := &captureRepoStub{}
	driver := &Driver{rpc: pool, network: crypto.NetworkRef{ID: id.New()}, blockRepo: repo}
	state := &crypto.WatcherState{NetworkID: driver.network.ID, WatcherKey: "tron:solidified", LastBlock: 9, Fingerprint: strings.ToUpper(blocks[9].BlockID)}
	watcher := &blockWatcher{driver: driver, key: "tron:solidified"}
	err := watcher.pollSolidified(t.Context(), state)
	require.Error(t, err)
	require.Equal(t, int64(9), state.LastBlock)
	require.Empty(t, repo.blocks)
	require.Len(t, watcher.prefetched, 3, "later agreed blocks survive an earlier missing receipt")
	latestState := *state
	latestState.WatcherKey = "tron:latest"
	latest := &blockWatcher{driver: driver, key: "tron:latest", latest: true, cutover: crypto.IngestionCutover{Height: 9}}
	pending, completed := map[int64]time.Time{}, map[int64]string{}
	seeded := false
	require.Error(t, latest.pollLatest(t.Context(), &latestState, pending, completed, &seeded))
	require.Equal(t, int64(13), latestState.LastBlock)
	require.Contains(t, pending, int64(10))
	require.NotContains(t, completed, int64(10))
	for _, capture := range repo.blocks {
		require.Empty(t, capture.Sources)
		require.Equal(t, crypto.FinalityProvisional, capture.Events[0].Finality)
	}
	repo.blocks = nil
	missing.Store(false)
	headCalls.Store(0)
	receiptCalls.Store(0)
	watcher.lastAudit = time.Now()
	require.NoError(t, watcher.pollSolidified(t.Context(), state))
	require.EqualValues(t, 4, headCalls.Load(), "four heights share two solidified and two latest head reads")
	require.EqualValues(t, 2, receiptCalls.Load(), "only the missing height is fetched again from both providers")
	require.Empty(t, watcher.prefetched, "committed heights release bounded prefetch memory")
	require.Equal(t, int64(13), state.LastBlock)
	require.Len(t, repo.blocks, 4)
	for i, capture := range repo.blocks {
		require.Equal(t, int64(10+i), capture.Commit.Block.BlockNumber)
		require.Len(t, capture.Sources, 2)
	}
	// A failed database commit also retains the bounded agreed window, while
	// neither checkpoint nor events advance until the commit succeeds.
	state.LastBlock, state.Fingerprint = 9, blocks[9].BlockID
	repo.blocks = nil
	repo.fail = fmt.Errorf("database temporarily unavailable")
	require.ErrorContains(t, watcher.pollSolidified(t.Context(), state), "database temporarily unavailable")
	require.Equal(t, int64(9), state.LastBlock)
	require.Empty(t, repo.blocks)
	require.Len(t, watcher.prefetched, 4)
	repo.fail = nil
	receiptCalls.Store(0)
	require.NoError(t, watcher.pollSolidified(t.Context(), state))
	require.Zero(t, receiptCalls.Load(), "a database retry does not repeat agreed RPC reads")
	require.Len(t, repo.blocks, 4)
	require.Equal(t, int64(13), state.LastBlock)
	require.Empty(t, watcher.prefetched)
	// A restart replays the latest window independently of the watermark.
	seeded = false
	pending = map[int64]time.Time{}
	completed = map[int64]string{}
	require.NoError(t, latest.pollLatest(t.Context(), &latestState, pending, completed, &seeded))
	require.Len(t, completed, 4)
}
