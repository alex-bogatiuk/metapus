//go:build integration

package crypto_worker

import (
	"context"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

func TestReplacementPublicationLocksAdvisoryAndWalletBeforeExecutionRow(t *testing.T) {
	for _, kind := range []string{"WithdrawalRequest", "CryptoSweepAttempt", "TreasuryWithdrawal"} {
		for _, resource := range []string{"document_advisory", "wallet"} {
			t.Run(kind+"/"+resource, func(t *testing.T) {
				runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
					var basis, document, source id.ID
					var documentType, rowSQL, hash string
					switch kind {
					case "WithdrawalRequest":
						wr := seedWithdrawalRecovery(t, ctx, tm, f)
						basis, document, source = wr.task.RequestID, wr.documentID, f.hot
						documentType, rowSQL = "CryptoWithdrawal", "SELECT id FROM doc_crypto_withdrawals WHERE id=$1 FOR UPDATE"
					case "CryptoSweepAttempt":
						sf := newSweepFinalFixture(t, ctx, tm, f, 2)
						basis, document, source = sf.tasks[0].LineID, sf.doc.ID, sf.tasks[1].SourceWalletID
						documentType, rowSQL = "CryptoSweep", "SELECT line_id FROM doc_crypto_sweeps__lines WHERE line_id=$1 FOR UPDATE"
					default:
						tf := newTreasuryFinalFixture(t, ctx, tm, f)
						basis, document, source = tf.doc.ID, tf.doc.ID, f.hot
						documentType, rowSQL = "TreasuryWithdrawal", "SELECT id FROM doc_treasury_withdrawals WHERE id=$1 FOR UPDATE"
					}
					rowKey := document
					if kind == "CryptoSweepAttempt" {
						rowKey = basis
					}
					assertWorkerResourceOrder(t, ctx,
						func(ctx context.Context) error {
							if resource == "document_advisory" {
								return postgres.NewDocLocker().LockDocument(ctx, documentType, document)
							}
							return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, source)
						}, func(ctx context.Context) error {
							if resource == "document_advisory" {
								return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, source)
							}
							_, err := tm.GetQuerier(ctx).Exec(ctx, rowSQL, rowKey)
							return err
						}, func(ctx context.Context) error {
							return NewOutboundRepo().UpdateOutboundReplacementHash(ctx, kind, basis, hash, hash)
						})
				})
			})
		}
	}
}

func TestSweepAuthorizationPublicationLocksCollectorBeforeSQLGuards(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		attempt := id.New()
		var from, to, network string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT s.address,d.address,n.network_identifier FROM cat_wallets s,cat_wallets d,cat_blockchain_networks n WHERE s.id=$1 AND d.id=$2 AND n.id=$3`, f.wallet, f.hot, f.network).Scan(&from, &to, &network))
		_, err := tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
            VALUES($1,$2,$3,$4,$5,$6,$7,23,'claimed',NOW()+interval '1 hour')`, attempt, f.wallet, f.token, f.network, f.hot, from, to)
		require.NoError(t, err)
		auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeSweep,
			BasisType: "CryptoSweepAttempt", BasisID: attempt, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.network, NetworkCode: "TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: network,
			SourceAddress: from, FeePayerAddress: from, VaultKeyID: "test/deposit", DestinationAddress: to, AssetStandard: "TRC-20", ContractAddress: "COORD-" + id.Compact(f.network) + "-contract", AmountMinorUnits: types.MustRawAmount("23"), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "pub-" + attempt.String(), PolicyVersion: "tron-v1"}
		unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
		assertWorkerResourceOrder(t, ctx,
			func(ctx context.Context) error {
				return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.hot)
			},
			func(ctx context.Context) error {
				_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT id FROM reg_crypto_sweep_attempts WHERE id=$1 FOR UPDATE", attempt)
				return err
			},
			func(ctx context.Context) error {
				_, err := NewOutboundRepo().InsertSigningAuthorization(ctx, auth, "synthetic-jws", unsigned)
				return err
			})
		var count int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, "SELECT count(*) FROM reg_crypto_signing_authorizations WHERE basis_id=$1", attempt).Scan(&count))
		require.Equal(t, 1, count)
	})
}

func TestWithdrawalReplacementPublicationRequiresExactSignedPredecessor(t *testing.T) {
	for _, scenario := range []string{"valid", "unsigned_predecessor", "destination", "amount", "sequence", "skipped_generation", "terminal_parent", "fresh_broadcast"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCustodyFixture(t)
			source := f.wallet(t, &f.a, "liquidity_hot", "free", "100")
			request := f.request(t, f.a, "70")
			repo := NewOutboundRepo()
			selected, err := repo.ReserveHotWalletForWithdrawal(f.ctx, request, f.a, f.network, f.token, types.MustRawAmount("70"))
			require.NoError(t, err)
			require.Equal(t, source, selected.ID)
			bundle := crypto.ApprovalBundle{IntentJWS: "synthetic-intent"}
			digest, err := crypto.DigestJSON(bundle)
			require.NoError(t, err)
			auth := crypto.SigningAuthorization{SchemaVersion: 2, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.a, Purpose: crypto.SigningPurposeWithdrawal,
				BasisType: "WithdrawalRequest", BasisID: request, Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.network, NetworkCode: f.network.String(), ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: f.network.String(),
				SourceAddress: selected.Address, FeePayerAddress: selected.Address, VaultKeyID: selected.VaultKeyID, DestinationAddress: "0x1111111111111111111111111111111111111111", AssetStandard: "native", AmountMinorUnits: types.MustRawAmount("70"), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "first-" + request.String(), ApprovalBundleDigest: digest, PolicyVersion: "evm-v1"}
			unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
			_, err = repo.InsertSigningAuthorization(f.ctx, auth, "synthetic-jws", unsigned, bundle)
			require.NoError(t, err)
			if scenario != "unsigned_predecessor" {
				signed := crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: "first-" + request.String(), Payload: []byte(`{}`)}
				require.NoError(t, repo.MarkSigningAuthorizationSigned(f.ctx, auth.AuthorizationID, signed))
				require.NoError(t, repo.MarkSigningAuthorizationBroadcast(f.ctx, auth.AuthorizationID, signed.TxHash))
			}
			status := "broadcast"
			if scenario == "terminal_parent" {
				status = "failed"
			}
			_, err = f.pool.Exec(f.ctx, "UPDATE doc_withdrawal_requests SET status=$2 WHERE id=$1", request, status)
			require.NoError(t, err)
			auth.AuthorizationID = id.New().String()
			auth.Generation = 2
			auth.UnsignedFingerprint = "replacement-" + request.String()
			switch scenario {
			case "destination":
				auth.DestinationAddress = "0x2222222222222222222222222222222222222222"
			case "amount":
				auth.AmountMinorUnits = types.MustRawAmount("71")
			case "sequence":
				auth.NetworkSequence = "1"
			case "skipped_generation":
				auth.Generation = 3
			case "fresh_broadcast":
				auth.Generation = 1
			}
			unsigned.Generation, unsigned.NetworkSequence, unsigned.UnsignedFingerprint = auth.Generation, auth.NetworkSequence, auth.UnsignedFingerprint
			_, err = repo.InsertSigningAuthorization(f.ctx, auth, "replacement-jws", unsigned, bundle)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			var count, movements int
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_signing_authorizations WHERE basis_id=$1),
                (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1)`, request).Scan(&count, &movements))
			expected := 1
			if scenario == "valid" {
				expected = 2
			}
			require.Equal(t, expected, count)
			require.Zero(t, movements, "replacement publication cannot debit the merchant")
		})
	}
}
