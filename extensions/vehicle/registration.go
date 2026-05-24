package vehicle

import (
	"metapus/internal/domain"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
)

// VehicleRegistration implements v1.CatalogRegistration + platform optional interfaces.
// This demonstrates the full extension pattern for a client-specific catalog.
type VehicleRegistration struct{}

// --- Required (v1.CatalogRegistration) ---

func (r *VehicleRegistration) RoutePrefix() string { return "vehicles" }
func (r *VehicleRegistration) Permission() string  { return "catalog:vehicle" }
func (r *VehicleRegistration) EntityName() string  { return "Vehicle" }

func (r *VehicleRegistration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
	repo := NewVehicleRepo()
	service := NewService(repo, deps.Numerator)
	service.SetPolicyEngine(deps.PolicyEngine)
	domain.NewEventLogCatalogService(service.CatalogService, "vehicle", deps.EventWriter)

	config := handlers.CatalogHandlerConfig[
		*Vehicle,
		CreateVehicleRequest,
		UpdateVehicleRequest,
	]{
		Service:    service.CatalogService,
		EntityName: "vehicle",
		MapCreateDTO: func(req CreateVehicleRequest) *Vehicle {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req UpdateVehicleRequest, existing *Vehicle) *Vehicle {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *Vehicle) any {
			return FromVehicle(entity)
		},
	}

	return handlers.NewCatalogHandler(deps.BaseHandler, config)
}

// --- Optional (platform.Presentable) ---

func (r *VehicleRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Транспортное средство",
		Plural:   "Транспортные средства",
		NewLabel: "Новое ТС",
		Genitive: "транспортного средства",
	}
}

// --- Optional (platform.Inspectable) ---

func (r *VehicleRegistration) EntityStruct() any { return Vehicle{} }

// --- Optional (platform.Labeled) ---

func (r *VehicleRegistration) EntityLabel() string {
	return "Транспортные средства"
}

// --- Optional (platform.ReferenceProvider) ---

func (r *VehicleRegistration) ReferenceTypes() []string { return []string{"vehicle"} }
