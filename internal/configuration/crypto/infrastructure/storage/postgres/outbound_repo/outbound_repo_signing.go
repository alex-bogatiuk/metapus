package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

const signingAttemptSelectColumns = `
	       a.id,
	       a.authorization_id,
	       a.tenant_id,
	       a.merchant_id,
	       a.purpose,
	       a.schema_version,
	       a.network_id,
	       a.network_code,
	       a.chain_family,
	       a.network_identifier,
	       a.source_address,
	       a.fee_payer_address,
	       a.vault_key_id,
	       a.destination_address,
	       a.asset_standard,
	       a.contract_address,
	       a.amount_minor_units,
	       a.max_network_fee_minor,
	       a.unsigned_fingerprint,
	       a.basis_type,
	       a.basis_id,
	       a.attempt,
	       a.generation,
	       a.network_sequence,
	       a.expires_at,
	       a.policy_version,
	       a.approval_bundle_digest,
	       a.approval_bundle_json,
	       a.authorization_jws,
	       a.status,
	       a.tx_hash,
	       a.unsigned_envelope,
	       a.signed_envelope`

// One transfer profile emits exactly one fully bound expected effect. Reuse
// this projection for both insertion and conflict validation so replay cannot
// silently accept another network, address, token, contract or extra ordinal.
const signedExpectedEffectSelect = `SELECT a.id AS signing_authorization_id,0 AS ordinal,a.network_id,
    CASE WHEN a.contract_address='' THEN 'native_value' ELSE 'contract_log' END AS event_kind,
    CASE WHEN a.chain_family='evm' THEN LOWER(a.source_address) ELSE a.source_address END AS source_address_key,
    CASE WHEN a.chain_family='evm' THEN LOWER(a.destination_address) ELSE a.destination_address END AS destination_address_key,
    (SELECT t.id FROM cat_tokens t WHERE t.network_id=a.network_id AND t.deletion_mark=FALSE
        AND ((a.contract_address='' AND t.contract_address='') OR LOWER(t.contract_address)=LOWER(a.contract_address))
        ORDER BY t.id LIMIT 1) AS token_id,
    CASE WHEN a.chain_family='evm' THEN LOWER(a.contract_address) ELSE a.contract_address END AS contract_address_key,
    a.amount_minor_units AS amount,a.tx_hash
    FROM reg_crypto_signing_authorizations a WHERE a.authorization_id=$1 AND a.tx_hash=$2`

// GetSigningAttempt resolves an immutable execution attempt by its business
// identity. Runtime routing remains bound to the persisted network_id.
func (r *OutboundRepo) GetSigningAttempt(ctx context.Context, basisType string, basisID id.ID, purpose crypto.SigningPurpose, attempt, generation int) (SigningAuthorizationRecord, bool, error) {
	if strings.TrimSpace(basisType) == "" || id.IsNil(basisID) || purpose == "" || attempt <= 0 || generation <= 0 {
		return SigningAuthorizationRecord{}, false, fmt.Errorf("basis type, basis ID, purpose, positive attempt, and generation are required")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var source HotWallet
	var record SigningAuthorizationRecord
	err := r.scanSigningAttempt(q.QueryRow(ctx, `
		SELECT w.id,
		       w.address,
		       w.vault_key_id,
		       0::numeric AS balance,`+signingAttemptSelectColumns+`
		FROM reg_crypto_signing_authorizations a
		JOIN cat_wallets w
		  ON w.network_id = a.network_id
		 AND w.address = a.source_address
		 AND w.vault_key_id = a.vault_key_id
		WHERE a.basis_type = $1
		  AND a.basis_id = $2
		  AND a.purpose = $3
		  AND a.attempt = $4
		  AND a.generation = $5
		LIMIT 1
	`, basisType, basisID, purpose, attempt, generation), &source, &record)
	if errors.Is(err, pgx.ErrNoRows) {
		return SigningAuthorizationRecord{}, false, nil
	}
	if err != nil {
		return SigningAuthorizationRecord{}, false, fmt.Errorf("get signing attempt: %w", err)
	}
	return record, true, nil
}

func (r *OutboundRepo) MarkSigningAuthorizationSigned(ctx context.Context, authID string, signedTx crypto.SignedTxEnvelope) error {
	if strings.TrimSpace(signedTx.TxHash) == "" || strings.TrimSpace(signedTx.UnsignedFingerprint) == "" {
		return fmt.Errorf("signed envelope tx hash and unsigned fingerprint are required")
	}
	txJSON, err := json.Marshal(signedTx)
	if err != nil {
		return fmt.Errorf("marshal signed tx: %w", err)
	}

	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		querier := txm.GetQuerier(txCtx)
		// Signed results are durable facts even after lease loss. Their
		// publication still takes the activation resources before the auth row.
		var activation *id.ID
		if err := querier.QueryRow(txCtx, `SELECT CASE WHEN basis_type='CryptoAccountActivationTask' AND purpose='activation' THEN basis_id ELSE NULL END FROM reg_crypto_signing_authorizations WHERE authorization_id=$1`, authID).Scan(&activation); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("signing authorization cannot be marked signed")
			}
			return fmt.Errorf("load signing publication basis: %w", err)
		}
		if activation != nil {
			if err := lockActivationState(txCtx, *activation); err != nil {
				return err
			}
		}
		tag, err := querier.Exec(txCtx, `
			UPDATE reg_crypto_signing_authorizations
			SET status = 'signed',
			    tx_hash = $2,
			    signed_envelope = $3::jsonb
			WHERE authorization_id = $1
			  AND status IN ('created','signed')
			  AND unsigned_fingerprint = $4
			  AND (tx_hash = '' OR tx_hash = $2)
		`, authID, signedTx.TxHash, string(txJSON), signedTx.UnsignedFingerprint)
		if err != nil {
			return fmt.Errorf("mark signing authorization signed: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("signing authorization cannot be marked signed")
		}

		// The expected effect becomes durable in the exact transaction that
		// persists the signed envelope and its transaction hash.
		_, err = querier.Exec(txCtx, `INSERT INTO reg_crypto_expected_chain_effects (
            signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,
            token_id,contract_address_key,amount,tx_hash) `+signedExpectedEffectSelect+`
            ON CONFLICT (signing_authorization_id,ordinal) DO NOTHING`, authID, signedTx.TxHash)
		if err != nil {
			return fmt.Errorf("persist expected chain effect: %w", err)
		}
		var matches bool
		if err := querier.QueryRow(txCtx, `SELECT EXISTS(SELECT 1 FROM (`+signedExpectedEffectSelect+`) expected
            JOIN reg_crypto_expected_chain_effects e ON
            (e.signing_authorization_id,e.ordinal,e.network_id,e.event_kind,e.source_address_key,e.destination_address_key,e.token_id,e.contract_address_key,e.amount,e.tx_hash)
             = (expected.signing_authorization_id,expected.ordinal,expected.network_id,expected.event_kind,expected.source_address_key,expected.destination_address_key,expected.token_id,expected.contract_address_key,expected.amount,expected.tx_hash)
            WHERE (SELECT count(*) FROM reg_crypto_expected_chain_effects all_effects WHERE all_effects.signing_authorization_id=expected.signing_authorization_id)=1)`, authID, signedTx.TxHash).Scan(&matches); err != nil {
			return fmt.Errorf("verify signed expected effect: %w", err)
		}
		if !matches {
			return apperror.NewConflict("existing expected chain effect does not match signed authorization")
		}

		return nil
	})
}

func (r *OutboundRepo) MarkSigningAuthorizationBroadcast(ctx context.Context, authID, txID string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET status = 'broadcast',
		    updated_at = NOW()
		WHERE authorization_id = $1
		  AND status IN ('signed','broadcast','held')
		  AND tx_hash = $2
	`, authID, txID)
	if err != nil {
		return fmt.Errorf("mark signing authorization broadcast: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("signing authorization cannot be marked broadcast")
	}
	return nil
}

func (r *OutboundRepo) MarkSigningAuthorizationHeld(ctx context.Context, authID, txID string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET status = 'held',
		    updated_at = NOW()
		WHERE authorization_id = $1
		  AND status IN ('created','signed','broadcast','held')
		  AND ($2 = '' OR tx_hash = $2)
	`, authID, txID)
	if err != nil {
		return fmt.Errorf("mark signing authorization held: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("signing authorization cannot be marked held")
	}
	return nil
}

func (r *OutboundRepo) MarkSigningAuthorizationFailed(ctx context.Context, authID, txID string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET status = 'failed',
		    updated_at = NOW()
		WHERE authorization_id = $1
		  AND status IN ('created','signed','held')
		  AND ($2 = '' OR tx_hash = $2)
	`, authID, txID)
	if err != nil {
		return fmt.Errorf("mark signing authorization failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("signing authorization cannot be marked failed")
	}
	return nil
}

func (r *OutboundRepo) scanSigningAttempt(row pgx.Row, hot *HotWallet, record *SigningAuthorizationRecord) error {
	var auth crypto.SigningAuthorization
	var purpose string
	var family string
	var merchantID *id.ID
	var unsignedJSON []byte
	var signedJSON []byte
	var approvalBundleJSON []byte

	err := row.Scan(
		&hot.ID,
		&hot.Address,
		&hot.VaultKeyID,
		&hot.Balance,
		&record.ID,
		&auth.AuthorizationID,
		&auth.TenantID,
		&merchantID,
		&purpose,
		&auth.SchemaVersion,
		&auth.NetworkID,
		&auth.NetworkCode,
		&family,
		&auth.NetworkIdentifier,
		&auth.SourceAddress,
		&auth.FeePayerAddress,
		&auth.VaultKeyID,
		&auth.DestinationAddress,
		&auth.AssetStandard,
		&auth.ContractAddress,
		&auth.AmountMinorUnits,
		&auth.MaxNetworkFeeMinor,
		&auth.UnsignedFingerprint,
		&auth.BasisType,
		&auth.BasisID,
		&auth.Attempt,
		&auth.Generation,
		&auth.NetworkSequence,
		&auth.ExpiresAt,
		&auth.PolicyVersion,
		&auth.ApprovalBundleDigest,
		&approvalBundleJSON,
		&record.JWS,
		&record.Status,
		&record.TxID,
		&unsignedJSON,
		&signedJSON,
	)
	if err != nil {
		return err
	}

	auth.MerchantID = merchantID
	auth.Purpose = crypto.SigningPurpose(purpose)
	auth.ChainFamily = crypto.ChainFamily(family)
	record.Authorization = auth
	record.SourceWallet = *hot

	if len(unsignedJSON) == 0 {
		return fmt.Errorf("signing authorization %s has empty unsigned_envelope", auth.AuthorizationID)
	}
	if err := json.Unmarshal(unsignedJSON, &record.UnsignedTx); err != nil {
		return fmt.Errorf("unmarshal unsigned signing envelope: %w", err)
	}
	if record.UnsignedTx.UnsignedFingerprint == "" || record.UnsignedTx.UnsignedFingerprint != auth.UnsignedFingerprint {
		return fmt.Errorf("stored unsigned envelope fingerprint mismatch: auth=%s", auth.AuthorizationID)
	}
	if record.UnsignedTx.Family != auth.ChainFamily {
		return fmt.Errorf("stored unsigned envelope family mismatch: auth=%s", auth.AuthorizationID)
	}

	if len(signedJSON) > 0 && string(signedJSON) != "null" {
		var signed crypto.SignedTxEnvelope
		if err := json.Unmarshal(signedJSON, &signed); err != nil {
			return fmt.Errorf("unmarshal signed signing envelope: %w", err)
		}
		if signed.TxHash == "" || signed.TxHash != record.TxID || signed.UnsignedFingerprint != auth.UnsignedFingerprint || signed.Family != auth.ChainFamily {
			return fmt.Errorf("stored signed envelope identity mismatch: auth=%s", auth.AuthorizationID)
		}
		record.SignedTx = &signed
	}
	if auth.Purpose.RequiresApprovalBundle() {
		if auth.ApprovalBundleDigest == "" {
			return fmt.Errorf("money movement signing authorization %s has empty approval_bundle_digest", auth.AuthorizationID)
		}
		if len(approvalBundleJSON) == 0 || string(approvalBundleJSON) == "{}" || string(approvalBundleJSON) == "null" {
			return fmt.Errorf("money movement signing authorization %s has empty approval_bundle_json", auth.AuthorizationID)
		}
		if err := json.Unmarshal(approvalBundleJSON, &record.ApprovalBundle); err != nil {
			return fmt.Errorf("unmarshal approval bundle: %w", err)
		}
	} else if len(approvalBundleJSON) > 0 && string(approvalBundleJSON) != "{}" && string(approvalBundleJSON) != "null" {
		return fmt.Errorf("non-money-movement signing authorization %s unexpectedly has approval_bundle_json", auth.AuthorizationID)
	}

	return nil
}

func (r *OutboundRepo) insertSigningAuthorization(ctx context.Context, auth crypto.SigningAuthorization, jws string, unsigned crypto.UnsignedTxEnvelope, bundle ...crypto.ApprovalBundle) (id.ID, error) {
	if err := auth.Validate(time.Now().UTC()); err != nil {
		return id.Nil(), fmt.Errorf("invalid signing authorization: %w", err)
	}
	var result id.ID
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockSigningPublication(ctx, auth); err != nil {
			return err
		}
		var err error
		result, err = r.insertSigningAuthorizationUnlocked(ctx, auth, jws, unsigned, bundle...)
		return err
	})
	return result, err
}

func (r *OutboundRepo) insertSigningAuthorizationUnlocked(ctx context.Context, auth crypto.SigningAuthorization, jws string, unsigned crypto.UnsignedTxEnvelope, bundle ...crypto.ApprovalBundle) (id.ID, error) {
	if err := auth.Validate(time.Now().UTC()); err != nil {
		return id.Nil(), fmt.Errorf("invalid signing authorization: %w", err)
	}
	if unsigned.UnsignedFingerprint == "" || unsigned.UnsignedFingerprint != auth.UnsignedFingerprint || unsigned.Family != auth.ChainFamily ||
		unsigned.Generation != auth.Generation || unsigned.NetworkSequence != auth.NetworkSequence {
		return id.Nil(), fmt.Errorf("unsigned envelope does not match authorization")
	}
	isMoneyMovement := auth.Purpose.RequiresApprovalBundle()

	unsignedJSON, err := json.Marshal(unsigned)
	if err != nil {
		return id.Nil(), fmt.Errorf("marshal unsigned tx: %w", err)
	}
	approvalBundleJSON := []byte("{}")
	if isMoneyMovement {
		if len(bundle) == 0 || bundle[0].IntentJWS == "" {
			return id.Nil(), fmt.Errorf("approval bundle is required for %s signing authorization", auth.Purpose)
		}
		var err error
		approvalBundleJSON, err = json.Marshal(bundle[0])
		if err != nil {
			return id.Nil(), fmt.Errorf("marshal approval bundle: %w", err)
		}
		if auth.ApprovalBundleDigest == "" {
			return id.Nil(), fmt.Errorf("approval_bundle_digest is required for %s signing authorization", auth.Purpose)
		}
	} else if len(bundle) > 0 && bundle[0].IntentJWS != "" {
		return id.Nil(), fmt.Errorf("approval bundle is not allowed for %s signing authorization", auth.Purpose)
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var merchantID any
	if auth.MerchantID != nil && !id.IsNil(*auth.MerchantID) {
		merchantID = *auth.MerchantID
	}

	rowID := id.New()
	tag, err := querier.Exec(ctx, `
		INSERT INTO reg_crypto_signing_authorizations (
			id, authorization_id, tenant_id, merchant_id, purpose, schema_version,
			network_id, network_code, chain_family, network_identifier,
			source_address, fee_payer_address, vault_key_id, destination_address, asset_standard,
			contract_address, amount_minor_units, max_network_fee_minor,
			unsigned_fingerprint, basis_type, basis_id, attempt, generation, network_sequence, expires_at, policy_version,
			approval_bundle_digest, approval_bundle_json, status, authorization_jws, tx_hash,
			unsigned_envelope
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18,
			$19, $20, $21, $22, $23, $24, $25, $26,
			$27, $28::jsonb, 'created', $29, '',
			$30::jsonb
		)
		ON CONFLICT (basis_type, basis_id, purpose, attempt, generation) DO NOTHING
	`, rowID, auth.AuthorizationID, auth.TenantID, merchantID, auth.Purpose, auth.SchemaVersion,
		auth.NetworkID, auth.NetworkCode, auth.ChainFamily, auth.NetworkIdentifier,
		auth.SourceAddress, auth.FeePayerAddress, auth.VaultKeyID, auth.DestinationAddress, auth.AssetStandard,
		auth.ContractAddress, auth.AmountMinorUnits, auth.MaxNetworkFeeMinor,
		auth.UnsignedFingerprint, auth.BasisType, auth.BasisID, auth.Attempt, auth.Generation, auth.NetworkSequence, auth.ExpiresAt, auth.PolicyVersion,
		auth.ApprovalBundleDigest, string(approvalBundleJSON), jws, string(unsignedJSON))
	if err != nil {
		return id.Nil(), fmt.Errorf("insert signing authorization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return id.Nil(), fmt.Errorf("%w for %s %s purpose %s attempt %d generation %d", ErrSigningAuthorizationExists, auth.BasisType, auth.BasisID, auth.Purpose, auth.Attempt, auth.Generation)
	}
	return rowID, nil
}

// SetSigningAuthorizationJWS completes the staged authorization row. The JWS
// is immutable once set; retries may only persist the identical signature.
func (r *OutboundRepo) SetSigningAuthorizationJWS(ctx context.Context, authID, jws string) error {
	if strings.TrimSpace(authID) == "" || strings.TrimSpace(jws) == "" {
		return fmt.Errorf("authorization ID and JWS are required")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET authorization_jws=$2
		WHERE authorization_id=$1 AND status='created'
		  AND (authorization_jws='' OR authorization_jws=$2)
	`, authID, jws)
	if err != nil {
		return fmt.Errorf("set signing authorization JWS: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("signing authorization JWS is immutable or authorization is not signable")
	}
	return nil
}

func (r *OutboundRepo) ClaimStuckReplacementAuthorizations(ctx context.Context, workerID string, limit int, age, lease time.Duration) ([]ReplacementCandidate, error) {
	if strings.TrimSpace(workerID) == "" || limit <= 0 || age <= 0 || lease <= 0 {
		return nil, fmt.Errorf("replacement claim requires worker, limit, age, and lease")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH picked AS (
			SELECT a.id
			FROM reg_crypto_signing_authorizations a
			WHERE a.status='broadcast' AND a.generation <= 3
			  AND BTRIM(COALESCE(a.network_sequence, '')) <> ''
			  AND a.updated_at <= NOW()-($2*interval '1 second')
			  AND (a.replacement_lease_expires_at IS NULL OR a.replacement_lease_expires_at < NOW())
			  AND NOT EXISTS (
			      SELECT 1 FROM reg_crypto_signing_authorizations newer
			      WHERE newer.authorization_id = a.authorization_id AND newer.generation > a.generation
			  )
			ORDER BY a.updated_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE reg_crypto_signing_authorizations target
		SET replacement_claimed_by=$3, replacement_claimed_at=NOW(), replacement_lease_expires_at=NOW()+($4*interval '1 second'), updated_at=NOW()
		FROM picked WHERE target.id=picked.id
		RETURNING target.authorization_id, target.basis_type, target.basis_id, target.purpose, target.attempt, target.generation
	`, limit, int64(age.Seconds()), workerID, int64(lease.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []ReplacementCandidate
	for rows.Next() {
		var candidate ReplacementCandidate
		var purpose string
		if err := rows.Scan(&candidate.AuthorizationID, &candidate.BasisType, &candidate.BasisID, &purpose, &candidate.Attempt, &candidate.Generation); err != nil {
			return nil, err
		}
		candidate.Purpose = crypto.SigningPurpose(purpose)
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (r *OutboundRepo) ReleaseReplacementClaim(ctx context.Context, authorizationID, workerID string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET replacement_claimed_by='',replacement_claimed_at=NULL,replacement_lease_expires_at=NULL,updated_at=NOW()
		WHERE authorization_id=$1 AND replacement_claimed_by=$2
	`, authorizationID, workerID)
	return err
}

func (r *OutboundRepo) ListSigningGenerations(ctx context.Context, candidate ReplacementCandidate) ([]SigningGeneration, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT authorization_id, generation, status, COALESCE(tx_hash,''), unsigned_fingerprint, signed_envelope
		FROM reg_crypto_signing_authorizations
		WHERE authorization_id=$1
		ORDER BY generation ASC
	`, candidate.AuthorizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SigningGeneration
	for rows.Next() {
		var gen SigningGeneration
		var rawSigned []byte
		if err := rows.Scan(&gen.AuthorizationID, &gen.Generation, &gen.Status, &gen.TxHash, &gen.UnsignedFingerprint, &rawSigned); err != nil {
			return nil, err
		}
		if len(rawSigned) > 0 && string(rawSigned) != "null" {
			var signed crypto.SignedTxEnvelope
			if err := json.Unmarshal(rawSigned, &signed); err != nil {
				return nil, err
			}
			gen.Signed = &signed
		}
		list = append(list, gen)
	}
	return list, rows.Err()
}

func (r *OutboundRepo) LoadSigningAsset(ctx context.Context, auth crypto.SigningAuthorization) (crypto.AssetRef, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var asset crypto.AssetRef
	var decimals int
	if auth.AssetStandard == "native" {
		if err := q.QueryRow(ctx, `SELECT native_decimals FROM cat_crypto_networks WHERE id=$1`, auth.NetworkID).Scan(&decimals); err != nil {
			return asset, fmt.Errorf("load native token decimals: %w", err)
		}
		asset.Standard = "native"
		asset.Decimals = decimals
		return asset, nil
	}
	err := q.QueryRow(ctx, `SELECT id, standard, decimals FROM cat_tokens WHERE network_id=$1 AND LOWER(contract_address)=LOWER($2)`, auth.NetworkID, auth.ContractAddress).Scan(&asset.TokenID, &asset.Standard, &asset.Decimals)
	if err != nil {
		return asset, fmt.Errorf("load contract token asset: %w", err)
	}
	return asset, nil
}

func (r *OutboundRepo) MarkSigningAuthorizationSuperseded(ctx context.Context, authorizationID, winnerTxHash string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET status='superseded',replacement_claimed_by='',replacement_claimed_at=NULL,replacement_lease_expires_at=NULL,updated_at=NOW()
		WHERE authorization_id=$1 AND status IN ('signed','broadcast','held') AND tx_hash<>$2
	`, authorizationID, winnerTxHash)
	return err
}

func (r *OutboundRepo) MarkSigningAuthorizationWinner(ctx context.Context, authorizationID, txHash string) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_signing_authorizations
		SET status='broadcast',replacement_claimed_by='',replacement_claimed_at=NULL,replacement_lease_expires_at=NULL,updated_at=NOW()
		WHERE authorization_id=$1 AND tx_hash=$2 AND status IN ('signed','broadcast','superseded','held')
	`, authorizationID, txHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("mined signing generation identity mismatch")
	}
	return nil
}

func (r *OutboundRepo) UpdateOutboundReplacementHash(ctx context.Context, basisType string, basisID id.ID, oldHash, newHash string) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockExecutionResources(ctx, basisType, basisID, nil); err != nil {
			return err
		}
		return r.updateOutboundReplacementHash(ctx, basisType, basisID, oldHash, newHash)
	})
}

func (r *OutboundRepo) updateOutboundReplacementHash(ctx context.Context, basisType string, basisID id.ID, oldHash, newHash string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var tag pgconn.CommandTag
	var err error
	switch basisType {
	case withdrawal_request.DocumentType:
		tag, err = q.Exec(ctx, `UPDATE doc_crypto_withdrawals cw SET tx_hash=$3,updated_at=NOW()
			FROM doc_withdrawal_requests wr WHERE wr.id=$1 AND wr.withdrawal_id=cw.id AND cw.tx_hash=$2`, basisID, oldHash, newHash)
	case _sweepAttemptBasisType:
		tag, err = q.Exec(ctx, `UPDATE doc_crypto_sweeps__lines l SET tx_hash=$3 FROM reg_crypto_sweep_attempts a
			WHERE a.id=$1 AND l.line_id=a.id AND l.document_id=a.sweep_id AND l.tx_hash=$2`, basisID, oldHash, newHash)
	case treasury_withdrawal.DocumentType:
		tag, err = q.Exec(ctx, `UPDATE doc_treasury_withdrawals SET tx_hash=$3,updated_at=NOW() WHERE id=$1 AND tx_hash=$2`, basisID, oldHash, newHash)
	case nativeFundingBasisType:
		tag, err = q.Exec(ctx, `UPDATE doc_crypto_native_fundings SET tx_hash=$3,updated_at=NOW() WHERE id=$1 AND tx_hash=$2`, basisID, oldHash, newHash)
	default:
		return fmt.Errorf("unsupported replacement basis type %s", basisType)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("outbound replacement hash identity mismatch")
	}
	return nil
}
