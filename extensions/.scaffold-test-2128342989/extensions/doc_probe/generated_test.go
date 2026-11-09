package doc_probe
import (
 "testing"
 "strings"
 "github.com/gin-gonic/gin"
 "metapus/internal/core/numerator"
 "metapus/internal/infrastructure/http/contracts"
 v1 "metapus/internal/infrastructure/http/v1"
 "metapus/internal/infrastructure/http/v1/handlers"
)
func TestGeneratedBoundary(t *testing.T) {
 gin.SetMode(gin.TestMode)
 registry:=v1.NewFactoryRegistry(); Register(registry)
 registration:=&DocProbeRegistration{}
 handler:=registration.Build(v1.DocumentDeps{BaseHandler:handlers.NewBaseHandler(),Numerator:numerator.Noop()})
 router:=gin.New()
 v1.RegisterDocumentRoutes(router.Group("/document/"+Definition.Route), handler, Definition.Permission(), registration.DocumentCapabilities())
 bundle:=registration.HTTPContracts()
 declared:=map[string]bool{}
 for _, route:=range bundle.Routes { declared[route.Method+" "+route.Path]=true }
 for _, route:=range router.Routes() {
  key:=route.Method+" "+route.Path
  if !declared[key] { t.Errorf("unregistered route: %s",key) }
  delete(declared,key)
 }
 if len(declared)!=0 { t.Fatalf("contracts without handler routes: %v",declared) }
 roots:=append(contracts.Registry(),bundle.Contracts...)
 output,err:=contracts.Generate(roots);if err!=nil {t.Fatal(err)}
 if !strings.Contains(string(output),"UpdateDocProbeRequest") {t.Fatal("missing update contract")}
 if _,err:=contracts.GenerateRoutes(roots,bundle.Routes);err!=nil {t.Fatal(err)}
 _=numerator.Noop(); _=handlers.NewBaseHandler()
}
