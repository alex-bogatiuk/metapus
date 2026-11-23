//go:build integration

package tron_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type shadowTestDeriver struct{}

func (shadowTestDeriver) DeriveAddress(context.Context, crypto.NetworkRef, string) (crypto.WalletDerivation, error) {
	return crypto.WalletDerivation{}, fmt.Errorf("unexpected wallet derivation")
}

func TestShadowWorkerPausesReleasesLeaseAndResumesWithoutRestart(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pg, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pg.Close()
	txm := sdktest.NewTxManager(pg)
	ctx := tenant.WithTxManager(t.Context(), txm)
	// The watcher registers ingestion only for the exact catalog identity of
	// its network, so the reference below mirrors these rows.
	network, nativeToken := id.New(), id.New()
	code := "SW" + id.Compact(network)
	_, err = pg.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,network_identifier,native_token_symbol,native_decimals,chain_family) VALUES($1,$2,'shadow worker',$2,'TRX',6,'tron')`, network, code)
	require.NoError(t, err)
	_, err = pg.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address) VALUES($1,$2,'Native',$3,'TRX',6,'native','')`, nativeToken, "NATIVE-"+code, network)
	require.NoError(t, err)
	blocks := map[int64]*tron.BlockInfo{}
	receipts := map[int64][]tron.TransactionInfo{}
	for height := int64(10); height <= 60; height++ {
		block := &tron.BlockInfo{BlockID: fmt.Sprintf("%064x", height)}
		block.BlockHeader.RawData.Number = height
		block.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", height-1)
		block.BlockHeader.RawData.Timestamp = time.Now().Add(-10*time.Second).UnixMilli() + height
		blocks[height], receipts[height] = block, []tron.TransactionInfo{}
	}
	var head, calls atomic.Int64
	head.Store(14)
	server := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var body struct {
				Number int64 `json:"num"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&body)
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "getnowblock"):
				height := head.Load()
				if strings.HasPrefix(r.URL.Path, "/wallet/") {
					height += 20
				}
				_ = json.NewEncoder(w).Encode(blocks[height])
			case strings.HasSuffix(r.URL.Path, "getblockbynum"):
				_ = json.NewEncoder(w).Encode(blocks[body.Number])
			case strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum"):
				_ = json.NewEncoder(w).Encode(receipts[body.Number])
			default:
				http.NotFound(w, r)
			}
		}))
	}
	a, b := server(), server()
	defer a.Close()
	defer b.Close()
	endpoints := []chainconfig.Endpoint{{ProviderID: "one", TrustGroup: "one", URL: a.URL}, {ProviderID: "two", TrustGroup: "two", URL: b.URL}}
	rpc, err := tron.NewRPCPool(tron.RPCPoolConfig{NetworkCode: "TRON-NILE", FullNode: endpoints, Solidity: endpoints, Settings: chainconfig.TRONSettings{IngestionMode: "shadow", RequestsPerSecond: 1000}})
	require.NoError(t, err)
	defer rpc.Close()
	repo := crypto_repo.NewBlockIngestionRepo()
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: 10, BlockHash: blocks[10].BlockID, Timestamp: blocks[10].BlockHeader.RawData.Timestamp}
	_, err = repo.EnsureCutover(ctx, crypto.NetworkRef{ID: network}, baseline, "shadow", nil)
	require.NoError(t, err)
	ref := crypto.NetworkRef{ID: network, Code: code, Family: crypto.ChainFamilyTRON, NetworkIdentifier: code, NativeTokenID: nativeToken, NativeDecimals: 6, RequiredConfirmations: 20, FinalityMode: crypto.FinalityByConfirmations}
	driver, err := tron.NewDriver(tron.DriverConfig{Network: ref, RPC: rpc, WatcherStateRepository: crypto_repo.NewWatcherStateRepo(), BlockCaptureRepository: repo, WalletDeriver: shadowTestDeriver{}, MaxFeeMinor: types.NewRawAmountFromInt64(30000000)})
	require.NoError(t, err)
	watchers, err := driver.NetworkWatchers(ctx, ref, nil, nil)
	require.NoError(t, err)
	require.Len(t, watchers, 1)
	w := watchers[0]
	work, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Start(work, nil) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watcher failed to drain")
		}
	}()
	control := crypto_repo.NewShadowControlRepo()
	read := func() crypto.ShadowControlState {
		s, err := control.GetShadowControl(ctx, network)
		require.NoError(t, err)
		return s
	}
	require.Eventually(t, func() bool { s := read(); return s.CheckpointHeight != nil && *s.CheckpointHeight == 14 }, 10*time.Second, 50*time.Millisecond)
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error { _, err := control.SetShadowPaused(ctx, network, 1, true); return err })
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !read().LeaseActive }, 10*time.Second, 50*time.Millisecond)
	paused := read()
	require.Equal(t, int64(14), *paused.CheckpointHeight)
	head.Store(18)
	before := calls.Load()
	time.Sleep(2500 * time.Millisecond)
	require.Equal(t, before, calls.Load(), "paused worker must not poll RPC")
	require.Equal(t, int64(14), *read().CheckpointHeight)
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error { _, err := control.SetShadowPaused(ctx, network, 2, false); return err })
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		s := read()
		return s.CheckpointHeight != nil && *s.CheckpointHeight == 18 && s.LeaseActive
	}, 10*time.Second, 50*time.Millisecond)
	var count int
	require.NoError(t, pg.QueryRow(ctx, `SELECT count(*) FROM sys_chain_watcher_block_history WHERE network_id=$1 AND watcher_key='tron:shadow' AND block_number BETWEEN 11 AND 18`, network).Scan(&count))
	require.Equal(t, 8, count, "resume must preserve sequential history without skipping blocks")
	var financial int
	require.NoError(t, pg.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&financial))
	require.Zero(t, financial)
}
