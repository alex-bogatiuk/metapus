package outbound_repo

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
)

func lockSigningPublication(ctx context.Context, auth crypto.SigningAuthorization) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var wallets []id.ID
	if err := q.QueryRow(ctx, `SELECT ARRAY(SELECT w.id FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
		WHERE w.network_id=$1 AND w.address_key IN (
		CASE WHEN n.chain_family='evm' THEN lower($2) ELSE $2 END,
		CASE WHEN n.chain_family='evm' THEN lower($3) ELSE $3 END,
		CASE WHEN n.chain_family='evm' THEN lower($4) ELSE $4 END))`, auth.NetworkID, auth.SourceAddress, auth.FeePayerAddress, auth.DestinationAddress).Scan(&wallets); err != nil {
		return err
	}
	if auth.Purpose == crypto.SigningPurposeWithdrawal && auth.BasisType != withdrawal_request.DocumentType && auth.BasisType != "CryptoAMLWithdrawalLine" {
		return apperror.NewValidation("withdrawal authorization requires its request or AML line basis")
	}
	if err := lockExecutionResources(ctx, auth.BasisType, auth.BasisID, wallets); err != nil {
		return err
	}
	var eligible bool
	switch auth.BasisType {
	case withdrawal_request.DocumentType:
		if err := q.QueryRow(ctx, `SELECT status IN ('approved','signing') OR (status='broadcast' AND $2>1) FROM doc_withdrawal_requests WHERE id=$1`, auth.BasisID, auth.Generation).Scan(&eligible); err != nil {
			return err
		}
	case "TreasuryWithdrawal":
		if err := q.QueryRow(ctx, `SELECT settlement_status<>'recorded' AND (status IN ('approved','signing') OR (status='broadcast' AND $2>1)) FROM doc_treasury_withdrawals WHERE id=$1 FOR UPDATE`, auth.BasisID, auth.Generation).Scan(&eligible); err != nil {
			return err
		}
	case "CryptoSweepAttempt":
		if err := q.QueryRow(ctx, `SELECT (status IN ('claimed','resource_ready','signing','held') AND expires_at>NOW()) OR (status='broadcast' AND $2>1) FROM reg_crypto_sweep_attempts WHERE id=$1 FOR UPDATE`, auth.BasisID, auth.Generation).Scan(&eligible); err != nil {
			return err
		}
	case nativeFundingBasisType:
		if err := q.QueryRow(ctx, `SELECT status IN ('created','reserved') OR (status IN ('broadcast','solidified') AND $2>1) FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE`, auth.BasisID, auth.Generation).Scan(&eligible); err != nil {
			return err
		}
	default:
		return nil // AML/profile SQL guards retain their original policy checks.
	}
	if !eligible {
		return apperror.NewConflict("execution cannot publish an authorization from its current state")
	}

	if auth.Generation <= 1 {
		return nil
	}
	// Replacement preserves the exact principal intent and account sequence.
	// A broadcast label alone cannot authorize a fresh or skipped generation.
	var prior bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a
		WHERE a.basis_type=$18 AND a.basis_id=$1 AND a.purpose=$19 AND a.attempt=$2 AND a.generation=$3-1
		AND a.signed_envelope IS NOT NULL AND a.status IN ('signed','broadcast','held','superseded')
		AND (a.tenant_id,a.network_id,a.chain_family,a.network_identifier,a.source_address,a.fee_payer_address,a.vault_key_id,
		 a.destination_address,a.asset_standard,a.contract_address,a.amount_minor_units,a.network_sequence,a.approval_bundle_digest)
		 = ($4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		AND a.merchant_id IS NOT DISTINCT FROM $17::uuid)`, auth.BasisID, auth.Attempt, auth.Generation,
		auth.TenantID, auth.NetworkID, auth.ChainFamily, auth.NetworkIdentifier, auth.SourceAddress, auth.FeePayerAddress, auth.VaultKeyID,
		auth.DestinationAddress, auth.AssetStandard, auth.ContractAddress, auth.AmountMinorUnits, auth.NetworkSequence, auth.ApprovalBundleDigest, auth.MerchantID, auth.BasisType, auth.Purpose).Scan(&prior)
	if err != nil {
		return err
	}
	if !prior {
		return apperror.NewConflict("replacement requires its exact signed predecessor")
	}
	return nil
}
