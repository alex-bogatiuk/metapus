package configuration

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
)

// An application is three layers (слои): the platform, a base
// configuration that automates a business on its own (a «типовая
// конфигурация»: trade, crypto processing) and at most one add-on built on
// it (надстройка) with the specifics of an industry. An add-on works with
// its base only: it imports the base's packages, adds catalogs, documents,
// registers, fields and movements, and changes the base's behaviour where
// the base declares a point for it. Two industries in one application are
// one add-on that combines them, so add-ons never meet at run time.
//
// AddOn is a configuration built on a base. Its Name, Migrations and
// frontend are its own; the platform registers, migrates, serves and seeds
// the base first and the add-on after it (Assemble).
type AddOn interface {
	Configuration
	// Base is the base configuration the add-on is built on.
	Base() Configuration
	// BaseVersion is the version of the base (Versioned) the add-on is
	// built for. The platform refuses a base of another major version or an
	// older minor one (CheckStack).
	BaseVersion() string
}

// Versioned is a base configuration with a version, MAJOR.MINOR.PATCH, that
// add-ons are built for: the minor grows with additions to the packages
// add-ons import, the major with changes that break them.
type Versioned interface {
	Version() string
}

// Stack returns the configurations of the application cfg assembles, the
// base first: [base, add-on] for an add-on, [cfg] for a base.
func Stack(cfg Configuration) []Configuration {
	switch c := cfg.(type) {
	case stacked:
		return []Configuration{c.base, c.addOn}
	case registered:
		return Stack(c.Configuration)
	case AddOn:
		return []Configuration{c.Base(), c}
	default:
		return []Configuration{cfg}
	}
}

// BaseName and AddOnName name the layers of the application cfg assembles;
// AddOnName is empty for a base alone.
func BaseName(cfg Configuration) string { return Stack(cfg)[0].Name() }

func AddOnName(cfg Configuration) string {
	if stack := Stack(cfg); len(stack) == 2 {
		return stack[1].Name()
	}
	return ""
}

// CheckStack fails when cfg is not a valid application: an add-on built on
// another add-on, on a base without a version, or on a base whose version
// its BaseVersion cannot run with; a base or an add-on whose platform API
// version the platform cannot serve (CheckAPIVersion).
func CheckStack(cfg Configuration) error {
	stack := Stack(cfg)
	for _, layer := range stack {
		if !ValidName(layer.Name()) {
			return fmt.Errorf("configuration name %q is not a lowercase identifier", layer.Name())
		}
		if err := checkAPIVersion(layer); err != nil {
			return err
		}
	}
	if len(stack) == 1 {
		return nil
	}
	base, addOn := stack[0], stack[1].(AddOn)
	if _, nested := base.(AddOn); nested {
		return fmt.Errorf("add-on %s is built on add-on %s; an add-on is built on a base configuration", addOn.Name(), base.Name())
	}
	if base.Name() == addOn.Name() {
		return fmt.Errorf("add-on %s has the name of its base", addOn.Name())
	}
	versioned, ok := base.(Versioned)
	if !ok {
		return fmt.Errorf("add-on %s is built on %s, which declares no version", addOn.Name(), base.Name())
	}
	if err := compatibleVersion(versioned.Version(), addOn.BaseVersion()); err != nil {
		return fmt.Errorf("add-on %s on %s: %w", addOn.Name(), base.Name(), err)
	}
	return nil
}

// compatibleVersion fails unless a version have can run what was built for
// required: the same major version and at least its minor one.
func compatibleVersion(have, required string) error {
	h, err := parseVersion(have)
	if err != nil {
		return err
	}
	r, err := parseVersion(required)
	if err != nil {
		return err
	}
	if h[0] != r[0] || h[1] < r[1] {
		return fmt.Errorf("version %s cannot run what was built for %s", have, required)
	}
	return nil
}

func parseVersion(version string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return result, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
		}
		result[i] = n
	}
	return result, nil
}

// Assemble is the application cfg describes as one Configuration: cfg
// itself for a base, the base and the add-on together for an add-on. The
// platform's processes assemble their configuration before they use it.
//
// The assembled value registers, contracts, serves, works and seeds the
// base first and the add-on after it; its Name is the add-on's. Its
// Migrations are the add-on's: the migration run and the expected schema
// take each layer's from Stack.
func Assemble(cfg Configuration) (Configuration, error) {
	if err := CheckStack(cfg); err != nil {
		return nil, err
	}
	stack := Stack(cfg)
	if len(stack) == 1 {
		return cfg, nil
	}
	if s, ok := cfg.(stacked); ok {
		return s, nil
	}
	return stacked{base: stack[0], addOn: stack[1]}, nil
}

// MustAssemble is Assemble for an application known to be valid, such as
// the repository's own configurations in commands and tests.
func MustAssemble(cfg Configuration) Configuration {
	assembled, err := Assemble(cfg)
	if err != nil {
		panic(err)
	}
	return assembled
}

// stacked is an add-on assembled with its base.
type stacked struct {
	base  Configuration
	addOn Configuration
}

func (s stacked) Name() string       { return s.addOn.Name() }
func (s stacked) APIVersion() string { return s.addOn.APIVersion() }

func (s stacked) Register(reg *v1.FactoryRegistry) {
	s.base.Register(reg)
	s.addOn.Register(reg)
}

func (s stacked) RouteContracts() []spec.Bundle {
	return append(append([]spec.Bundle(nil), s.base.RouteContracts()...), s.addOn.RouteContracts()...)
}

func (s stacked) Migrations() fs.FS { return s.addOn.Migrations() }

// IntegrationCatalog is the add-on's when it declares one, else the base's:
// an add-on that adds integrations lists the base's too.
func (s stacked) IntegrationCatalog() *integrations.Catalog {
	if catalog := s.addOn.IntegrationCatalog(); catalog != nil {
		return catalog
	}
	return s.base.IntegrationCatalog()
}

func (s stacked) PoolConsumers() []integrations.PoolConsumer {
	return append(append([]integrations.PoolConsumer(nil), s.base.PoolConsumers()...), s.addOn.PoolConsumers()...)
}

func (s stacked) Server(ctx context.Context, env ServerEnv) (Server, error) {
	base, err := s.base.Server(ctx, env)
	if err != nil {
		return Server{}, fmt.Errorf("%s: %w", s.base.Name(), err)
	}
	addOn, err := s.addOn.Server(ctx, env)
	if err != nil {
		return Server{}, fmt.Errorf("%s: %w", s.addOn.Name(), err)
	}
	if base.AccessScopes != nil && addOn.AccessScopes != nil {
		return Server{}, errors.New("both the base and the add-on provide access scopes")
	}
	if base.IntegrationConnectionTests != nil && addOn.IntegrationConnectionTests != nil {
		return Server{}, errors.New("both the base and the add-on test integration connections")
	}
	baseRegister, addOnRegister := base.Register, addOn.Register
	if baseRegister == nil {
		baseRegister = s.base.Register
	}
	if addOnRegister == nil {
		addOnRegister = s.addOn.Register
	}
	merged := Server{
		Register: func(reg *v1.FactoryRegistry) {
			baseRegister(reg)
			addOnRegister(reg)
		},
		AccessScopes:               base.AccessScopes,
		PostingModules:             append(append(base.PostingModules[:0:0], base.PostingModules...), addOn.PostingModules...),
		RouteModules:               append(append(base.RouteModules[:0:0], base.RouteModules...), addOn.RouteModules...),
		IntegrationConnectionTests: base.IntegrationConnectionTests,
	}
	if addOn.AccessScopes != nil {
		merged.AccessScopes = addOn.AccessScopes
	}
	if addOn.IntegrationConnectionTests != nil {
		merged.IntegrationConnectionTests = addOn.IntegrationConnectionTests
	}
	return merged, nil
}

func (s stacked) Worker(ctx context.Context, env WorkerEnv) (Worker, error) {
	base, err := s.base.Worker(ctx, env)
	if err != nil {
		return Worker{}, fmt.Errorf("%s: %w", s.base.Name(), err)
	}
	addOn, err := s.addOn.Worker(ctx, env)
	if err != nil {
		return Worker{}, fmt.Errorf("%s: %w", s.addOn.Name(), err)
	}
	if base.Tenant == nil || addOn.Tenant == nil {
		if base.Tenant == nil {
			return addOn, nil
		}
		return base, nil
	}
	return Worker{Tenant: func(ctx context.Context, env TenantEnv) (TenantWorker, error) {
		b, err := base.Tenant(ctx, env)
		if err != nil {
			return TenantWorker{}, err
		}
		a, err := addOn.Tenant(ctx, env)
		if err != nil {
			return TenantWorker{}, err
		}
		return TenantWorker{
			CheckSchema: func(ctx context.Context, pool *pgxpool.Pool) error {
				for _, check := range []func(context.Context, *pgxpool.Pool) error{b.CheckSchema, a.CheckSchema} {
					if check != nil {
						if err := check(ctx, pool); err != nil {
							return err
						}
					}
				}
				return nil
			},
			Outbox:     append(append(b.Outbox[:0:0], b.Outbox...), a.Outbox...),
			Components: append(append(b.Components[:0:0], b.Components...), a.Components...),
			Cleanups:   append(append(b.Cleanups[:0:0], b.Cleanups...), a.Cleanups...),
		}, nil
	}}, nil
}

func (s stacked) Seed(ctx context.Context, env SeedEnv) error {
	if err := s.base.Seed(ctx, env); err != nil {
		return fmt.Errorf("%s: %w", s.base.Name(), err)
	}
	if err := s.addOn.Seed(ctx, env); err != nil {
		return fmt.Errorf("%s: %w", s.addOn.Name(), err)
	}
	return nil
}
