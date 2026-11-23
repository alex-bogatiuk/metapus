package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/stretchr/testify/require"
)

// Intentionally has no currency methods: non-monetary documents use both bases.
type lifecycleDocument struct {
	entity.Document
	Lines []int
}

func (*lifecycleDocument) GetDocumentType() string { return "Lifecycle" }
func (d *lifecycleDocument) GetLines() []int       { return d.Lines }
func (d *lifecycleDocument) SetLines(lines []int)  { d.Lines = lines }

type lifecycleRepository struct {
	HeaderDocumentRepository[*lifecycleDocument]
	doc                *lifecycleDocument
	writes, lineWrites int
}

func (r *lifecycleRepository) Create(context.Context, *lifecycleDocument) error {
	r.writes++
	return nil
}
func (r *lifecycleRepository) Update(context.Context, *lifecycleDocument) error {
	r.writes++
	return nil
}
func (r *lifecycleRepository) Delete(context.Context, id.ID) error { r.writes++; return nil }
func (r *lifecycleRepository) GetByID(context.Context, id.ID) (*lifecycleDocument, error) {
	copy := *r.doc
	return &copy, nil
}
func (r *lifecycleRepository) GetLines(context.Context, id.ID) ([]int, error) {
	return r.doc.Lines, nil
}
func (r *lifecycleRepository) SaveLines(context.Context, id.ID, []int) error {
	r.lineWrites++
	return nil
}

func lifecycleService(lines bool, repo *lifecycleRepository, manager *outboxTestTx) (DocumentService[*lifecycleDocument], *HookRegistry[*lifecycleDocument]) {
	if lines {
		s := NewBaseDocumentService(BaseDocumentServiceConfig[*lifecycleDocument, int]{Repo: repo, TxManager: manager, Numerator: numerator.Noop(), PostingEngine: posting.NewEngine(nil), EntityName: "lifecycle"})
		return s, s.GetHooks()
	}
	s := NewBaseHeaderDocumentService(BaseHeaderDocumentServiceConfig[*lifecycleDocument]{Repo: repo, TxManager: manager, Numerator: numerator.Noop(), PostingEngine: posting.NewEngine(nil), EntityName: "lifecycle"})
	return s, s.GetHooks()
}

func TestDocumentLifecycleHooksShareTransactionForBothServiceShapes(t *testing.T) {
	for _, lines := range []bool{false, true} {
		for _, operation := range []string{"create", "update", "delete", "postAndSave", "repost", "mark", "unmark"} {
			t.Run(operation+map[bool]string{false: "/header", true: "/lines"}[lines], func(t *testing.T) {
				for _, fail := range []bool{false, true} {
					manager := &outboxTestTx{}
					doc := &lifecycleDocument{Document: entity.NewDocument(), Lines: []int{1}}
					doc.Number = "L-1"
					if operation == "unmark" {
						doc.MarkDeleted()
					}
					if operation == "repost" {
						doc.MarkPosted()
					}
					repo := &lifecycleRepository{doc: doc}
					svc, hooks := lifecycleService(lines, repo, manager)
					before, mandatory, after := BeforeUpdate, TransactionUpdate, AfterUpdate
					switch operation {
					case "create", "postAndSave":
						before, mandatory, after = BeforeCreate, TransactionCreate, AfterCreate
					case "delete":
						before, mandatory, after = BeforeDelete, TransactionDelete, AfterDelete
					case "mark":
						before, mandatory, after = "", TransactionSetDeletionMark, ""
					case "unmark":
						before, mandatory, after = "", TransactionClearDeletionMark, ""
					}
					var order []string
					if before != "" {
						hooks.On(before, func(ctx context.Context, _ *lifecycleDocument) error {
							require.NotNil(t, ctx.Value(outboxTxKey{}))
							order = append(order, "before")
							return nil
						})
					}
					failure := errors.New("mandatory hook failed")
					hooks.On(mandatory, func(ctx context.Context, _ *lifecycleDocument) error {
						require.NotNil(t, ctx.Value(outboxTxKey{}))
						require.Equal(t, 1, repo.writes)
						if lines && operation != "delete" && operation != "mark" && operation != "unmark" {
							require.Equal(t, 1, repo.lineWrites)
						}
						order = append(order, "transaction")
						if fail {
							return failure
						}
						return nil
					})
					if after != "" {
						hooks.On(after, func(ctx context.Context, _ *lifecycleDocument) error {
							require.Nil(t, ctx.Value(outboxTxKey{}))
							require.Equal(t, 1, manager.committed)
							order = append(order, "after")
							return nil
						})
					}
					ctx := tenant.WithTxManager(context.Background(), manager)
					var err error
					switch operation {
					case "create":
						err = svc.Create(ctx, doc)
					case "update":
						err = svc.Update(ctx, doc)
					case "delete":
						err = svc.Delete(ctx, doc.ID)
					case "postAndSave":
						err = svc.PostAndSave(ctx, doc)
					case "repost":
						err = svc.UpdateAndRepost(ctx, doc)
					case "mark":
						err = svc.SetDeletionMark(ctx, doc.ID, true)
					case "unmark":
						err = svc.SetDeletionMark(ctx, doc.ID, false)
					}
					want := []string{}
					if before != "" {
						want = append(want, "before")
					}
					want = append(want, "transaction")
					if fail {
						require.ErrorIs(t, err, failure)
						require.Equal(t, 1, manager.rolledBack)
						require.Zero(t, manager.committed)
					} else {
						require.NoError(t, err)
						if after != "" {
							want = append(want, "after")
						}
					}
					require.Equal(t, want, order)
				}
			})
		}
	}
}

func TestDocumentAfterHooksWaitForOuterCommitAndUseSnapshot(t *testing.T) {
	for _, lines := range []bool{false, true} {
		manager := &outboxTestTx{}
		doc := &lifecycleDocument{Document: entity.NewDocument()}
		doc.Number = "L-2"
		doc.Description = "saved"
		svc, hooks := lifecycleService(lines, &lifecycleRepository{doc: doc}, manager)
		var observations []string
		hooks.OnAfterCreate(func(_ context.Context, snapshot *lifecycleDocument) error {
			observations = append(observations, snapshot.Description)
			return nil
		})
		ctx := tenant.WithTxManager(context.Background(), manager)
		rollback := errors.New("outer rollback")
		require.ErrorIs(t, manager.RunInTransaction(ctx, func(ctx context.Context) error {
			require.NoError(t, svc.Create(ctx, doc))
			require.Empty(t, observations)
			return rollback
		}), rollback)
		require.Empty(t, observations)
		require.NoError(t, manager.RunInTransaction(ctx, func(ctx context.Context) error {
			require.NoError(t, svc.Create(ctx, doc))
			doc.Description = "mutated after save"
			require.Empty(t, observations)
			return nil
		}))
		require.Equal(t, []string{"saved"}, observations)
	}
}
