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
	// Parts are collections stored in their own SQL tables named
	// Table + PartSeparator + Key, with document_id referencing Table(id).
	Parts []Part
	// DerivedParts are API collections of the entity (metadata table parts
	// by their JSON name) that have no SQL table of their own.
	DerivedParts []string
	// Code declares how a catalog's Code is assigned (see CodeRule); the
	// zero value numbers the catalog from its own series.
	Code CodeRule
}

// PartSeparator joins a header table name and a part key. It is reserved for
// physical table parts: no other table name may contain it.
const PartSeparator = "__"

// MaxIdentifierBytes is PostgreSQL's NAMEDATALEN-1; longer names are truncated silently.
const MaxIdentifierBytes = 63

// Part declares a physical table part. Key is chosen explicitly and does not
// follow the Go or JSON field name; Field binds it to the metadata table part
// (the JSON name of the collection) used by filters, FLS and reference search.
type Part struct {
	Key   string
	Field string
}

// PartTableName returns the SQL table of a physical table part.
func PartTableName(header, key string) string { return header + PartSeparator + key }

// PartTable returns the SQL table of the declared part with the given key.
func (d Definition) PartTable(key string) (string, bool) {
	for _, part := range d.Parts {
		if part.Key == key {
			return PartTableName(d.Table, key), true
		}
	}
	return "", false
}

// MustPartTable is PartTable for statically declared parts.
func (d Definition) MustPartTable(key string) string {
	table, ok := d.PartTable(key)
	if !ok {
		panic(fmt.Sprintf("entity %q declares no table part %q", d.Name, key))
	}
	return table
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

// sqlName forbids PartSeparator: words are separated by single underscores.
var sqlName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func (d Definition) Validate() error {
	if d.Kind != CatalogKind && d.Kind != DocumentKind {
		return fmt.Errorf("invalid entity kind %q", d.Kind)
	}
	if !definitionKey.MatchString(d.Key) || !definitionName.MatchString(d.Name) || !definitionRoute.MatchString(d.Route) || !definitionKey.MatchString(d.Table) {
		return fmt.Errorf("invalid entity definition for %q", d.Name)
	}
	if !sqlName.MatchString(d.Table) || len(d.Table) > MaxIdentifierBytes {
		return fmt.Errorf("entity %q: table %q must be at most %d bytes of words joined by single underscores", d.Name, d.Table, MaxIdentifierBytes)
	}
	fields := make(map[string]bool, len(d.Parts)+len(d.DerivedParts))
	keys := make(map[string]bool, len(d.Parts))
	for _, part := range d.Parts {
		if !sqlName.MatchString(part.Key) {
			return fmt.Errorf("entity %q: table part key %q must be words joined by single underscores", d.Name, part.Key)
		}
		if table := PartTableName(d.Table, part.Key); len(table) > MaxIdentifierBytes {
			return fmt.Errorf("entity %q: table part %q exceeds %d bytes", d.Name, table, MaxIdentifierBytes)
		}
		if part.Field == "" || keys[part.Key] || fields[part.Field] {
			return fmt.Errorf("entity %q: table part %q must bind one unique field", d.Name, part.Key)
		}
		keys[part.Key], fields[part.Field] = true, true
	}
	for _, field := range d.DerivedParts {
		if field == "" || fields[field] {
			return fmt.Errorf("entity %q: derived part %q is empty or declared twice", d.Name, field)
		}
		fields[field] = true
	}
	return d.validateCode()
}
