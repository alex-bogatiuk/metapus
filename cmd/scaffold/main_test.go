package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsInvalidOptionsBeforeWriting(t *testing.T) {
	for _, args := range [][]string{{"--name", "../escape"}, {"--name", "type"}, {"--name", "ok", "--type", "other"}, {"--name", "ok", "--type", "document", "--hierarchical"}, {"--name", "ok", "--table", "cat_ok;drop"}, {"--name", "ok", "--migration-version", "1"}} {
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
	}{{"a", "catalog", false}, {"tree_probe", "catalog", true}, {"doc_probe", "document", false}} {
		args := []string{"--name", fixture.name, "--type", fixture.kind}
		if fixture.hierarchy {
			args = append(args, "--hierarchical")
		}
		if err := run(args, work, io.Discard); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(work, "extensions", fixture.name)
		migrations, err := filepath.Glob(filepath.Join(dir, "migrations", "*.sql"))
		if err != nil || len(migrations) != 1 {
			t.Fatalf("migration: %v %v", migrations, err)
		}
		if !strings.HasPrefix(filepath.Base(migrations[0]), []string{"10001_", "10002_", "10003_"}[i]) {
			t.Fatal("migration versions must not collide")
		}
		source, _ := os.ReadFile(migrations[0])
		if strings.Contains(string(source), "fn_uuid_v7") || strings.Contains(string(source), "UNIQUE (") {
			t.Fatal("obsolete SQL in generated migration")
		}
		probe := strings.ReplaceAll(generatedProbe, "PACKAGE", fixture.name)
		pascal := []string{"A", "TreeProbe", "DocProbe"}[i]
		probe = strings.ReplaceAll(probe, "ENTITY", pascal)
		mount := `v1.RegisterCatalogRoutes(router.Group("/catalog/"+Definition.Route), handler, Definition.Permission())`
		if fixture.kind == "document" {
			mount = `v1.RegisterDocumentRoutes(router.Group("/document/"+Definition.Route), handler, Definition.Permission(), registration.DocumentCapabilities())`
		}
		probe = strings.ReplaceAll(probe, "MOUNT", mount)
		probe = strings.ReplaceAll(probe, "DEPS", "v1."+map[string]string{"catalog": "Catalog", "document": "Document"}[fixture.kind]+"Deps{BaseHandler:handlers.NewBaseHandler(),Numerator:numerator.Noop()}")
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
 registration:=&ENTITYRegistration{}
 handler:=registration.Build(DEPS)
 router:=gin.New()
 MOUNT
 bundle:=registration.HTTPContracts()
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
