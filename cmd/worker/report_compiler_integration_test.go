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

	"metapus/internal/content"
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

func TestExtensionReportHTTPAndScheduledAccess(t *testing.T) {
	db := testsupport.NewSecurityTenants(t)[0]
	conn := db.Pool.Config().ConnConfig
	entry := &tenant.Tenant{ID: db.ID.String(), DBHost: conn.Host, DBPort: int(conn.Port), DBName: conn.Database, Status: tenant.StatusActive}
	ctx := tenant.WithTenant(tenant.WithTxManager(t.Context(), db.Tx), entry)
	factories := v1.NewFactoryRegistry()
	content.RegisterPlatform(factories)
	registerProbe(factories)
	sqlDB := stdlib.OpenDBFromPool(db.Pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, err := migration.SyncPermissions(ctx, sqlDB, factories.Permissions())
	require.NoError(t, err)

	exec := func(query string, args ...any) {
		t.Helper()
		tag, err := db.Pool.Exec(ctx, query, args...)
		require.NoError(t, err)
		require.EqualValues(t, 1, tag.RowsAffected())
	}
	_, err = db.Pool.Exec(ctx, `CREATE TABLE cat_report_probe_items (code text NOT NULL, _deleted_at timestamptz)`)
	require.NoError(t, err)
	const itemCode = "PROBE-001"
	exec(`INSERT INTO cat_report_probe_items (code) VALUES ($1)`, itemCode)
	userID, roleID, sessionID := id.New(), id.New(), id.New()
	const email = "reader@example.test"
	exec(`INSERT INTO auth_users (id,email,password_hash,first_name,last_name) VALUES ($1,$2,'unused','Report','Reader')`, userID, email)
	exec(`INSERT INTO auth_roles (id,code,name,description) VALUES ($1,'report_reader','Report reader','')`, roleID)
	exec(`INSERT INTO auth_user_roles (user_id,role_id) VALUES ($1,$2)`, userID, roleID)
	sourcePermission := probeCatalog.Permission() + ":read"
	for _, permission := range []string{probeDataset.Permission, sourcePermission} {
		exec(`INSERT INTO auth_role_permissions (role_id,permission_id) SELECT $1,id FROM auth_permissions WHERE code=$2`, roleID, permission)
	}
	exec(`INSERT INTO auth_sessions (id,user_id,user_auth_version,policy_version,expires_at)
		SELECT $1,u.id,u.auth_version,p.version,now()+interval '1 hour' FROM auth_users u CROSS JOIN auth_policy_state p WHERE u.id=$2`, sessionID, userID)

	policies, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(policies.Stop)
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)
	cfg := tenantdb.DefaultManagerConfig()
	cfg.DBUser, cfg.DBPassword = conn.User, conn.Password
	manager := tenantdb.NewManager(cfg, reportTenantRegistry{entry: entry}, log)
	t.Cleanup(manager.Close)
	tokens := authjwt.NewJWTService(authjwt.DefaultJWTConfig("report integration test"))
	sessions := auth_repo.NewSessionRepo()
	router := v1.NewRouter(v1.RouterConfig{
		Logger: log, TenantManager: manager, Registry: factories, PolicyEngine: policies, Numerator: numerator.Noop(),
		ProfileProvider: security_profile.NewCachedProfileProvider(security_repo.NewProfileRepo(), time.Minute, 4),
		JWTValidator:    auth.NewAccessTokenValidator(tokens, sessions, nil),
	})
	runHTTP := func() *httptest.ResponseRecorder {
		t.Helper()
		state, err := sessions.GetSessionState(ctx, userID, sessionID)
		require.NoError(t, err)
		perms, err := auth_repo.NewUserRepo().LoadPermissions(ctx, userID)
		require.NoError(t, err)
		token, _, err := tokens.GenerateAccessToken(userID.String(), entry.ID, sessionID.String(), email, state.UserAuthVersion, state.PolicyVersion, []string{"report_reader"}, perms, false, nil)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+probeDataset.Key, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", entry.ID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	worker := &MultiTenantWorker{factories: factories}
	engine, err := worker.buildAutomationEngine(policies)
	require.NoError(t, err)
	rule, err := postgres.NewAutomationRuleRepo().Create(ctx, automations.CreateRuleRequest{
		Name: "Extension report", TriggerType: automations.TriggerScheduled, EventType: "cron:0 9 * * *",
		ReactionType: automations.ReactionGenerateReport, NotifSeverity: "info", MessageFormat: "text", IsActive: true,
		ReportConfig: &automations.ReportActionConfig{ExecutorUserID: userID, DatasetKey: probeDataset.Key, PeriodType: automations.PeriodAsOfNow},
	})
	require.NoError(t, err)

	rec := runHTTP()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result compiler.QueryResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result.Items, 1)
	require.Equal(t, itemCode, result.Items[0]["code"])

	require.NoError(t, engine.HandleScheduledReportRule(ctx, *rule, time.Now()))
	var data []byte
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT file_data FROM sys_automation_files WHERE rule_id=$1`, rule.ID).Scan(&data))
	book, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = book.Close() })
	rows, err := book.GetRows(book.GetSheetName(0))
	require.NoError(t, err)
	require.Contains(t, rows, []string{itemCode})

	exec(`DELETE FROM auth_role_permissions WHERE role_id=$1 AND permission_id=(SELECT id FROM auth_permissions WHERE code=$2)`, roleID, sourcePermission)
	rec = runHTTP()
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	var denied apperror.AppError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &denied))
	require.Equal(t, sourcePermission, denied.Details["required_permission"])
	err = engine.HandleScheduledReportRule(ctx, *rule, time.Now())
	require.True(t, apperror.IsForbidden(err), "expected a source permission error, got %v", err)
	jobError, _ := apperror.AsAppError(err)
	require.Equal(t, sourcePermission, jobError.Details["required_permission"])
	var fileCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM sys_automation_files WHERE rule_id=$1`, rule.ID).Scan(&fileCount))
	require.Equal(t, 1, fileCount)
}
