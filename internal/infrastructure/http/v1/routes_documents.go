package v1

import (
	"github.com/gin-gonic/gin"
	printadapter "metapus/internal/infrastructure/printing"

	"metapus/internal/core/entity"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/registers/settlement"
	"metapus/internal/domain/reports/variants"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

// registerDocumentRoutes registers document endpoints via the Abstract Factory registry.
// Each document type is wired by its DocumentRegistration (see document_factory.go).
// Also populates the metadata registry.
func registerDocumentRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry, eventWriter eventlog.Writer) {
	docsGroup := rg.Group("/document")

	settlementRepo := register_repo.NewSettlementRepo()
	settlementSvc := settlement.NewService(settlementRepo)

	// Use injected PostingEngine or create default
	postingEngine := cfg.PostingEngine
	if postingEngine == nil {
		docLocker := postgres.NewDocLocker()
		recorders := posting.DefaultRecorders(settlementSvc)
		postingEngine = posting.NewEngine(docLocker, recorders...)
	}

	// Configuration registers extend the engine and expose their movements.
	movementProviders := []entity.MovementProvider{settlementSvc}
	for _, module := range cfg.PostingModules {
		movementProviders = append(movementProviders, module.InstallPosting(postingEngine)...)
	}

	// CurrencyResolver is guaranteed non-nil here — created in NewRouter before catalog/document registration.
	currencyResolver := cfg.CurrencyResolver

	printRegistry := documentPrintForms(factoryReg)
	printRenderer, printErr := printadapter.NewRenderer()
	if printErr != nil {
		cfg.Logger.Errorw("failed to load print templates", "error", printErr)
	}

	deps := DocumentDeps{
		BaseHandler:      handlers.NewBaseHandler(),
		PostingEngine:    postingEngine,
		Numerator:        cfg.Numerator,
		CurrencyResolver: currencyResolver,
		PolicyEngine:     cfg.PolicyEngine,
		EventWriter:      eventWriter,
		OutboxPublisher:  postgres.NewOutboxPublisher(),
		PrintRegistry:    printRegistry,
		PrintRenderer:    printRenderer,
		PDFRenderer:      cfg.PDFRenderer,
		RelatedDocFinder: postgres.NewRelatedDocRepo(reg),
		StepUpGuard: func(actionCode string) gin.HandlerFunc {
			return middleware.SensitiveActionGuard(cfg.MFARepo, actionCode, cfg.EventLogRepo)
		},
		StepUpChecker:            cfg.MFARepo,
		MovementProviders:        movementProviders,
		MovementRefResolver:      postgres.NewRefResolverRepo(reg),
		SettingsRepo:             postgres.NewSettingsRepo(),
		CurrencyMetadataResolver: cfg.CurrencyMetadataResolver,
	}

	// Build refEndpoints from catalog factories for document metadata
	refEndpoints := map[string]string{
		"parent": "",
	}
	for _, factory := range factoryReg.Catalogs() {
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
			}
		}
	}

	// Iterate over registered document factories
	for _, factory := range factoryReg.Documents() {
		handler := factory.Build(deps)
		caps := documentCapabilitiesForStandardRegistration(factory, handler)
		RegisterDocumentRoutes(docsGroup.Group("/"+factory.RoutePrefix()), handler, factory.Permission(), caps)
		registerDocumentMetadata(factory, reg, refEndpoints, caps)
	}

	for _, factory := range factoryReg.CustomDocuments() {
		factory.RegisterRoutes(docsGroup.Group("/"+factory.RoutePrefix()), deps)
		caps := documentCapabilitiesForCustomRegistration(factory)
		registerDocumentMetadata(factory, reg, refEndpoints, caps)
	}
}

type documentMetadataRegistration interface {
	entity.DefinitionProvider
	RoutePrefix() string
	Permission() string
	EntityName() string
}

func documentCapabilitiesForStandardRegistration(factory DocumentRegistration, handler DocumentRouteHandler) metadata.DocumentCapabilities {
	if provider, ok := factory.(DocumentCapabilitiesProvider); ok {
		return provider.DocumentCapabilities()
	}
	return InferDocumentCapabilities(handler)
}

func documentCapabilitiesForCustomRegistration(factory CustomDocumentRegistration) metadata.DocumentCapabilities {
	if provider, ok := factory.(DocumentCapabilitiesProvider); ok {
		return provider.DocumentCapabilities()
	}
	return metadata.DocumentCapabilities{
		List: true,
		Read: true,
	}
}

func documentActions(factory documentMetadataRegistration) []metadata.EntityActionDef {
	if provider, ok := factory.(DocumentActionsProvider); ok {
		return provider.DocumentActions()
	}
	return nil
}

func registerDocumentMetadata(factory documentMetadataRegistration, reg *metadata.Registry, refEndpoints map[string]string, caps metadata.DocumentCapabilities) {
	// Auto-register metadata (optional: Inspectable, Presentable)
	var def metadata.EntityDef
	if insp, ok := factory.(platform.Inspectable); ok {
		def = metadata.Inspect(insp.EntityStruct(), factory.EntityName(), metadata.TypeDocument)
	} else {
		def = metadata.EntityDef{Name: factory.EntityName(), Type: metadata.TypeDocument}
	}
	if pres, ok := factory.(platform.Presentable); ok {
		def.Presentation = pres.EntityPresentation()
	}
	identity := factory.EntityDefinition()
	def.Key = identity.Key
	def.ReadPermission = identity.Permission() + ":read"
	def.RoutePrefix = identity.Route
	def.Capabilities = &caps
	def.Actions = documentActions(factory)
	def.TableName = identity.Table
	metadata.MustBindParts(&def, identity)

	def.SetRefEndpoints(refEndpoints)
	if rls, ok := factory.(platform.RLSProvider); ok {
		def.RLSDimensions = rls.RLSDimensions()
	}
	if sf, ok := factory.(platform.SearchFieldsProvider); ok {
		fields := sf.SearchableFields()
		def.SearchColumns = &metadata.SearchColumns{
			SearchCols:  fields.SearchCols,
			TitleCol:    fields.TitleCol,
			SubtitleCol: fields.SubtitleCol,
		}
	}
	reg.Register(def)
}

// registerRegisterRoutes registers accumulation register endpoints via the factory registry.
func registerRegisterRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, metadataReg *metadata.Registry) {
	registers := rg.Group("/registers")
	for _, reg := range factoryReg.Registers() {
		if readable, ok := reg.(ReadMetadataRegistration); ok {
			metadataReg.Register(InspectReadMetadata(readable, metadata.TypeRegister))
		}
		reg.RegisterRoutes(registers.Group("/"+reg.RoutePrefix()), cfg)
	}
}

// registerReportRoutes registers report endpoints via the factory registry.
// All reports use the Dataset-based Query Engine.
func registerReportRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry) *compiler.Compiler {
	reportsGroup := rg.Group("/reports")

	datasets := factoryReg.Datasets()
	if len(datasets) == 0 {
		return nil
	}

	baseHandler := handlers.NewBaseHandler()
	comp := compiler.NewCompiler(reg, datasets, postgres.NewAuthorizedReadService(reg, cfg.PolicyEngine), factoryReg.DatasetExecutors())
	dsHandler := handlers.NewDatasetReportHandler(baseHandler, comp, reg)

	variantRepo := postgres.NewReportVariantRepo()
	datasetPermissions := make(map[string]string, len(datasets))
	for _, ds := range datasets {
		datasetPermissions[ds.Key] = ds.Permission
	}
	variantSvc := variants.NewService(variantRepo, datasetPermissions)
	variantHandler := handlers.NewReportVariantHandler(baseHandler, variantSvc)

	for _, ds := range datasets {
		group := reportsGroup.Group("/" + ds.Key)
		group.Use(middleware.RequirePermission(ds.Permission))
		{
			group.GET("/metadata", dsHandler.HandleMeta(ds.Key))
			group.POST("", dsHandler.HandleExecute(ds.Key))
			group.POST("/export", dsHandler.HandleExport(ds.Key))
			group.POST("/grouped", dsHandler.HandleGrouped(ds.Key))

			group.GET("/variants", variantHandler.GetList(ds.Key))
		}
	}

	reportsGroup.POST("/variants", variantHandler.Create)
	reportsGroup.PUT("/variants/:id", variantHandler.Update)
	reportsGroup.DELETE("/variants/:id", variantHandler.Delete)

	// Mount metadata under /metadata/reports/{key} for discoverability
	metaGroup := rg.Group("/metadata/reports")
	for _, ds := range datasets {
		metaGroup.GET("/"+ds.Key, dsHandler.HandleMeta(ds.Key))
	}

	return comp
}

// documentPrintForms collects the print forms that document registrations
// declare through PrintFormsProvider.
func documentPrintForms(factoryReg *FactoryRegistry) *printing.PrintFormRegistry {
	registry := printing.NewPrintFormRegistry()
	for _, factory := range factoryReg.Documents() {
		if provider, ok := factory.(PrintFormsProvider); ok {
			for _, form := range provider.PrintForms() {
				registry.Register(factory.EntityDefinition().Key, form)
			}
		}
	}
	return registry
}
