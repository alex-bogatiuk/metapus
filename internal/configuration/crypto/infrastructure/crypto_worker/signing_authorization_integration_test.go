//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"

	"metapus/pkg/sdk/types"
)

type integrationControlAuthorizationSigner struct{}

func (integrationControlAuthorizationSigner) SignAuthorization(context.Context, string, crypto.SigningAuthorization) (string, error) {
	return "signed-jws", nil
}

func (integrationControlAuthorizationSigner) CheckReady(context.Context, string) error { return nil }

func TestIntegrationSigningAuthorizationJWSIsWriteOnce(t *testing.T) {
	ctx, pool := custodyTestDatabase(t)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback signing authorization fixture")
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var networkID id.ID
		var networkCode, networkIdentifier string
		var chainFamily crypto.ChainFamily
		require.NoError(t, q.QueryRow(txCtx, `
			SELECT id,code,network_identifier,chain_family
			FROM cat_blockchain_networks
			WHERE deletion_mark=FALSE
			ORDER BY created_at,id
			LIMIT 1
		`).Scan(&networkID, &networkCode, &networkIdentifier, &chainFamily))

		// Signing now checks actual custody wallet identities before JWS creation.
		for _, row := range []struct{ address, role string }{{"source", "merchant_pool"}, {"destination", "liquidity_hot"}} {
			walletID := id.New()
			_, err := q.Exec(txCtx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,wallet_role) VALUES($1,$2,'Signing custody fixture',$3,$4,'test-key',$5)`, walletID, walletID.String(), networkID, row.address, row.role)
			require.NoError(t, err)
		}
		authorizationID := id.New().String()
		basisID := id.New()
		fingerprint := id.Compact(id.New())
		tenantID := id.New().String()
		expiresAt := time.Now().Add(time.Hour)
		networkSequence := ""
		sequenceModel := crypto.SequenceNone
		if chainFamily == crypto.ChainFamilyEVM {
			networkSequence = "0"
			sequenceModel = crypto.SequenceAccount
		}
		_, err := q.Exec(txCtx, `
			INSERT INTO reg_crypto_signing_authorizations (
				authorization_id,tenant_id,purpose,schema_version,network_id,network_code,
				chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,
				destination_address,asset_standard,amount_minor_units,max_network_fee_minor,
				basis_type,basis_id,network_sequence,expires_at,policy_version,status,
				authorization_jws,unsigned_fingerprint,unsigned_envelope
			) VALUES (
				$1,$12,'sweep',$2,$3,$4,$5,$6,
				'source','source','test-key','destination','native',1,1,
				'SigningAuthorizationWriteOnceTest',$7,$8,$9,$10,'created','',$11,'{}'::jsonb
			)
		`, authorizationID, crypto.SigningAuthorizationSchemaVersionFor(sequenceModel),
			networkID, networkCode, chainFamily, networkIdentifier, basisID, networkSequence,
			expiresAt, crypto.SigningAuthorizationPolicyVersion(chainFamily), fingerprint, tenantID)
		require.NoError(t, err)

		repo := NewOutboundRepo()
		authorization := crypto.SigningAuthorization{
			SchemaVersion:       crypto.SigningAuthorizationSchemaVersionFor(sequenceModel),
			AuthorizationID:     authorizationID,
			TenantID:            tenantID,
			Purpose:             crypto.SigningPurposeSweep,
			BasisType:           "SigningAuthorizationWriteOnceTest",
			BasisID:             basisID,
			Attempt:             1,
			Generation:          1,
			NetworkSequence:     networkSequence,
			ExpiresAt:           expiresAt,
			NetworkID:           networkID,
			NetworkCode:         networkCode,
			ChainFamily:         chainFamily,
			NetworkIdentifier:   networkIdentifier,
			SourceAddress:       "source",
			FeePayerAddress:     "source",
			VaultKeyID:          "test-key",
			DestinationAddress:  "destination",
			AssetStandard:       "native",
			AmountMinorUnits:    types.MustRawAmount("1"),
			MaxNetworkFeeMinor:  types.MustRawAmount("1"),
			UnsignedFingerprint: fingerprint,
			PolicyVersion:       crypto.SigningAuthorizationPolicyVersion(chainFamily),
		}
		service, err := NewSigningAuthorizationService(repo, integrationControlAuthorizationSigner{}, tenantID)
		require.NoError(t, err)
		_, err = service.SignOrReuse(txCtx, nil, SigningAuthorizationRecord{Authorization: authorization, Status: "created"})
		// An external signature must never begin under an uncommitted caller
		// transaction, even when a test signer could answer synchronously.
		require.ErrorContains(t, err, "external dispatch requires a committed SQL permit")
		var persistedJWS string
		require.NoError(t, q.QueryRow(txCtx, `
			SELECT authorization_jws FROM reg_crypto_signing_authorizations WHERE authorization_id=$1
		`, authorizationID).Scan(&persistedJWS))
		require.Empty(t, persistedJWS, "no external signature is persisted before a committed dispatch start")

		require.NoError(t, repo.SetSigningAuthorizationJWS(txCtx, authorizationID, "signed-jws"))
		require.NoError(t, repo.SetSigningAuthorizationJWS(txCtx, authorizationID, "signed-jws"))
		require.ErrorContains(t, repo.SetSigningAuthorizationJWS(txCtx, authorizationID, "replacement-jws"), "immutable")

		err = txm.RunInSavepoint(txCtx, func(savepointCtx context.Context) error {
			_, updateErr := txm.GetQuerier(savepointCtx).Exec(savepointCtx, `
				UPDATE reg_crypto_signing_authorizations
				SET authorization_jws='replacement-jws'
				WHERE authorization_id=$1
			`, authorizationID)
			return updateErr
		})
		require.ErrorContains(t, err, "signing authorization identity is immutable")

		err = txm.RunInSavepoint(txCtx, func(savepointCtx context.Context) error {
			_, updateErr := txm.GetQuerier(savepointCtx).Exec(savepointCtx, `
				UPDATE reg_crypto_signing_authorizations
				SET destination_address='different-destination'
				WHERE authorization_id=$1
			`, authorizationID)
			return updateErr
		})
		require.ErrorContains(t, err, "signing authorization identity is immutable")
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}
