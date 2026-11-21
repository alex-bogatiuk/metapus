package tron

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

const (
	rotationSolidHead  = 30
	rotationLatestHead = 32
)

// chainBlockServer serves an empty-block chain for both TRON roles: the
// solidity head is rotationSolidHead and the full-node head rotationLatestHead.
// salt changes every block ID, making the server disagree with the others.
// It counts solidified block reads by height.
type chainBlockServer struct {
	*httptest.Server
	mu    sync.Mutex
	reads map[int64]int
}

func newChainBlockServer(t *testing.T, salt int64) *chainBlockServer {
	t.Helper()
	s := &chainBlockServer{reads: map[int64]int{}}
	block := func(height int64) BlockInfo {
		b := BlockInfo{BlockID: fmt.Sprintf("%064x", height+salt)}
		b.BlockHeader.RawData.Number = height
		b.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", height-1+salt)
		b.BlockHeader.RawData.Timestamp = fixtureEpoch + height
		return b
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		solidity := strings.HasPrefix(r.URL.Path, "/walletsolidity/")
		var body struct {
			Num int64 `json:"num"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "getnowblock"):
			head := int64(rotationLatestHead)
			if solidity {
				head = rotationSolidHead
			}
			_ = json.NewEncoder(w).Encode(block(head))
		case strings.HasSuffix(r.URL.Path, "getblockbynum"):
			if solidity {
				s.mu.Lock()
				s.reads[body.Num]++
				s.mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(block(body.Num))
		case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *chainBlockServer) reset() {
	s.mu.Lock()
	s.reads = map[int64]int{}
	s.mu.Unlock()
}

func (s *chainBlockServer) heights() map[int64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make(map[int64]int, len(s.reads))
	for height, count := range s.reads {
		copied[height] = count
	}
	return copied
}

// fastRotationPool is newTestRPCPool with a request rate that keeps these
// multi-height tests short: package fixtures age with wall-clock time.
func fastRotationPool(t *testing.T, urls []string) *RPCPool {
	t.Helper()
	endpoints := func(prefix string) []chainconfig.Endpoint {
		result := make([]chainconfig.Endpoint, 0, len(urls))
		for index, url := range urls {
			result = append(result, chainconfig.Endpoint{ProviderID: prefix + "-" + strconv.Itoa(index+1), TrustGroup: "source-" + strconv.Itoa(index+1), Priority: index, URL: url})
		}
		return result
	}
	pool, err := NewRPCPool(RPCPoolConfig{NetworkCode: "TRON-NILE", FailureThreshold: 2, OpenDuration: time.Minute,
		FullNode: endpoints("fullnode"), Solidity: endpoints("solidity"), Settings: chainconfig.TRONSettings{RequestsPerSecond: 1000, IngestionMode: "hybrid"}})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func rotationPool(t *testing.T, servers ...*chainBlockServer) (*rpcGeneration, func()) {
	t.Helper()
	urls := make([]string, 0, len(servers))
	for _, s := range servers {
		urls = append(urls, s.URL)
	}
	gen, release, err := fastRotationPool(t, urls).runtime.Acquire(t.Context())
	require.NoError(t, err)
	return gen, release
}

func TestCatchUpWindowRotatesHeightsOverEveryPairOfThreeSources(t *testing.T) {
	servers := []*chainBlockServer{newChainBlockServer(t, 0), newChainBlockServer(t, 0), newChainBlockServer(t, 0)}
	gen, release := rotationPool(t, servers...)
	defer release()
	window, err := gen.prepareBlockWindow(t.Context())
	require.NoError(t, err)
	window = gen.extendBlockWindow(t.Context(), window)
	require.Equal(t, 3, window.members())
	require.Len(t, window.pairs, 3)
	for _, s := range servers {
		s.reset()
	}

	for height := int64(1); height <= 12; height++ {
		_, sources, err := gen.readAgreedWindowBlock(t.Context(), height, window)
		require.NoError(t, err)
		require.Len(t, sources, 2)
		require.NotEqual(t, sources[0].TrustGroup, sources[1].TrustGroup)
		require.Equal(t, int64(rotationLatestHead), sources[0].HeadHeight)
		require.Equal(t, sources[0].HeadHash, sources[1].HeadHash, "every pair keeps the window's agreed head")
		require.Equal(t, sources[1].TrustGroup, strings.Replace(sources[1].ProviderID, "solidity-", "source-", 1))
	}
	for height := int64(1); height <= 12; height++ {
		readers := 0
		for _, s := range servers {
			readers += s.heights()[height]
		}
		require.Equal(t, 2, readers, "height %d is read by exactly two sources", height)
	}
	for i, s := range servers {
		require.Len(t, s.heights(), 8, "source %d serves two thirds of the heights", i)
	}
}

func TestCatchUpWindowLeavesOutDisagreeingOrDependentSources(t *testing.T) {
	for _, scenario := range []string{"disagrees", "same_group", "shadow"} {
		t.Run(scenario, func(t *testing.T) {
			salt := int64(0)
			if scenario == "disagrees" {
				salt = 1000
			}
			servers := []*chainBlockServer{newChainBlockServer(t, 0), newChainBlockServer(t, 0), newChainBlockServer(t, salt)}
			gen, release := rotationPool(t, servers...)
			defer release()
			if scenario == "same_group" {
				for _, endpoints := range gen.byRole {
					endpoints[2].trustGroup = endpoints[0].trustGroup
				}
			}
			window, err := gen.prepareBlockWindow(t.Context())
			require.NoError(t, err)
			if scenario == "shadow" {
				gen.settings.IngestionMode = "shadow"
			}
			window = gen.extendBlockWindow(t.Context(), window)
			require.Empty(t, window.pairs)
			require.Equal(t, 2, window.members())
			servers[2].reset()
			for height := int64(1); height <= 3; height++ {
				_, sources, err := gen.readAgreedWindowBlock(t.Context(), height, window)
				require.NoError(t, err)
				require.Len(t, sources, 2)
			}
			require.Empty(t, servers[2].heights(), "a source outside the window reads no blocks")
		})
	}
}

func TestSolidifiedScannerCatchUpSpreadsEvidenceOverThreeSources(t *testing.T) {
	blocks := map[int64]*BlockInfo{}
	receipts := map[int64][]TransactionInfo{}
	const last, head = int64(9), int64(30)
	for height := last; height <= head; height++ {
		blocks[height], receipts[height] = blockFixture(t, height, false)
	}
	server := func() *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request struct {
				Number int64 `json:"num"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			switch {
			case strings.HasSuffix(r.URL.Path, "getnowblock"):
				_ = json.NewEncoder(w).Encode(blocks[head])
			case strings.HasSuffix(r.URL.Path, "getblockbynum"):
				_ = json.NewEncoder(w).Encode(blocks[request.Number])
			case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
				_ = json.NewEncoder(w).Encode(receipts[request.Number])
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(s.Close)
		return s
	}
	urls := []string{server().URL, server().URL, server().URL}
	pool := fastRotationPool(t, urls)
	repo := &captureRepoStub{}
	driver := &Driver{rpc: pool, network: crypto.NetworkRef{ID: id.New()}, blockRepo: repo}
	state := &crypto.WatcherState{NetworkID: driver.network.ID, WatcherKey: "tron:solidified", LastBlock: last, Fingerprint: blocks[last].BlockID}
	watcher := &blockWatcher{driver: driver, key: "tron:solidified", lastAudit: time.Now()}

	require.NoError(t, watcher.pollSolidified(t.Context(), state))
	require.Equal(t, last+int64(pool.settings.Defaults().SolidifiedCatchUpBlocks), state.LastBlock)
	served := map[string]int{}
	for i, block := range repo.blocks {
		require.Equal(t, last+int64(i)+1, block.Commit.Block.BlockNumber, "blocks are captured in height order")
		require.Len(t, block.Sources, 2)
		require.NotEqual(t, block.Sources[0].TrustGroup, block.Sources[1].TrustGroup)
		for _, source := range block.Sources {
			served[source.TrustGroup]++
		}
	}
	require.Len(t, served, 3, "all three sources take part in the window")
	for group, count := range served {
		require.Less(t, count, len(repo.blocks), "source %s does not serve every block", group)
	}
}
