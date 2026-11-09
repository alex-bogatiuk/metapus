//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type rolloutControlSigner struct{ t *testing.T }

func (s rolloutControlSigner) SignAuthorization(context.Context, string, crypto.SigningAuthorization) (string, error) {
	s.t.Error("preflight must never sign")
	return "", errors.New("unexpected signature")
}
func (s rolloutControlSigner) CheckReady(ctx context.Context, _ string) error {
	require.Nil(s.t, postgres.MustGetTxManager(ctx).GetTx(ctx), "Vault reads must happen after the SQL snapshot ends")
	return nil
}
func (s rolloutControlSigner) AuthorizationKeyID(context.Context, string) (string, error) {
	return "synthetic-key", nil
}

type rolloutCustodyOnlySigner struct{ t *testing.T }

func (s rolloutCustodyOnlySigner) Family() crypto.ChainFamily { return crypto.ChainFamilyEVM }
func (s rolloutCustodyOnlySigner) CheckReady(context.Context, crypto.NetworkRef, string) error {
	return nil
}
func (s rolloutCustodyOnlySigner) CheckMerchantCustodyReady(context.Context, crypto.NetworkRef) error {
	return nil
}
func (s rolloutCustodyOnlySigner) Sign(context.Context, crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	s.t.Error("preflight must never dispatch to signer")
	return crypto.SignedTxEnvelope{}, errors.New("unexpected signature")
}

type rolloutAMLSigner struct {
	rolloutCustodyOnlySigner
	err    error
	checks *int
}

func (s rolloutAMLSigner) CheckAMLReady(context.Context, crypto.NetworkRef) error {
	*s.checks++
	return s.err
}

func amlRolloutFixture(t *testing.T) (custodyFixture, id.ID, *SigningAuthorizationService) {
	t.Helper()
	f := newCustodyFixture(t)
	f.enforce(t)
	namespace, account := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),'synthetic rollout')`, namespace)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_integration_accounts(id,code,name,provider) VALUES($1,$1::uuid::text,'Synthetic rollout AML','getblock_net')`, account)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_risk_policies(version,low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals) VALUES('rollout-v1',3000,7000,3600,600,ARRAY['wallet'],ARRAY[]::text[])`)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses) VALUES($1,$2,'rollout-v1','synthetic','','ETH','',TRUE,18,TRUE,'synthetic rollout fixture','{"synthetic":true}',ARRAY['PENDING'])`, f.network, f.token)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version) VALUES($1,$2,$3,TRUE,'rollout-v1',$4,'rollout-v1')`, f.a, f.network, f.token, account)
	require.NoError(t, err)
	service, err := NewSigningAuthorizationService(NewOutboundRepo(), rolloutControlSigner{t}, namespace.String())
	require.NoError(t, err)
	return f, namespace, service
}

func TestAMLConfigurationPreflightAndRuntimeRejectSignerVersionSkew(t *testing.T) {
	f, namespace, service := amlRolloutFixture(t)
	network := crypto.NetworkRef{ID: f.network, Family: crypto.ChainFamilyEVM, NetworkIdentifier: f.network.String()}
	old := rolloutCustodyOnlySigner{t}
	require.ErrorContains(t, service.CheckCustodySigner(f.ctx, network, old), "no AML signing contract support")
	checks := 0
	upgraded := rolloutAMLSigner{rolloutCustodyOnlySigner: old, checks: &checks}
	broken := upgraded
	broken.err = errors.New("old plugin https://internal-vault.example/SECRET")
	require.Error(t, service.CheckCustodySigner(f.ctx, network, broken))
	require.NoError(t, service.CheckCustodySigner(f.ctx, network, upgraded))
	for _, tc := range []struct {
		name    string
		signer  crypto.PolicyAwareTransactionSigner
		ready   bool
		blocker string
	}{
		{"custody only", old, false, "aml_signer_contract_unavailable"},
		{"wrong contract", broken, false, "aml_signer_contract_incompatible"},
		{"upgraded", upgraded, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := crypto.NewFamilySignerRegistry()
			require.NoError(t, registry.Register(tc.signer))
			processor := &CryptoProcessor{cfg: CryptoProcessorConfig{TenantID: namespace.String()}, familySigners: registry, authorizations: service}
			report, err := processor.RunAMLConfigurationPreflight(f.ctx, "")
			require.NoError(t, err)
			require.Equal(t, tc.ready, report.Ready)
			encoded, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "internal-vault.example")
			require.NotContains(t, string(encoded), "SECRET")
			require.Len(t, report.Networks, 1)
			require.False(t, report.Networks[0].DispatchEnabled, "preflight must leave network policy paused")
			if tc.blocker != "" {
				require.Equal(t, []string{tc.blocker}, report.Networks[0].Blockers)
			}
		})
	}
	var signatures, jobs int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_signing_authorizations`).Scan(&signatures))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_jobs`).Scan(&jobs))
	require.Zero(t, signatures)
	require.Zero(t, jobs)
}

func TestCustodyOnlySignerRemainsCompatibleBeforeAMLEnablement(t *testing.T) {
	f := newCustodyFixture(t)
	f.enforce(t)
	service, err := NewSigningAuthorizationService(NewOutboundRepo(), rolloutControlSigner{t}, id.New().String())
	require.NoError(t, err)
	require.NoError(t, service.CheckCustodySigner(f.ctx, crypto.NetworkRef{ID: f.network, Family: crypto.ChainFamilyEVM}, rolloutCustodyOnlySigner{t}))
}
