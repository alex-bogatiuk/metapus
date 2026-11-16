package crypto_repo

import (
	"context"
	"fmt"
	"metapus/internal/domain/crypto/cryptointegrations"

	"metapus/internal/core/apperror"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

func (r *AMLRepository) LoadAMLRuntimeConfig(ctx context.Context, job aml.Job) (aml.RuntimeConfig, error) {
	var cfg aml.RuntimeConfig
	if (job.Provider != string(cryptointegrations.GetBlockNet) && job.Provider != aml.TestnetProvider) || job.RequestDigest != job.Request.Digest(job.MappingVersion) {
		return cfg, apperror.NewValidation("invalid pinned AML provider context")
	}
	var encrypted []byte
	var seed *string
	var delay, unknown *int
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT a.id,a.credentials_enc,a.provider,n.chain_family,n.network_identifier,e.seed,e.max_delay_seconds,e.unknown_rate_bp,
 m.version,m.contract_version,m.network_id,m.token_id,m.contract_address,m.currency,m.provider_token_id,m.native,m.pending_statuses,m.failure_statuses
 FROM cat_integration_accounts a JOIN sys_crypto_aml_provider_mappings m ON m.network_id=$2 AND m.token_id=$3 AND m.version=$4 AND m.capability_accepted
 JOIN cat_blockchain_networks n ON n.id=m.network_id
 LEFT JOIN sys_crypto_aml_testnet_emulators e ON e.account_id=a.id AND e.network_id=n.id
 WHERE a.id=$1 AND a.provider=$5 AND a.is_active AND NOT a.deletion_mark AND a._deleted_at IS NULL`, job.ProviderAccountID, job.Request.NetworkID, job.Request.TokenID, job.MappingVersion, job.Provider).Scan(
		&cfg.AccountID, &encrypted, &cfg.Provider, &cfg.NetworkFamily, &cfg.NetworkIdentifier, &seed, &delay, &unknown, &cfg.MappingVersion, &cfg.ContractVersion, &cfg.NetworkID, &cfg.TokenID, &cfg.ContractAddress, &cfg.Currency, &cfg.ProviderTokenID, &cfg.Native, &cfg.PendingStatuses, &cfg.FailureStatuses)
	if err != nil {
		return cfg, fmt.Errorf("load pinned AML account/mapping: %w", err)
	}
	if cfg.Provider == aml.TestnetProvider {
		if seed == nil || delay == nil || unknown == nil || cfg.ContractVersion != aml.TestnetContract {
			return cfg, apperror.NewValidation("emulated AML account has no pinned testnet binding")
		}
		cfg.Emulator = &aml.EmulatorConfig{Seed: *seed, MaxDelaySeconds: *delay, UnknownRateBP: *unknown}
		return cfg, cfg.Emulator.Validate(cfg.NetworkFamily, cfg.NetworkIdentifier)
	}
	key, err := catalog_repo.IntegrationEncryptionKey()
	if err != nil {
		return cfg, err
	}
	raw, err := corecrypto.Decrypt(encrypted, key)
	if err != nil {
		return cfg, apperror.NewInternal(fmt.Errorf("decrypt AML account credentials"))
	}
	var credentials integrations.Credentials
	if json.Unmarshal(raw, &credentials) != nil || credentials.Validate() != nil {
		return cfg, apperror.NewValidation("AML account credentials are unavailable")
	}
	bearer, ok := credentials.Key("default")
	if !ok || len(credentials.Keys) != 1 || credentials.APISecret != "" || credentials.Username != "" || !cryptointegrations.ValidGetBlockNetToken(bearer) {
		return cfg, apperror.NewValidation("AML account requires one bearer token")
	}
	cfg.BearerToken = bearer
	return cfg, nil
}

// AdmitAMLSubmit commits a once-only debit of the local paid-request budget
// before HTTP. Even a crash before HTTP conservatively consumes that admission.
// A retry of this job must never gain another paid wire attempt.
func (r *AMLRepository) AdmitAMLSubmit(ctx context.Context, job aml.Job) error {
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var allowed bool
		if err := q.QueryRow(ctx, `SELECT p.allow_submissions FROM cat_integration_accounts a
 JOIN sys_crypto_aml_account_policies p ON p.account_id=a.id
 WHERE a.id=$1 AND a.provider='getblock_net' AND a.is_active AND NOT a.deletion_mark AND a._deleted_at IS NULL FOR UPDATE OF a,p`, job.ProviderAccountID).Scan(&allowed); err != nil {
			return fmt.Errorf("lock AML account admission: %w", err)
		}
		if !allowed {
			return apperror.NewConflict("AML paid submissions are disabled")
		}
		var active bool
		if err := q.QueryRow(ctx, `SELECT status='submitting' AND lease_token=$2 AND lease_expires_at>NOW() AND
 provider_account_id=$3 AND request_digest=$4 AND mapping_version=$5 FROM reg_crypto_aml_jobs WHERE id=$1 FOR UPDATE`, job.ID, job.LeaseToken, job.ProviderAccountID, job.RequestDigest, job.MappingVersion).Scan(&active); err != nil {
			return err
		}
		if !active {
			return apperror.NewConflict("AML submit lease is stale")
		}
		var available bool
		if err := q.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(units) FROM reg_crypto_aml_package_credits WHERE account_id=$1),0)>
            (SELECT COUNT(*) FROM reg_crypto_aml_paid_admissions WHERE account_id=$1)`, job.ProviderAccountID).Scan(&available); err != nil {
			return err
		}
		if !available {
			return apperror.NewConflict("AML paid-request budget is exhausted")
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_paid_admissions(job_id,account_id) VALUES($1,$2) ON CONFLICT(job_id) DO NOTHING`, job.ID, job.ProviderAccountID)
		if err != nil {
			return fmt.Errorf("persist AML paid admission: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML job has already consumed its paid admission")
		}
		return nil
	})
}
