//go:build integration

package search_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/search"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"metapus/internal/metadata"
)

type testSearchItem struct {
	entity.Catalog
	OrganizationID *id.ID `db:"organization_id" json:"organizationId"`
	Address        string `db:"address" json:"address"`
}

type testSearchDoc struct {
	entity.Document
	TotalAmount types.MinorUnits `db:"total_amount" json:"totalAmount"`
	CurrencyID  id.ID            `db:"currency_id" json:"currencyId"`
}

func TestAuthorizedSupplementalReadMatrixAcrossTenants(t *testing.T) {
	tenants := testsupport.NewSecurityTenants(t)
	reg := metadata.NewRegistry()
	def := metadata.Inspect(testSearchItem{}, "Wallet", metadata.TypeCatalog)
	def.Key, def.TableName, def.RoutePrefix, def.ReadPermission = "wallet", "test_search_items", "wallets", "wallet:read"
	def.Presentation = metadata.Presentation{Singular: "Кошелек"}
	def.RLSDimensions = map[string]string{"organization": "organization_id"}
	def.PreviewFields = []metadata.PreviewFieldDef{{Name: "address", Column: "address", Label: "Address"}}
	reg.Register(def)
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	reads := postgres.NewAuthorizedReadService(reg, engine)
	svc := search.NewService(reg, reads)
	resolver := postgres.NewRefResolverRepo(reg)
	visible, denied, nullOrg, hidden, missing := id.New(), id.New(), id.New(), id.New(), id.New()
	orgA, orgB := id.New(), id.New()
	for i, tenant := range tenants {
		ctx := context.Background()
		_, err := tenant.Pool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS test_search_items (
				id UUID PRIMARY KEY,
				code VARCHAR(50) NOT NULL,
				name VARCHAR(255) NOT NULL,
				organization_id UUID,
				address TEXT,
				parent_id UUID,
				is_folder BOOLEAN NOT NULL DEFAULT FALSE,
				deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
				version INT NOT NULL DEFAULT 1,
				attributes JSONB NOT NULL DEFAULT '{}',
				_deleted_at TIMESTAMPTZ,
				_txid BIGINT NOT NULL DEFAULT 0
			);
			CREATE TABLE IF NOT EXISTS test_search_docs (
				id UUID PRIMARY KEY,
				number VARCHAR(50) NOT NULL,
				date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				organization_id UUID,
				counterparty_id UUID,
				merchant_id UUID,
				currency_id UUID,
				created_by UUID,
				updated_by UUID,
				total_amount BIGINT NOT NULL DEFAULT 0,
				description TEXT NOT NULL DEFAULT '',
				supplier_doc_number TEXT NOT NULL DEFAULT '',
				posted BOOLEAN NOT NULL DEFAULT FALSE,
				deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
				version INT NOT NULL DEFAULT 1,
				attributes JSONB NOT NULL DEFAULT '{}',
				_deleted_at TIMESTAMPTZ,
				_txid BIGINT NOT NULL DEFAULT 0,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				posted_version INT NOT NULL DEFAULT 0,
				basis_type TEXT NOT NULL DEFAULT '',
				basis_id UUID
			);
		`)
		require.NoError(t, err)
		_, err = tenant.Pool.Exec(ctx, `INSERT INTO cat_organizations(id,code,name) VALUES($1,'OA','Org A'),($2,'OB','Org B')`, orgA, orgB)
		require.NoError(t, err)
		for _, row := range []struct {
			id         id.ID
			code, name string
			org        *id.ID
		}{
			{visible, "VISIBLE", "Needle allowed", &orgA}, {denied, "DENIED", "Needle denied", &orgB},
			{nullOrg, "NULL", "Needle nullable", nil}, {hidden, "HIDDEN", "Secretneedle", &orgA},
		} {
			name := row.name
			if i == 1 {
				name = "Second tenant " + name
			}
			_, err = tenant.Pool.Exec(ctx, `INSERT INTO test_search_items(id,code,name,organization_id,address) VALUES($1,$2,$3,$4,'secret-address')`, row.id, row.code, name, row.org)
			require.NoError(t, err)
		}
	}
	reader := appctx.UserContext{UserID: id.New().String(), Permissions: []string{"wallet:read"}}
	base := readaccess.WithService(tenants[0].Context(reader), reads)
	cases := []struct {
		name    string
		ctx     context.Context
		allowed bool
		status  int
	}{
		{"unassigned profile", base, true, http.StatusOK},
		{"no permission", readaccess.WithService(tenants[0].Context(appctx.UserContext{UserID: id.New().String()}), reads), false, http.StatusForbidden},
		{"matching RLS", security.WithDataScope(base, &security.DataScope{Dimensions: map[string][]string{"organization": {orgA.String()}}}), true, http.StatusOK},
		{"other RLS", security.WithDataScope(base, &security.DataScope{Dimensions: map[string][]string{"organization": {orgB.String()}}}), false, http.StatusNotFound},
		{"empty RLS", security.WithDataScope(base, &security.DataScope{Dimensions: map[string][]string{"organization": {}}}), false, http.StatusNotFound},
		{"CEL deny", security.WithPolicyRules(base, []security.PolicyRule{&security_profile.PolicyRule{ID: id.New(), EntityName: "wallet", Actions: []string{"read"}, Expression: "doc.code == 'VISIBLE'", Effect: "deny", Enabled: true}}), false, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reads.ReadOne(tc.ctx, "wallet", visible)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Equal(t, tc.status, apperror.GetHTTPStatus(err))
			}
			preview, err := svc.Preview(tc.ctx, "catalog", "wallet", visible.String())
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, "Needle allowed", preview.Title)
			} else {
				require.Equal(t, tc.status, apperror.GetHTTPStatus(err))
			}
			batch, err := resolver.ResolveRefs(tc.ctx, []domain.RefResolveRequest{{RefType: "Wallet", RefID: visible}, {RefType: "Wallet", RefID: missing}, {RefType: "Wallet", RefID: visible}})
			require.NoError(t, err)
			if tc.allowed {
				require.Len(t, batch, 1)
				require.Equal(t, visible, batch[0].RefID)
			} else {
				require.Empty(t, batch)
			}
			result, err := svc.Search(tc.ctx, "VISIBLE", 10)
			require.NoError(t, err)
			if tc.allowed {
				require.Len(t, result.Results, 1)
				require.Equal(t, visible.String(), result.Results[0].EntityID)
			} else {
				require.Empty(t, result.Results)
			}
		})
	}
	t.Run("nullable dimensions never bypass RLS", func(t *testing.T) {
		for _, ids := range [][]string{{}, {orgA.String()}} {
			ctx := security.WithDataScope(base, &security.DataScope{Dimensions: map[string][]string{"organization": ids}})
			_, err := reads.ReadOne(ctx, "wallet", nullOrg)
			require.True(t, apperror.IsNotFound(err), "%v", err)
		}
	})
	t.Run("hidden field cannot be searched or presented", func(t *testing.T) {
		ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"wallet:read": {AllowedFields: []string{"*", "-name", "-address"}}})
		result, err := svc.Search(ctx, "Secretneedle", 10)
		require.NoError(t, err)
		require.Empty(t, result.Results)
		preview, err := svc.Preview(ctx, "catalog", "wallet", hidden.String())
		require.NoError(t, err)
		require.Equal(t, "Кошелек", preview.Title)
		require.NotContains(t, preview.Fields, search.PreviewField{Label: "Address", Value: "secret-address"})
		generic := postgres.NewReferenceResolver()
		generic.Add("test_search_items", hidden)
		refs, err := generic.Resolve(ctx)
		require.NoError(t, err)
		require.Empty(t, refs.Get("test_search_items", hidden).Name)
	})
	t.Run("tenant connection defines the data boundary", func(t *testing.T) {
		other := readaccess.WithService(tenants[1].Context(reader), reads)
		preview, err := svc.Preview(other, "catalog", "wallet", visible.String())
		require.NoError(t, err)
		require.Equal(t, "Second tenant Needle allowed", preview.Title)
	})
	t.Run("monetary preview and refs use independently authorized currency units", func(t *testing.T) {
		moneyRegistry := metadata.NewRegistry()
		currencyDef := metadata.Inspect(currency.Currency{}, "Currency", metadata.TypeCatalog)
		currencyDef.Key, currencyDef.TableName, currencyDef.ReadPermission = "currency", "cat_currencies", "currency:read"
		moneyRegistry.Register(currencyDef)
		documentDef := metadata.Inspect(testSearchDoc{}, "CryptoInvoice", metadata.TypeDocument)
		documentDef.Key, documentDef.TableName, documentDef.RoutePrefix, documentDef.ReadPermission = "crypto_invoice", "test_search_docs", "crypto-invoices", "crypto_invoice:read"
		moneyRegistry.Register(documentDef)
		moneyReads := postgres.NewAuthorizedReadService(moneyRegistry, engine)
		moneySearch := search.NewService(moneyRegistry, moneyReads)
		moneyRefs := postgres.NewRefResolverRepo(moneyRegistry)
		user := appctx.UserContext{UserID: reader.UserID, Permissions: []string{"crypto_invoice:read", "currency:read"}}
		ctx := readaccess.WithService(tenants[0].Context(user), moneyReads)
		_, err := tenants[0].Pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,'preview@test','fixture')`, user.UserID)
		require.NoError(t, err)
		party := id.New()
		_, err = tenants[0].Pool.Exec(ctx, `INSERT INTO cat_counterparties(id,code,name) VALUES($1,'PREVIEW-P','Party')`, party)
		require.NoError(t, err)
		for _, fixture := range []struct {
			scale   int
			code    string
			display string
		}{
			{0, "JPY", "9007199254740993"}, {2, "USD", "90071992547409.93"}, {3, "KWD", "9007199254740.993"}, {9, "TON", "9007199.254740993"},
		} {
			cur, doc := id.New(), id.New()
			code := fixture.code
			_, err := tenants[0].Pool.Exec(ctx, `INSERT INTO cat_currencies(id,code,name,iso_code,symbol,decimal_places) VALUES($1,$2,$2,$2,$2,$3)`, cur, code, fixture.scale)
			require.NoError(t, err)
			_, err = tenants[0].Pool.Exec(ctx, `INSERT INTO test_search_docs(id,number,date,organization_id,counterparty_id,merchant_id,currency_id,created_by,updated_by,total_amount,description,supplier_doc_number) VALUES($1,$2,'2026-01-05',$3,$4,$5,$6,$7,$7,9007199254740993,'','')`, doc, "PREVIEW-"+code, orgA, party, visible, cur, reader.UserID)
			require.NoError(t, err)
			preview, err := moneySearch.Preview(ctx, "document", "crypto_invoice", doc.String())
			require.NoError(t, err)
			require.Contains(t, preview.Fields, search.PreviewField{Label: "Сумма", Value: fixture.display + " " + code})
			refs := []domain.RefResolveRequest{{RefType: "CryptoInvoice", RefID: doc}, {RefType: "Currency", RefID: cur}, {RefType: "CryptoInvoice", RefID: doc}}
			resolved, err := moneyRefs.ResolveRefs(ctx, refs)
			require.NoError(t, err)
			require.Len(t, resolved, 2)
			require.Equal(t, "9007199254740993", resolved[0].Amount)
			for _, ref := range resolved {
				require.Equal(t, code, ref.CurrencyCode)
				require.NotNil(t, ref.DecimalPlaces)
				require.Equal(t, fixture.scale, *ref.DecimalPlaces)
			}
			for _, denied := range []context.Context{
				readaccess.WithService(tenants[0].Context(appctx.UserContext{UserID: user.UserID, Permissions: []string{"crypto_invoice:read"}}), moneyReads),
				security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"currency:read": {AllowedFields: []string{"*", "-iso_code"}}}),
				security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"currency:read": {AllowedFields: []string{"*", "-decimal_places"}}}),
				security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_invoice:read": {AllowedFields: []string{"*", "-total_amount"}}}),
				security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_invoice:read": {AllowedFields: []string{"*", "-currency_id"}}}),
				security.WithPolicyRules(ctx, []security.PolicyRule{&security_profile.PolicyRule{ID: id.New(), EntityName: "currency", Actions: []string{"read"}, Expression: "true", Effect: "deny", Enabled: true}}),
			} {
				preview, err := moneySearch.Preview(denied, "document", "crypto_invoice", doc.String())
				require.NoError(t, err)
				for _, field := range preview.Fields {
					require.NotEqual(t, "Сумма", field.Label)
				}
				resolved, err := moneyRefs.ResolveRefs(denied, refs[:1])
				require.NoError(t, err)
				require.Len(t, resolved, 1)
				require.Empty(t, resolved[0].Amount)
				require.Nil(t, resolved[0].DecimalPlaces)
			}
		}
	})
	t.Run("source and movements use a consistent snapshot", func(t *testing.T) {
		before := "Needle allowed"
		provider := snapshotMovementProvider{load: func(ctx context.Context) ([]entity.DocumentMovement, error) {
			var name string
			err := tenants[0].Tx.GetQuerier(ctx).QueryRow(ctx, `SELECT name FROM test_search_items WHERE id=$1`, visible).Scan(&name)
			if err != nil {
				return nil, err
			}
			return []entity.DocumentMovement{{PolicyEntity: "test", Columns: []entity.MovementColumnDef{{Key: "value", Type: "text"}}, Data: map[string]any{"value": name}}}, nil
		}}
		movements, err := reads.ReadMovements(base, "wallet", visible, postgres.ReadSnapshot,
			func(ctx context.Context, uid id.ID) error {
				source, err := reads.ReadOne(ctx, "wallet", uid)
				if err != nil {
					return err
				}
				require.Equal(t, before, source.Fields["name"])
				_, err = tenants[0].Pool.Exec(context.Background(), `UPDATE test_search_items SET name='Changed after source read' WHERE id=$1`, uid)
				return err
			}, []entity.MovementProvider{provider}, nil)
		require.NoError(t, err)
		require.Len(t, movements, 1)
		require.Equal(t, before, movements[0].Data["value"])
		current, err := reads.ReadOne(base, "wallet", visible)
		require.NoError(t, err)
		require.Equal(t, "Changed after source read", current.Fields["name"])
	})

}

type snapshotMovementProvider struct {
	load func(context.Context) ([]entity.DocumentMovement, error)
}

func (snapshotMovementProvider) RegisterName() string { return "snapshot" }
func (p snapshotMovementProvider) GetDocumentMovements(ctx context.Context, _ id.ID) ([]entity.DocumentMovement, error) {
	return p.load(ctx)
}
