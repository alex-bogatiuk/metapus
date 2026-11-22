//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func TestAccountingCustodyGuardRequiresProofOutsideLegacyActivation(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	tokenID, walletID := id.New(), id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,contract_address,decimal_places,token_standard)
		VALUES($1,$2,'Custody guard asset',$3,'USDT',$4,6,'ERC-20')`, tokenID, "CG-"+id.Compact(tokenID), proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,derivation_path)
		VALUES($1,$2,'Custody guard wallet',$3,$4,'custody-key',$2)`, walletID, "CG-"+id.Compact(walletID), proof.Event.NetworkID, proof.Event.ToAddress)
	require.NoError(t, err)
	repo := NewBlockIngestionRepo()
	proofID, err := repo.SaveVerification(ctx, proof)
	require.NoError(t, err)
	event := proof.Event
	event.VerificationID = &proofID
	basis := crypto.AccountingTransferBasis{
		NetworkID: event.NetworkID, WalletID: walletID, TokenID: tokenID,
		TxHash: event.TxHash, EventKind: event.EventKind, EventIndex: event.EventIndex,
		FromAddress: event.FromAddress, Amount: event.Amount, BlockNumber: event.BlockNumber,
		BlockHash: event.BlockHash, RequiredConfs: 19, VerificationID: &proofID,
	}
	tm := postgres.MustGetTxManager(ctx)
	authorize := func(b crypto.AccountingTransferBasis, e crypto.BlockchainEvent) (crypto.VerifiedCustodyEffect, error) {
		var result crypto.VerifiedCustodyEffect
		err := tm.RunInTransaction(ctx, func(txCtx context.Context) error {
			var inner error
			result, inner = repo.AuthorizeCustodyEffect(txCtx, b, e)
			return inner
		})
		return result, err
	}
	verified, err := authorize(basis, event)
	require.NoError(t, err)
	require.Equal(t, proofID, verified.VerificationID)
	require.False(t, id.IsNil(verified.OccurrenceID))
	source := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Controlled outgoing source',$2,$3,'source-key')`, source, event.NetworkID, strings.ToUpper(event.FromAddress))
	require.NoError(t, err)
	outgoing := basis
	outgoing.RecordType = entity.RecordTypeExpense
	outgoing.WalletID = source
	outgoing.ToAddress = event.ToAddress
	outgoing.FromAddress = ""
	verified, err = authorize(outgoing, event)
	require.NoError(t, err)
	require.Equal(t, proofID, verified.VerificationID)
	outgoing.ToAddress = event.FromAddress
	_, err = authorize(outgoing, event)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
	_, err = repo.AuthorizeCustodyEffect(ctx, basis, event)
	require.ErrorContains(t, err, "requires a transaction")

	wrong := basis
	wrong.Amount = types.MustRawAmount("24")
	_, err = authorize(wrong, event)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
	wrong = basis
	wrong.BlockNumber++
	_, err = authorize(wrong, event)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch, "frozen document block must match its proof")
	incomplete := event
	incomplete.FromAddress = ""
	_, err = authorize(basis, incomplete)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch, "strict admission cannot skip economics binding for an incomplete observation")
	wrong = basis
	wrong.VerificationID = nil
	_, err = authorize(wrong, event)
	require.ErrorContains(t, err, "basis is required")
	_, err = pool.Exec(ctx, "UPDATE cat_blockchain_networks SET confirmations_needed=30 WHERE id=$1", event.NetworkID)
	require.NoError(t, err)
	_, err = authorize(basis, event)
	require.ErrorIs(t, err, crypto.ErrVerificationPending, "caller cannot lower current network finality")
	_, err = pool.Exec(ctx, "UPDATE cat_blockchain_networks SET confirmations_needed=19 WHERE id=$1", event.NetworkID)
	require.NoError(t, err)

	// Legacy network activation remains absent, yet strict receipt admission
	// never treats missing identity as permission to proceed.
	legacy := proof
	legacy.AccountingIdentity = nil
	legacy.Event.TxHash = "0x" + strings.Repeat("d", 64)
	legacyID, err := repo.SaveVerification(ctx, legacy)
	require.NoError(t, err)
	event = legacy.Event
	event.VerificationID = &legacyID
	basis.TxHash, basis.VerificationID = event.TxHash, &legacyID
	_, err = authorize(basis, event)
	require.ErrorIs(t, err, crypto.ErrVerificationPending)
}
