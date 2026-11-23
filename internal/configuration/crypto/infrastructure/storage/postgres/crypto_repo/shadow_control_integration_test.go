//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type shadowAuditFailure struct{}

func (shadowAuditFailure) Write(context.Context, eventlog.Event) error {
	return fmt.Errorf("injected audit failure")
}
func (shadowAuditFailure) WriteBatch(context.Context, []eventlog.Event) error {
	return fmt.Errorf("injected audit failure")
}

func TestShadowPauseFencesCapturePreservesCheckpointAndRollsBackWithAudit(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	txm := sdktest.NewTxManager(pool)
	ctx := tenant.WithTxManager(t.Context(), txm)
	network := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,network_identifier,native_token_symbol,native_decimals,chain_family) VALUES($1,$2,'shadow',$2,'TRX',6,'tron')`, network, "SH"+id.Compact(network))
	require.NoError(t, err)
	blockRepo := NewBlockIngestionRepo()
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: 10, BlockHash: fmt.Sprintf("%064x", 10), Timestamp: 1750000000000}
	_, err = blockRepo.EnsureCutover(ctx, crypto.NetworkRef{ID: network}, baseline, "shadow", nil)
	require.NoError(t, err)
	stateRepo := NewWatcherStateRepo()
	state, err := stateRepo.Acquire(ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: "tron:shadow", LastBlock: 10, Fingerprint: baseline.BlockHash, LastTimestamp: baseline.Timestamp}, "test-shadow")
	require.NoError(t, err)
	candidate := *state
	candidate.LastBlock = 11
	candidate.Fingerprint = fmt.Sprintf("%064x", 11)
	candidate.LastTimestamp += 3000
	capture := crypto.CapturedBlock{DecoderVersion: 2, Shadow: true, Sequential: true, ParentHash: baseline.BlockHash, Commit: crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: 10, ExpectedHash: baseline.BlockHash, Block: crypto.WatcherBlockFingerprint{BlockNumber: 11, BlockHash: candidate.Fingerprint, Timestamp: candidate.LastTimestamp}}}
	repo := NewShadowControlRepo()
	admin := appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), IsAdmin: true})
	svc := crypto.NewShadowControlAdminService(repo, shadowAuditFailure{})
	_, err = svc.SetPaused(admin, network, 1, true, "audit unavailable")
	require.ErrorContains(t, err, "audit failure")
	control, err := repo.GetShadowControl(ctx, network)
	require.NoError(t, err)
	require.False(t, control.Paused)
	require.Equal(t, 1, control.Version)
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error { _, err := repo.SetShadowPaused(txCtx, network, 1, true); return err })
	require.NoError(t, err)
	require.ErrorIs(t, blockRepo.CaptureBlock(ctx, capture), crypto.ErrShadowPaused)
	current, err := stateRepo.Get(ctx, network, "tron:shadow")
	require.NoError(t, err)
	require.Equal(t, int64(10), current.LastBlock)
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		_, err := repo.SetShadowPaused(txCtx, network, 1, false)
		return err
	})
	require.Error(t, err, "stale command must not resume")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		_, err := repo.SetShadowPaused(txCtx, network, 2, false)
		return err
	})
	require.NoError(t, err)
	require.NoError(t, blockRepo.CaptureBlock(ctx, capture))
	current, err = stateRepo.Get(ctx, network, "tron:shadow")
	require.NoError(t, err)
	require.Equal(t, int64(11), current.LastBlock)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET mode='hybrid',activated_at=NOW() WHERE network_id=$1`, network)
	require.NoError(t, err)
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error { _, err := repo.SetShadowPaused(txCtx, network, 3, true); return err })
	require.Error(t, err, "financial activation must fence a command prepared earlier")
	control, err = repo.GetShadowControl(ctx, network)
	require.NoError(t, err)
	require.False(t, control.Paused)
	require.Equal(t, "financial", control.Observed(control.ActivatedAt.UTC()).Status)
}
