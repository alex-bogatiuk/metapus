package tenant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvisionerPublishesOnlyAfterMigration(t *testing.T) {
	repo := newFakeProvisioningRegistry()
	db := &fakeDatabaseAdmin{created: true}
	migrator := &fakeSchemaMigrator{version: 50}
	svc := NewProvisioner(repo, db, migrator)

	result, err := svc.Provision(context.Background(), CreateTenantInput{
		Slug:        "acme-eu",
		DisplayName: "ACME EU",
	})

	require.NoError(t, err)
	require.True(t, result.DatabaseCreated)
	require.False(t, result.AlreadyProvisioned)
	require.Equal(t, StatusActive, result.Tenant.Status)
	require.Equal(t, 50, result.Tenant.SchemaVersion)
	require.Regexp(t, `^mt_[0-9a-f]{32}$`, result.Tenant.DBName)
	require.Equal(t, 1, db.calls)
	require.Equal(t, 1, migrator.calls)
}

func TestProvisionerRecordsDatabaseFailureAndResumes(t *testing.T) {
	repo := newFakeProvisioningRegistry()
	db := &fakeDatabaseAdmin{err: errors.New("permission denied")}
	migrator := &fakeSchemaMigrator{version: 50}
	svc := NewProvisioner(repo, db, migrator)

	result, err := svc.Provision(context.Background(), CreateTenantInput{
		Slug:        "acme-eu",
		DisplayName: "ACME EU",
	})
	require.ErrorContains(t, err, "permission denied")
	require.NotNil(t, result.Tenant)
	failed, getErr := repo.GetBySlug(context.Background(), "acme-eu")
	require.NoError(t, getErr)
	require.Equal(t, StatusProvisionFailed, failed.Status)
	require.Equal(t, ProvisioningStageDatabase, failed.ProvisioningStage)
	require.Contains(t, failed.ProvisioningError, "permission denied")
	require.Equal(t, 0, migrator.calls)

	db.err = nil
	db.created = false
	resumed, err := svc.Provision(context.Background(), CreateTenantInput{
		Slug:        "acme-eu",
		DisplayName: "ACME EU",
	})
	require.NoError(t, err)
	require.False(t, resumed.DatabaseCreated)
	require.Equal(t, result.Tenant.ID, resumed.Tenant.ID)
	require.Equal(t, StatusActive, resumed.Tenant.Status)
	require.Equal(t, 1, migrator.calls)
}

func TestProvisionerDoesNotPublishMigrationFailure(t *testing.T) {
	repo := newFakeProvisioningRegistry()
	db := &fakeDatabaseAdmin{created: true}
	migrator := &fakeSchemaMigrator{err: errors.New("broken migration")}
	svc := NewProvisioner(repo, db, migrator)

	_, err := svc.Provision(context.Background(), CreateTenantInput{
		Slug:        "acme-eu",
		DisplayName: "ACME EU",
	})

	require.ErrorContains(t, err, "broken migration")
	failed, getErr := repo.GetBySlug(context.Background(), "acme-eu")
	require.NoError(t, getErr)
	require.Equal(t, StatusProvisionFailed, failed.Status)
	require.Equal(t, ProvisioningStageMigration, failed.ProvisioningStage)
	require.Equal(t, 1, db.calls)
	require.Equal(t, 1, migrator.calls)
}

func TestProvisionerSerializesConcurrentAttempts(t *testing.T) {
	repo := newFakeProvisioningRegistry()
	db := &fakeDatabaseAdmin{created: true}
	migrator := &fakeSchemaMigrator{version: 50}
	svc := NewProvisioner(repo, db, migrator)
	input := CreateTenantInput{Slug: "acme-eu", DisplayName: "ACME EU"}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := svc.Provision(context.Background(), input)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, 1, db.calls)
	require.Equal(t, 1, migrator.calls)
}

func TestProvisionerRepairsLegacyActiveTenantWithoutDatabase(t *testing.T) {
	repo := newFakeProvisioningRegistry()
	existing := &Tenant{
		ID:          "0195d5a8-f32a-7bc8-a901-1234567890ab",
		Slug:        "legacy-eu",
		DisplayName: "Legacy EU",
		DBName:      "mt_legacy-eu",
		DBHost:      "localhost",
		DBPort:      5432,
		Status:      StatusActive,
		Plan:        PlanStandard,
	}
	require.NoError(t, repo.Create(context.Background(), existing))
	db := &fakeDatabaseAdmin{created: true, exists: false}
	migrator := &fakeSchemaMigrator{version: 50}
	svc := NewProvisioner(repo, db, migrator)

	result, err := svc.Provision(context.Background(), CreateTenantInput{
		Slug:        "legacy-eu",
		DisplayName: "Legacy EU",
	})

	require.NoError(t, err)
	require.Equal(t, "mt_legacy-eu", result.Tenant.DBName)
	require.Equal(t, StatusActive, result.Tenant.Status)
	require.Equal(t, 1, db.calls)
	require.Equal(t, 1, migrator.calls)
}

type fakeProvisioningRegistry struct {
	mu     sync.Mutex
	bySlug map[string]*Tenant
	byID   map[string]*Tenant
}

func newFakeProvisioningRegistry() *fakeProvisioningRegistry {
	return &fakeProvisioningRegistry{
		bySlug: make(map[string]*Tenant),
		byID:   make(map[string]*Tenant),
	}
}

func (r *fakeProvisioningRegistry) WithProvisioningLock(
	ctx context.Context,
	_ string,
	fn func(context.Context) error,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(ctx)
}

func (r *fakeProvisioningRegistry) GetByID(_ context.Context, tenantID string) (*Tenant, error) {
	t, ok := r.byID[tenantID]
	if !ok {
		return nil, ErrTenantNotFound
	}
	return t, nil
}

func (r *fakeProvisioningRegistry) GetBySlug(_ context.Context, slug string) (*Tenant, error) {
	t, ok := r.bySlug[strings.ToLower(slug)]
	if !ok {
		return nil, ErrTenantNotFound
	}
	return t, nil
}

func (r *fakeProvisioningRegistry) ListActive(context.Context) ([]*Tenant, error) { return nil, nil }
func (r *fakeProvisioningRegistry) ListAll(context.Context) ([]*Tenant, error)    { return nil, nil }

func (r *fakeProvisioningRegistry) Create(_ context.Context, t *Tenant) error {
	r.bySlug[strings.ToLower(t.Slug)] = t
	r.byID[t.ID] = t
	return nil
}

func (r *fakeProvisioningRegistry) SetProvisioningStage(
	_ context.Context,
	tenantID string,
	stage ProvisioningStage,
) error {
	t, ok := r.byID[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = StatusProvisioning
	t.ProvisioningStage = stage
	t.ProvisioningError = ""
	return nil
}

func (r *fakeProvisioningRegistry) MarkProvisioningFailed(
	_ context.Context,
	tenantID string,
	stage ProvisioningStage,
	message string,
) error {
	t, ok := r.byID[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = StatusProvisionFailed
	t.ProvisioningStage = stage
	t.ProvisioningError = message
	return nil
}

func (r *fakeProvisioningRegistry) MarkProvisioned(_ context.Context, tenantID string, version int) error {
	t, ok := r.byID[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = StatusActive
	t.SchemaVersion = version
	t.ProvisioningStage = ProvisioningStageComplete
	t.ProvisioningError = ""
	return nil
}

func (r *fakeProvisioningRegistry) UpdateStatusByID(_ context.Context, tenantID string, status Status) error {
	t, ok := r.byID[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.Status = status
	return nil
}

func (r *fakeProvisioningRegistry) UpdateSchemaVersion(_ context.Context, tenantID string, version int) error {
	t, ok := r.byID[tenantID]
	if !ok {
		return ErrTenantNotFound
	}
	t.SchemaVersion = version
	return nil
}

type fakeDatabaseAdmin struct {
	created bool
	err     error
	calls   int
	exists  bool
}

func (a *fakeDatabaseAdmin) EnsureDatabase(context.Context, string) (bool, error) {
	a.calls++
	if a.err == nil {
		a.exists = true
	}
	return a.created, a.err
}

func (a *fakeDatabaseAdmin) DatabaseExists(context.Context, string) (bool, error) {
	return a.exists, a.err
}

type fakeSchemaMigrator struct {
	version int
	err     error
	calls   int
}

func (m *fakeSchemaMigrator) Migrate(context.Context, *Tenant) (int, error) {
	m.calls++
	return m.version, m.err
}
