//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
	"metapus/pkg/sdk/types"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestTRONBlockCaptureAtomicFenceProofAndTargetRegistration(t *testing.T) {
	version := crypto.TRONTransferEvidenceSchema
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network, wallet, token := id.New(), id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,network_identifier,native_token_symbol,native_decimals) VALUES($1,$2,'TRON test',$2,'TRX',6)`, network, "T"+id.Compact(network))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$2,'TRX',$3,'TRX',6,'native')`, token, "T"+id.Compact(token), network)
	require.NoError(t, err)
	repo := NewBlockIngestionRepo()
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: 100, BlockHash: fmt.Sprintf("%064x", 100), Timestamp: 1750000000000}
	_, err = repo.EnsureCutover(ctx, crypto.NetworkRef{ID: network}, baseline, "hybrid", nil)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET decoder_version=$2 WHERE network_id=$1`, network, version)
	require.NoError(t, err)
	to := "T" + id.Compact(wallet)
	state, err := NewWatcherStateRepo().Acquire(ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: "tron:solidified", LastBlock: 100, Fingerprint: baseline.BlockHash, LastTimestamp: baseline.Timestamp}, "first")
	require.NoError(t, err)
	event := crypto.BlockchainEvent{NetworkID: network, TxHash: strings.Repeat("a", 64), EventKind: crypto.EventKindInternalValue, EventIndex: 3, FromAddress: "source", ToAddress: to, Amount: types.NewRawAmountFromInt64(23), BlockNumber: 101, BlockHash: fmt.Sprintf("%064x", 101), Confirmations: 20, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, EventType: crypto.EventTypeTransfer, Timestamp: time.UnixMilli(baseline.Timestamp + 3000)}
	candidate := *state
	candidate.LastBlock = 101
	candidate.Fingerprint = event.BlockHash
	candidate.LastTimestamp = event.Timestamp.UnixMilli()
	sources := []crypto.VerificationSource{{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "digest", HeadHeight: 120, HeadHash: "head"}, {ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "digest", HeadHeight: 120, HeadHash: "head"}}
	event.DecoderVersion, event.TraceHash = 2, strings.Repeat("e", 64)
	traceIndex, callIndex := int64(3), int64(0)
	event.TraceIndex, event.CallValueIndex = &traceIndex, &callIndex
	for i := range sources {
		sources[i].DecoderVersion, sources[i].SourceDigest = 2, "source"
	}
	capture := crypto.CapturedBlock{DecoderVersion: version, Commit: crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: 100, ExpectedHash: baseline.BlockHash, Block: crypto.WatcherBlockFingerprint{BlockNumber: 101, BlockHash: event.BlockHash, Timestamp: event.Timestamp.UnixMilli()}, RetainFrom: 0}, ParentHash: baseline.BlockHash, Events: []crypto.BlockchainEvent{event}, Sources: sources, Sequential: true}
	for _, unsupported := range []int{0, 1, 99} {
		invalid := capture
		invalid.DecoderVersion = unsupported
		require.ErrorIs(t, repo.CaptureBlock(ctx, invalid), crypto.ErrDecoderVersionConflict)
	}
	// Registration happens after RPC/decode has finished, before commit. The
	// transaction must match the newly issued, inactive permanent address.
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,vault_key_id,allocation_mode,is_active) VALUES($1,$2,'permanent',$3,$2,$2,'non-secret-key-reference','persistent',FALSE)`, wallet, to, network)
	require.NoError(t, err)
	var start int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT start_height FROM sys_crypto_watch_targets WHERE network_id=$1 AND target_kind='address' AND target_key=$2`, network, to).Scan(&start))
	require.Equal(t, int64(101), start)
	_, err = NewWatcherStateRepo().Acquire(ctx, state, "second")
	require.ErrorIs(t, err, crypto.ErrWatcherLeaseHeld)
	stale := capture
	stale.Commit.Candidate.FencingToken++
	require.ErrorIs(t, repo.CaptureBlock(ctx, stale), crypto.ErrWatcherLeaseLost)
	var proofs, observations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&proofs))
	require.Zero(t, proofs)
	bad := event
	bad.BlockHash = "different"
	capture.Events = append(capture.Events, bad)
	require.Error(t, repo.CaptureBlock(ctx, capture))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&proofs))
	require.Zero(t, proofs, "a later event failure must roll back earlier proof and observation")
	capture.Events = []crypto.BlockchainEvent{event}
	// Expiry/restart grants a new fencing token. The old worker cannot
	// commit a block it prefetched before the lease changed ownership.
	_, err = pool.Exec(ctx, `UPDATE sys_chain_watcher_state SET lease_expires_at=NOW()-interval '1 second' WHERE network_id=$1 AND watcher_key='tron:solidified'`, network)
	require.NoError(t, err)
	replacement, err := NewWatcherStateRepo().Acquire(ctx, state, "second")
	require.NoError(t, err)
	require.Greater(t, replacement.FencingToken, state.FencingToken)
	require.ErrorIs(t, repo.CaptureBlock(ctx, capture), crypto.ErrWatcherLeaseLost)
	capture.Commit.Candidate.LeaseOwner = replacement.LeaseOwner
	capture.Commit.Candidate.FencingToken = replacement.FencingToken
	require.NoError(t, repo.CaptureBlock(ctx, capture))
	current, err := NewWatcherStateRepo().Get(ctx, network, "tron:solidified")
	require.NoError(t, err)
	require.Equal(t, int64(101), current.LastBlock)
	require.ErrorIs(t, repo.CaptureBlock(ctx, capture), crypto.ErrWatcherCheckpointConflict)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&proofs))
	require.Equal(t, 1, proofs)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_chain_observations WHERE network_id=$1`, network).Scan(&observations))
	require.Equal(t, 1, observations)
	var proofID id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&proofID))
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_transfer_verifications SET confirmations=confirmations+1 WHERE id=$1`, proofID)
	require.ErrorContains(t, err, "immutable")
	payment := &crypto_payment.CryptoPayment{NetworkID: network, TokenID: token, WalletID: wallet, TxHash: event.TxHash, EventKind: string(event.EventKind), EventIndex: 3, FromAddress: event.FromAddress, Amount: event.Amount, RequiredConfs: 19, BlockNumber: 101, BlockHash: event.BlockHash, Confirmations: 20, FinalityStatus: string(event.Finality), Canonicality: string(event.Canonicality), VerificationID: &proofID}
	// This test checks captured custody evidence, without constructing a
	// payment accounting aggregate. Financial posting has a separate admission.
	validateEvidence := func() error {
		return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(txCtx context.Context) error {
			basis := accountingPaymentBasis(payment)
			proven, err := repo.ExpectedAccountingTransfer(txCtx, basis)
			if err != nil {
				return err
			}
			proven.VerificationID, proven.Confirmations = payment.VerificationID, payment.Confirmations
			proven.Finality, proven.Canonicality = crypto.FinalityStatus(payment.FinalityStatus), crypto.CanonicalityStatus(payment.Canonicality)
			_, err = repo.AuthorizeCustodyEffect(txCtx, basis, proven)
			return err
		})
	}
	require.NoError(t, validateEvidence())
	expected, err := repo.ExpectedTransfer(ctx, payment)
	require.NoError(t, err)
	require.Equal(t, event.EconomicsHash(), expected.EconomicsHash())
	require.Equal(t, event.TraceHash, expected.TraceHash)
	require.Equal(t, event.CallValueIndex, expected.CallValueIndex)

	payment.Amount = types.NewRawAmountFromInt64(24)
	require.ErrorIs(t, validateEvidence(), crypto.ErrVerificationPending)
	payment.Amount = event.Amount
	payment.RequiredConfs = 21
	require.ErrorIs(t, validateEvidence(), crypto.ErrVerificationPending)
	payment.ID, payment.Posted, payment.PostedVersion = id.New(), true, 1
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,from_address,amount,event_kind,event_index,block_number,block_hash,confirmations,required_confs,finality_status,status,posted,posted_version,verification_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,101,$12,20,19,'solidified','settled',TRUE,1,$13)`, payment.ID, id.New(), id.New(), network, token, wallet, event.TxHash, event.FromAddress, event.Amount, event.EventKind, event.EventIndex, event.BlockHash, proofID)
	require.NoError(t, err)
	absent := event
	absent.BlockHash, absent.EventType, absent.Canonicality = strings.Repeat("d", 64), crypto.EventTypeReorg, crypto.CanonicalityOrphaned
	absenceID, err := repo.SaveVerification(ctx, crypto.TransferVerification{Event: absent, Sources: sources, DecoderVersion: version, VerifiedAt: time.Now(), Outcome: "absent", ExecutionStatus: crypto.ReceiptPending})
	require.NoError(t, err)
	absent.VerificationID = &absenceID
	require.NoError(t, postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(txCtx context.Context) error {
		// Independent absence of a posted payment authorizes the automatic
		// compensation of its attribution; the guard itself moves no money.
		proven, err := repo.AuthorizeReorg(txCtx, payment, absent)
		require.NoError(t, err)
		require.Equal(t, absenceID, proven)
		return nil
	}))
	var compensations int
	var posted bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_reorg_compensations WHERE payment_id=$1),(SELECT posted FROM doc_crypto_payments WHERE id=$1)`, payment.ID).Scan(&compensations, &posted))
	require.Zero(t, compensations)
	require.True(t, posted)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_block_ingestion SET mode='paused' WHERE network_id=$1`, network)
	require.NoError(t, err)
	payment.RequiredConfs = 19
	require.ErrorIs(t, validateEvidence(), crypto.ErrVerificationPending)
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET deletion_mark=TRUE WHERE id=$1`, wallet)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_crypto_watch_targets WHERE owner_id=$1`, wallet).Scan(&observations))
	require.Equal(t, 1, observations)
}

func TestTRONShadowStoresAuditWithoutFinancialInboxOrEvidence(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), os.Getenv("METAPUS_TEST_DATABASE_DSN"))
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network, wallet := id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,network_identifier,native_token_symbol,native_decimals) VALUES($1,$2,'Shadow test',$2,'TRX',6)`, network, "S"+id.Compact(network))
	require.NoError(t, err)
	repo := NewBlockIngestionRepo()
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: 10, BlockHash: fmt.Sprintf("%064x", 10), Timestamp: 1750000000000}
	_, err = repo.EnsureCutover(ctx, crypto.NetworkRef{ID: network}, baseline, "shadow", nil)
	require.NoError(t, err)
	to := "T" + id.Compact(wallet)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,vault_key_id,allocation_mode) VALUES($1,$2,'shadow',$3,$2,$2,'non-secret-key-reference','persistent')`, wallet, to, network)
	require.NoError(t, err)
	state, err := NewWatcherStateRepo().Acquire(ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: "tron:shadow", LastBlock: 10, Fingerprint: baseline.BlockHash}, "shadow")
	require.NoError(t, err)
	event := crypto.BlockchainEvent{DecoderVersion: 2, NetworkID: network, TxHash: strings.Repeat("b", 64), EventKind: crypto.EventKindNativeValue, FromAddress: "source", ToAddress: to, Amount: types.NewRawAmountFromInt64(1), BlockNumber: 11, BlockHash: fmt.Sprintf("%064x", 11), Confirmations: 1, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, EventType: crypto.EventTypeTransfer, Timestamp: time.UnixMilli(baseline.Timestamp + 3000)}
	candidate := *state
	candidate.LastBlock, candidate.LastTimestamp, candidate.Fingerprint = 11, event.Timestamp.UnixMilli(), event.BlockHash
	capture := crypto.CapturedBlock{DecoderVersion: 2, Commit: crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: 10, ExpectedHash: baseline.BlockHash, Block: crypto.WatcherBlockFingerprint{BlockNumber: 11, BlockHash: event.BlockHash, Timestamp: candidate.LastTimestamp}}, ParentHash: baseline.BlockHash, Events: []crypto.BlockchainEvent{event}, Sources: []crypto.VerificationSource{{ProviderID: "untrusted-a"}, {ProviderID: "untrusted-b"}}, Shadow: true, Sequential: true}
	require.NoError(t, repo.CaptureBlock(ctx, capture))
	var status, mode string
	var proofs int
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,payload->>'observationMode' FROM reg_crypto_chain_observations WHERE network_id=$1`, network).Scan(&status, &mode))
	require.Equal(t, "processed", status)
	require.Equal(t, "shadow", mode)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_transfer_verifications WHERE network_id=$1`, network).Scan(&proofs))
	require.Zero(t, proofs)
}
