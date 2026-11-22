//go:build integration

package crypto_worker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/logger"
	"metapus/pkg/sdk/sdktest"

	"metapus/pkg/sdk/telemetry"
	"metapus/pkg/sdk/tenant"
)

type remediationApprovalSigner struct {
	keys map[crypto.ApprovalSigningRole]ed25519.PrivateKey
}

func (s remediationApprovalSigner) SignApprovalJWS(_ context.Context, _ string, role crypto.ApprovalSigningRole, payload any) (string, error) {
	return crypto.SignEdDSAJWS(payload, s.keys[role])
}

type remediationApprovalProvider struct {
	tenantID string
	signer   *crypto.MoneyMovementApprovalSigner
	keys     crypto.ApprovalPublicKeys
}

func (p remediationApprovalProvider) SignerForTenant(tenantID string) (*crypto.MoneyMovementApprovalSigner, error) {
	if tenantID != p.tenantID {
		return nil, fmt.Errorf("unexpected signing tenant")
	}
	return p.signer, nil
}

func (p remediationApprovalProvider) PublicKeysForTenant(tenantID string) (crypto.ApprovalPublicKeys, error) {
	if tenantID != p.tenantID {
		return crypto.ApprovalPublicKeys{}, fmt.Errorf("unexpected signing tenant")
	}
	return p.keys, nil
}

func newRemediationApprovalProvider(t *testing.T, tenantID string) remediationApprovalProvider {
	t.Helper()
	private := make(map[crypto.ApprovalSigningRole]ed25519.PrivateKey)
	public := make(map[crypto.ApprovalSigningRole]ed25519.PublicKey)
	for _, role := range []crypto.ApprovalSigningRole{
		crypto.ApprovalSigningRoleIntent, crypto.ApprovalSigningRoleRiskAssessment,
		crypto.ApprovalSigningRolePolicyDecision, crypto.ApprovalSigningRoleAdminApproval,
		crypto.ApprovalSigningRoleRiskApproval,
	} {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		private[role], public[role] = priv, pub
	}
	policyPub, policyPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys := crypto.ApprovalPublicKeys{
		PolicyPublicKey:        policyPub,
		IntentPublicKey:        public[crypto.ApprovalSigningRoleIntent],
		RiskPublicKey:          public[crypto.ApprovalSigningRoleRiskAssessment],
		DecisionPublicKey:      public[crypto.ApprovalSigningRolePolicyDecision],
		AdminApprovalPublicKey: public[crypto.ApprovalSigningRoleAdminApproval],
		RiskApprovalPublicKey:  public[crypto.ApprovalSigningRoleRiskApproval],
	}
	now := time.Now().UTC()
	policyJWS, err := crypto.SignEdDSAJWS(crypto.DefaultMoneyMovementApprovalPolicy(tenantID, now), policyPriv)
	require.NoError(t, err)
	policy, err := crypto.NewSignedApprovalPolicy(policyJWS, policyPub, now)
	require.NoError(t, err)
	signer, err := crypto.NewMoneyMovementApprovalSigner(remediationApprovalSigner{private}, keys, policy)
	require.NoError(t, err)
	return remediationApprovalProvider{tenantID, signer, keys}
}

func TestRemediationWithdrawalSigningJobCompletesOutsideRequest(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f remediationFixture) {
		ctx = tenant.WithTenant(ctx, &tenant.Tenant{ID: "remediation-signing"})
		q := txm.GetQuerier(ctx)
		addressID, requestID := id.New(), id.New()
		address := "0x1111111111111111111111111111111111111111"
		_, err := q.Exec(ctx, `INSERT INTO reg_withdrawal_addresses
			(id,merchant_id,network_id,address,address_key,label)
			VALUES($1,$2,$3,$4,$4,'Signing test')`, addressID, f.merchant, f.network, address)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO doc_withdrawal_requests
			(id,number,merchant_id,token_id,amount,dest_address,address_id,status,posted,posted_version)
			VALUES($1,$2,$3,$4,200000000,$5,$6,'pending_signature',TRUE,1)`,
			requestID, "WR-"+id.Compact(requestID), f.merchant, f.token, address, addressID)
		require.NoError(t, err)
		repo := money_movement_repo.New()
		intent, err := repo.BuildWithdrawalIntent(ctx, tenant.GetTenantID(ctx), requestID)
		require.NoError(t, err)
		require.NoError(t, portal_repo.NewDashboardRepo().EnqueueApprovalSigning(ctx, intent))
		processor := &CryptoProcessor{
			approvalRepo: repo, approvalSigningProvider: newRemediationApprovalProvider(t, tenant.GetTenantID(ctx)),
			workerID: "signing-integration", log: logger.Default(),
		}
		require.NoError(t, processor.processApprovalSigningJobs(ctx))
		var status, jobStatus string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM doc_withdrawal_requests WHERE id=$1`, requestID).Scan(&status))
		require.Equal(t, "pending_approval", status)
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM reg_money_movement_signing_jobs WHERE basis_id=$1`, requestID).Scan(&jobStatus))
		require.Equal(t, "completed", jobStatus)
		var artifactCount int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_money_movement_intents WHERE basis_id=$1`, requestID).Scan(&artifactCount))
		require.Equal(t, 1, artifactCount)
		require.NoError(t, processor.processApprovalSigningJobs(ctx))
	})
}

func TestRemediationTreasurySigningJobCompletesOutsideRequest(t *testing.T) {
	withRemediationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f remediationFixture) {
		ctx = tenant.WithTenant(ctx, &tenant.Tenant{ID: "remediation-treasury-signing"})
		q := txm.GetQuerier(ctx)
		addressID, withdrawalID := id.New(), id.New()
		address := "0x2222222222222222222222222222222222222222"
		_, err := q.Exec(ctx, `INSERT INTO reg_treasury_withdrawal_addresses
			(id,network_id,address,address_key,label,is_active)
			VALUES($1,$2,$3,$3,'Treasury signing test',TRUE)`, addressID, f.network, address)
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO doc_treasury_withdrawals
			(id,number,token_id,source_wallet_id,dest_address,address_id,amount,status)
			VALUES($1,$2,$3,$4,$5,$6,600000000,'pending_signature')`,
			withdrawalID, "TW-"+id.Compact(withdrawalID), f.token, f.wallet, address, addressID)
		require.NoError(t, err)
		repo := money_movement_repo.New()
		intent, err := repo.BuildTreasuryIntent(ctx, tenant.GetTenantID(ctx), withdrawalID)
		require.NoError(t, err)
		_, err = telemetry.ConfigureTracing(ctx, "remediation-test", "dev", func(string) string { return "" })
		require.NoError(t, err)
		traceID, err := oteltrace.TraceIDFromHex("33333333333333333333333333333333")
		require.NoError(t, err)
		spanID, err := oteltrace.SpanIDFromHex("4444444444444444")
		require.NoError(t, err)
		ctx = oteltrace.ContextWithSpanContext(ctx, oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
			TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled,
		}))
		require.NoError(t, repo.EnqueueApprovalSigning(ctx, intent))
		var savedTraceparent, savedCorrelation, savedCausation string
		require.NoError(t, q.QueryRow(ctx, `SELECT traceparent,correlation_id,causation_id
			FROM reg_money_movement_signing_jobs WHERE basis_id=$1`, withdrawalID).
			Scan(&savedTraceparent, &savedCorrelation, &savedCausation))
		require.Contains(t, savedTraceparent, traceID.String())
		require.Equal(t, intent.IntentID, savedCorrelation)
		require.Equal(t, withdrawalID.String(), savedCausation)
		_, err = q.Exec(ctx, `UPDATE reg_money_movement_signing_jobs
			SET status='needs_review',attempts=8,last_error='temporary signer outage'
			WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1`, withdrawalID)
		require.NoError(t, err)
		jobState, err := repo.GetSigningJobStatus(ctx, "TreasuryWithdrawal", withdrawalID)
		require.NoError(t, err)
		require.Equal(t, "needs_review", jobState.Status)
		actorID := id.New()
		require.NoError(t, repo.ReplaySigningJob(ctx, "TreasuryWithdrawal", withdrawalID, actorID))
		require.Error(t, repo.ReplaySigningJob(ctx, "TreasuryWithdrawal", withdrawalID, actorID))
		var replayCount int
		var previousError string
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*),MAX(previous_error)
			FROM reg_money_movement_signing_job_replays WHERE actor_id=$1`, actorID).Scan(&replayCount, &previousError))
		require.Equal(t, 1, replayCount)
		require.Equal(t, "temporary signer outage", previousError)
		processor := &CryptoProcessor{
			approvalRepo: repo, approvalSigningProvider: newRemediationApprovalProvider(t, tenant.GetTenantID(ctx)),
			workerID: "treasury-signing-integration", log: logger.Default(),
		}
		require.NoError(t, processor.processApprovalSigningJobs(ctx))
		var status, jobStatus string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM doc_treasury_withdrawals WHERE id=$1`, withdrawalID).Scan(&status))
		require.Equal(t, "pending_approval", status)
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM reg_money_movement_signing_jobs WHERE basis_id=$1`, withdrawalID).Scan(&jobStatus))
		require.Equal(t, "completed", jobStatus)
		var artifactCount int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_money_movement_intents WHERE basis_id=$1`, withdrawalID).Scan(&artifactCount))
		require.Equal(t, 1, artifactCount)
		require.NoError(t, processor.processApprovalSigningJobs(ctx))
	})
}
