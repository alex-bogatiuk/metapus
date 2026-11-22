package tron

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
)

func TestAMLEmptyCheckpointUsesIndependentSolidifiedState(t *testing.T) {
	for _, scenario := range []string{"empty", "nonempty", "one_source", "same_group", "different_balance", "moving_head", "catchup", "different_height"} {
		t.Run(scenario, func(t *testing.T) {
			server := func(index int) *httptest.Server {
				var heads atomic.Int32
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/walletsolidity/getnowblock":
						height := int64(10)
						if index == 1 && scenario == "catchup" && heads.Add(1) <= 2 {
							height = 9
						}
						if index == 1 && scenario == "different_height" {
							height = 9
						}
						if scenario == "moving_head" && index == 1 && heads.Add(1) > 1 {
							height++
						}
						block := BlockInfo{BlockID: fmt.Sprintf("%064x", height)}
						block.BlockHeader.RawData.Number = height
						block.BlockHeader.RawData.Timestamp = time.Now().UnixMilli()
						_ = json.NewEncoder(w).Encode(block)
					case "/walletsolidity/getaccount":
						amount := 0
						if scenario == "nonempty" || scenario == "different_balance" && index == 1 {
							amount = 5
						}
						_, _ = fmt.Fprintf(w, `{"balance":%d}`, amount)
					default:
						t.Errorf("unexpected checkpoint RPC %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
			}
			first, second := server(0), server(1)
			defer first.Close()
			defer second.Close()
			urls := []string{first.URL, second.URL}
			if scenario == "one_source" {
				urls = urls[:1]
			}
			pool := newTestRPCPool(t, urls, urls)
			if scenario == "same_group" {
				for _, e := range pool.byRole[chainconfig.RoleSolidity] {
					e.trustGroup = "same"
				}
			}
			driver := &Driver{network: crypto.NetworkRef{ID: id.New()}, rpc: pool}
			proof, err := driver.EmptyCheckpoint(t.Context(), crypto.AssetRef{TokenID: id.New(), Standard: token.TokenStandardNative}, "TQv6ey7rE6YuK1rXeRgQpb7WLHaZakWQCK")
			if scenario == "empty" || scenario == "nonempty" || scenario == "catchup" {
				require.NoError(t, err)
				require.Equal(t, scenario != "nonempty", proof.Empty())
				require.Len(t, proof.Sources, 2)
				require.Equal(t, strings.Repeat("0", 63)+"a", proof.Hash)
			} else {
				require.Error(t, err)
			}
		})
	}
}
