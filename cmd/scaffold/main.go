// Package main provides a scaffolding CLI for creating new extension entities.
//
// Usage:
//
//	go run cmd/scaffold/main.go --name vehicle --type catalog
//	go run cmd/scaffold/main.go --name waybill --type document --table doc_waybills
//
// This generates:
//
//	extensions/<name>/
//	  ├── model.go           — Domain model with platform.Catalog/Document embed
//	  ├── dto.go             — Request/Response DTOs
//	  ├── repo.go            — Repository interface
//	  ├── <name>_repo.go     — PostgreSQL repository (BaseCatalogRepo embed)
//	  ├── service.go         — Business service with hooks
//	  ├── registration.go    — CatalogRegistration/DocumentRegistration impl
//	  ├── register.go        — Entry point Register(reg, cfg)
//	  └── migrations/
//	      └── 10001_<table>.sql  — goose migration
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

func main() {
	var name, entityType, table string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				name = os.Args[i+1]
				i++
			}
		case "--type":
			if i+1 < len(os.Args) {
				entityType = os.Args[i+1]
				i++
			}
		case "--table":
			if i+1 < len(os.Args) {
				table = os.Args[i+1]
				i++
			}
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		}
	}

	if name == "" {
		fmt.Println("Error: --name is required")
		printUsage()
		os.Exit(1)
	}
	if entityType == "" {
		entityType = "catalog"
	}
	if entityType != "catalog" && entityType != "document" {
		fmt.Printf("Error: --type must be 'catalog' or 'document', got '%s'\n", entityType)
		os.Exit(1)
	}

	snakeName := toSnakeCase(name)
	pascalName := toPascalCase(name)
	kebabName := toKebabCase(name)

	if table == "" {
		prefix := "cat_"
		if entityType == "document" {
			prefix = "doc_"
		}
		table = prefix + snakeName + "s"
	}

	data := scaffoldData{
		Name:       name,
		PascalName: pascalName,
		SnakeName:  snakeName,
		KebabName:  kebabName,
		CamelName:  toCamelCase(name),
		TableName:  table,
		EntityType: entityType,
		CodePrefix: strings.ToUpper(snakeName[:2]),
	}

	dir := filepath.Join("extensions", name)
	migrDir := filepath.Join(dir, "migrations")

	// Check if dir exists
	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("Error: directory '%s' already exists\n", dir)
		os.Exit(1)
	}

	// Create directories
	if err := os.MkdirAll(migrDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		os.Exit(1)
	}

	// Generate files
	files := map[string]string{
		"model.go":        modelTpl,
		"dto.go":          dtoTpl,
		"repo.go":         repoTpl,
		name + "_repo.go": repoImplTpl,
		"service.go":      serviceTpl,
		"registration.go": registrationTpl,
		"register.go":     registerTpl,
	}

	for filename, tpl := range files {
		if err := generateFile(filepath.Join(dir, filename), tpl, data); err != nil {
			fmt.Printf("Error generating %s: %v\n", filename, err)
			os.Exit(1)
		}
	}

	// Generate migration
	migrFile := filepath.Join(migrDir, fmt.Sprintf("10001_%s.sql", table))
	if entityType == "catalog" {
		if err := generateFile(migrFile, catalogMigrationTpl, data); err != nil {
			fmt.Printf("Error generating migration: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := generateFile(migrFile, documentMigrationTpl, data); err != nil {
			fmt.Printf("Error generating migration: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("✓ Extension '%s' scaffolded at %s/\n\n", name, dir)
	fmt.Println("Files created:")
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Printf("  %s\n", rel)
		return nil
	})

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Review and customize the generated code\n")
	fmt.Printf("  2. Uncomment in cmd/server/main.go:\n")
	fmt.Printf("     %s.Register(factoryReg, platform.ExtensionConfig{})\n", name)
	fmt.Printf("  3. Run migrations:\n")
	fmt.Printf("     go run cmd/tenant/main.go migrate --all\n")
	fmt.Printf("  4. Verify build:\n")
	fmt.Printf("     make check-extensions\n")
}

func printUsage() {
	fmt.Println(`Metapus Extension Scaffold

Usage:
  go run cmd/scaffold/main.go --name <name> [--type catalog|document] [--table <table_name>]

Options:
  --name    Entity name in lowercase (e.g. vehicle, waybill). Required.
  --type    Entity type: catalog (default) or document.
  --table   Database table name. Auto-generated if not provided.

Examples:
  go run cmd/scaffold/main.go --name vehicle --type catalog
  go run cmd/scaffold/main.go --name waybill --type document
  go run cmd/scaffold/main.go --name payment_order --type document --table doc_payment_orders`)
}

// ── Template data ─────────────────────────────────────────────────────────

type scaffoldData struct {
	Name       string // original (vehicle)
	PascalName string // Vehicle
	SnakeName  string // vehicle
	KebabName  string // vehicle
	CamelName  string // vehicle
	TableName  string // cat_vehicles
	EntityType string // catalog | document
	CodePrefix string // VH
}

// ── Templates ─────────────────────────────────────────────────────────────

var modelTpl = `package {{.Name}}

import (
	"context"

	"metapus/internal/platform"
)

// {{.PascalName}} represents the {{.Name}} entity.
type {{.PascalName}} struct {
	platform.Catalog

	// TODO: Add entity-specific fields with struct tags:
	// FieldName string ` + "`" + `db:"field_name" json:"fieldName" meta:"label:Field Label,required"` + "`" + `
}

// New{{.PascalName}} creates a new {{.PascalName}} with required fields.
func New{{.PascalName}}(code, name string) *{{.PascalName}} {
	return &{{.PascalName}}{
		Catalog: platform.NewCatalog(code, name),
	}
}

// Validate implements entity.Validatable interface.
func (e *{{.PascalName}}) Validate(ctx context.Context) error {
	if err := e.Catalog.Validate(ctx); err != nil {
		return err
	}

	// TODO: Add entity-specific validation rules:
	// if e.FieldName == "" {
	//     return platform.NewValidation("field_name is required").
	//         WithDetail("field", "fieldName")
	// }

	return nil
}
`

var dtoTpl = `package {{.Name}}

import "metapus/internal/platform"

// --- Request DTOs ---

// Create{{.PascalName}}Request is the request body for creating.
type Create{{.PascalName}}Request struct {
	Code     string  ` + "`" + `json:"code"` + "`" + `
	Name     string  ` + "`" + `json:"name" binding:"required"` + "`" + `
	ParentID *string ` + "`" + `json:"parentId"` + "`" + `
	IsFolder bool    ` + "`" + `json:"isFolder"` + "`" + `
	// TODO: Add entity-specific fields
}

// ToEntity converts DTO to domain entity.
func (r *Create{{.PascalName}}Request) ToEntity() *{{.PascalName}} {
	e := New{{.PascalName}}(r.Code, r.Name)
	e.IsFolder = r.IsFolder
	if r.ParentID != nil && *r.ParentID != "" {
		if pid, err := platform.ParseID(*r.ParentID); err == nil {
			e.SetParent(pid)
		}
	}
	return e
}

// Update{{.PascalName}}Request is the request body for updating.
type Update{{.PascalName}}Request struct {
	Name     *string ` + "`" + `json:"name"` + "`" + `
	ParentID *string ` + "`" + `json:"parentId"` + "`" + `
	IsFolder *bool   ` + "`" + `json:"isFolder"` + "`" + `
	// TODO: Add entity-specific fields
}

// ApplyTo applies the update DTO to an existing entity.
func (r *Update{{.PascalName}}Request) ApplyTo(e *{{.PascalName}}) {
	if r.Name != nil {
		e.Name = *r.Name
	}
	if r.IsFolder != nil {
		e.IsFolder = *r.IsFolder
	}
	if r.ParentID != nil {
		if *r.ParentID == "" {
			e.ClearParent()
		} else if pid, err := platform.ParseID(*r.ParentID); err == nil {
			e.SetParent(pid)
		}
	}
}

// --- Response DTO ---

// {{.PascalName}}Response is the API response.
type {{.PascalName}}Response struct {
	ID           string              ` + "`" + `json:"id"` + "`" + `
	Code         string              ` + "`" + `json:"code"` + "`" + `
	Name         string              ` + "`" + `json:"name"` + "`" + `
	ParentID     *string             ` + "`" + `json:"parentId,omitempty"` + "`" + `
	IsFolder     bool                ` + "`" + `json:"isFolder"` + "`" + `
	DeletionMark bool                ` + "`" + `json:"deletionMark"` + "`" + `
	Attributes   platform.Attributes ` + "`" + `json:"attributes,omitempty"` + "`" + `
	Version      int                 ` + "`" + `json:"version"` + "`" + `
	// TODO: Add entity-specific response fields
}

// From{{.PascalName}} creates response DTO from domain entity.
func From{{.PascalName}}(e *{{.PascalName}}) *{{.PascalName}}Response {
	resp := &{{.PascalName}}Response{
		ID:           e.ID.String(),
		Code:         e.Code,
		Name:         e.Name,
		IsFolder:     e.IsFolder,
		DeletionMark: e.DeletionMark,
		Attributes:   e.Attributes,
		Version:      e.Version,
	}
	if e.ParentID != nil {
		s := e.ParentID.String()
		resp.ParentID = &s
	}
	return resp
}
`

var repoTpl = `package {{.Name}}

import (
	"metapus/internal/domain"
)

// Repository defines the interface for {{.PascalName}} persistence.
type Repository interface {
	domain.CatalogRepository[*{{.PascalName}}]

	// TODO: Add entity-specific repository methods:
	// FindBySomething(ctx context.Context, value string) (*{{.PascalName}}, error)
}
`

var repoImplTpl = `package {{.Name}}

import (
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

const {{.CamelName}}Table = "{{.TableName}}"

// {{.PascalName}}Repo implements Repository using BaseCatalogRepo.
type {{.PascalName}}Repo struct {
	*catalog_repo.BaseCatalogRepo[*{{.PascalName}}]
}

// New{{.PascalName}}Repo creates a new {{.Name}} repository.
func New{{.PascalName}}Repo() *{{.PascalName}}Repo {
	return &{{.PascalName}}Repo{
		BaseCatalogRepo: catalog_repo.NewBaseCatalogRepo[*{{.PascalName}}](
			{{.CamelName}}Table,
			postgres.ExtractDBColumns[{{.PascalName}}](),
			func() *{{.PascalName}} { return &{{.PascalName}}{} },
			true, // hierarchical
		),
	}
}
`

var serviceTpl = `package {{.Name}}

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/domain"
	"metapus/internal/platform"
)

// Service provides business logic for {{.PascalName}}.
type Service struct {
	*domain.CatalogService[*{{.PascalName}}]
	numerator platform.Generator
}

// NewService creates a new {{.PascalName}} service.
func NewService(
	repo Repository,
	numerator platform.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*{{.PascalName}}]{
		Repo:       repo,
		Numerator:  numerator,
		EntityName: "{{.SnakeName}}",
	})

	svc := &Service{
		CatalogService: base,
		numerator:      numerator,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)

	return svc
}

func (s *Service) prepareForCreate(ctx context.Context, e *{{.PascalName}}) error {
	if e.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, platform.DefaultNumeratorConfig("{{.CodePrefix}}"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		e.Code = code
	}
	return nil
}
`

var registrationTpl = `package {{.Name}}

import (
	"metapus/internal/domain"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
)

// {{.PascalName}}Registration implements v1.CatalogRegistration.
type {{.PascalName}}Registration struct{}

func (r *{{.PascalName}}Registration) RoutePrefix() string { return "{{.KebabName}}s" }
func (r *{{.PascalName}}Registration) Permission() string  { return "catalog:{{.SnakeName}}" }
func (r *{{.PascalName}}Registration) EntityName() string   { return "{{.PascalName}}" }

// EntityPresentation returns display names (implements platform.Presentable).
func (r *{{.PascalName}}Registration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "{{.PascalName}}",   // TODO: localize
		Plural:   "{{.PascalName}}s",  // TODO: localize
	}
}

// EntityStruct for metadata inspection (implements platform.Inspectable).
func (r *{{.PascalName}}Registration) EntityStruct() interface{} { return {{.PascalName}}{} }

// Build creates the route handler for this entity.
func (r *{{.PascalName}}Registration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := New{{.PascalName}}Repo()
	service := NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "{{.SnakeName}}", deps.EventWriter)

	config := handlers.CatalogHandlerConfig[
		*{{.PascalName}},
		Create{{.PascalName}}Request,
		Update{{.PascalName}}Request,
	]{
		Service:    service.CatalogService,
		EntityName: "{{.SnakeName}}",
		MapCreateDTO: func(req Create{{.PascalName}}Request) *{{.PascalName}} {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req Update{{.PascalName}}Request, existing *{{.PascalName}}) *{{.PascalName}} {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *{{.PascalName}}) any {
			return From{{.PascalName}}(entity)
		},
	}

	return handlers.NewCatalogHandler(deps.BaseHandler, config)
}
`

var registerTpl = `// register.go — Entry point for the {{.PascalName}} client extension.
// Call Register() from cmd/server/main.go to enable the {{.PascalName}}.
package {{.Name}}

import (
	"metapus/internal/platform"
	v1 "metapus/internal/infrastructure/http/v1"
)

// Register adds all {{.PascalName}} extension entities to the factory registry.
// Call this after content.RegisterDefaults(reg) in main.go:
//
//	factoryReg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(factoryReg)
//	{{.Name}}.Register(factoryReg, platform.ExtensionConfig{})
func Register(reg *v1.FactoryRegistry, _ platform.ExtensionConfig) {
	reg.RegisterCatalog(&{{.PascalName}}Registration{})
}
`

var catalogMigrationTpl = `-- +goose Up
-- Extension: {{.PascalName}} catalog

CREATE TABLE IF NOT EXISTS {{.TableName}} (
    id            UUID PRIMARY KEY DEFAULT fn_uuid_v7(),
    code          TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    parent_id     UUID REFERENCES {{.TableName}}(id),
    is_folder     BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version       INT NOT NULL DEFAULT 1,
    attributes    JSONB NOT NULL DEFAULT '{}',

    -- TODO: Add entity-specific columns here

    -- CDC fields (required by Go infrastructure)
    _deleted_at   TIMESTAMPTZ,
    _txid         BIGINT NOT NULL DEFAULT txid_current(),

    -- Timestamps
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT {{.TableName}}_code_unique UNIQUE (code) WHERE (_deleted_at IS NULL AND code != '')
);

-- Keyset pagination index
CREATE INDEX IF NOT EXISTS idx_{{.TableName}}_keyset
    ON {{.TableName}} (created_at, id) WHERE _deleted_at IS NULL;

-- CDC trigger
CREATE TRIGGER tr_{{.TableName}}_cdc
    BEFORE UPDATE ON {{.TableName}}
    FOR EACH ROW EXECUTE FUNCTION fn_cdc_soft_delete();

-- +goose Down
DROP TABLE IF EXISTS {{.TableName}} CASCADE;
`

var documentMigrationTpl = `-- +goose Up
-- Extension: {{.PascalName}} document

CREATE TABLE IF NOT EXISTS {{.TableName}} (
    id            UUID PRIMARY KEY DEFAULT fn_uuid_v7(),
    number        TEXT NOT NULL DEFAULT '',
    date          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted        BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version       INT NOT NULL DEFAULT 1,
    attributes    JSONB NOT NULL DEFAULT '{}',

    -- TODO: Add document header columns here

    -- CDC fields
    _deleted_at   TIMESTAMPTZ,
    _txid         BIGINT NOT NULL DEFAULT txid_current(),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT {{.TableName}}_number_unique UNIQUE (number) WHERE (_deleted_at IS NULL AND number != '')
);

-- Keyset pagination index
CREATE INDEX IF NOT EXISTS idx_{{.TableName}}_keyset
    ON {{.TableName}} (date, id) WHERE _deleted_at IS NULL;

-- CDC trigger
CREATE TRIGGER tr_{{.TableName}}_cdc
    BEFORE UPDATE ON {{.TableName}}
    FOR EACH ROW EXECUTE FUNCTION fn_cdc_soft_delete();

-- TODO: Add table parts (lines):
-- CREATE TABLE IF NOT EXISTS {{.TableName}}_lines (
--     id         UUID PRIMARY KEY DEFAULT fn_uuid_v7(),
--     header_id  UUID NOT NULL REFERENCES {{.TableName}}(id) ON DELETE CASCADE,
--     line_num   INT NOT NULL DEFAULT 0,
--     ...
-- );

-- +goose Down
DROP TABLE IF EXISTS {{.TableName}} CASCADE;
`

// ── String helpers ────────────────────────────────────────────────────────

func generateFile(path, tplText string, data scaffoldData) (retErr error) {
	t, err := template.New("").Parse(tplText)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() {
		if cErr := f.Close(); cErr != nil && retErr == nil {
			retErr = fmt.Errorf("close file: %w", cErr)
		}
	}()

	return t.Execute(f, data)
}

func toSnakeCase(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	return strings.ToLower(s)
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(c rune) bool {
		return c == '_' || c == '-'
	})
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += string(unicode.ToUpper(rune(p[0]))) + p[1:]
		}
	}
	return result
}

func toCamelCase(s string) string {
	p := toPascalCase(s)
	if len(p) == 0 {
		return p
	}
	return string(unicode.ToLower(rune(p[0]))) + p[1:]
}

func toKebabCase(s string) string {
	s = strings.ReplaceAll(s, "_", "-")
	return strings.ToLower(s)
}
