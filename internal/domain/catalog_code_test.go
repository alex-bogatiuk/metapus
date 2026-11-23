package domain

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
)

type codedItem struct{ entity.Catalog }

// codeRepo is a catalog repository that keeps written items by code.
type codeRepo struct {
	CatalogRepository[*codedItem]
	items map[string]*codedItem
}

func (r *codeRepo) Create(_ context.Context, item *codedItem) error {
	r.items[item.Code] = item
	return nil
}
func (r *codeRepo) GetByID(_ context.Context, uid id.ID) (*codedItem, error) {
	for _, item := range r.items {
		if item.ID == uid {
			return item, nil
		}
	}
	return nil, apperror.NewNotFound("coded_item", uid)
}
func (r *codeRepo) Update(_ context.Context, item *codedItem) error {
	r.items[item.Code] = item
	return nil
}
func (r *codeRepo) ExistsByCode(_ context.Context, code string) (bool, error) {
	_, ok := r.items[code]
	return ok, nil
}

// seriesCounter numbers like the PostgreSQL numerator and records the configs it was asked for.
type seriesCounter struct {
	next    map[string]int64
	configs []numerator.Config
}

func (c *seriesCounter) GetNextNumber(_ context.Context, cfg numerator.Config, _ time.Time) (string, error) {
	c.configs = append(c.configs, cfg)
	c.next[cfg.Key]++
	return fmt.Sprintf("%s-%0*d", cfg.Prefix, cfg.PadWidth, c.next[cfg.Key]), nil
}
func (c *seriesCounter) SetNextNumber(context.Context, numerator.Config, time.Time, int64) error {
	return nil
}

func codeService(t *testing.T, rule entity.CodeRule, gen numerator.Generator) (*CatalogService[*codedItem], *codeRepo) {
	t.Helper()
	def := entity.Definition{Kind: entity.CatalogKind, Key: "coded_item", Name: "CodedItem", Route: "coded-items", Table: "cat_coded_items", Code: rule}
	repo := &codeRepo{items: map[string]*codedItem{}}
	return NewCatalogService(CatalogServiceConfig[*codedItem]{Definition: &def, Repo: repo, Numerator: gen, TxManager: &outboxTestTx{}}), repo
}

func newCodedItem(code string) *codedItem {
	return &codedItem{Catalog: entity.NewCatalog(code, "Элемент")}
}

func TestCatalogServiceNumbersAnEmptyCodeFromTheDefinitionSeries(t *testing.T) {
	ctx := context.Background()
	counter := &seriesCounter{next: map[string]int64{}}
	svc, repo := codeService(t, entity.CodeRule{Prefix: "ORG"}, counter)

	first, second := newCodedItem(""), newCodedItem("  ")
	require.NoError(t, svc.Create(ctx, first))
	require.NoError(t, svc.Create(ctx, second))
	require.Equal(t, "ORG-000001", first.Code)
	require.Equal(t, "ORG-000002", second.Code)
	require.Equal(t, numerator.Config{Key: "catalog.coded_item", Prefix: "ORG", PadWidth: 6, ResetPeriod: "never"}, counter.configs[0],
		"the counter is keyed by the catalog, not by the displayed prefix")

	manual := newCodedItem(" M-7 ")
	require.NoError(t, svc.Create(ctx, manual))
	require.Equal(t, "M-7", manual.Code, "a code entered by the user is kept")
	require.Len(t, counter.configs, 2, "a manual code takes no number")

	repo.items["ORG-000003"], repo.items["ORG-000004"] = newCodedItem("ORG-000003"), newCodedItem("ORG-000004")
	skipped := newCodedItem("")
	require.NoError(t, svc.Create(ctx, skipped))
	require.Equal(t, "ORG-000005", skipped.Code, "codes of the series taken by hand are skipped")

	edited := *first
	edited.Code = ""
	require.NoError(t, svc.Update(ctx, &edited))
	require.Equal(t, "ORG-000001", edited.Code, "an update without a code keeps the stored one")

	legacy := newCodedItem("")
	repo.items[""] = legacy
	stored := *legacy
	require.NoError(t, svc.Update(ctx, &stored))
	require.Equal(t, "ORG-000006", stored.Code, "an item stored without a code is numbered when it is written")
}

func TestCatalogServiceNumbersCatalogsWithTheirOwnHooks(t *testing.T) {
	counter := &seriesCounter{next: map[string]int64{}}
	svc, _ := codeService(t, entity.CodeRule{}, counter)
	var seen string
	svc.Hooks().OnBeforeCreate(func(_ context.Context, item *codedItem) error {
		seen = item.Code
		return nil
	})
	item := newCodedItem("")
	require.NoError(t, svc.Create(context.Background(), item))
	require.Empty(t, seen, "hooks run before the code is taken, so a failed check spends no number")
	require.Equal(t, "CI-000001", item.Code, "the default prefix is derived from the entity key")
}

func TestCatalogServiceRequiresDerivedAndManualCodes(t *testing.T) {
	for _, source := range []entity.CodeSource{entity.CodeDerived, entity.CodeManual} {
		svc, repo := codeService(t, entity.CodeRule{Source: source}, numerator.Noop())
		err := svc.Create(context.Background(), newCodedItem(""))
		appErr, ok := apperror.AsAppError(err)
		require.True(t, ok && appErr.Code == apperror.CodeValidation, "%s: %v", source, err)
		require.Empty(t, repo.items)
		usd := newCodedItem("USD")
		require.NoError(t, svc.Create(context.Background(), usd))
		edited := *usd
		edited.Code = ""
		require.NoError(t, svc.Update(context.Background(), &edited), "%s: an update without a code keeps the stored one", source)
		require.Equal(t, "USD", edited.Code)
	}
}

func TestCatalogServiceWithoutNumeratorRefusesAnEmptySeriesCode(t *testing.T) {
	svc, repo := codeService(t, entity.CodeRule{}, nil)
	require.Error(t, svc.Create(context.Background(), newCodedItem("")))
	require.Empty(t, repo.items, "an item is never written without a code")
}
