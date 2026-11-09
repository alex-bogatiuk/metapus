package tron

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

func TestCanonicalPreflightChecksIndependentDataAndPositiveTransfer(t *testing.T) {
	for _, scenario := range []string{"verified", "different_amount", "unknown_note", "no_positive"} {
		t.Run(scenario, func(t *testing.T) {
			servers := make([]*httptest.Server, 0, 3)
			endpoints := make([]chainconfig.Endpoint, 0, 3)
			for i := 0; i < 3; i++ {
				providerIndex := i
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					height := int64(30)
					if !strings.HasSuffix(r.URL.Path, "getnowblock") {
						var request struct {
							Num int64 `json:"num"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
						height = request.Num
					}
					block, receipts := blockFixture(t, height, true)
					trace := traceFixture(t, "call", "a", 23)
					if scenario == "different_amount" && providerIndex == 1 && height == 10 {
						trace.CallValueInfo[0].CallValue++
					}
					if scenario == "unknown_note" && providerIndex == 0 && height == 10 {
						trace.Note = "6e65774f70636f6465"
					}
					if scenario == "no_positive" {
						trace.CallValueInfo[0].CallValue = 0
					}
					receipts[0].InternalTransactions = []InternalTransaction{trace}
					if strings.HasSuffix(r.URL.Path, "gettransactioninfobyblocknum") {
						require.NoError(t, json.NewEncoder(w).Encode(receipts))
					} else {
						require.NoError(t, json.NewEncoder(w).Encode(block))
					}
				}))
				servers = append(servers, server)
				endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: string(rune('a' + i)), TrustGroup: string(rune('a' + i)), URL: server.URL})
			}
			defer func() {
				for _, s := range servers {
					s.Close()
				}
			}()
			pool, err := NewRPCPool(RPCPoolConfig{NetworkCode: "TRON-NILE", FullNode: endpoints, Solidity: endpoints, Settings: chainconfig.TRONSettings{IngestionMode: "shadow", RequestsPerSecond: 1000}})
			require.NoError(t, err)
			defer pool.Close()
			driver := Driver{network: crypto.NetworkRef{ID: id.New(), Code: "TRON-NILE"}, rpc: pool}
			result, err := driver.CheckIngestionRPC(t.Context(), nil, crypto.IngestionRPCCheckOptions{Samples: 1, HistoricalBlock: 10})
			require.NoError(t, err)
			require.Equal(t, scenario == "verified", result.Ready)
			require.NotEmpty(t, result.Comparisons)
			if scenario == "different_amount" {
				var found bool
				for _, c := range result.Comparisons {
					if c.Height == 10 && !c.Matched {
						found = true
					}
				}
				require.True(t, found)
			}
		})
	}
}
