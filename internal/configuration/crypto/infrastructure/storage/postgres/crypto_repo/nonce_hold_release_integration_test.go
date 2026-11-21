//go:build integration

package crypto_repo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
)

// A re-verified false nonce divergence resolves exactly the incidents of its
// source and the network holds they own; other sources stay held and a replay
// is a no-op.
func TestReleaseNetworkHoldsResolvesOnlyTheVerifiedSubject(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	repo := NewNetworkHoldRepo()
	reason := "RPC providers disagree on latest or pending nonce"
	held := map[string]any{"sourceAddress": "0x00000000000000000000000000000000000000a1", "reason": reason, "dbNextNonce": "3"}
	other := map[string]any{"sourceAddress": "0x00000000000000000000000000000000000000b2", "reason": reason, "dbNextNonce": "1"}
	require.NoError(t, repo.HoldNetwork(f.Ctx, f.Network, crypto.OperationalHoldNonceDivergence, held))
	require.NoError(t, repo.HoldNetwork(f.Ctx, f.Network, crypto.OperationalHoldNonceDivergence, other))

	state := func(source string) (holds, incidents string) {
		t.Helper()
		require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT
 COALESCE((SELECT string_agg(h.status,',') FROM reg_crypto_network_holds h JOIN reg_crypto_security_incidents i ON i.id::text=h.owner_id
   WHERE h.network_id=$1 AND i.details->>'sourceAddress'=$2),''),
 COALESCE((SELECT string_agg(status,',') FROM reg_crypto_security_incidents WHERE network_id=$1 AND details->>'sourceAddress'=$2),'')`,
			f.Network, source).Scan(&holds, &incidents))
		return holds, incidents
	}
	subject := map[string]string{"sourceAddress": held["sourceAddress"].(string), "reason": reason}
	resolution := "provider disagreement re-verified"
	require.NoError(t, repo.ReleaseNetworkHolds(f.Ctx, f.Network, crypto.OperationalHoldNonceDivergence, subject, resolution))

	holds, incidents := state(held["sourceAddress"].(string))
	require.Equal(t, "resolved", holds)
	require.Equal(t, "resolved", incidents)
	holds, incidents = state(other["sourceAddress"].(string))
	require.Equal(t, "open", holds, "another source keeps its hold")
	require.Equal(t, "open", incidents)

	require.NoError(t, repo.ReleaseNetworkHolds(f.Ctx, f.Network, crypto.OperationalHoldNonceDivergence, subject, resolution), "replay is a no-op")
	var resolvedBy, recorded string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT h.resolved_by,h.resolution FROM reg_crypto_network_holds h JOIN reg_crypto_security_incidents i ON i.id::text=h.owner_id
 WHERE h.network_id=$1 AND i.details->>'sourceAddress'=$2`, f.Network, subject["sourceAddress"]).Scan(&resolvedBy, &recorded))
	require.Equal(t, "crypto-worker", resolvedBy)
	require.Equal(t, resolution, recorded)
}
