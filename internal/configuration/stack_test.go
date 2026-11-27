package configuration_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration"
	"github.com/alex-bogatiuk/metapus/internal/configuration/none"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
)

// layer is a configuration of a stack under test: none's behaviour with its
// own name, migrations, version and registration log.
type layer struct {
	configuration.Configuration
	name       string
	version    string
	migrations fs.FS
	log        *[]string
	modules    []posting.Module
}

func (l layer) Name() string      { return l.name }
func (l layer) Version() string   { return l.version }
func (l layer) Migrations() fs.FS { return l.migrations }
func (l layer) Register(*v1.FactoryRegistry) {
	*l.log = append(*l.log, l.name)
}
func (l layer) Server(context.Context, configuration.ServerEnv) (configuration.Server, error) {
	return configuration.Server{PostingModules: l.modules}, nil
}
func (l layer) Seed(context.Context, configuration.SeedEnv) error {
	*l.log = append(*l.log, "seed "+l.name)
	return nil
}

type addOn struct {
	layer
	base        configuration.Configuration
	baseVersion string
}

func (a addOn) Base() configuration.Configuration { return a.base }
func (a addOn) BaseVersion() string               { return a.baseVersion }

func migrations(highest string) fs.FS {
	return fstest.MapFS{highest + "_probe.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}
}

func stack(log *[]string) (layer, addOn) {
	base := layer{Configuration: none.New(), name: "store", version: "1.4.0", migrations: migrations("00003"), log: log}
	shop := addOn{layer: layer{Configuration: none.New(), name: "shop", log: log}, base: base, baseVersion: "1.2.0"}
	return base, shop
}

func TestAnAddOnIsAssembledOnItsBase(t *testing.T) {
	t.Parallel()
	var log []string
	base, shop := stack(&log)
	require.Equal(t, []configuration.Configuration{base, shop}, configuration.Stack(shop))
	require.Equal(t, []configuration.Configuration{base}, configuration.Stack(base))
	require.Equal(t, "store", configuration.BaseName(shop))
	require.Equal(t, "shop", configuration.AddOnName(shop))
	require.Empty(t, configuration.AddOnName(base))

	app, err := configuration.Assemble(shop)
	require.NoError(t, err)
	require.Equal(t, "shop", app.Name())
	require.Equal(t, []configuration.Configuration{base, shop}, configuration.Stack(app), "the assembled application keeps its layers")
	again, err := configuration.Assemble(app)
	require.NoError(t, err)
	require.Equal(t, app, again)

	configuration.Compose(shop)
	require.Equal(t, []string{"store", "shop"}, log, "the base registers first")
	log = nil
	require.NoError(t, app.Seed(context.Background(), configuration.SeedEnv{}))
	require.Equal(t, []string{"seed store", "seed shop"}, log, "the base seeds first")
}

func TestAnAddOnRunsOnlyOnACompatibleBase(t *testing.T) {
	t.Parallel()
	var log []string
	_, shop := stack(&log)
	require.NoError(t, configuration.CheckStack(shop))

	for name, broken := range map[string]func(*addOn){
		"a newer minor base":    func(a *addOn) { a.baseVersion = "1.5.0" },
		"another major base":    func(a *addOn) { a.baseVersion = "2.0.0" },
		"no version":            func(a *addOn) { a.baseVersion = "next" },
		"its base's name":       func(a *addOn) { a.name = "store" },
		"an unversioned base":   func(a *addOn) { a.base = none.New() },
		"an add-on as its base": func(a *addOn) { inner := *a; inner.name = "inner"; a.base = inner },
	} {
		addOn := shop
		broken(&addOn)
		require.Error(t, configuration.CheckStack(addOn), name)
		_, err := configuration.Assemble(addOn)
		require.Error(t, err, name)
	}
}

func TestTheServerPartsOfTheLayersAreMerged(t *testing.T) {
	t.Parallel()
	var log []string
	base, shop := stack(&log)
	first, second := posting.Module(nil), posting.Module(nil)
	base.modules = []posting.Module{first}
	shop.modules = []posting.Module{second}
	shop.base = base
	app := configuration.MustAssemble(shop)
	part, err := app.Server(context.Background(), configuration.ServerEnv{})
	require.NoError(t, err)
	require.Len(t, part.PostingModules, 2)
	reg := v1.NewFactoryRegistry()
	part.Register(reg)
	require.Equal(t, []string{"store", "shop"}, log, "without a register of their own the layers register as declared")
}

func TestTheTenantSchemaHoldsTheBaseAndTheAddOn(t *testing.T) {
	t.Parallel()
	var log []string
	base, shop := stack(&log)
	shop.migrations = migrations("00002")
	schema, err := configuration.ExpectedSchema(shop)
	require.NoError(t, err)
	require.Equal(t, "store", schema.Configuration)
	require.Equal(t, 3, schema.ConfigurationVersion)
	require.Equal(t, "shop", schema.AddOn)
	require.Equal(t, 2, schema.AddOnVersion)

	alone, err := configuration.ExpectedSchema(base)
	require.NoError(t, err)
	require.Empty(t, alone.AddOn)
	require.Zero(t, alone.AddOnVersion)
	require.Len(t, configuration.MigrationOptions(shop), len(configuration.MigrationOptions(base))+1, "the add-on's migrations have a history of their own")
}
