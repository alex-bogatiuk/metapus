package tron

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/json"
)

func canonicalServer(t *testing.T, height int64, hash string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var block BlockInfo
		block.BlockID = fmt.Sprintf("%064x", 123)
		block.BlockHeader.RawData.Number = 123
		block.BlockHeader.RawData.Timestamp = time.Now().UnixMilli()
		block.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", 122)
		if strings.HasSuffix(r.URL.Path, "getblockbynum") {
			var request struct {
				Number int64 `json:"num"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.EqualValues(t, 123, request.Number)
			block.BlockHeader.RawData.Number = height
			block.BlockID = hash
		}
		_ = json.NewEncoder(w).Encode(block)
	}))
}
func TestDriverCanonicalBlockRequiresIndependentAgreement(t *testing.T) {
	hash := fmt.Sprintf("%064x", 123)
	first, second := canonicalServer(t, 123, hash), canonicalServer(t, 123, hash)
	defer first.Close()
	defer second.Close()
	driver := &Driver{rpc: newTestRPCPool(t, []string{first.URL, second.URL}, []string{first.URL, second.URL})}
	actual, err := driver.CanonicalBlock(t.Context(), 123)
	require.NoError(t, err)
	require.Equal(t, hash, actual)
	single := &Driver{rpc: newTestRPCPool(t, []string{first.URL}, []string{first.URL})}
	_, err = single.CanonicalBlock(t.Context(), 123)
	require.ErrorIs(t, err, crypto.ErrChainQuorumUnavailable)
}
func TestDriverCanonicalBlockRejectsWrongHeightAndDisagreement(t *testing.T) {
	for _, tt := range []struct {
		name   string
		height int64
		hash   string
	}{
		{"wrong height", 122, fmt.Sprintf("%064x", 122)},
		{"different hash", 123, fmt.Sprintf("%064x", 124)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first, second := canonicalServer(t, 123, fmt.Sprintf("%064x", 123)), canonicalServer(t, tt.height, tt.hash)
			defer first.Close()
			defer second.Close()
			driver := &Driver{rpc: newTestRPCPool(t, []string{first.URL, second.URL}, []string{first.URL, second.URL})}
			_, err := driver.CanonicalBlock(t.Context(), 123)
			require.Error(t, err)
		})
	}
	driver := &Driver{}
	_, err := driver.CanonicalBlock(context.Background(), -1)
	require.ErrorContains(t, err, "cannot be negative")
}
