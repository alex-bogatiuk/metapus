package integrations

import (
	"fmt"
	"regexp"
	"slices"

	"metapus/internal/core/apperror"
)

// providerIDPattern matches the cat_integration_accounts.provider check.
var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Catalog is the immutable set of integration kinds and providers of one
// application: the platform's plus its configuration's. It is built once at
// the composition root and passed to the services, repositories and HTTP
// adapters that need provider rules; there is no global registry.
type Catalog struct {
	kinds     []KindDefinition
	providers []ProviderDefinition
	index     map[Provider]int
}

// NewCatalog validates unique identifiers and that every provider names known kinds.
func NewCatalog(kinds []KindDefinition, providers []ProviderDefinition) (*Catalog, error) {
	c := &Catalog{index: make(map[Provider]int, len(providers))}
	known := make(map[Kind]bool, len(kinds))
	for _, kind := range kinds {
		if kind.ID == "" || known[kind.ID] {
			return nil, fmt.Errorf("integration kind %q is empty or duplicated", kind.ID)
		}
		known[kind.ID] = true
		c.kinds = append(c.kinds, kind)
	}
	for _, provider := range providers {
		if !providerIDPattern.MatchString(string(provider.ID)) {
			return nil, fmt.Errorf("integration provider %q has an invalid identifier", provider.ID)
		}
		if _, exists := c.index[provider.ID]; exists {
			return nil, fmt.Errorf("integration provider %q is duplicated", provider.ID)
		}
		for _, kind := range provider.Kinds {
			if !known[kind] {
				return nil, fmt.Errorf("integration provider %q uses unknown kind %q", provider.ID, kind)
			}
		}
		c.index[provider.ID] = len(c.providers)
		c.providers = append(c.providers, cloneProvider(provider))
	}
	return c, nil
}

// MustCatalog is NewCatalog for definitions fixed in code.
func MustCatalog(kinds []KindDefinition, providers []ProviderDefinition) *Catalog {
	c, err := NewCatalog(kinds, providers)
	if err != nil {
		panic(err)
	}
	return c
}

// PlatformCatalog holds only the platform's kinds and providers.
func PlatformCatalog() *Catalog { return MustCatalog(PlatformKinds(), PlatformProviders()) }

// Kinds returns a copy of the kind definitions in declaration order.
func (c *Catalog) Kinds() []KindDefinition { return slices.Clone(c.kinds) }

// Providers returns copies of the provider definitions in declaration order.
func (c *Catalog) Providers() []ProviderDefinition {
	out := make([]ProviderDefinition, len(c.providers))
	for i, provider := range c.providers {
		out[i] = cloneProvider(provider)
	}
	return out
}

// Definition returns a copy of the provider's definition.
func (c *Catalog) Definition(p Provider) (ProviderDefinition, bool) {
	i, ok := c.index[p]
	if !ok {
		return ProviderDefinition{}, false
	}
	return cloneProvider(c.providers[i]), true
}

// Supports reports whether the provider serves the kind.
func (c *Catalog) Supports(p Provider, kind Kind) bool {
	i, ok := c.index[p]
	return ok && slices.Contains(c.providers[i].Kinds, kind)
}

// Require returns the definition of a known provider, or a validation error.
func (c *Catalog) Require(p Provider) (ProviderDefinition, error) {
	definition, ok := c.Definition(p)
	if !ok {
		return ProviderDefinition{}, apperror.NewValidation("Неизвестный провайдер интеграции").WithDetail("field", "provider")
	}
	return definition, nil
}

// ValidateCreation admits only providers whose accounts may be created.
func (c *Catalog) ValidateCreation(p Provider) error {
	definition, ok := c.Definition(p)
	if !ok || !definition.CanCreate {
		return apperror.NewValidation("Выберите провайдера, поддерживаемого аккаунтами интеграций").WithDetail("field", "provider")
	}
	return nil
}
