package tron

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
)

func TestRelativeLagNeedsTwoIndependentGroupsAndIgnoresSingleHighOutlier(t *testing.T) {
	pool := &rpcGeneration{}
	makeEndpoint := func(group string, height int64) (*rpcEndpoint, *BlockInfo) {
		e := &rpcEndpoint{trustGroup: group, breaker: breaker{state: BreakerClosed}}
		b := &BlockInfo{}
		b.BlockHeader.RawData.Number = height
		return e, b
	}
	first, a := makeEndpoint("first", 200)
	second, b := makeEndpoint("second", 200)
	outlier, c := makeEndpoint("outlier", 50000)
	pool.quarantineRelativeLag(map[*rpcEndpoint]*BlockInfo{first: a, second: b, outlier: c})
	require.False(t, endpointQuarantined(first))
	require.False(t, endpointQuarantined(second))
	old, d := makeEndpoint("old", 100)
	pool.quarantineRelativeLag(map[*rpcEndpoint]*BlockInfo{first: a, second: b, old: d})
	require.True(t, endpointQuarantined(old))
	first.trustGroup, second.trustGroup = "same", "same"
	old.breaker.success()
	pool.quarantineRelativeLag(map[*rpcEndpoint]*BlockInfo{first: a, second: b, old: d})
	require.False(t, endpointQuarantined(old), "two URLs of one group cannot establish a lag floor")
}

func TestHeadFreshnessAndRegressionAreEnforcedBeforeNextProbe(t *testing.T) {
	now := time.Now()
	p := &rpcGeneration{now: func() time.Time { return now }}
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		e := &rpcEndpoint{role: role, breaker: breaker{state: BreakerClosed}}
		b := &BlockInfo{BlockID: strings.Repeat("a", 64)}
		b.BlockHeader.RawData.Number, b.BlockHeader.RawData.Timestamp = 500, now.UnixMilli()
		require.NoError(t, p.observeHead(e, b))
		b.BlockHeader.RawData.Number = 499
		require.NoError(t, p.observeHead(e, b), "small sampled changes must not cause false quarantine")
		b.BlockHeader.RawData.Number = 400
		require.ErrorIs(t, p.observeHead(e, b), errRPCHeadLagging)
		require.True(t, endpointQuarantined(e))
		b.BlockHeader.RawData.Number = 501
		b.BlockHeader.RawData.Timestamp = now.Add(-2 * time.Minute).UnixMilli()
		require.ErrorIs(t, p.observeHead(e, b), errRPCHeadLagging)
		require.True(t, endpointQuarantined(e))
	}
}

func TestLagRecoveryRequiresSpacedProgressAndCompleteReceipts(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "missing_receipt"}[missing], func(t *testing.T) {
			now := time.Now()
			n := newSimulatedRPC(t, now.UnixMilli())
			eps := []chainconfig.Endpoint{{ProviderID: "recovering", TrustGroup: "recovering", URL: n.server.URL}}
			p, err := NewRPCPool(RPCPoolConfig{NetworkCode: "RECOVERY", FullNode: eps, Solidity: eps, Settings: chainconfig.TRONSettings{RequestsPerSecond: 1000}})
			require.NoError(t, err)
			t.Cleanup(p.Close)
			p.now = func() time.Time { return now }
			n.head.Store(100)
			p.Probe(t.Context())
			e := p.byRole[chainconfig.RoleSolidity][0]
			require.True(t, endpointQuarantined(e))
			n.head.Store(190)
			p.Probe(t.Context())
			require.True(t, endpointQuarantined(e), "one healthy head is insufficient")
			if missing {
				n.fault.Store(simMissingReceipt)
			}
			now = now.Add(6 * time.Second)
			n.head.Store(192)
			p.Probe(t.Context())
			require.Equal(t, missing, endpointQuarantined(e))
			if missing {
				n.fault.Store(simHealthy)
				p.Probe(t.Context())
				require.True(t, endpointQuarantined(e), "failed receipts reset the recovery sequence")
				now = now.Add(6 * time.Second)
				n.head.Store(194)
				p.Probe(t.Context())
				require.False(t, endpointQuarantined(e))
			}
		})
	}
}

func TestPairSkewCorroborationAndSingleHighOutlier(t *testing.T) {
	for _, highOutlier := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh_timestamp_on_old_head", true: "one_high_outlier"}[highOutlier], func(t *testing.T) {
			var eps []chainconfig.Endpoint
			var nodes []*simulatedRPC
			epoch := time.Now().UnixMilli()
			for _, group := range []string{"first", "second", "reserve"} {
				n := newSimulatedRPC(t, epoch)
				nodes = append(nodes, n)
				eps = append(eps, chainconfig.Endpoint{ProviderID: group, TrustGroup: group, URL: n.server.URL})
			}
			if highOutlier {
				nodes[0].head.Store(50000)
				nodes[0].freshTime.Store(true)
			} else {
				nodes[1].head.Store(100)
				nodes[1].freshTime.Store(true)
			}
			p, err := NewRPCPool(RPCPoolConfig{NetworkCode: "SKEW", FullNode: eps, Solidity: eps, Settings: chainconfig.TRONSettings{IngestionMode: "hybrid", RequestsPerSecond: 1000}})
			require.NoError(t, err)
			t.Cleanup(p.Close)
			_, sources, err := p.ReadAgreedBlock(t.Context(), 151)
			require.NoError(t, err)
			require.Len(t, sources, 2)
			if highOutlier {
				require.False(t, endpointQuarantined(p.byRole[chainconfig.RoleSolidity][1]))
				require.False(t, endpointQuarantined(p.byRole[chainconfig.RoleSolidity][2]))
			} else {
				require.Equal(t, "first", sources[0].TrustGroup)
				require.Equal(t, "reserve", sources[1].TrustGroup)
			}
		})
	}
}
