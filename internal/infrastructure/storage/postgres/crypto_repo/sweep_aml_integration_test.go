//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

func TestSweepV2AMLUsesExactLineReceiptBeforeWholeBatchFinishes(t *testing.T) {
	f, destination, _ := readyAMLInventory(t)
	request := f.allocationRequest(t, destination, "50")
	plan, err := f.repo.ReserveAMLInternalAllocation(f.ctx, request)
	require.NoError(t, err)
	auth, unsigned := f.allocationAuthorization(t, request)
	authID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	hash := strings.ReplaceAll(request.SweepAttemptID.String()+request.SweepAttemptID.String(), "-", "")
	signed, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: hash, Payload: []byte(`{}`)})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',tx_hash=$2,signed_envelope=$3::jsonb WHERE id=$1`, authID, hash, string(signed))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
		VALUES($1,0,$2,'contract_log',$3,$4,$5,'test-contract',$6,$7)`, authID, request.NetworkID, auth.SourceAddress, auth.DestinationAddress, request.TokenID, request.Amount, hash)
	require.NoError(t, err)
	sweep, native := id.New(), id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,token_standard,contract_address,decimal_places) VALUES($1,$1::uuid::text,'Native',$2,'TRX','native','',6)`, native, request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweeps(id,token_id,hot_wallet_id,total_amount,status) VALUES($1,$2,$3,51,'broadcast')`, sweep, request.TokenID, destination)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweep_lines(line_id,document_id,line_no,wallet_id,amount,tx_hash,settlement_status)
		VALUES($1,$2,1,$3,50,$4,'unknown'),($5,$2,2,$3,1,'','not_applicable')`, request.SweepAttemptID, sweep, request.SourceWalletID, hash, id.New())
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_sweep_attempts SET sweep_id=$2,status='broadcast' WHERE id=$1`, request.SweepAttemptID, sweep)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status)
		VALUES($1,$2,$3,$4,0,'CryptoSweepAttempt',$5,'sweep',1,'native_burn',1,$6,'broadcast')`, request.NetworkID, request.SourceWalletID, native, request.TokenID, request.SweepAttemptID, hash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,1,'checkpoint',1,'paused',2)`, request.NetworkID)
	require.NoError(t, err)
	proof := f.proof(auth.DestinationAddress, hash, 1)
	proof.Event.FromAddress, proof.Event.Amount, proof.Event.Direction = auth.SourceAddress, request.Amount, crypto.TransferDirectionInternal
	proof.DecoderVersion, proof.Event.DecoderVersion = 2, 2
	for i := range proof.Sources {
		proof.Sources[i].DecoderVersion = 2
		proof.Sources[i].SourceDigest = "signed-sweep"
	}
	require.NoError(t, proof.BindTRONAccountingIdentity())
	proofID, _ := f.save(t, proof)
	receipt := crypto.ChainReceipt{NetworkID: request.NetworkID, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHeight: proof.Event.BlockNumber, BlockHash: proof.Event.BlockHash,
		Confirmations: proof.Event.Confirmations, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: native}}}
	basis := crypto.SweepAccountingBasis{DocumentID: sweep, LineID: request.SweepAttemptID, NetworkID: request.NetworkID, TokenID: request.TokenID,
		SourceWalletID: request.SourceWalletID, DestinationWalletID: destination, Amount: request.Amount, TxHash: hash, VerificationID: &proofID}
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		repo := NewSweepCustodyRepo()
		proceed, err := repo.Prepare(ctx, basis, receipt)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var fee id.ID
		if err := q.QueryRow(ctx, "SELECT id FROM reg_crypto_fee_reservations WHERE basis_id=$1", request.SweepAttemptID).Scan(&fee); err != nil {
			return err
		}
		if err := recognizeTestNetworkFee(ctx, fee, receipt); err != nil {
			return err
		}
		if err := repo.RecordPrincipal(ctx, basis, receipt); err != nil {
			return err
		}
		_, err = q.Exec(ctx, "UPDATE doc_crypto_sweep_lines SET settlement_status='recorded',chain_result='SUCCESS',confirmed=true,network_fee=0,network_fee_token_id=$2 WHERE line_id=$1", request.SweepAttemptID, native)
		return err
	})
	require.NoError(t, err)
	f.rebuild(t)
	var applied string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT status FROM reg_crypto_aml_internal_allocations WHERE id=$1", plan.ID).Scan(&applied))
	require.Equal(t, "applied", applied)
	var principal, fee string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1 AND token_id=$2", destination, request.TokenID).Scan(&principal, &fee))
	require.Equal(t, []string{plan.Partition.Principal.String(), plan.Partition.ServiceFee.String()}, []string{principal, fee})
	require.Equal(t, aml.Low, f.class(t, destination))
}
