package tron

import (
	"context"
	"fmt"
	"io"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/configuration/crypto/infrastructure/crypto/cryptoprofiles"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	integrationhttp "metapus/internal/infrastructure/integrations"
)

type chainstackTestAccounts struct {
	mu       sync.Mutex
	accounts []model.Selection
	current  int
	reports  int
}

func (r *chainstackTestAccounts) Select(context.Context, id.ID, string, model.Provider) (model.Selection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == len(r.accounts) {
		return model.Selection{}, &model.PoolUnavailableError{Reason: model.Exhausted, RetryAt: time.Now().Add(time.Hour)}
	}
	return r.accounts[r.current], nil
}
func (r *chainstackTestAccounts) Report(_ context.Context, s model.Selection, f model.Failure, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f != "" && r.current < len(r.accounts) && r.accounts[r.current].AccountID == s.AccountID {
		r.current++
		r.reports++
	}
	return nil
}

type chainstackTestRoundTrip func(*http.Request) (*http.Response, error)

func (f chainstackTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChainstackAccountRotationThenWholeBlockProviderFailover(t *testing.T) {
	version := crypto.TRONTransferEvidenceSchema
	block, receipts := blockFixture(t, 10, false)
	trace := traceFixture(t, "call", "a", 23)
	receipts[0].InternalTransactions = []InternalTransaction{trace}
	var secondaryReceipts []TransactionInfo
	raw, err := json.Marshal(receipts)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &secondaryReceipts))
	freeze := traceFixture(t, "freezeBalanceV2ForEnergy", "b", 100)
	freeze.TransferToAddress = freeze.CallerAddress
	receipts[0].InternalTransactions = append([]InternalTransaction{freeze}, receipts[0].InternalTransactions...)
	var receiptCalls atomic.Int32
	primary := rpcBlockServer(t, block, receipts, "", &receiptCalls)
	defer primary.Close()
	secondary := rpcBlockServer(t, block, secondaryReceipts, "", &receiptCalls)
	defer secondary.Close()
	reserve := rpcBlockServer(t, block, receipts, "", &receiptCalls)
	defer reserve.Close()
	pid := id.New()
	repo := &chainstackTestAccounts{accounts: []model.Selection{{PoolID: pid, AccountID: id.New(), Slot: "nile", Key: "key-a"}, {PoolID: pid, AccountID: id.New(), Slot: "nile", Key: "key-b"}}}
	var exhaustB atomic.Bool
	wrap := func(http.RoundTripper) http.RoundTripper {
		return &integrationhttp.Transport{Profiles: cryptoprofiles.Registry(), Provider: cryptointegrations.Chainstack, PoolID: pid, Slot: "nile", Repository: repo, Base: chainstackTestRoundTrip(func(req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.Path, "/key-a/") || exhaustB.Load() && strings.HasSuffix(req.URL.Path, "gettransactioninfobyblocknum") {
				return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"monthly quota exhausted"}`))}, nil
			}
			r := req.Clone(req.Context())
			u, _ := url.Parse(secondary.URL)
			u.Path = strings.TrimPrefix(req.URL.Path, "/key-b")
			r.URL = u
			r.Host = ""
			return http.DefaultTransport.RoundTrip(r)
		})}
	}
	endpoints := []chainconfig.Endpoint{{ProviderID: "trongrid", TrustGroup: "trongrid", URL: primary.URL}, {ProviderID: "chainstack", TrustGroup: "chainstack", URL: "https://tron-nile.core.chainstack.com", AdmissionScope: "tenant/integration-pool:" + pid.String(), WrapTransport: wrap}, {ProviderID: "reserve", TrustGroup: "reserve", URL: reserve.URL}}
	pool, err := NewRPCPool(RPCPoolConfig{NetworkCode: "TRON-NILE", FullNode: endpoints, Solidity: endpoints, Settings: chainconfig.TRONSettings{IngestionMode: "hybrid", RequestsPerSecond: 1000}})
	require.NoError(t, err)
	defer pool.Close()
	firstData, sources, err := pool.ReadAgreedBlock(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, "chainstack", sources[1].TrustGroup)
	repo.mu.Lock()
	require.Equal(t, 1, repo.current)
	repo.mu.Unlock()
	exhaustB.Store(true)
	secondData, sources, err := pool.ReadAgreedBlock(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, "trongrid", sources[0].TrustGroup)
	require.Equal(t, "reserve", sources[1].TrustGroup)
	require.Equal(t, firstData.Digest, secondData.Digest)
	for _, source := range sources {
		require.Equal(t, version, source.DecoderVersion)
	}
	a, err := decodeTransfers(crypto.NetworkRef{}, firstData, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	b, err := decodeTransfers(crypto.NetworkRef{}, secondData, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	require.Len(t, a, 2)
	require.Len(t, b, 2)
	require.Equal(t, a[1].EconomicsHash(), b[1].EconomicsHash())
	require.Equal(t, int64(0), a[1].EventIndex)
	_, err = pool.AgreedBlockHash(t.Context(), 10)
	require.NoError(t, err)
	// A known-empty pool cannot consume the attempt budget needed by reserve.
	for _, endpoint := range pool.byRole[chainconfig.RoleFullNode] {
		endpoint.mu.Lock()
		endpoint.unavailableUntil = time.Time{}
		endpoint.mu.Unlock()
	}
	_, err = pool.GetCurrentBlockInfo(t.Context())
	require.NoError(t, err)
	repo.mu.Lock()
	require.Equal(t, 2, repo.reports)
	repo.mu.Unlock()
}

func TestShadowCannotCountTwoChainstackBindingsAsIndependent(t *testing.T) {
	block, receipts := blockFixture(t, 10, false)
	var calls atomic.Int32
	first := rpcBlockServer(t, block, receipts, "", &calls)
	defer first.Close()
	second := rpcBlockServer(t, block, receipts, "", &calls)
	defer second.Close()
	pool := newTestRPCPool(t, []string{first.URL, second.URL}, []string{first.URL, second.URL})
	pool.settings.IngestionMode = "shadow"
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for _, e := range pool.byRole[role] {
			e.trustGroup = "chainstack"
		}
	}
	_, _, err := pool.ReadAgreedBlock(t.Context(), 10)
	require.ErrorIs(t, err, crypto.ErrChainQuorumUnavailable)
}

func TestQuorumMismatchDoesNotShopForThirdProvider(t *testing.T) {
	for _, scenario := range []string{"different_amount", "missing_internal_transfer"} {
		t.Run(scenario, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, false)
			other, otherReceipts := blockFixture(t, 10, false)
			if scenario == "different_amount" {
				other.Transactions[0].RawData.Contract[0].Parameter.Value.Amount++
			} else {
				// A required monetary trace cannot be omitted even when block and receipt counts agree.
				require.NoError(t, json.Unmarshal([]byte(`[{"hash":"`+strings.Repeat("a", 64)+`","caller_address":"`+testFromHex+`","transferTo_address":"`+testToHex+`","callValueInfo":[{"callValue":1217309}],"note":"63616c6c"}]`), &receipts[0].InternalTransactions))
			}
			var calls atomic.Int32
			first := rpcBlockServer(t, block, receipts, "", &calls)
			defer first.Close()
			second := rpcBlockServer(t, other, otherReceipts, "", &calls)
			defer second.Close()
			third := rpcBlockServer(t, block, receipts, "", &calls)
			defer third.Close()
			pool := newTestRPCPool(t, []string{first.URL, second.URL, third.URL}, []string{first.URL, second.URL, third.URL})
			pool.settings.IngestionMode = "hybrid"
			for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
				for i, e := range pool.byRole[role] {
					e.trustGroup = fmt.Sprint(i)
				}
			}
			_, _, err := pool.ReadAgreedBlock(t.Context(), 10)
			require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
			for _, e := range pool.byRole[chainconfig.RoleSolidity][2:] {
				e.mu.Lock()
				require.Empty(t, e.operations, "third provider must not be consulted to bypass disagreement")
				e.mu.Unlock()
			}
		})
	}
}
