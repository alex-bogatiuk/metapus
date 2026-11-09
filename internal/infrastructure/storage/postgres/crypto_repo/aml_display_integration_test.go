//go:build integration

package crypto_repo

import (
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/aml"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAMLDisplayUsesPaymentEffectAndRetainsHighAfterReorg(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "display-low", 1)
	proofID, effect := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effect, 1000, true)
	f.rebuild(t)
	var payment id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM doc_crypto_payments WHERE tx_hash=$1`, proof.Event.TxHash).Scan(&payment))
	values, err := f.repo.ReadPaymentRiskDisplays(f.ctx, []id.ID{payment})
	require.NoError(t, err)
	require.Equal(t, aml.Low, values[payment].Class)
	wallets, err := f.repo.ReadWalletRiskDisplays(f.ctx, []id.ID{f.input.WalletID})
	require.NoError(t, err)
	require.Equal(t, aml.Low, wallets[f.input.WalletID].Class)
	require.True(t, wallets[f.input.WalletID].HasFunds)
	// A different incoming payment must not borrow the first payment's score.
	other := f.proof(f.input.Request.Address, "display-pending", 1)
	otherProof, _ := f.save(t, other)
	f.postPayment(t, other, otherProof, f.input.WalletID)
	f.creditNet(t, other.Event.TxHash)
	var second id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM doc_crypto_payments WHERE tx_hash=$1`, other.Event.TxHash).Scan(&second))
	values, err = f.repo.ReadPaymentRiskDisplays(f.ctx, []id.ID{payment, second})
	require.NoError(t, err)
	require.Equal(t, aml.Low, values[payment].Class)
	require.Equal(t, aml.Unknown, values[second].Class)
	// A high decision is seeded through the normal report flow on the second effect.
	var secondEffect id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_effects WHERE tx_hash=$1`, other.Event.TxHash).Scan(&secondEffect))
	f.complete(t, secondEffect, 9000, true)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_payments SET canonicality='orphaned' WHERE id=$1`, second)
	require.NoError(t, err)
	values, err = f.repo.ReadPaymentRiskDisplays(f.ctx, []id.ID{second})
	require.NoError(t, err)
	require.Equal(t, aml.High, values[second].Class)
	require.NotNil(t, values[second].ScoreBP)
	require.WithinDuration(t, time.Now(), *values[second].EvaluatedAt, time.Minute)
}
