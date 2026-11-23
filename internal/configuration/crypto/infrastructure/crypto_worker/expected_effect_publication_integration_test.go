//go:build integration

package crypto_worker

import (
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func TestSignedAuthorizationValidatesCompleteExpectedEffectReplay(t *testing.T) {
	for _, scenario := range []string{"identical", "different_destination", "extra_ordinal"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCustodyFixture(t)
			source := f.wallet(t, &f.a, "merchant_pool", "free", "100")
			var address, key string
			require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT address,vault_key_id FROM cat_wallets WHERE id=$1", source).Scan(&address, &key))
			auth := crypto.SigningAuthorization{SchemaVersion: 2, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeSweep,
				BasisType: "ExpectedEffectPublicationTest", BasisID: id.New(), Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.network, NetworkCode: f.network.String(), ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: f.network.String(),
				SourceAddress: address, FeePayerAddress: address, VaultKeyID: key, DestinationAddress: "0x1111111111111111111111111111111111111111", AssetStandard: "native", AmountMinorUnits: types.MustRawAmount("70"), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "fp-" + id.New().String(), PolicyVersion: "evm-v1"}
			unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{}`)}
			repo := NewOutboundRepo()
			row, err := repo.InsertSigningAuthorization(f.ctx, auth, "synthetic-jws", unsigned)
			require.NoError(t, err)
			hash := id.Compact(id.New())
			destination, ordinal := auth.DestinationAddress, 0
			if scenario == "different_destination" {
				destination = "0x2222222222222222222222222222222222222222"
			}
			if scenario == "extra_ordinal" {
				ordinal = 1
			}
			_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
                VALUES($1,$2,$3,'native_value',lower($4),lower($5),$6,'',70,$7)`, row, ordinal, f.network, address, destination, f.token, hash)
			require.NoError(t, err)
			signed := crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: hash, Payload: []byte(`{}`)}
			err = repo.MarkSigningAuthorizationSigned(f.ctx, auth.AuthorizationID, signed)
			var status string
			var effects int
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,(SELECT count(*) FROM reg_crypto_expected_chain_effects WHERE signing_authorization_id=$1) FROM reg_crypto_signing_authorizations WHERE id=$1`, row).Scan(&status, &effects))
			require.Equal(t, 1, effects)
			if scenario == "identical" {
				require.NoError(t, err)
				require.Equal(t, "signed", status)
				require.NoError(t, repo.MarkSigningAuthorizationSigned(f.ctx, auth.AuthorizationID, signed))
			} else {
				require.ErrorContains(t, err, "does not match")
				require.Equal(t, "created", status, "a conflicting effect must roll back signed-envelope persistence too")
			}
		})
	}
}
