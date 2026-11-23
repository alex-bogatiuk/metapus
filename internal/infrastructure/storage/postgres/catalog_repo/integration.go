package catalog_repo

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/secretbox"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

type IntegrationAccountRepo struct {
	*BaseCatalogRepo[*integrations.Account]
	consumers []integrations.PoolConsumer
}

// NewIntegrationAccountRepo checks credential changes against the consumers
// of the account's pools.
func NewIntegrationAccountRepo(consumers ...integrations.PoolConsumer) *IntegrationAccountRepo {
	return &IntegrationAccountRepo{NewCatalogRepoForDefinition[*integrations.Account](integrations.AccountDefinition, postgres.ExtractDBColumns[integrations.Account](), func() *integrations.Account { return &integrations.Account{} }), consumers}
}
func (r *IntegrationAccountRepo) Prepare(ctx context.Context, a *integrations.Account, definition integrations.ProviderDefinition) error {
	if _, err := time.LoadLocation(a.ResetTimezone); err != nil {
		return apperror.NewValidation("Неизвестный часовой пояс квоты").WithDetail("field", "resetTimezone")
	}
	if err := LockIntegrationConfiguration(ctx); err != nil {
		return err
	}
	// Provider identity cannot change under existing pools.
	var count int
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT count(*) FROM sys_integration_pool_accounts m JOIN cat_integration_pools p ON p.id=m.pool_id WHERE m.account_id=$1 AND p.provider<>$2`, a.ID, a.Provider).Scan(&count)
	if err != nil {
		return fmt.Errorf("check integration account references: %w", err)
	}
	if a.DeletionMark {
		var used bool
		if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_integration_pool_accounts WHERE account_id=$1)`, a.ID).Scan(&used); err != nil {
			return err
		}
		if used {
			return apperror.NewValidation("Сначала исключите аккаунт из пулов")
		}
	}
	if count > 0 {
		return apperror.NewValidation("Провайдер аккаунта не соответствует использующим его пулам")
	}
	if definition.ManualPeriod() {
		previous := &integrations.Account{}
		err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT credentials_enc,credentials_version,credentials_expires_at,manual_period_started_at,manual_period_version,version FROM cat_integration_accounts WHERE id=$1`, a.ID).Scan(&previous.CredentialsEnc, &previous.CredentialsVersion, &previous.CredentialsExpiresAt, &previous.ManualPeriodStartedAt, &previous.ManualPeriodVersion, &previous.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			previous = nil
		} else if err != nil {
			return fmt.Errorf("load current credential period: %w", err)
		}
		sameKey := false
		if previous != nil {
			if previous.Version != a.Version {
				return apperror.NewConcurrentModification("integration_account", a.ID.String())
			}
			a.CredentialsVersion = previous.CredentialsVersion
			if len(previous.CredentialsEnc) > 0 && a.PendingCredentials != nil && len(a.PendingCredentials.Keys) > 0 {
				old, err := OpenCredentials(previous.CredentialsEnc)
				if err != nil {
					return err
				}
				next := a.PendingCredentials.Keys[0]
				if value, ok := old.Key(next.Name); ok {
					sameKey = subtle.ConstantTimeCompare([]byte(value), []byte(next.Value)) == 1
				}
			}
		}
		if err := a.PrepareManualPeriod(definition, previous, sameKey, time.Now().UTC()); err != nil {
			return err
		}
	}
	if a.PendingCredentials == nil {
		return nil
	}
	// Even clearing credentials needs the key: the account stays usable only
	// on an installation that can seal its next credentials.
	if err := secretbox.Ready(); err != nil {
		return err
	}
	if len(a.PendingCredentials.Keys) == 0 && a.PendingCredentials.APISecret == "" && a.PendingCredentials.Username == "" {
		a.CredentialsEnc = nil
	} else {
		raw, err := json.Marshal(a.PendingCredentials)
		if err != nil {
			return fmt.Errorf("encode integration credentials: %w", err)
		}
		enc, err := secretbox.Seal(raw)
		if err != nil {
			return fmt.Errorf("encrypt integration credentials: %w", err)
		}
		a.CredentialsEnc = enc
	}
	a.CredentialsVersion++
	now := time.Now().UTC()
	a.CredentialsUpdatedAt = &now
	a.PendingCredentials = nil
	return nil
}

type IntegrationPoolRepo struct {
	*BaseCatalogRepo[*integrations.Pool]
	consumers []integrations.PoolConsumer
}

// NewIntegrationPoolRepo keeps pools usable by their consumers: a used pool
// cannot be deleted or change provider, and its members must satisfy them.
func NewIntegrationPoolRepo(consumers ...integrations.PoolConsumer) *IntegrationPoolRepo {
	return &IntegrationPoolRepo{NewCatalogRepoForDefinition[*integrations.Pool](integrations.PoolDefinition, postgres.ExtractDBColumns[integrations.Pool](), func() *integrations.Pool { return &integrations.Pool{AccountIDs: []id.ID{}} }), consumers}
}

// poolInUse reports whether exchange rate sources or a consumer read through the pool.
func (r *IntegrationPoolRepo) poolInUse(ctx context.Context, poolID id.ID) (bool, error) {
	var used bool
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_rate_sources WHERE integration_pool_id=$1 AND NOT deletion_mark)`, poolID).Scan(&used); err != nil || used {
		return used, err
	}
	for _, consumer := range r.consumers {
		if used, err := consumer.UsesPool(ctx, poolID); err != nil || used {
			return used, err
		}
	}
	return false, nil
}
func (r *IntegrationPoolRepo) ValidateMembers(ctx context.Context, p *integrations.Pool) error {
	if err := LockIntegrationConfiguration(ctx); err != nil {
		return err
	}
	used, err := r.poolInUse(ctx, p.ID)
	if err != nil {
		return err
	}
	var incompatible bool
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_integration_pools WHERE id=$1 AND provider<>$2)`, p.ID, p.Provider).Scan(&incompatible); err != nil {
		return err
	}
	if used && (p.DeletionMark || incompatible) {
		return apperror.NewValidation("Сначала измените подключения, использующие пул")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	for _, uid := range p.AccountIDs {
		var valid bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=$1 AND provider=$2 AND NOT deletion_mark AND _deleted_at IS NULL AND NOT is_folder)`, uid, p.Provider).Scan(&valid); err != nil {
			return fmt.Errorf("validate pool account: %w", err)
		}
		if !valid {
			return apperror.NewValidation("Все аккаунты пула должны принадлежать выбранному провайдеру и быть доступны")
		}
	}
	if p.DeletionMark {
		return nil
	}
	for _, consumer := range r.consumers {
		if err := consumer.ValidatePool(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
func (r *IntegrationPoolRepo) saveMembers(ctx context.Context, p *integrations.Pool) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `DELETE FROM sys_integration_pool_accounts WHERE pool_id=$1`, p.ID); err != nil {
		return fmt.Errorf("replace pool members: %w", err)
	}
	for i, uid := range p.AccountIDs {
		if _, err := q.Exec(ctx, `INSERT INTO sys_integration_pool_accounts(pool_id,account_id,position) VALUES($1,$2,$3)`, p.ID, uid, i); err != nil {
			return fmt.Errorf("save pool member: %w", err)
		}
	}
	return nil
}
func (r *IntegrationPoolRepo) Create(ctx context.Context, p *integrations.Pool) error {
	if err := r.BaseCatalogRepo.Create(ctx, p); err != nil {
		return err
	}
	return r.saveMembers(ctx, p)
}
func (r *IntegrationPoolRepo) Update(ctx context.Context, p *integrations.Pool) error {
	if err := r.BaseCatalogRepo.Update(ctx, p); err != nil {
		return err
	}
	return r.saveMembers(ctx, p)
}
func (r *IntegrationPoolRepo) GetByID(ctx context.Context, uid id.ID) (*integrations.Pool, error) {
	p, err := r.BaseCatalogRepo.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return p, r.loadMembers(ctx, []*integrations.Pool{p})
}
func (r *IntegrationPoolRepo) GetByCode(ctx context.Context, code string) (*integrations.Pool, error) {
	p, err := r.BaseCatalogRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return p, r.loadMembers(ctx, []*integrations.Pool{p})
}
func (r *IntegrationPoolRepo) List(ctx context.Context, f domain.ListFilter) (domain.CursorListResult[*integrations.Pool], error) {
	res, err := r.BaseCatalogRepo.List(ctx, f)
	if err != nil {
		return res, err
	}
	return res, r.loadMembers(ctx, res.Items)
}
func (r *IntegrationPoolRepo) loadMembers(ctx context.Context, pools []*integrations.Pool) error {
	if len(pools) == 0 {
		return nil
	}
	ids := make([]id.ID, 0, len(pools))
	byID := make(map[id.ID]*integrations.Pool, len(pools))
	for _, p := range pools {
		ids = append(ids, p.ID)
		p.AccountIDs = []id.ID{}
		byID[p.ID] = p
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT pool_id,account_id FROM sys_integration_pool_accounts WHERE pool_id=ANY($1) ORDER BY pool_id,position`, ids)
	if err != nil {
		return fmt.Errorf("load pool members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pid, aid id.ID
		if err := rows.Scan(&pid, &aid); err != nil {
			return fmt.Errorf("scan pool member: %w", err)
		}
		byID[pid].AccountIDs = append(byID[pid].AccountIDs, aid)
	}
	return rows.Err()
}

// Catalog mutations share a short tenant-local lock to validate cross-catalog profiles.
// Runtime selection uses its own pool-local row locks and never holds this lock over HTTP.
func LockIntegrationConfiguration(ctx context.Context) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('metapus_integration_configuration'))`)
	return err
}

func (r *IntegrationAccountRepo) SetDeletionMark(ctx context.Context, uid id.ID, marked bool) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		if marked {
			var used bool
			if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_integration_pool_accounts WHERE account_id=$1)`, uid).Scan(&used); err != nil {
				return err
			}
			if used {
				return apperror.NewValidation("Сначала исключите аккаунт из пулов")
			}
		} else {
			a, err := r.GetByID(ctx, uid)
			if err != nil {
				return err
			}
			a.DeletionMark = false
			if err := r.validateUniqueKeys(ctx, a); err != nil {
				return err
			}
		}
		return r.BaseCatalogRepo.SetDeletionMark(ctx, uid, marked)
	})
}
func (r *IntegrationPoolRepo) SetDeletionMark(ctx context.Context, uid id.ID, marked bool) error {
	if marked {
		if err := LockIntegrationConfiguration(ctx); err != nil {
			return err
		}
		used, err := r.poolInUse(ctx, uid)
		if err != nil {
			return err
		}
		if used {
			return apperror.NewValidation("Сначала измените подключения, использующие пул")
		}
	}
	return r.BaseCatalogRepo.SetDeletionMark(ctx, uid, marked)
}
