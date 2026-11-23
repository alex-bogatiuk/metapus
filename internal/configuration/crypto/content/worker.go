package content

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/crypto_provider_account"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	chainbootstrap "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/bootstrap"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/resources/itrx"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/webhook"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto/webhooksecret"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/envconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/secretfile"
)

// resourceSettings tune TRON resource rental for every tenant's crypto
// processor. They are read once at startup so a bad value stops the worker
// instead of being replaced by a default inside a tenant goroutine.
type resourceSettings struct {
	LeaseTargetEnergy int64
	SafetyMarginPct   int64
	ReservationCutoff time.Duration
}

func defaultResourceSettings() resourceSettings {
	return resourceSettings{LeaseTargetEnergy: 65000, SafetyMarginPct: 10, ReservationCutoff: 120 * time.Second}
}

func loadResourceSettings() (resourceSettings, error) {
	defaults := defaultResourceSettings()
	env := envconfig.New()
	settings := resourceSettings{
		LeaseTargetEnergy: nonNegativeInt64(env, "CRYPTO_RESOURCE_LEASE_TARGET_ENERGY", defaults.LeaseTargetEnergy),
		SafetyMarginPct:   nonNegativeInt64(env, "CRYPTO_RESOURCE_SAFETY_MARGIN_PCT", defaults.SafetyMarginPct),
		ReservationCutoff: time.Duration(nonNegativeInt64(env, "CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS", int64(defaults.ReservationCutoff/time.Second))) * time.Second,
	}
	if err := env.Err(); err != nil {
		return resourceSettings{}, err
	}
	return settings, nil
}

func nonNegativeInt64(env *envconfig.Reader, key string, fallback int64) int64 {
	value := env.Int64(key, fallback)
	if value < 0 {
		env.Fail(key, fmt.Errorf("must not be negative, got %d", value))
		return fallback
	}
	return value
}

// validateProductionVaultEnvironment rejects static Vault tokens and local
// signing keys in production: the crypto worker signs through Vault Proxy.
func validateProductionVaultEnvironment(getenv func(string) string) error {
	if !strings.EqualFold(strings.TrimSpace(getenv("APP_ENV")), "production") {
		return nil
	}
	if strings.TrimSpace(getenv("VAULT_TOKEN")) != "" {
		return fmt.Errorf("VAULT_TOKEN is forbidden in production; use tokenless Vault Proxy mode")
	}
	if strings.TrimSpace(getenv("VAULT_TOKEN_FILE")) != "" {
		return fmt.Errorf("VAULT_TOKEN_FILE is forbidden in production; use tokenless Vault Proxy mode")
	}
	if strings.TrimSpace(getenv("CRYPTO_SIGNING_AUTH_PRIVATE_KEY")) != "" {
		return fmt.Errorf("CRYPTO_SIGNING_AUTH_PRIVATE_KEY is forbidden in production; use Vault Transit")
	}
	if strings.TrimSpace(getenv("CRYPTO_SIGNING_AUTH_PRIVATE_KEY_FILE")) != "" {
		return fmt.Errorf("CRYPTO_SIGNING_AUTH_PRIVATE_KEY_FILE is forbidden in production; use Vault Transit")
	}
	mode := strings.ToLower(strings.TrimSpace(getenv("CRYPTO_AUTH_SIGNER")))
	if mode != "" && mode != "transit" {
		return fmt.Errorf("production CRYPTO_AUTH_SIGNER must be transit or empty")
	}
	if mode == "transit" && strings.TrimSpace(getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("VAULT_ADDR is required for production Transit signing")
	}
	if strings.TrimSpace(getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("VAULT_ADDR is required for production webhook secrets")
	}
	return nil
}

// workerSettings are the crypto configuration's worker settings.
type workerSettings struct {
	resources  resourceSettings
	processing bool
}

// setting reads key, or the file key_FILE names, with a fallback.
// loadWorkerSettings has checked that every key it reads resolves.
func (workerSettings) setting(key, fallback string) string {
	value, _ := secretfile.Get(key, fallback)
	return value
}

func loadWorkerSettings(getenv func(string) string) (workerSettings, error) {
	if err := validateProductionVaultEnvironment(getenv); err != nil {
		return workerSettings{}, err
	}
	resources, err := loadResourceSettings()
	if err != nil {
		return workerSettings{}, err
	}
	settings := workerSettings{resources: resources}
	for _, key := range []string{
		"APP_ENV", "CRYPTO_CHAIN_CONFIG_FILE", "VAULT_ADDR", "VAULT_TOKEN", "CRYPTO_AUTH_SIGNER", "CRYPTO_TRANSIT_MOUNT",
		"CRYPTO_LOCAL_AUTH_KID", "CRYPTO_SIGNING_AUTH_PRIVATE_KEY", "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY",
		"CRYPTO_INTENT_SIGNING_PUBLIC_KEY", "CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY", "CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY",
		"CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY", "CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY",
		"CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", "CRYPTO_APPROVAL_POLICY_BUNDLES_JWS", "WEBHOOK_SECRET_KV_MOUNT", "CRYPTO_PROCESSING_ENABLED",
	} {
		if _, err := secretfile.Get(key, ""); err != nil {
			return workerSettings{}, fmt.Errorf("invalid configuration for %s: %w", key, err)
		}
	}
	settings.processing, err = strconv.ParseBool(settings.setting("CRYPTO_PROCESSING_ENABLED", "false"))
	if err != nil {
		return workerSettings{}, fmt.Errorf("invalid CRYPTO_PROCESSING_ENABLED: %w", err)
	}
	return settings, nil
}

// Worker builds the crypto configuration's part of the background worker:
// public checkout references and merchant webhooks from the outbox, webhook
// delivery, the crypto processor when CRYPTO_PROCESSING_ENABLED is set, and
// the cleanup of retired webhook signing secrets.
func (cryptoConfiguration) Worker(_ context.Context, env configuration.WorkerEnv) (configuration.Worker, error) {
	settings, err := loadWorkerSettings(os.Getenv)
	if err != nil {
		return configuration.Worker{}, err
	}

	// Webhook signing secrets are resolved from Vault at each delivery attempt.
	var webhookSecrets crypto.WebhookSecretStore
	if vaultAddr := strings.TrimSpace(settings.setting("VAULT_ADDR", "")); vaultAddr != "" {
		client, err := vaultclient.New(vaultclient.Config{Addr: vaultAddr, Token: strings.TrimSpace(settings.setting("VAULT_TOKEN", ""))})
		if err != nil {
			return configuration.Worker{}, fmt.Errorf("create webhook Vault client: %w", err)
		}
		webhookSecrets, err = webhooksecret.NewVaultStore(client, settings.setting("WEBHOOK_SECRET_KV_MOUNT", "secret"))
		if err != nil {
			return configuration.Worker{}, fmt.Errorf("create webhook secret store: %w", err)
		}
	}
	checkoutReferences := crypto_worker.NewInvoiceCheckoutHandler(crypto_repo.NewCheckoutReferenceRepo(env.MetaPool))
	log := env.Logger

	return configuration.Worker{Tenant: func(_ context.Context, tenantEnv configuration.TenantEnv) (configuration.TenantWorker, error) {
		merchantWebhookRepo := crypto_repo.NewMerchantWebhookRepo()
		merchantWebhooks := crypto_worker.NewMerchantWebhookHandler(
			webhook.NewDispatcher(), crypto_repo.NewWebhookDeliveryRepo(), merchantWebhookRepo, webhookSecrets,
		)
		delivery := crypto_worker.NewWebhookDeliveryWorker(merchantWebhookRepo, merchantWebhooks, tenantEnv.Jobs)
		part := configuration.TenantWorker{
			CheckSchema: checkWorkerSchema,
			Outbox: []configuration.OutboxRoute{
				{Job: "outbox.checkout", AggregateType: crypto_worker.InvoiceCheckoutAggregateType, Handler: checkoutReferences},
				{Job: "outbox.webhook_jobs", AggregateType: crypto.MerchantWebhookAggregateType, Handler: merchantWebhookRoute{merchantWebhooks}},
			},
			Components: []configuration.Component{{
				Name: "webhook_delivery",
				Run: func(ctx context.Context, _ func(bool)) error {
					delivery.Start(ctx)
					return ctx.Err()
				},
			}},
		}
		if webhookSecrets != nil {
			part.Cleanups = append(part.Cleanups, configuration.Cleanup{Job: "cleanup.webhook_secrets", Run: func(ctx context.Context) (int, error) {
				return merchantWebhooks.CleanupRetiredSecrets(ctx, 100)
			}})
		}
		if settings.processing {
			part.Components = append(part.Components, configuration.Component{
				Name: "crypto", Supervised: true,
				Run: func(ctx context.Context, ready func(bool)) error {
					return settings.processor(tenantEnv, ready, log).Start(ctx)
				},
			})
		}
		return part, nil
	}}, nil
}

// processor builds one tenant's crypto processor from the validated settings.
func (s workerSettings) processor(env configuration.TenantEnv, ready func(bool), log *logger.Logger) *cryptoProcessor {
	return &cryptoProcessor{settings: s, env: env, ready: ready, log: log}
}

// cryptoProcessor defers building the chain drivers to each start, so a bad
// chain configuration is retried by the supervisor instead of stopping the
// worker.
type cryptoProcessor struct {
	settings workerSettings
	env      configuration.TenantEnv
	ready    func(bool)
	log      *logger.Logger
}

func (p *cryptoProcessor) Start(ctx context.Context) error {
	s := p.settings
	appEnv := s.setting("APP_ENV", "development")
	driverBuilder, err := chainbootstrap.NewBuilder(s.setting("CRYPTO_CHAIN_CONFIG_FILE", ""), appEnv)
	if err != nil {
		return err
	}
	return crypto_worker.NewCryptoProcessor(crypto_worker.CryptoProcessorConfig{
		Ready:                          p.ready,
		AppEnv:                         appEnv,
		DriverRegistryBuilder:          driverBuilder,
		VaultAddr:                      s.setting("VAULT_ADDR", ""),
		VaultToken:                     s.setting("VAULT_TOKEN", ""),
		AuthorizationSigner:            s.setting("CRYPTO_AUTH_SIGNER", ""),
		TransitMount:                   s.setting("CRYPTO_TRANSIT_MOUNT", "transit"),
		LocalAuthorizationKID:          s.setting("CRYPTO_LOCAL_AUTH_KID", ""),
		SigningAuthorizationPrivateKey: s.setting("CRYPTO_SIGNING_AUTH_PRIVATE_KEY", ""),
		ApprovalPolicyPublicKey:        s.setting("CRYPTO_APPROVAL_POLICY_PUBLIC_KEY", ""),
		IntentSigningPublicKey:         s.setting("CRYPTO_INTENT_SIGNING_PUBLIC_KEY", ""),
		RiskAssessmentSigningPublicKey: s.setting("CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY", ""),
		PolicyDecisionSigningPublicKey: s.setting("CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY", ""),
		AdminApprovalSigningPublicKey:  s.setting("CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY", ""),
		RiskApprovalSigningPublicKey:   s.setting("CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY", ""),
		ApprovalPolicyBundleJWS:        s.setting("CRYPTO_APPROVAL_POLICY_BUNDLE_JWS", ""),
		ApprovalPolicyBundlesJWS:       s.setting("CRYPTO_APPROVAL_POLICY_BUNDLES_JWS", ""),
		TenantID:                       p.env.Tenant.ID,
		JobRecorder:                    p.env.Jobs,
		AccountProviderFactory:         accountProviderFactory(appEnv),
		ResourceLeaseTargetEnergy:      s.resources.LeaseTargetEnergy,
		ResourceSafetyMarginPct:        s.resources.SafetyMarginPct,
		ResourceReservationCutoff:      s.resources.ReservationCutoff,
	}, p.log).Start(ctx)
}

func accountProviderFactory(appEnv string) crypto_worker.AccountProviderFactory {
	return func(ctx context.Context, providerCode string, credsJSON []byte) (crypto.ExternalResourceProvider, error) {
		switch providerCode {
		case crypto.ResourceProviderITRX:
			var creds crypto_provider_account.ProviderCredentials
			if err := json.Unmarshal(credsJSON, &creds); err != nil {
				return nil, fmt.Errorf("unmarshal itrx credentials: %w", err)
			}
			return itrx.New(itrx.Config{
				BaseURL:     creds.BaseURL,
				APIKey:      creds.APIKey,
				APISecret:   creds.APISecret,
				CallbackURL: creds.CallbackURL,
				AppEnv:      appEnv,
			})
		default:
			return nil, fmt.Errorf("unsupported resource provider: %s", providerCode)
		}
	}
}

// merchantWebhookRoute retries merchant webhook jobs on the webhook schedule.
type merchantWebhookRoute struct {
	*crypto_worker.MerchantWebhookHandler
}

func (merchantWebhookRoute) BackoffFor(*postgres.OutboxMessage) postgres.BackoffStrategy {
	return postgres.WebhookBackoff()
}

func (merchantWebhookRoute) RetryPolicyFor(msg *postgres.OutboxMessage) postgres.RetryPolicy {
	return postgres.WebhookRetryPolicy(msg)
}

// checkWorkerSchema requires the leased webhook delivery queue.
func checkWorkerSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var ready bool
	err := pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sys_webhook_delivery_jobs' AND column_name IN ('claim_token','lease_until'))=2
  AND to_regclass('uq_webhook_delivery_jobs_processing_merchant') IS NOT NULL`).Scan(&ready)
	if err != nil {
		return fmt.Errorf("validate crypto worker schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("crypto worker webhook delivery schema is missing; migrate the tenant database first")
	}
	return nil
}
