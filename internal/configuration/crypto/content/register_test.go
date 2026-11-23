package content

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
)

func TestCryptoSweepHTTPExposesReadRoutesAndRetry(t *testing.T) {
	registration := &CryptoSweepRegistration{}
	caps := registration.DocumentCapabilities()
	if !caps.List || !caps.Read || !caps.ExportList || !caps.Movements || !caps.RelatedDocuments {
		t.Fatalf("missing sweep read capability: %#v", caps)
	}
	if caps.Create || caps.Update || caps.Delete || caps.SetDeletionMark || caps.Post || caps.Unpost || caps.Repost || caps.Copy || caps.BatchAction || caps.BatchActionByFilter || caps.PrintForms {
		t.Fatalf("worker-owned sweep must not expose generic HTTP mutations: %#v", caps)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := registration.Build(v1.DocumentDeps{BaseHandler: handlers.NewBaseHandler()})
	v1.RegisterDocumentRoutes(router.Group("/document/crypto-sweep"), handler, registration.Permission(), caps)
	want := map[string]bool{
		"GET /document/crypto-sweep":                       true,
		"GET /document/crypto-sweep/:id":                   true,
		"POST /document/crypto-sweep/export-list":          true,
		"POST /document/crypto-sweep/:id/retry":            true,
		"GET /document/crypto-sweep/:id/movements":         true,
		"GET /document/crypto-sweep/:id/related-documents": true,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if !want[key] {
			t.Errorf("unexpected sweep route %s", key)
		}
		delete(want, key)
	}
	for route := range want {
		t.Errorf("missing sweep route %s", route)
	}
}

func TestChainOwnedCryptoDocumentsDisableGenericMutationRoutes(t *testing.T) {
	registrations := []v1.DocumentRegistration{
		&CryptoPaymentRegistration{},
		&CryptoWithdrawalRegistration{},
		&CryptoSweepRegistration{},
	}
	gin.SetMode(gin.TestMode)
	for _, registration := range registrations {
		t.Run(registration.EntityName(), func(t *testing.T) {
			provider, ok := registration.(v1.DocumentCapabilitiesProvider)
			if !ok {
				t.Fatal("chain-owned document must declare capabilities")
			}
			caps := provider.DocumentCapabilities()
			if !caps.List || !caps.Read || !caps.ExportList || !caps.Movements || !caps.RelatedDocuments {
				t.Fatalf("missing read capabilities: %#v", caps)
			}
			if caps.Create || caps.Update || caps.Delete || caps.SetDeletionMark || caps.Post || caps.Unpost || caps.Repost || caps.Copy || caps.BatchAction || caps.BatchActionByFilter {
				t.Fatalf("chain-owned document exposes a generic mutation: %#v", caps)
			}
			router := gin.New()
			prefix := "/document/" + registration.RoutePrefix()
			handler := registration.Build(v1.DocumentDeps{BaseHandler: handlers.NewBaseHandler()})
			v1.RegisterDocumentRoutes(router.Group(prefix), handler, registration.Permission(), caps)
			allowedMutations := map[string]bool{
				http.MethodPost + " " + prefix + "/export-list": true,
			}
			if _, ok := registration.(*CryptoSweepRegistration); ok {
				allowedMutations[http.MethodPost+" "+prefix+"/:id/retry"] = true
			}
			for _, route := range router.Routes() {
				if route.Method != http.MethodGet && !allowedMutations[route.Method+" "+route.Path] {
					t.Errorf("unexpected mutation route: %s %s", route.Method, route.Path)
				}
			}
		})
	}
}

func TestCryptoDocumentWorkflowRegistrations(t *testing.T) {
	reg := configuration.Compose(Crypto(Services{}))

	var treasury v1.DocumentRegistration
	for _, doc := range reg.Documents() {
		if doc.EntityName() == "TreasuryWithdrawal" {
			treasury = doc
			break
		}
	}
	if treasury == nil {
		t.Fatal("TreasuryWithdrawal must be registered as a standard document")
	}
	if treasury.RoutePrefix() != "treasury-withdrawal" {
		t.Fatalf("unexpected treasury route prefix: %s", treasury.RoutePrefix())
	}
	if treasury.Permission() != "document:treasury_withdrawal" {
		t.Fatalf("unexpected treasury permission prefix: %s", treasury.Permission())
	}
	treasuryCapsProvider, ok := treasury.(v1.DocumentCapabilitiesProvider)
	if !ok {
		t.Fatal("TreasuryWithdrawal must expose document capabilities")
	}
	treasuryCaps := treasuryCapsProvider.DocumentCapabilities()
	if !treasuryCaps.List || !treasuryCaps.Read || !treasuryCaps.Create || !treasuryCaps.Update || !treasuryCaps.Delete {
		t.Fatalf("unexpected treasury standard capabilities: %#v", treasuryCaps)
	}
	if treasuryCaps.Post || treasuryCaps.Unpost || treasuryCaps.Repost || treasuryCaps.BatchAction || treasuryCaps.BatchActionByFilter {
		t.Fatalf("treasury workflow capabilities must disable generic posting/batch routes: %#v", treasuryCaps)
	}
	treasuryActionsProvider, ok := treasury.(v1.DocumentActionsProvider)
	if !ok {
		t.Fatal("TreasuryWithdrawal must expose document actions")
	}
	treasuryActions := treasuryActionsProvider.DocumentActions()
	if !containsAction(treasuryActions, "approve") || !containsAction(treasuryActions, "reject") {
		t.Fatalf("treasury approve/reject actions must be exposed: %#v", treasuryActions)
	}

	var withdrawalRequest v1.CustomDocumentRegistration
	for _, doc := range reg.CustomDocuments() {
		if doc.EntityName() == "WithdrawalRequest" {
			withdrawalRequest = doc
			break
		}
	}
	if withdrawalRequest == nil {
		t.Fatal("WithdrawalRequest must be registered as a custom document")
	}
	if withdrawalRequest.RoutePrefix() != "withdrawal-request" {
		t.Fatalf("unexpected withdrawal request route prefix: %s", withdrawalRequest.RoutePrefix())
	}
	if withdrawalRequest.Permission() != "document:withdrawal_request" {
		t.Fatalf("unexpected withdrawal request permission prefix: %s", withdrawalRequest.Permission())
	}
	withdrawalCapsProvider, ok := withdrawalRequest.(v1.DocumentCapabilitiesProvider)
	if !ok {
		t.Fatal("WithdrawalRequest must expose document capabilities")
	}
	withdrawalCaps := withdrawalCapsProvider.DocumentCapabilities()
	if !withdrawalCaps.List || !withdrawalCaps.Read {
		t.Fatalf("withdrawal request must expose list/read: %#v", withdrawalCaps)
	}
	if !withdrawalCaps.RelatedDocuments {
		t.Fatalf("withdrawal request must expose related documents: %#v", withdrawalCaps)
	}
	if withdrawalCaps.Create || withdrawalCaps.Update || withdrawalCaps.Delete || withdrawalCaps.Post || withdrawalCaps.Unpost || withdrawalCaps.Repost || withdrawalCaps.SetDeletionMark {
		t.Fatalf("withdrawal request must not expose generic CRUD/posting capabilities: %#v", withdrawalCaps)
	}
	withdrawalActionsProvider, ok := withdrawalRequest.(v1.DocumentActionsProvider)
	if !ok {
		t.Fatal("WithdrawalRequest must expose document actions")
	}
	if !containsAction(withdrawalActionsProvider.DocumentActions(), "approve") {
		t.Fatalf("withdrawal request approve action must be exposed: %#v", withdrawalActionsProvider.DocumentActions())
	}

	var networkOperation v1.DocumentRegistration
	for _, doc := range reg.Documents() {
		if doc.EntityName() == "CryptoNetworkOperation" {
			networkOperation = doc
			break
		}
	}
	if networkOperation == nil {
		t.Fatal("CryptoNetworkOperation must be registered")
	}
	capabilities := networkOperation.(v1.DocumentCapabilitiesProvider).DocumentCapabilities()
	if capabilities.Create || capabilities.Copy || capabilities.Delete || capabilities.Update || capabilities.Post ||
		capabilities.Unpost || capabilities.Repost || capabilities.SetDeletionMark || !capabilities.Read || !capabilities.Movements {
		t.Fatalf("network operations are read-only: %#v", capabilities)
	}
	if _, ok := networkOperation.(v1.DocumentActionsProvider); ok {
		t.Fatal("network operations expose no user actions")
	}
}

func TestCryptoSweepWorkerStatusesAreRegisteredInMetadata(t *testing.T) {
	def := metadata.Inspect(crypto_sweep.CryptoSweep{}, "CryptoSweep", metadata.TypeDocument)
	statusValues := map[string]struct{}{}
	for _, field := range def.Fields {
		if field.Name != "status" {
			continue
		}
		for _, enumValue := range field.EnumValues {
			statusValues[enumValue.Value] = struct{}{}
		}
	}
	for _, status := range []crypto_sweep.SweepStatus{
		crypto_sweep.SweepStatusCreated,
		crypto_sweep.SweepStatusSigned,
		crypto_sweep.SweepStatusBroadcast,
		crypto_sweep.SweepStatusConfirmed,
		crypto_sweep.SweepStatusPartialFailed,
		crypto_sweep.SweepStatusHeld,
	} {
		if _, ok := statusValues[string(status)]; !ok {
			t.Fatalf("CryptoSweep status %q is not registered in metadata", status)
		}
	}
}

func TestBlockchainNetworkEnumsAreRegisteredInMetadata(t *testing.T) {
	def := metadata.Inspect(blockchain_network.BlockchainNetwork{}, "BlockchainNetwork", metadata.TypeCatalog)
	var chainFamilyField, finalityModeField *metadata.FieldDef
	for i := range def.Fields {
		if def.Fields[i].Name == "chainFamily" {
			chainFamilyField = &def.Fields[i]
		}
		if def.Fields[i].Name == "finalityMode" {
			finalityModeField = &def.Fields[i]
		}
	}
	if chainFamilyField == nil {
		t.Fatal("chainFamily field not found in metadata")
	}
	if chainFamilyField.Type != metadata.TypeEnum {
		t.Fatalf("chainFamily type = %v, want %v", chainFamilyField.Type, metadata.TypeEnum)
	}
	if len(chainFamilyField.EnumValues) != 4 {
		t.Fatalf("chainFamily enum values count = %d, want 4", len(chainFamilyField.EnumValues))
	}

	if finalityModeField == nil {
		t.Fatal("finalityMode field not found in metadata")
	}
	if finalityModeField.Type != metadata.TypeEnum {
		t.Fatalf("finalityMode type = %v, want %v", finalityModeField.Type, metadata.TypeEnum)
	}
	if len(finalityModeField.EnumValues) != 3 {
		t.Fatalf("finalityMode enum values count = %d, want 3", len(finalityModeField.EnumValues))
	}
}

func TestTokenStandardEnumIsRegisteredInMetadata(t *testing.T) {
	def := metadata.Inspect(token.Token{}, "Token", metadata.TypeCatalog)
	var standardField *metadata.FieldDef
	for i := range def.Fields {
		if def.Fields[i].Name == "tokenStandard" {
			standardField = &def.Fields[i]
		}
	}
	if standardField == nil {
		t.Fatal("tokenStandard field not found in metadata")
	}
	if standardField.Type != metadata.TypeEnum {
		t.Fatalf("tokenStandard type = %v, want %v", standardField.Type, metadata.TypeEnum)
	}
	if len(standardField.EnumValues) != 6 {
		t.Fatalf("tokenStandard enum values count = %d, want 6", len(standardField.EnumValues))
	}
}

func containsAction(actions []metadata.EntityActionDef, key string) bool {
	for _, action := range actions {
		if action.Key == key {
			return true
		}
	}
	return false
}
