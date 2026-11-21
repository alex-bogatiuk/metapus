package rpcpolicy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
)

func TestDiagnosticsExposeOperationFailureDespiteHealthyHeader(t *testing.T) {
	status := crypto.RuntimeEndpointStatus{State: "closed"}
	head := &Circuit{}
	broken := &Circuit{Failures: 3, LastError: "timeout", OpenUntil: time.Now().Add(time.Minute)}
	AddDiagnostic(&status, head.Diagnostic("header"))
	AddDiagnostic(&status, broken.Diagnostic("full_block"))
	require.Equal(t, "open", status.State)
	require.Equal(t, 3, status.Failures)
	require.Equal(t, "timeout", status.LastError)
	require.Len(t, status.Operations, 2)
	require.Equal(t, "unknown", SafeReason("https://provider.example/private-token"))
}

func TestRuntimeSnapshotVersionTracksInstalledGeneration(t *testing.T) {
	value := 1
	r := NewRuntime[int, int](&value, "initial", nil, nil, func(*int) {})
	defer r.Close()
	current, revision, release, err := r.PeekSnapshot()
	require.NoError(t, err)
	defer release()
	require.Equal(t, 1, *current)
	require.Equal(t, "initial", revision)
}
