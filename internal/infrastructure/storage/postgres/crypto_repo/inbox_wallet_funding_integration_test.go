//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// Stand: a HOT-EVM funding recorded by the Funding CLI stayed an unmatched
// inbox event and was retried 159 times. Such an event is retired; a plain
// unidentified transfer remains a receipt candidate.
func TestReceiptCandidatesRetireTransfersRecognizedAsWalletFunding(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	txm := postgres.MustGetTxManager(f.Ctx)
	rollback := errors.New("rollback inbox funding fixture")
	err := txm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `SET LOCAL session_replication_role = replica`) // fixture rows only
		require.NoError(t, err)
		funded, plain := "0xFUNDED"+id.Compact(id.New()), "0xplain"+id.Compact(id.New())
		insert := func(tx string) id.ID {
			var row id.ID
			require.NoError(t, q.QueryRow(ctx, `INSERT INTO reg_crypto_inbound_chain_events(network_id,tx_hash,event_kind,event_index,to_address,amount,block_timestamp,wallet_id,token_id,match_status,match_reason,created_at)
 VALUES($1,$2,'native_value',0,'hot',500,NOW(),$3,$4,'unmatched','no_allocation',NOW()-interval '1 hour') RETURNING id`, f.Network, tx, f.Hot, f.Native).Scan(&row))
			return row
		}
		fundedRow, plainRow := insert(funded), insert(plain)
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_wallet_fundings(id,network_id,wallet_id,token_id,tx_hash,from_address,to_address,amount,block_number,confirmations,block_timestamp,verification_id,occurrence_id,posting_id)
 VALUES(gen_random_uuid_v7(),$1,$2,$3,lower($4),'payer','hot',500,1,12,NOW(),gen_random_uuid_v7(),gen_random_uuid_v7(),gen_random_uuid_v7())`, f.Network, f.Hot, f.Native, funded)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `SET LOCAL session_replication_role = origin`)
		require.NoError(t, err)

		claimed, err := NewChainEventInboxRepo().ClaimReceiptCandidates(ctx, 50, time.Minute, time.Minute)
		require.NoError(t, err)
		claimedIDs := map[id.ID]bool{}
		for _, row := range claimed {
			claimedIDs[row.ID] = true
		}
		require.True(t, claimedIDs[plainRow], "an unidentified transfer stays a receipt candidate")
		require.False(t, claimedIDs[fundedRow])
		var status, reason string
		require.NoError(t, q.QueryRow(ctx, `SELECT match_status,match_reason FROM reg_crypto_inbound_chain_events WHERE id=$1`, fundedRow).Scan(&status, &reason))
		require.Equal(t, string(crypto.InboxIgnored), status)
		require.Equal(t, crypto.MatchReasonWalletFunding, reason)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func TestInFlightInboundCountsOnlyUnrecognizedTransfers(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	txm := postgres.MustGetTxManager(f.Ctx)
	rollback := errors.New("rollback in-flight fixture")
	err := txm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `SET LOCAL session_replication_role = replica`)
		require.NoError(t, err)
		insert := func(amount int, status, canonicality string, receipt bool) {
			var receiptID any
			if receipt {
				receiptID = id.New()
			}
			_, err := q.Exec(ctx, `INSERT INTO reg_crypto_inbound_chain_events(network_id,tx_hash,event_kind,event_index,to_address,amount,block_timestamp,wallet_id,token_id,match_status,canonicality,receipt_id)
 VALUES($1,$2,'native_value',0,'hot',$3,NOW(),$4,$5,$6,$7,$8)`, f.Network, "tx"+id.Compact(id.New()), amount, f.Hot, f.Native, status, canonicality, receiptID)
			require.NoError(t, err)
		}
		insert(100, "unmatched", "canonical", false) // awaiting receipt
		insert(20, "matched", "unknown", false)      // awaiting payment posting
		insert(7, "unmatched", "canonical", true)    // already receipted
		insert(5, "ignored", "canonical", false)     // not money of the wallet ledger
		insert(3, "unmatched", "orphaned", false)    // left the canonical chain
		_, err = q.Exec(ctx, `SET LOCAL session_replication_role = origin`)
		require.NoError(t, err)
		inFlight, err := NewWorkerRuntimeRepo().InFlightInbound(ctx, f.Hot, f.Native)
		require.NoError(t, err)
		require.Equal(t, "120", inFlight.String())
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
