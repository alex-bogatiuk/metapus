//go:build integration

package funding

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func TestInternalFundingReplayKeepsOneReceiptAndLedgerMovement(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	tm := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(t.Context(), tm)
	rollback := errors.New("rollback funding fixture")
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		network, walletID, tokenID := id.New(), id.New(), id.New()
		code := "FV2" + id.Compact(network)
		_, err := q.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed) VALUES($1,$2,'funding v2','tron',$2,'TRX',6,19)`, network, code)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$2,'funding TRX',$3,'TRX',6,'native')`, tokenID, code, network)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,address_key,vault_key_id) VALUES($1,$2,'funding wallet',$3,'recipient','recipient','test-key-ref')`, walletID, code, network)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version,activated_at) VALUES($1,100,$2,1750000000000,'hybrid',2,NOW())`, network, strings.Repeat("a", 64))
		require.NoError(t, err)
		index, rawIndex := int64(0), int64(1)
		event := crypto.BlockchainEvent{NetworkID: network, TxHash: strings.Repeat("b", 64), EventKind: crypto.EventKindInternalValue, DecoderVersion: 2, TraceHash: strings.Repeat("c", 64), TraceIndex: &rawIndex, CallValueIndex: &index, FromAddress: "sender", ToAddress: "recipient", Amount: types.NewCryptoAmountFromInt64(23), BlockNumber: 101, BlockHash: strings.Repeat("d", 64), Confirmations: 20, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, Timestamp: time.Unix(1750000003, 0)}
		proof := crypto.TransferVerification{Event: event, DecoderVersion: 2, VerifiedAt: time.Now(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded, Sources: []crypto.VerificationSource{{ProviderID: "first", LatestProviderID: "first", TrustGroup: "first", Digest: "canonical", SourceDigest: "extended", DecoderVersion: 2, HeadHeight: 120, HeadHash: "head"}, {ProviderID: "second", LatestProviderID: "second", TrustGroup: "second", Digest: "canonical", SourceDigest: "standard", DecoderVersion: 2, HeadHeight: 120, HeadHash: "head"}}}
		identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyTRON, 0)
		require.NoError(t, err)
		proof.AccountingIdentity = &identity
		repo := crypto_repo.NewBlockIngestionRepo()
		proofID, err := repo.SaveVerification(ctx, proof)
		require.NoError(t, err)
		target := Target{NetworkID: network, WalletID: walletID, TokenID: tokenID, TokenCode: code, TokenStandard: token.TokenStandardNative, Address: event.ToAddress}
		transfer := ConfirmedTransfer{VerificationID: &proofID, EventKind: event.EventKind, EventIndex: event.EventIndex, BlockHash: event.BlockHash, FromAddress: event.FromAddress, ToAddress: event.ToAddress, Amount: event.Amount, BlockNumber: event.BlockNumber, BlockTimestamp: event.Timestamp, Confirmations: event.Confirmations}
		first, err := Record(ctx, code, target, event.TxHash, transfer)
		require.NoError(t, err)
		require.False(t, first.Existing)
		// Another node omits metadata before the same trace. The original proof
		// remains immutable, while its economic identity is unchanged on replay.
		rawIndex = 0
		proof.Sources[0].SourceDigest = "standard"
		replayedProofID, err := repo.SaveVerification(ctx, proof)
		require.NoError(t, err)
		require.Equal(t, proofID, replayedProofID)
		second, err := Record(ctx, code, target, event.TxHash, transfer)
		require.NoError(t, err)
		require.True(t, second.Existing)
		require.Equal(t, first.ID, second.ID)
		var receipts, movements int
		require.NoError(t, q.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_wallet_fundings WHERE network_id=$1),(SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$2)`, network, first.ID).Scan(&receipts, &movements))
		require.Equal(t, 1, receipts)
		require.Equal(t, 1, movements)
		transfer.Amount = types.NewCryptoAmountFromInt64(24)
		_, err = Record(ctx, code, target, event.TxHash, transfer)
		require.Error(t, err)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
