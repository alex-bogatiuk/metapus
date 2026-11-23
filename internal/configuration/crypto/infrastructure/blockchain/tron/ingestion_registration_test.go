package tron

import (
	"context"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/stretchr/testify/require"
)

type registrationRetryRepo struct {
	captureRepoStub
	responses []error
	calls     int
	cancel    context.CancelFunc
}

func (r *registrationRetryRepo) EnsureCutover(_ context.Context, _ crypto.NetworkRef, block crypto.WatcherBlockFingerprint, mode string, _ []crypto.AssetRef) (crypto.IngestionCutover, error) {
	err := r.responses[r.calls]
	r.calls++
	if r.cancel != nil {
		r.cancel()
	}
	return crypto.IngestionCutover{Height: block.BlockNumber, Hash: block.BlockHash, Mode: mode}, err
}

func TestIngestionRegistrationRetriesOnlyBusyAndHonorsCancellation(t *testing.T) {
	block := crypto.WatcherBlockFingerprint{BlockNumber: 10, BlockHash: "registered", Timestamp: 1000}
	repo := &registrationRetryRepo{responses: []error{crypto.ErrIngestionRegistrationBusy, crypto.ErrIngestionRegistrationBusy, nil}}
	driver := &Driver{blockRepo: repo}
	result, err := driver.ensureCutover(t.Context(), crypto.NetworkRef{}, block, "hybrid", nil)
	require.NoError(t, err)
	require.Equal(t, 3, repo.calls)
	require.Equal(t, block.BlockNumber, result.Height)
	repo = &registrationRetryRepo{responses: []error{crypto.ErrVerificationMismatch}}
	driver.blockRepo = repo
	_, err = driver.ensureCutover(t.Context(), crypto.NetworkRef{}, block, "hybrid", nil)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
	require.Equal(t, 1, repo.calls)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repo = &registrationRetryRepo{responses: []error{crypto.ErrIngestionRegistrationBusy}, cancel: cancel}
	driver.blockRepo = repo
	_, err = driver.ensureCutover(ctx, crypto.NetworkRef{}, block, "hybrid", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.calls)
}
