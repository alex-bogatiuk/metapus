package entity

import (
	"fmt"
	"regexp"
)

type Kind string

const (
	CatalogKind  Kind = "catalog"
	DocumentKind Kind = "document"
)

// Definition is the shared identity and persistence configuration of an entity.
// It contains no HTTP DTOs or infrastructure dependencies.
type Definition struct {
	Key       string
	Name      string
	Route     string
	Table     string
	Kind      Kind
	Hierarchy CatalogMeta
}

type DefinitionProvider interface{ EntityDefinition() Definition }

func (d Definition) EntityName() string           { return d.Name }
func (d Definition) RoutePrefix() string          { return d.Route }
func (d Definition) TableName() string            { return d.Table }
func (d Definition) Permission() string           { return string(d.Kind) + ":" + d.Key }
func (d Definition) EntityDefinition() Definition { return d }

var definitionKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var definitionRoute = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var definitionName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

func (d Definition) Validate() error {
	if d.Kind != CatalogKind && d.Kind != DocumentKind {
		return fmt.Errorf("invalid entity kind %q", d.Kind)
	}
	if !definitionKey.MatchString(d.Key) || !definitionName.MatchString(d.Name) || !definitionRoute.MatchString(d.Route) || !definitionKey.MatchString(d.Table) {
		return fmt.Errorf("invalid entity definition for %q", d.Name)
	}
	return nil
}
