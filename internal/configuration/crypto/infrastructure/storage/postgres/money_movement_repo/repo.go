package money_movement_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type Repo struct{}

func New() *Repo {
	return &Repo{}
}

func (r *Repo) SaveArtifacts(ctx context.Context, basisType string, basisID id.ID, artifacts crypto.MoneyMovementApprovalArtifacts) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	required := make([]string, 0, len(artifacts.RequiredPrincipals))
	for _, principal := range artifacts.RequiredPrincipals {
		required = append(required, string(principal))
	}

	tag, err := q.Exec(ctx, `
		INSERT INTO reg_money_movement_intents (
			basis_type, basis_id, purpose, intent_digest, policy_version, policy_digest,
			policy_bundle_jws, intent_jws, risk_assessment_jws, policy_decision_jws,
			required_principals, matched_rule_ids, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, basisType, basisID, artifacts.Intent.Purpose, artifacts.IntentDigest, artifacts.Intent.PolicyVersion, artifacts.Intent.PolicyDigest,
		artifacts.Bundle.PolicyBundleJWS, artifacts.Bundle.IntentJWS, artifacts.Bundle.RiskAssessmentJWS, artifacts.Bundle.PolicyDecisionJWS,
		required, artifacts.MatchedRuleIDs, artifacts.PolicyDecision.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save money movement intent artifacts: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("money movement intent was not inserted")
	}

	jwsByPrincipal := make(map[crypto.ApprovalPrincipal]string, len(artifacts.Bundle.Approvals))
	for _, approval := range artifacts.Bundle.Approvals {
		jwsByPrincipal[approval.PrincipalType] = approval.JWS
	}
	for _, approval := range artifacts.Approvals {
		jws := jwsByPrincipal[approval.PrincipalType]
		if jws == "" {
			return fmt.Errorf("approval JWS missing for principal %s", approval.PrincipalType)
		}
		if err := r.SaveApproval(ctx, basisType, basisID, artifacts.Intent.PolicyDigest, approval, jws); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) SaveApproval(ctx context.Context, basisType string, basisID id.ID, policyDigest string, approval crypto.MoneyMovementApproval, approvalJWS string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
			INSERT INTO reg_money_movement_approvals (
				basis_type, basis_id, intent_digest, principal_type, principal_id, approval_jws,
				policy_version, policy_digest, policy_decision_digest, expires_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, basisType, basisID, approval.IntentDigest, approval.PrincipalType, approval.PrincipalID, approvalJWS,
		approval.PolicyVersion, policyDigest, approval.PolicyDecisionDigest, approval.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save money movement approval %s: %w", approval.PrincipalType, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("money movement approval %s was not inserted", approval.PrincipalType)
	}
	return nil
}

func (r *Repo) LoadBundle(ctx context.Context, basisType string, basisID id.ID) (crypto.ApprovalBundle, bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var bundle crypto.ApprovalBundle
	err := q.QueryRow(ctx, `
		SELECT policy_bundle_jws, intent_jws, risk_assessment_jws, policy_decision_jws
		FROM reg_money_movement_intents
		WHERE basis_type = $1 AND basis_id = $2
	`, basisType, basisID).Scan(
		&bundle.PolicyBundleJWS,
		&bundle.IntentJWS,
		&bundle.RiskAssessmentJWS,
		&bundle.PolicyDecisionJWS,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return crypto.ApprovalBundle{}, false, nil
		}
		return crypto.ApprovalBundle{}, false, fmt.Errorf("load money movement intent bundle: %w", err)
	}

	rows, err := q.Query(ctx, `
		SELECT principal_type, approval_jws
		FROM reg_money_movement_approvals
		WHERE basis_type = $1 AND basis_id = $2
		ORDER BY principal_type, created_at
	`, basisType, basisID)
	if err != nil {
		return crypto.ApprovalBundle{}, false, fmt.Errorf("load money movement approvals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item crypto.ApprovalBundleApproval
		if err := rows.Scan(&item.PrincipalType, &item.JWS); err != nil {
			return crypto.ApprovalBundle{}, false, fmt.Errorf("scan money movement approval: %w", err)
		}
		bundle.Approvals = append(bundle.Approvals, item)
	}
	if err := rows.Err(); err != nil {
		return crypto.ApprovalBundle{}, false, err
	}
	return bundle, true, nil
}

func (r *Repo) BuildWithdrawalIntent(ctx context.Context, tenantID string, requestID id.ID) (crypto.MoneyMovementIntent, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var intent crypto.MoneyMovementIntent
	intent.SchemaVersion = crypto.MoneyMovementIntentV1
	intent.IntentID = id.New().String()
	intent.TenantID = tenantID
	intent.Purpose = crypto.SigningPurposeWithdrawal
	intent.BasisType = withdrawal_request.DocumentType
	intent.BasisID = requestID
	var merchantID id.ID
	err := q.QueryRow(ctx, `
		SELECT wr.merchant_id,
		       wr.token_id,
		       COALESCE(n.code, '') AS network_code,
		       t.token_standard,
		       COALESCE(t.contract_address, '') AS contract_address,
		       wr.amount,
		       wr.dest_address,
		       wr.address_id
		FROM doc_withdrawal_requests wr
		JOIN cat_tokens t ON t.id = wr.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE wr.id = $1
		  AND wr._deleted_at IS NULL
	`, requestID).Scan(
		&merchantID,
		&intent.TokenID,
		&intent.NetworkCode,
		&intent.TokenStandard,
		&intent.ContractAddress,
		&intent.AmountMinorUnits,
		&intent.DestinationAddress,
		&intent.AddressID,
	)
	if err != nil {
		return intent, fmt.Errorf("build withdrawal money movement intent: %w", err)
	}
	intent.MerchantID = &merchantID
	return intent, nil
}

func (r *Repo) BuildTreasuryIntent(ctx context.Context, tenantID string, withdrawalID id.ID) (crypto.MoneyMovementIntent, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var intent crypto.MoneyMovementIntent
	intent.SchemaVersion = crypto.MoneyMovementIntentV1
	intent.IntentID = id.New().String()
	intent.TenantID = tenantID
	intent.Purpose = crypto.SigningPurposeTreasury
	intent.BasisType = treasury_withdrawal.DocumentType
	intent.BasisID = withdrawalID
	err := q.QueryRow(ctx, `
		SELECT tw.token_id,
		       COALESCE(n.code, '') AS network_code,
		       t.token_standard,
		       COALESCE(t.contract_address, '') AS contract_address,
		       tw.amount,
		       tw.dest_address,
		       tw.address_id,
		       w.address,
		       w.vault_key_id
		FROM doc_treasury_withdrawals tw
		JOIN cat_tokens t ON t.id = tw.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		JOIN cat_wallets w ON w.id = tw.source_wallet_id
		WHERE tw.id = $1
		  AND tw._deleted_at IS NULL
	`, withdrawalID).Scan(
		&intent.TokenID,
		&intent.NetworkCode,
		&intent.TokenStandard,
		&intent.ContractAddress,
		&intent.AmountMinorUnits,
		&intent.DestinationAddress,
		&intent.AddressID,
		&intent.SourceAddress,
		&intent.SourceVaultKeyID,
	)
	if err != nil {
		return intent, fmt.Errorf("build treasury money movement intent: %w", err)
	}
	return intent, nil
}

// BuildTreasuryIntentForCreate resolves immutable catalog metadata before the
// document transaction starts. This keeps remote approval signing outside the
// database transaction while allowing the document and signed artifacts to be
// persisted atomically afterwards.
func (r *Repo) BuildTreasuryIntentForCreate(ctx context.Context, tenantID string, doc *treasury_withdrawal.TreasuryWithdrawal) (crypto.MoneyMovementIntent, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	intent := crypto.MoneyMovementIntent{
		SchemaVersion:      crypto.MoneyMovementIntentV1,
		IntentID:           id.New().String(),
		TenantID:           tenantID,
		Purpose:            crypto.SigningPurposeTreasury,
		BasisType:          treasury_withdrawal.DocumentType,
		BasisID:            doc.ID,
		TokenID:            doc.TokenID,
		AmountMinorUnits:   doc.Amount,
		DestinationAddress: doc.DestAddress,
		AddressID:          doc.AddressID,
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(n.code, ''),
		       t.token_standard,
		       COALESCE(t.contract_address, ''),
		       w.address,
		       w.vault_key_id
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		JOIN cat_wallets w ON w.id = $2 AND w.network_id = t.network_id
		WHERE t.id = $1
	`, doc.TokenID, doc.SourceWalletID).Scan(
		&intent.NetworkCode,
		&intent.TokenStandard,
		&intent.ContractAddress,
		&intent.SourceAddress,
		&intent.SourceVaultKeyID,
	); err != nil {
		return intent, fmt.Errorf("build treasury money movement intent for create: %w", err)
	}
	return intent, nil
}
