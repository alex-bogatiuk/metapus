//go:build integration

package content

import (
	"context"
	"errors"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/readaccess"
)

// markedFixture runs fn in a transaction that is always rolled back, with
// the full metadata registry, an admin user and the authorized read service.
func markedFixture(t *testing.T, fn func(ctx context.Context, f *markedEnv)) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "a migrated disposable database is required")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	factories := configuration.Compose(Crypto(Services{}))
	registry := v1.BuildReadRegistry(factories)
	ctx = appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}})
	ctx = readaccess.WithService(ctx, postgres.NewAuthorizedReadService(registry, nil))

	rollback := errors.New("rollback marked deletion fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		env := &markedEnv{
			t:      t,
			txm:    txm,
			repo:   postgres.NewMarkedObjectsRepo(registry, postgres.NewEventLogRepo()),
			finder: postgres.NewRefFinderRepo(registry),
		}
		fn(ctx, env)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

type markedEnv struct {
	t      *testing.T
	txm    *sdktest.TxManager
	repo   *postgres.MarkedObjectsRepo
	finder *postgres.RefFinderRepo
}

func (e *markedEnv) exec(ctx context.Context, sql string, args ...any) {
	e.t.Helper()
	_, err := e.txm.GetQuerier(ctx).Exec(ctx, sql, args...)
	require.NoError(e.t, err)
}

func (e *markedEnv) counterparty(ctx context.Context, name string, marked bool, parent *id.ID) domain.MarkedObjectRef {
	uid := id.New()
	e.exec(ctx, `INSERT INTO cat_counterparties(id, code, name, deletion_mark, parent_id) VALUES ($1, $2, $3, $4, $5)`,
		uid, "MD-"+uid.String()[24:], name, marked, parent)
	return domain.MarkedObjectRef{EntityName: "Counterparty", EntityID: uid}
}

func (e *markedEnv) contract(ctx context.Context, name string, marked bool, counterparty domain.MarkedObjectRef) domain.MarkedObjectRef {
	uid := id.New()
	e.exec(ctx, `INSERT INTO cat_contracts(id, code, name, deletion_mark, counterparty_id) VALUES ($1, $2, $3, $4, $5)`,
		uid, "MD-"+uid.String()[24:], name, marked, counterparty.EntityID)
	return domain.MarkedObjectRef{EntityName: "Contract", EntityID: uid}
}

func (e *markedEnv) exists(ctx context.Context, table string, ref domain.MarkedObjectRef) bool {
	e.t.Helper()
	var found bool
	require.NoError(e.t, e.txm.GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = $1)`, ref.EntityID).Scan(&found))
	return found
}

func planItem(t *testing.T, plan domain.DeletionPlan, ref domain.MarkedObjectRef) domain.MarkedObject {
	t.Helper()
	i := slices.IndexFunc(plan.Items, func(item domain.MarkedObject) bool {
		return item.EntityName == ref.EntityName && item.EntityID == ref.EntityID
	})
	require.GreaterOrEqual(t, i, 0, "%s %s is missing from the plan", ref.EntityName, ref.EntityID)
	return plan.Items[i]
}

func outcomeOf(t *testing.T, result domain.DeleteMarkedResult, ref domain.MarkedObjectRef) domain.DeletedObjectResult {
	t.Helper()
	i := slices.IndexFunc(result.Items, func(item domain.DeletedObjectResult) bool {
		return item.EntityName == ref.EntityName && item.EntityID == ref.EntityID
	})
	require.GreaterOrEqual(t, i, 0)
	return result.Items[i]
}

func TestMarkedDeletionBlocksObjectUsedByLiveContract(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		cp := e.counterparty(ctx, "Блокированный контрагент", true, nil)
		ct := e.contract(ctx, "Живой договор", false, cp)

		plan, err := e.repo.PlanDeletion(ctx, nil)
		require.NoError(t, err)
		item := planItem(t, plan, cp)
		require.Contains(t, item.Presentation, "Блокированный контрагент", "marked objects must resolve their names")
		require.Equal(t, domain.DeletionBlocked, item.Status)
		require.Equal(t, domain.BlockLiveReference, item.Reason)
		require.Equal(t, 1, item.RefCount)
		require.Equal(t, 1, item.LiveRefCount)
		require.NotNil(t, item.BlockedBy)
		require.Equal(t, ct.EntityID, item.BlockedBy.SourceID)
		require.Equal(t, "counterpartyId", item.BlockedBy.SourceField)
		require.Contains(t, item.BlockedBy.Presentation, "Живой договор")
		require.False(t, item.BlockedBy.SourceMarked)

		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{cp})
		require.NoError(t, err)
		require.Equal(t, 1, result.Skipped)
		require.Equal(t, domain.BlockLiveReference, outcomeOf(t, result, cp).Reason)
		require.True(t, e.exists(ctx, "cat_counterparties", cp))
	})
}

func TestMarkedDeletionDeletesMarkedChainInOneRun(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		cp := e.counterparty(ctx, "Контрагент цепочки", true, nil)
		ct := e.contract(ctx, "Помеченный договор", true, cp)

		plan, err := e.repo.PlanDeletion(ctx, nil)
		require.NoError(t, err)
		cpItem := planItem(t, plan, cp)
		require.Equal(t, domain.DeletionWithGroup, cpItem.Status)
		require.Len(t, cpItem.References, 1)
		require.True(t, cpItem.References[0].SourceMarked)
		require.Equal(t, domain.DeletionReady, cpItem.References[0].SourceStatus)
		require.Equal(t, domain.DeletionReady, planItem(t, plan, ct).Status)

		// "Найти ссылки" lists the marked contract too.
		found, err := e.finder.FindReferences(ctx, domain.FindReferencesRequest{EntityName: "Counterparty", EntityID: cp.EntityID})
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.True(t, found[0].DeletionMark)

		// The referenced object comes first in the request on purpose.
		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{cp, ct, cp})
		require.NoError(t, err)
		require.Equal(t, 2, result.Deleted, "%+v", result)
		require.Len(t, result.Items, 2)
		require.Contains(t, outcomeOf(t, result, cp).Presentation, "Контрагент цепочки")
		require.False(t, e.exists(ctx, "cat_counterparties", cp))
		require.False(t, e.exists(ctx, "cat_contracts", ct))

		var events int
		require.NoError(t, e.txm.GetQuerier(ctx).QueryRow(ctx, `SELECT count(*) FROM sys_event_log
			WHERE entity_id = ANY($1) AND details->>'operation' = 'delete_marked'`,
			[]id.ID{cp.EntityID, ct.EntityID}).Scan(&events))
		require.Equal(t, 2, events)
	})
}

func TestMarkedDeletionKeepsObjectReferencedByUnselectedMarkedObject(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		cp := e.counterparty(ctx, "Контрагент", true, nil)
		ct := e.contract(ctx, "Невыбранный договор", true, cp)

		plan, err := e.repo.PlanDeletion(ctx, []domain.MarkedObjectRef{cp})
		require.NoError(t, err)
		require.Equal(t, domain.BlockNotSelected, planItem(t, plan, cp).Reason)
		require.Equal(t, domain.DeletionExcluded, planItem(t, plan, ct).Status)
		require.Equal(t, domain.DeletionExcluded, planItem(t, plan, cp).References[0].SourceStatus)

		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{cp})
		require.NoError(t, err)
		require.Equal(t, domain.DeletionOutcomeSkipped, outcomeOf(t, result, cp).Outcome)
		require.True(t, e.exists(ctx, "cat_counterparties", cp))
	})
}

func TestMarkedDeletionHonoursHierarchy(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		group := e.counterparty(ctx, "Группа", true, nil)
		child := e.counterparty(ctx, "Элемент группы", true, &group.EntityID)
		liveGroup := e.counterparty(ctx, "Группа с живым элементом", true, nil)
		e.counterparty(ctx, "Живой элемент", false, &liveGroup.EntityID)

		plan, err := e.repo.PlanDeletion(ctx, nil)
		require.NoError(t, err)
		require.Equal(t, domain.DeletionWithGroup, planItem(t, plan, group).Status)
		require.Equal(t, "parentId", planItem(t, plan, group).References[0].SourceField)
		require.Equal(t, domain.BlockLiveReference, planItem(t, plan, liveGroup).Reason)

		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{group, child, liveGroup})
		require.NoError(t, err)
		require.Equal(t, 2, result.Deleted, "%+v", result)
		require.Equal(t, 1, result.Skipped)
		require.False(t, e.exists(ctx, "cat_counterparties", group))
		require.True(t, e.exists(ctx, "cat_counterparties", liveGroup))
	})
}

func TestMarkedDeletionSeesForeignKeysOutsideMetadata(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		e.exec(ctx, `CREATE TABLE zz_marked_fk_probe (id uuid PRIMARY KEY, counterparty_id uuid REFERENCES cat_counterparties(id))`)
		cp := e.counterparty(ctx, "Используется системой", true, nil)
		e.exec(ctx, `INSERT INTO zz_marked_fk_probe VALUES ($1, $2), ($3, $2)`, id.New(), cp.EntityID, id.New())

		plan, err := e.repo.PlanDeletion(ctx, nil)
		require.NoError(t, err)
		item := planItem(t, plan, cp)
		require.Equal(t, domain.BlockLiveReference, item.Reason)
		require.Equal(t, domain.SourceKindSystem, item.BlockedBy.SourceEntityType)
		require.Equal(t, "zz_marked_fk_probe.counterparty_id", item.BlockedBy.SourceField)
		require.Equal(t, 2, item.BlockedBy.Count)
		require.Equal(t, 2, item.LiveRefCount)
	})
}

func TestMarkedDeletionIsolatesFailingGroup(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		failing := e.counterparty(ctx, "Сломанный", true, nil)
		failingContract := e.contract(ctx, "Договор сломанного", true, failing)
		healthy := e.counterparty(ctx, "Исправный", true, nil)
		e.exec(ctx, `CREATE FUNCTION zz_refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'refused'; END $$`)
		e.exec(ctx, `CREATE TRIGGER zz_refuse_delete BEFORE DELETE ON cat_counterparties
			FOR EACH ROW WHEN (OLD.id = '`+failing.EntityID.String()+`'::uuid) EXECUTE FUNCTION zz_refuse_delete()`)

		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{failing, failingContract, healthy})
		require.NoError(t, err)
		require.Equal(t, 1, result.Deleted, "%+v", result)
		require.Equal(t, 2, result.Errors)
		require.Equal(t, "Не удалось удалить объект", outcomeOf(t, result, failing).Message)
		// The whole group rolls back, the contract included.
		require.True(t, e.exists(ctx, "cat_contracts", failingContract))
		require.True(t, e.exists(ctx, "cat_counterparties", failing))
		require.False(t, e.exists(ctx, "cat_counterparties", healthy))
	})
}

func TestMarkedDeletionSkipsUnmarkedAndUnknownObjects(t *testing.T) {
	markedFixture(t, func(ctx context.Context, e *markedEnv) {
		live := e.counterparty(ctx, "Без пометки", false, nil)
		unknown := domain.MarkedObjectRef{EntityName: "NoSuchEntity", EntityID: id.New()}

		result, err := e.repo.DeleteMarked(ctx, []domain.MarkedObjectRef{live, unknown})
		require.NoError(t, err)
		require.Equal(t, domain.DeletionOutcomeSkipped, outcomeOf(t, result, live).Outcome)
		require.Equal(t, domain.DeletionOutcomeFailed, outcomeOf(t, result, unknown).Outcome)
		require.True(t, e.exists(ctx, "cat_counterparties", live))
	})
}
