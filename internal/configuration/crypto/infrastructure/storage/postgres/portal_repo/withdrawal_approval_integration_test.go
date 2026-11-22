//go:build integration

package portal_repo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"

	"metapus/pkg/sdk/tenant"
)

type integrationApprovalSigner struct {
	privateKeys map[crypto.ApprovalSigningRole]ed25519.PrivateKey
}

func (s integrationApprovalSigner) SignApprovalJWS(_ context.Context, _ string, role crypto.ApprovalSigningRole, payload any) (string, error) {
	return crypto.SignEdDSAJWS(payload, s.privateKeys[role])
}

type integrationApprovalProvider struct {
	tenantID string
	signer   *crypto.MoneyMovementApprovalSigner
	keys     crypto.ApprovalPublicKeys
}

func (p integrationApprovalProvider) SignerForTenant(tenantID string) (*crypto.MoneyMovementApprovalSigner, error) {
	if tenantID != p.tenantID {
		return nil, errors.New("unexpected approval tenant")
	}
	return p.signer, nil
}

func (p integrationApprovalProvider) PublicKeysForTenant(tenantID string) (crypto.ApprovalPublicKeys, error) {
	if tenantID != p.tenantID {
		return crypto.ApprovalPublicKeys{}, errors.New("unexpected approval tenant")
	}
	return p.keys, nil
}

type allowIntegrationStepUp struct{}

func (allowIntegrationStepUp) HasFreshStepUp(context.Context, id.ID, id.ID, string, string, time.Time) (bool, error) {
	return true, nil
}

func TestConcurrentWithdrawalApprovalsCreateOneFrozenTransition(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	tenantID := "withdrawal-approval-integration"
	ctx = tenant.WithTenant(tenant.WithTxManager(ctx, txm), &tenant.Tenant{ID: tenantID})

	merchantID, networkID, tokenID, addressID, requestID := id.New(), id.New(), id.New(), id.New(), id.New()
	suffix := id.Compact(requestID)
	destination := "TWithdrawalApproval" + suffix
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_merchants(id,code,name)
		VALUES($1,$2,'Withdrawal approval race merchant');
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($3,$4,'Withdrawal approval race network','tron',$5,'TRX',6,FALSE);
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($6,$7,'Withdrawal approval race token',$3,$8,$9,6,'TRC-20');
		INSERT INTO reg_withdrawal_addresses(
			id,merchant_id,network_id,address,address_key,label)
		VALUES($10,$1,$3,$11,$11,'external checker destination');
		INSERT INTO doc_withdrawal_requests(
			id,merchant_id,token_id,amount,dest_address,address_id,status,created_by,updated_by)
		VALUES($12,$1,$6,500000000,$11,$10,'pending_approval',$13,$13)
	`, pgx.QueryExecModeSimpleProtocol, merchantID, "WA-MERCHANT-"+suffix,
		networkID, "WA-NET-"+suffix, "wa-"+suffix,
		tokenID, "WA-TOKEN-"+suffix, "TWithdrawalToken"+suffix,
		"WA"+suffix[:6], addressID, destination, requestID, id.New())
	require.NoError(t, err)

	provider := newIntegrationApprovalProvider(t, tenantID)
	movementRepo := money_movement_repo.New()
	approvalService := cryptoapp.NewMoneyMovementApprovalService(movementRepo, provider, allowIntegrationStepUp{})
	intent, err := movementRepo.BuildWithdrawalIntent(ctx, tenantID, requestID)
	require.NoError(t, err)
	artifacts, err := approvalService.BuildArtifacts(ctx, intent)
	require.NoError(t, err)
	require.Contains(t, artifacts.RequiredPrincipals, crypto.ApprovalPrincipalAdmin)
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return approvalService.SaveArtifacts(txCtx, artifacts)
	}))

	service := cryptoapp.NewWithdrawalApprovalService(NewDashboardRepo(), movementRepo, approvalService)
	actors := []id.ID{id.New(), id.New()}
	errorsByActor := make(chan error, len(actors))
	var group sync.WaitGroup
	for _, actorID := range actors {
		group.Add(1)
		go func(actorID id.ID) {
			defer group.Done()
			actorCtx := appctx.WithUser(ctx, &appctx.UserContext{
				UserID: actorID.String(), TenantID: tenantID, SessionID: id.New().String(), IsAdmin: true,
			})
			errorsByActor <- service.Approve(actorCtx, requestID, actorID)
		}(actorID)
	}
	group.Wait()
	close(errorsByActor)

	successes, conflicts := 0, 0
	for approveErr := range errorsByActor {
		if approveErr == nil {
			successes++
			continue
		}
		appErr, ok := apperror.AsAppError(approveErr)
		if ok && appErr.Code == apperror.CodeConflict {
			conflicts++
			continue
		}
		t.Fatalf("unexpected approval result: %v", approveErr)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	var status withdrawal_request.Status
	var version, adminApprovals, riskApprovals, intents int
	var approvedBy id.ID
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT request.status,request.version,request.approved_by,
		       (SELECT COUNT(*) FROM reg_money_movement_intents
		        WHERE basis_type=$2 AND basis_id=request.id),
		       (SELECT COUNT(*) FROM reg_money_movement_approvals
		        WHERE basis_type=$2 AND basis_id=request.id AND principal_type='admin'),
		       (SELECT COUNT(*) FROM reg_money_movement_approvals
		        WHERE basis_type=$2 AND basis_id=request.id AND principal_type='risk')
		FROM doc_withdrawal_requests request WHERE request.id=$1
	`, requestID, withdrawal_request.DocumentType).Scan(
		&status, &version, &approvedBy, &intents, &adminApprovals, &riskApprovals,
	))
	require.Equal(t, withdrawal_request.StatusApproved, status)
	require.Equal(t, 2, version)
	require.Contains(t, actors, approvedBy)
	require.Equal(t, 1, intents)
	require.Equal(t, 1, adminApprovals)
	require.Equal(t, 1, riskApprovals)
}

func newIntegrationApprovalProvider(t *testing.T, tenantID string) integrationApprovalProvider {
	t.Helper()
	roles := []crypto.ApprovalSigningRole{
		crypto.ApprovalSigningRoleIntent,
		crypto.ApprovalSigningRoleRiskAssessment,
		crypto.ApprovalSigningRolePolicyDecision,
		crypto.ApprovalSigningRoleAdminApproval,
		crypto.ApprovalSigningRoleRiskApproval,
	}
	privateKeys := make(map[crypto.ApprovalSigningRole]ed25519.PrivateKey, len(roles))
	publicByRole := make(map[crypto.ApprovalSigningRole]ed25519.PublicKey, len(roles))
	for _, role := range roles {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		privateKeys[role] = privateKey
		publicByRole[role] = publicKey
	}
	policyPublic, policyPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keys := crypto.ApprovalPublicKeys{
		PolicyPublicKey:        policyPublic,
		IntentPublicKey:        publicByRole[crypto.ApprovalSigningRoleIntent],
		RiskPublicKey:          publicByRole[crypto.ApprovalSigningRoleRiskAssessment],
		DecisionPublicKey:      publicByRole[crypto.ApprovalSigningRolePolicyDecision],
		AdminApprovalPublicKey: publicByRole[crypto.ApprovalSigningRoleAdminApproval],
		RiskApprovalPublicKey:  publicByRole[crypto.ApprovalSigningRoleRiskApproval],
	}
	now := time.Now().UTC()
	policyJWS, err := crypto.SignEdDSAJWS(crypto.DefaultMoneyMovementApprovalPolicy(tenantID, now), policyPrivate)
	require.NoError(t, err)
	signedPolicy, err := crypto.NewSignedApprovalPolicy(policyJWS, policyPublic, now)
	require.NoError(t, err)
	signer, err := crypto.NewMoneyMovementApprovalSigner(integrationApprovalSigner{privateKeys: privateKeys}, keys, signedPolicy)
	require.NoError(t, err)
	return integrationApprovalProvider{tenantID: tenantID, signer: signer, keys: keys}
}
