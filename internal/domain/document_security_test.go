package domain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
)

type rlsTestDocument struct {
	entity.Document
	OrgID string `db:"organization_id" json:"organizationId"`
	Lines []int
}

func (*rlsTestDocument) GetDocumentType() string { return "RLSTest" }
func (d *rlsTestDocument) GetLines() []int       { return d.Lines }
func (d *rlsTestDocument) SetLines(l []int)      { d.Lines = l }
func (d *rlsTestDocument) GetRLSDimensions() map[string]string {
	return map[string]string{"organization": d.OrgID}
}

type rlsTestDocRepo struct {
	HeaderDocumentRepository[*rlsTestDocument]
	stored *rlsTestDocument
	writes int
}

func (r *rlsTestDocRepo) Create(_ context.Context, doc *rlsTestDocument) error {
	r.writes++
	copyDoc := *doc
	r.stored = &copyDoc
	return nil
}

func (r *rlsTestDocRepo) Update(_ context.Context, doc *rlsTestDocument) error {
	r.writes++
	copyDoc := *doc
	r.stored = &copyDoc
	return nil
}

func (r *rlsTestDocRepo) GetByID(context.Context, id.ID) (*rlsTestDocument, error) {
	copyDoc := *r.stored
	return &copyDoc, nil
}

func (r *rlsTestDocRepo) GetLines(context.Context, id.ID) ([]int, error) { return r.stored.Lines, nil }
func (r *rlsTestDocRepo) SaveLines(context.Context, id.ID, []int) error  { return nil }

type testPolicyRule struct {
	id, expr, effect string
	actions          map[string]bool
}

func (r testPolicyRule) GetID() string               { return r.id }
func (r testPolicyRule) GetExpression() string       { return r.expr }
func (r testPolicyRule) GetEffect() string           { return r.effect }
func (r testPolicyRule) GetPriority() int            { return 100 }
func (r testPolicyRule) GetEnabled() bool            { return true }
func (r testPolicyRule) MatchesAction(a string) bool { return r.actions[a] }
func (r testPolicyRule) MatchesEntity(string) bool   { return true }

func buildRLSDocServices(t *testing.T, repo *rlsTestDocRepo, engine *security.PolicyEngine) []struct {
	name string
	svc  DocumentService[*rlsTestDocument]
} {
	t.Helper()
	txm := &outboxTestTx{}
	linesSvc := NewBaseDocumentService(BaseDocumentServiceConfig[*rlsTestDocument, int]{
		Repo: repo, TxManager: txm, Numerator: numerator.Noop(),
		PostingEngine: posting.NewEngine(nil), PolicyEngine: engine, EntityName: "rls_doc",
	})
	headerSvc := NewBaseHeaderDocumentService(BaseHeaderDocumentServiceConfig[*rlsTestDocument]{
		Repo: repo, TxManager: txm, Numerator: numerator.Noop(),
		PostingEngine: posting.NewEngine(nil), PolicyEngine: engine, EntityName: "rls_doc",
	})
	return []struct {
		name string
		svc  DocumentService[*rlsTestDocument]
	}{
		{name: "lines", svc: linesSvc},
		{name: "header", svc: headerSvc},
	}
}

func TestDocumentCreateAndPostEnforceReadOnlyRLSAndPostCEL(t *testing.T) {
	celEngine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer celEngine.Stop()

	tests := []struct {
		name      string
		giveScope *security.DataScope
		giveRules []security.PolicyRule
		giveOrg   string
		op        string
		wantDeny  bool
	}{
		{
			name:      "create denied for read-only scope",
			giveScope: &security.DataScope{ReadOnly: true},
			giveOrg:   "org-a",
			op:        "create",
			wantDeny:  true,
		},
		{
			name:      "create denied for foreign RLS dimension",
			giveScope: &security.DataScope{Dimensions: map[string][]string{"organization": {"org-a"}}},
			giveOrg:   "org-b",
			op:        "create",
			wantDeny:  true,
		},
		{
			name:      "postAndSave denied for foreign RLS dimension",
			giveScope: &security.DataScope{Dimensions: map[string][]string{"organization": {"org-a"}}},
			giveOrg:   "org-b",
			op:        "postAndSave",
			wantDeny:  true,
		},
		{
			name:      "postAndSave denied by CEL post rule",
			giveScope: &security.DataScope{Dimensions: map[string][]string{"organization": {"org-a"}}},
			giveRules: []security.PolicyRule{
				testPolicyRule{id: "deny-post", expr: "true", effect: "deny", actions: map[string]bool{"post": true}},
			},
			giveOrg:  "org-a",
			op:       "postAndSave",
			wantDeny: true,
		},
		{
			name:      "updateAndRepost denied by CEL post rule",
			giveScope: &security.DataScope{Dimensions: map[string][]string{"organization": {"org-a"}}},
			giveRules: []security.PolicyRule{
				testPolicyRule{id: "deny-post", expr: "true", effect: "deny", actions: map[string]bool{"post": true}},
			},
			giveOrg:  "org-a",
			op:       "repost",
			wantDeny: true,
		},
		{
			name:      "allowed org passes create and postAndSave",
			giveScope: &security.DataScope{Dimensions: map[string][]string{"organization": {"org-a"}}},
			giveOrg:   "org-a",
			op:        "postAndSave",
			wantDeny:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := &rlsTestDocument{Document: entity.NewDocument(), OrgID: "org-a"}
			stored.Number = "D-1"
			repo := &rlsTestDocRepo{stored: stored}
			for _, target := range buildRLSDocServices(t, repo, celEngine) {
				t.Run(target.name, func(t *testing.T) {
					repo.writes = 0
					ctx := tenant.WithTxManager(context.Background(), &outboxTestTx{})
					ctx = security.WithDataScope(ctx, tt.giveScope)
					if len(tt.giveRules) > 0 {
						ctx = security.WithPolicyRules(ctx, tt.giveRules)
					}
					doc := &rlsTestDocument{Document: entity.NewDocument(), OrgID: tt.giveOrg}
					doc.ID = stored.ID
					doc.Number = "D-1"

					var callErr error
					switch tt.op {
					case "create":
						callErr = target.svc.Create(ctx, doc)
					case "postAndSave":
						callErr = target.svc.PostAndSave(ctx, doc)
					case "repost":
						callErr = target.svc.UpdateAndRepost(ctx, doc)
					}
					if tt.wantDeny {
						require.Error(t, callErr)
						require.True(t, apperror.IsForbidden(callErr), "expected forbidden, got %v", callErr)
						require.Zero(t, repo.writes)
					} else {
						require.NoError(t, callErr)
						require.Equal(t, 1, repo.writes)
					}
				})
			}
		})
	}
}

func TestDocumentDeletionMarkDeniedForReadOnlyScope(t *testing.T) {
	stored := &rlsTestDocument{Document: entity.NewDocument(), OrgID: "org-a"}
	repo := &rlsTestDocRepo{stored: stored}
	for _, target := range buildRLSDocServices(t, repo, nil) {
		t.Run(target.name, func(t *testing.T) {
			repo.writes = 0
			ctx := tenant.WithTxManager(context.Background(), &outboxTestTx{})
			ctx = security.WithDataScope(ctx, &security.DataScope{ReadOnly: true})

			err := target.svc.SetDeletionMark(ctx, stored.ID, true)
			require.True(t, apperror.IsForbidden(err), "expected forbidden, got %v", err)
			require.Zero(t, repo.writes)
		})
	}
}
