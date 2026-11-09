package outbound_repo

import (
	"context"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// Always called after the shared workflow/operation -> policy -> wallets -> row prefix.
func validateActivationExecutionClaim(ctx context.Context, task AccountActivationTask) error {
	if id.IsNil(task.ClaimToken) {
		return apperror.NewConflict("activation execution claim is missing or stale")
	}
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM reg_crypto_account_activations a JOIN cat_wallets w ON w.id=a.wallet_id
 JOIN cat_wallets payer ON payer.id=$6 AND payer.network_id=a.network_id
 JOIN cat_tokens t ON t.id=$9 AND t.network_id=a.network_id AND t.token_standard='native'
 WHERE a.id=$1 AND a.network_id=$2 AND a.wallet_id=$3 AND a.activation_claim_token=$4
 AND a.activation_lease_expires_at>clock_timestamp() AND a.activation_claimed_by<>''
 AND a.status IN ('claimed','held','signing','broadcast','confirmed')
 AND w.address=$5 AND payer.address=$7 AND payer.vault_key_id=$8)`, task.ActivationID, task.NetworkID, task.WalletID, task.ClaimToken, task.Address, task.HotWallet.ID, task.HotWallet.Address, task.HotWallet.VaultKeyID, task.CostTokenID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation execution claim is missing or stale")
	}
	return nil
}

func requireUnclaimedActivation(ctx context.Context, key id.ID) error {
	var live bool
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE(activation_lease_expires_at>clock_timestamp(),FALSE) FROM reg_crypto_account_activations WHERE id=$1`, key).Scan(&live); err != nil {
		return err
	}
	if live {
		return apperror.NewConflict("activation is owned by an execution claim")
	}
	return nil
}
func lockActivationCommandState(ctx context.Context, key id.ID, claim *AccountActivationTask) error {
	var extra []id.ID
	if claim != nil {
		extra = append(extra, claim.HotWallet.ID)
	}
	if err := lockActivationState(ctx, key, extra...); err != nil {
		return err
	}
	if claim == nil {
		return requireUnclaimedActivation(ctx, key)
	}
	return validateActivationExecutionClaim(ctx, *claim)
}
func mutateActivationCommandState(ctx context.Context, key id.ID, claim *AccountActivationTask, work func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockActivationCommandState(ctx, key, claim); err != nil {
			return err
		}
		return work(ctx)
	})
}

func (r *OutboundRepo) ApplyAccountActivationClaim(ctx context.Context, task AccountActivationTask, update outboundapp.ActivationClaimUpdate) error {
	switch update.Action {
	case outboundapp.ActivationClaimHeld:
		return r.updateAccountActivationStatus(ctx, task.ActivationID, "held", wallet.ChainActivationHeld, update.Reason, &task)
	case outboundapp.ActivationClaimFailed:
		return r.updateAccountActivationStatus(ctx, task.ActivationID, "failed", wallet.ChainActivationFailed, update.Reason, &task)
	case outboundapp.ActivationClaimSigning:
		return r.markAccountActivationSigning(ctx, task.ActivationID, &task)
	case outboundapp.ActivationClaimExternallyActive:
		return r.markAccountActivationExternallyActive(ctx, task.ActivationID, &task)
	case outboundapp.ActivationClaimDeferred:
		return r.deferAccountActivation(ctx, task.ActivationID, update.Delay, &task)
	default:
		return apperror.NewValidation("unsupported activation claim update")
	}
}

// Accepted transaction identity is evidence, independent of an actor lease.
// The complete prefix and exact persisted signed authorization are required.
func (r *OutboundRepo) PrepareActivationBroadcastRecovery(ctx context.Context, task AccountActivationTask, evidence outboundapp.ActivationBroadcastEvidence) error {
	if err := lockActivationState(ctx, task.ActivationID, task.HotWallet.ID); err != nil {
		return err
	}
	if err := validateActivationBroadcastRecovery(ctx, task, evidence.TxHash); err != nil {
		return err
	}
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s JOIN reg_crypto_fee_reservations f ON f.id=$2 AND f.basis_type=s.basis_type AND f.basis_id=s.basis_id AND f.purpose=s.purpose AND f.attempt=s.attempt
 WHERE s.authorization_id=$1 AND s.basis_type='CryptoAccountActivationTask' AND s.basis_id=$3 AND s.network_id=$4 AND s.purpose='activation'
 AND s.tx_hash=$5 AND s.signed_envelope IS NOT NULL AND s.unsigned_fingerprint=$6 AND f.unsigned_fingerprint=$6
 AND f.network_id=s.network_id AND f.tx_hash IN ('',$5))`, evidence.AuthorizationID, evidence.ReservationID, task.ActivationID, task.NetworkID, evidence.TxHash, evidence.Fingerprint).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation broadcast evidence differs from its original authorization or fee reservation")
	}
	return nil
}
func validateActivationBroadcastRecovery(ctx context.Context, task AccountActivationTask, hash string) error {
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM reg_crypto_account_activations a JOIN cat_wallets target ON target.id=a.wallet_id
 WHERE a.id=$1 AND a.network_id=$2 AND a.wallet_id=$3 AND target.address=$4 AND $5<>'' AND (
 EXISTS(SELECT 1 FROM doc_crypto_network_operations o WHERE o.id=a.network_operation_id AND o.network_id=a.network_id
 AND o.target_wallet_id=a.wallet_id AND o.tx_hash=$5 AND o.basis_type='CryptoAccountActivationTask' AND o.basis_id=a.id)
 OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s JOIN reg_crypto_fee_reservations f
 ON f.basis_type=s.basis_type AND f.basis_id=s.basis_id AND f.purpose=s.purpose AND f.attempt=s.attempt
 JOIN cat_wallets source ON source.id=f.payer_wallet_id AND source.network_id=s.network_id
 WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=a.id AND s.purpose='activation' AND s.network_id=a.network_id
 AND s.tx_hash=$5 AND s.signed_envelope IS NOT NULL AND s.signed_envelope->>'txHash'=$5
 AND s.destination_address=target.address AND s.source_address=source.address AND s.attempt=1 AND s.generation=1 AND s.amount_minor_units=0 AND s.asset_standard='native' AND s.contract_address=''
 AND f.network_id=a.network_id AND f.native_token_id=$6 AND f.operation_token_id=$6 AND f.max_fee_minor=s.max_network_fee_minor AND f.unsigned_fingerprint=s.unsigned_fingerprint AND f.tx_hash IN ('',$5))))`, task.ActivationID, task.NetworkID, task.WalletID, task.Address, hash, task.CostTokenID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation broadcast recovery requires its original signed transaction")
	}
	return nil
}

func (r *OutboundRepo) GetActivationBroadcastEvidence(ctx context.Context, task AccountActivationTask, hash string) (outboundapp.ActivationBroadcastEvidence, bool, error) {
	evidence := outboundapp.ActivationBroadcastEvidence{TxHash: hash}
	var found bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=$1 AND s.purpose='activation' AND s.network_id=$2 AND s.attempt=1 AND s.generation=1 AND s.tx_hash=$3 AND s.signed_envelope IS NOT NULL)`, task.ActivationID, task.NetworkID, hash).Scan(&found)
	if err != nil || !found {
		return evidence, found, err
	}
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT s.authorization_id,f.id,s.unsigned_fingerprint FROM reg_crypto_signing_authorizations s JOIN reg_crypto_fee_reservations f ON f.basis_type=s.basis_type AND f.basis_id=s.basis_id AND f.purpose=s.purpose AND f.attempt=s.attempt
 WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=$1 AND s.purpose='activation' AND s.network_id=$2 AND s.attempt=1 AND s.generation=1 AND s.tx_hash=$3 AND s.signed_envelope IS NOT NULL`, task.ActivationID, task.NetworkID, hash).Scan(&evidence.AuthorizationID, &evidence.ReservationID, &evidence.Fingerprint)
	return evidence, err == nil, err
}

func (r *OutboundRepo) BeginAccountActivationDispatch(ctx context.Context, task AccountActivationTask, auth crypto.SigningAuthorization, phase string) error {
	if phase != "authorize" && phase != "sign" && phase != "broadcast" {
		return apperror.NewValidation("unsupported activation dispatch phase")
	}
	if auth.BasisType != "CryptoAccountActivationTask" || auth.BasisID != task.ActivationID || auth.NetworkID != task.NetworkID || auth.Purpose != crypto.SigningPurposeActivation {
		return apperror.NewConflict("activation dispatch authorization identity differs")
	}
	if auth.DestinationAddress != task.Address || auth.SourceAddress != task.HotWallet.Address || auth.FeePayerAddress != task.HotWallet.Address || auth.VaultKeyID != task.HotWallet.VaultKeyID || !auth.AmountMinorUnits.IsZero() || auth.AssetStandard != "native" || auth.ContractAddress != "" || auth.Attempt != 1 || auth.Generation != 1 {
		return apperror.NewConflict("activation dispatch differs from its original intent")
	}
	return mutateActivationCommandState(ctx, task.ActivationID, &task, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_activation_dispatches(authorization_id,activation_id,phase,claim_token,network_id,unsigned_fingerprint) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, auth.AuthorizationID, task.ActivationID, phase, task.ClaimToken, task.NetworkID, auth.UnsignedFingerprint)
		if err != nil {
			return err
		}
		if phase == "sign" && tag.RowsAffected() != 1 {
			return apperror.NewConflict("activation signing was already dispatched; signed result recovery is required")
		}
		var exact bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_activation_dispatches WHERE authorization_id=$1 AND activation_id=$2 AND phase=$3 AND claim_token=$4 AND network_id=$5 AND unsigned_fingerprint=$6)`, auth.AuthorizationID, task.ActivationID, phase, task.ClaimToken, task.NetworkID, auth.UnsignedFingerprint).Scan(&exact); err != nil {
			return err
		}
		if !exact {
			return apperror.NewConflict("activation dispatch replay differs from its original authorization")
		}
		return nil
	})
}
func (r *OutboundRepo) HasAccountActivationSigningExposure(ctx context.Context, task AccountActivationTask) (bool, error) {
	var exposed bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=$1 AND s.purpose='activation' AND (
 s.signed_envelope IS NOT NULL OR EXISTS(SELECT 1 FROM reg_crypto_activation_dispatches d WHERE d.authorization_id=s.authorization_id AND d.phase='sign')))`, task.ActivationID).Scan(&exposed)
	return exposed, err
}
