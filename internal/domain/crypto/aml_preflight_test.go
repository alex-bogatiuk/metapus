package crypto

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestEmptyCheckpointRequiresIndependentFreshConsistentState(t *testing.T) {
	now := time.Now().UTC()
	valid := func() EmptyCheckpoint {
		return EmptyCheckpoint{NetworkID: id.New(), Asset: AssetRef{TokenID: id.New()}, Address: "address", Height: 10, Hash: strings.Repeat("a", 64), ObservedAt: now, Sources: []EmptyCheckpointSource{{ProviderID: "one", TrustGroup: "first", Height: 10, Hash: strings.Repeat("a", 64), Method: "solidified_bracket"}, {ProviderID: "two", TrustGroup: "second", Height: 10, Hash: strings.Repeat("a", 64), Method: "solidified_bracket"}}}
	}
	require.NoError(t, valid().Validate(now))
	require.True(t, valid().Empty())
	for _, change := range []func(*EmptyCheckpoint){
		func(p *EmptyCheckpoint) { p.Sources = p.Sources[:1] },
		func(p *EmptyCheckpoint) { p.Sources[1].ProviderID = " ONE " },
		func(p *EmptyCheckpoint) { p.Sources[1].TrustGroup = "first" },
		func(p *EmptyCheckpoint) { p.Sources[1].Balance = types.MustCryptoAmount("1") },
		func(p *EmptyCheckpoint) { p.Sources[1].Height++ },
		func(p *EmptyCheckpoint) { p.Sources[1].Hash = strings.Repeat("b", 64) },
		func(p *EmptyCheckpoint) { p.Sources[1].Method = "latest" },
		func(p *EmptyCheckpoint) { p.ObservedAt = now.Add(-2 * time.Minute) },
		func(p *EmptyCheckpoint) { p.ObservedAt = now.Add(time.Minute) },
	} {
		p := valid()
		change(&p)
		require.Error(t, p.Validate(now))
	}
	p := valid()
	for i := range p.Sources {
		p.Sources[i].Balance = types.MustCryptoAmount("9")
	}
	require.NoError(t, p.Validate(now))
	require.False(t, p.Empty())
}
