package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	dbassets "metapus/db"
)

func TestRejectsInvalidOptionsBeforeWriting(t *testing.T) {
	for _, args := range [][]string{{"--name", "../escape"}, {"--name", "type"}, {"--name", "ok", "--type", "other"}, {"--name", "ok", "--type", "document", "--hierarchical"}, {"--name", "ok", "--table", "cat_ok;drop"}, {"--name", "ok", "--migration-version", "1"}, {"--name", "ok", "--parts", "goods"}, {"--name", "ok", "--type", "document", "--parts", "Goods"}, {"--name", "ok", "--type", "document", "--parts", "goods,goods"}} {
		root := t.TempDir()
		if err := run(args, root, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("invalid arguments created files: %v", entries)
		}
	}
}

func TestMigrationGoesToTheEmbeddedTenantSequence(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"db/migrations/00001_core.sql":                "core",
		"db/configuration/other/10004_cat_others.sql": "other",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"--name", "vehicle"}, root, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "db", "configuration", "vehicle", "10005_cat_vehicles.sql")); err != nil {
		t.Fatalf("migration must take the next free version in db/configuration/vehicle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "extensions", "vehicle", "migrations")); !os.IsNotExist(err) {
		t.Fatalf("extensions/<name>/migrations is not embedded and must not be generated: %v", err)
	}
	if err := run([]string{"--name", "vehicle", "--migration-version", "10004"}, root, io.Discard); err == nil {
		t.Fatal("accepted an existing module")
	}
}

func TestRefusesAnExistingConfigurationMigrationDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "db", "configuration", "crypto"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--name", "crypto"}, root, io.Discard); err == nil {
		t.Fatal("accepted a name whose db/configuration directory already exists")
	}
	if _, err := os.Stat(filepath.Join(root, "extensions", "crypto")); !os.IsNotExist(err) {
		t.Fatalf("refused run left a module behind: %v", err)
	}
}

func TestGeneratedModulesCompileAndMatchTheirHTTPContracts(t *testing.T) {
	// Keep generated packages inside the module so Go's internal visibility rules
	// are tested, without registering a test entity in the application.
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(filepath.Join(root, "extensions"), ".scaffold-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	for i, fixture := range []struct {
		name, kind string
		hierarchy  bool
		parts      string
	}{{"a", "catalog", false, ""}, {"tree_probe", "catalog", true, ""}, {"doc_probe", "document", false, ""}, {"parts_probe", "document", false, "goods,extra_costs"}} {
		args := []string{"--name", fixture.name, "--type", fixture.kind}
		if fixture.hierarchy {
			args = append(args, "--hierarchical")
		}
		if fixture.parts != "" {
			args = append(args, "--parts", fixture.parts)
		}
		if err := run(args, work, io.Discard); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(work, "extensions", fixture.name)
		migrations, err := filepath.Glob(filepath.Join(work, "db", "configuration", fixture.name, "*.sql"))
		if err != nil || len(migrations) != 1 {
			t.Fatalf("migration: %v %v", migrations, err)
		}
		if !strings.HasPrefix(filepath.Base(migrations[0]), []string{"10001_", "10002_", "10003_", "10004_"}[i]) {
			t.Fatal("migration versions must not collide")
		}
		source, _ := os.ReadFile(migrations[0])
		if err := dbassets.CheckMigrationFormat(string(source)); err != nil {
			t.Fatalf("generated migration breaks the migration format: %v\n%s", err, source)
		}
		if strings.Contains(string(source), "fn_uuid_v7") || strings.Contains(string(source), "UNIQUE (") {
			t.Fatal("obsolete SQL in generated migration")
		}
		probe := strings.ReplaceAll(generatedProbe, "PACKAGE", fixture.name)
		if fixture.parts != "" {
			for _, want := range []string{"CREATE TABLE doc_parts_probes__goods", "CREATE TABLE doc_parts_probes__extra_costs", "REFERENCES doc_parts_probes(id) ON DELETE CASCADE", "DROP TABLE doc_parts_probes__extra_costs;"} {
				if !strings.Contains(string(source), want) {
					t.Fatalf("part migration lacks %q:\n%s", want, source)
				}
			}
			model, _ := os.ReadFile(filepath.Join(dir, "model.go"))
			for _, want := range []string{`{Key: "extra_costs", Field: "extraCosts"}`, "ExtraCosts []PartsProbeExtraCostsLine", "type PartsProbeGoodsLine struct"} {
				if !strings.Contains(string(model), want) {
					t.Fatalf("part model lacks %q:\n%s", want, model)
				}
			}
		}
		pascal := []string{"A", "TreeProbe", "DocProbe", "PartsProbe"}[i]
		probe = strings.ReplaceAll(probe, "ENTITY", pascal)
		for _, generated := range []string{"model.go", "register.go"} {
			if _, err := os.Stat(filepath.Join(dir, generated)); err != nil {
				t.Fatalf("declarative module lacks %s: %v", generated, err)
			}
		}
		if files, _ := filepath.Glob(filepath.Join(dir, "*.go")); len(files) != 2 {
			t.Fatalf("a declarative module is a model and its registration, got %v", files)
		}
		mount := `v1.RegisterCatalogRoutes(router.Group("/catalog/"+Definition.Route), registry.Catalogs()[0].Build(v1.CatalogDeps{BaseHandler:handlers.NewBaseHandler(),Numerator:numerator.Noop()}), Definition.Permission())`
		if fixture.kind == "document" {
			mount = `registration:=registry.Documents()[0]; v1.RegisterDocumentRoutes(router.Group("/document/"+Definition.Route), registration.Build(v1.DocumentDeps{BaseHandler:handlers.NewBaseHandler(),Numerator:numerator.Noop()}), Definition.Permission(), registration.(v1.DocumentCapabilitiesProvider).DocumentCapabilities())`
		}
		probe = strings.ReplaceAll(probe, "MOUNT", mount)
		if err := os.WriteFile(filepath.Join(dir, "generated_test.go"), []byte(probe), 0644); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("go", "test", dir)
		command.Dir = root
		command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", fixture.name, err, output)
		}
	}
}

const generatedProbe = `package PACKAGE
import (
 "testing"
 "strings"
 "github.com/gin-gonic/gin"
 "metapus/internal/core/numerator"
 "metapus/internal/content"
 "metapus/internal/infrastructure/http/contracts"
 v1 "metapus/internal/infrastructure/http/v1"
 "metapus/internal/infrastructure/http/v1/handlers"
)
func TestGeneratedBoundary(t *testing.T) {
 gin.SetMode(gin.TestMode)
 registry:=v1.NewFactoryRegistry(); Register(registry)
 router:=gin.New()
 MOUNT
 bundle:=registry.HTTPContracts()
 declared:=map[string]bool{}
 for _, route:=range bundle.Routes { declared[route.Method+" "+route.Path]=true }
 for _, route:=range router.Routes() {
  key:=route.Method+" "+route.Path
  if !declared[key] { t.Errorf("unregistered route: %s",key) }
  delete(declared,key)
 }
 if len(declared)!=0 { t.Fatalf("contracts without handler routes: %v",declared) }
 roots:=append(contracts.Registry(content.HTTPContractBundles()...),bundle.Contracts...)
 output,err:=contracts.Generate(roots);if err!=nil {t.Fatal(err)}
 if !strings.Contains(string(output),"UpdateENTITYRequest") {t.Fatal("missing update contract")}
 if _,err:=contracts.GenerateRoutes(roots,bundle.Routes);err!=nil {t.Fatal(err)}
 _=numerator.Noop(); _=handlers.NewBaseHandler()
}
`
