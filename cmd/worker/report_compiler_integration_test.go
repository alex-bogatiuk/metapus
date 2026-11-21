//go:build integration

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"metapus/internal/application/automation"
	"metapus/internal/configuration"
	"metapus/internal/configuration/none"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/automations"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/authjwt"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/auth_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/security_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/pkg/logger"
)

type reportTenantRegistry struct {
	tenant.Registry
	entry *tenant.Tenant
}

func (r reportTenantRegistry) GetByID(_ context.Context, tenantID string) (*tenant.Tenant, error) {
	if tenantID != r.entry.ID {
		return nil, tenant.ErrTenantNotFound
	}
	return r.entry, nil
}

const (
	itemOfA = "PROBE-A"
	itemOfB = "PROBE-B"
)

// reportFixture is a tenant with one probe item for each of merchants A and
// B, the server's HTTP router and the worker's automation engine.
type reportFixture struct {
	db                   *testsupport.SecurityTenant
	ctx                  context.Context
	entry                *tenant.Tenant
	router               http.Handler
	engine               *automation.Engine
	tokens               *authjwt.JWTService
	sessions             *auth_repo.SessionRepo
	roleID               id.ID
	merchantA, merchantB id.ID
}

type reportReader struct{ userID, sessionID id.ID }

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	db := testsupport.NewSecurityTenants(t)[0]
	conn := db.Pool.Config().ConnConfig
	entry := &tenant.Tenant{ID: db.ID.String(), DBHost: conn.Host, DBPort: int(conn.Port), DBName: conn.Database, Status: tenant.StatusActive}
	f := &reportFixture{db: db, entry: entry, ctx: tenant.WithTenant(tenant.WithTxManager(t.Context(), db.Tx), entry),
		roleID: id.New(), merchantA: id.New(), merchantB: id.New()}
	factories := configuration.Compose(none.New(), registerProbe)
	sqlDB := stdlib.OpenDBFromPool(db.Pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, err := migration.SyncPermissions(f.ctx, sqlDB, factories.Permissions())
	require.NoError(t, err)

	_, err = db.Pool.Exec(f.ctx, `CREATE TABLE cat_report_probe_items (code text NOT NULL, merchant_id uuid NOT NULL, _deleted_at timestamptz)`)
	require.NoError(t, err)
	f.exec(t, `INSERT INTO cat_merchants (id,code,name) VALUES ($1,'A','Merchant A'),($2,'B','Merchant B')`, 2, f.merchantA, f.merchantB)
	f.exec(t, `INSERT INTO cat_report_probe_items (code,merchant_id) VALUES ($1,$2),($3,$4)`, 2, itemOfA, f.merchantA, itemOfB, f.merchantB)
	f.exec(t, `INSERT INTO auth_roles (id,code,name,description) VALUES ($1,'report_reader','Report reader','')`, 1, f.roleID)
	for _, permission := range []string{probeDataset.Permission, probeSourcePermission()} {
		f.exec(t, `INSERT INTO auth_role_permissions (role_id,permission_id) SELECT $1,id FROM auth_permissions WHERE code=$2`, 1, f.roleID, permission)
	}

	policies, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(policies.Stop)
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)
	cfg := tenantdb.DefaultManagerConfig()
	cfg.DBUser, cfg.DBPassword = conn.User, conn.Password
	manager := tenantdb.NewManager(cfg, reportTenantRegistry{entry: entry}, log)
	t.Cleanup(manager.Close)
	f.tokens = authjwt.NewJWTService(authjwt.DefaultJWTConfig("report integration test"))
	f.sessions = auth_repo.NewSessionRepo()
	f.router = v1.NewRouter(v1.RouterConfig{
		Logger: log, TenantManager: manager, Registry: factories, PolicyEngine: policies, Numerator: numerator.Noop(),
		ProfileProvider: security_profile.NewCachedProfileProvider(security_repo.NewProfileRepo(), time.Minute, 4),
		JWTValidator:    auth.NewAccessTokenValidator(f.tokens, f.sessions, nil),
	})
	worker := &MultiTenantWorker{factories: factories}
	f.engine, err = worker.buildAutomationEngine(policies)
	require.NoError(t, err)
	return f
}

func probeSourcePermission() string { return probeCatalog.Permission() + ":read" }

func (f *reportFixture) exec(t *testing.T, query string, rows int64, args ...any) {
	t.Helper()
	tag, err := f.db.Pool.Exec(f.ctx, query, args...)
	require.NoError(t, err)
	require.Equal(t, rows, tag.RowsAffected())
}

// reader is a non-admin user with the report and source permissions. A nil
// profile leaves the user without a security profile; a non-nil one, even
// empty, limits the merchant dimension to it. portal lists the merchants
// whose portal the user may open.
func (f *reportFixture) reader(t *testing.T, profile []id.ID, portal ...id.ID) reportReader {
	t.Helper()
	r := reportReader{userID: id.New(), sessionID: id.New()}
	f.exec(t, `INSERT INTO auth_users (id,email,password_hash,first_name,last_name) VALUES ($1,$2,'unused','Report','Reader')`, 1, r.userID, f.email(r))
	f.exec(t, `INSERT INTO auth_user_roles (user_id,role_id) VALUES ($1,$2)`, 1, r.userID, f.roleID)
	if profile != nil {
		profileID := id.New()
		allowed := make([]string, 0, len(profile))
		for _, merchant := range profile {
			allowed = append(allowed, merchant.String())
		}
		f.exec(t, `INSERT INTO auth_security_profiles (id,code,name,description) VALUES ($1,$2,'Merchant reader','')`, 1, profileID, "reader_"+id.Compact(profileID))
		f.exec(t, `INSERT INTO auth_security_profile_dimensions (profile_id,dimension_name,allowed_ids) VALUES ($1,'merchant',$2)`, 1, profileID, allowed)
		f.exec(t, `INSERT INTO auth_user_security_profiles (user_id,profile_id) VALUES ($1,$2)`, 1, r.userID, profileID)
	}
	for _, merchant := range portal {
		f.exec(t, `INSERT INTO sys_merchant_users (user_id,merchant_id,role) VALUES ($1,$2,3)`, 1, r.userID, merchant)
	}
	f.exec(t, `INSERT INTO auth_sessions (id,user_id,user_auth_version,policy_version,expires_at)
		SELECT $1,u.id,u.auth_version,p.version,now()+interval '1 hour' FROM auth_users u CROSS JOIN auth_policy_state p WHERE u.id=$2`, 1, r.sessionID, r.userID)
	return r
}

func (f *reportFixture) email(r reportReader) string {
	return "reader-" + id.Compact(r.userID) + "@example.test"
}

func (f *reportFixture) runHTTP(t *testing.T, r reportReader) *httptest.ResponseRecorder {
	t.Helper()
	state, err := f.sessions.GetSessionState(f.ctx, r.userID, r.sessionID)
	require.NoError(t, err)
	perms, err := auth_repo.NewUserRepo().LoadPermissions(f.ctx, r.userID)
	require.NoError(t, err)
	token, _, err := f.tokens.GenerateAccessToken(r.userID.String(), f.entry.ID, r.sessionID.String(), f.email(r), state.UserAuthVersion, state.PolicyVersion, []string{"report_reader"}, perms, false, nil)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+probeDataset.Key, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant-ID", f.entry.ID)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// httpItems returns the item codes the report shows the reader over HTTP.
func (f *reportFixture) httpItems(t *testing.T, r reportReader) []string {
	t.Helper()
	rec := f.runHTTP(t, r)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result compiler.QueryResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	items := []string{}
	for _, item := range result.Items {
		items = append(items, item["code"].(string))
	}
	return items
}

func (f *reportFixture) schedule(t *testing.T, r reportReader) *automations.Rule {
	t.Helper()
	rule, err := postgres.NewAutomationRuleRepo().Create(f.ctx, automations.CreateRuleRequest{
		Name: "Extension report", TriggerType: automations.TriggerScheduled, EventType: "cron:0 9 * * *",
		ReactionType: automations.ReactionGenerateReport, NotifSeverity: "info", MessageFormat: "text", IsActive: true,
		// An empty report is delivered too, so every run leaves an XLSX to read.
		ReportConfig: &automations.ReportActionConfig{ExecutorUserID: r.userID, DatasetKey: probeDataset.Key, PeriodType: automations.PeriodAsOfNow, IncludeEmptyReport: true},
	})
	require.NoError(t, err)
	return rule
}

// scheduledItems runs the rule once and returns the item codes in its XLSX.
func (f *reportFixture) scheduledItems(t *testing.T, rule *automations.Rule) []string {
	t.Helper()
	require.NoError(t, f.engine.HandleScheduledReportRule(f.ctx, *rule, time.Now()))
	var data []byte
	require.NoError(t, f.db.Pool.QueryRow(f.ctx, `SELECT file_data FROM sys_automation_files WHERE rule_id=$1 ORDER BY created_at DESC LIMIT 1`, rule.ID).Scan(&data))
	book, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = book.Close() })
	rows, err := book.GetRows(book.GetSheetName(0))
	require.NoError(t, err)
	items := []string{}
	for _, row := range rows {
		if len(row) > 0 && strings.HasPrefix(row[0], "PROBE-") {
			items = append(items, row[0])
		}
	}
	return items
}

func TestExtensionReportHTTPAndScheduledAccess(t *testing.T) {
	f := newReportFixture(t)
	reader := f.reader(t, nil)
	rule := f.schedule(t, reader)
	require.ElementsMatch(t, []string{itemOfA, itemOfB}, f.httpItems(t, reader))
	require.ElementsMatch(t, []string{itemOfA, itemOfB}, f.scheduledItems(t, rule))

	sourcePermission := probeSourcePermission()
	f.exec(t, `DELETE FROM auth_role_permissions WHERE role_id=$1 AND permission_id=(SELECT id FROM auth_permissions WHERE code=$2)`, 1, f.roleID, sourcePermission)
	rec := f.runHTTP(t, reader)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var denied apperror.AppError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &denied))
	require.Equal(t, sourcePermission, denied.Details["required_permission"])
	err := f.engine.HandleScheduledReportRule(f.ctx, *rule, time.Now())
	require.True(t, apperror.IsForbidden(err), "expected a source permission error, got %v", err)
	jobError, _ := apperror.AsAppError(err)
	require.Equal(t, sourcePermission, jobError.Details["required_permission"])
	var fileCount int
	require.NoError(t, f.db.Pool.QueryRow(f.ctx, `SELECT count(*) FROM sys_automation_files WHERE rule_id=$1`, rule.ID).Scan(&fileCount))
	require.Equal(t, 1, fileCount)
}

// The rows of an ERP report come from the reader's security profile, the same
// over HTTP and in a scheduled XLSX. Portal access to a merchant neither
// widens nor narrows them.
func TestExtensionReportRowsFollowTheSecurityProfileNotPortalAccess(t *testing.T) {
	f := newReportFixture(t)
	for _, tc := range []struct {
		name    string
		profile []id.ID
		portal  []id.ID
		want    []string
	}{
		{"profile A, portal A and B", []id.ID{f.merchantA}, []id.ID{f.merchantA, f.merchantB}, []string{itemOfA}},
		{"profile A and B, portal A", []id.ID{f.merchantA, f.merchantB}, []id.ID{f.merchantA}, []string{itemOfA, itemOfB}},
		{"explicitly empty profile, portal A", []id.ID{}, []id.ID{f.merchantA}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := f.reader(t, tc.profile, tc.portal...)
			require.ElementsMatch(t, tc.want, f.httpItems(t, reader), "over HTTP")
			require.ElementsMatch(t, tc.want, f.scheduledItems(t, f.schedule(t, reader)), "in the scheduled XLSX")
		})
	}
}
