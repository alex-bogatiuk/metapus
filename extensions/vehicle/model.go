// Package vehicle provides the Vehicle catalog — a client extension example.
// Demonstrates how to add a custom catalog type to Metapus without modifying core code.
//
// This package lives within the main metapus module as a scaffold/template.
// To create a standalone client extension, copy this to a separate repository
// and use pkg/extension/ as the public SDK.
package vehicle

import (
	"context"

	"metapus/internal/platform"
)

// Vehicle represents a fleet vehicle (client-specific catalog).
type Vehicle struct {
	platform.Catalog

	// PlateNumber is the license plate (unique within tenant)
	PlateNumber string `db:"plate_number" json:"plateNumber" meta:"label:Гос. номер,required"`

	// Brand is the vehicle manufacturer
	Brand string `db:"brand" json:"brand" meta:"label:Марка"`

	// Model is the vehicle model name
	Model string `db:"model" json:"model" meta:"label:Модель"`

	// Year is the manufacture year
	Year int `db:"year" json:"year" meta:"label:Год выпуска"`

	// VIN is the vehicle identification number (optional)
	VIN *string `db:"vin" json:"vin,omitempty" meta:"label:VIN"`

	// IsActive indicates if vehicle is in service
	IsActive bool `db:"is_active" json:"isActive" meta:"label:В эксплуатации"`

	// Description is a free-form note
	Description *string `db:"description" json:"description,omitempty" meta:"label:Описание"`
}

// NewVehicle creates a new Vehicle with required fields.
func NewVehicle(code, name, plateNumber, brand string) *Vehicle {
	return &Vehicle{
		Catalog:     platform.NewCatalog(code, name),
		PlateNumber: plateNumber,
		Brand:       brand,
		IsActive:    true,
	}
}

// Validate implements entity.Validatable interface.
func (v *Vehicle) Validate(ctx context.Context) error {
	if err := v.Catalog.Validate(ctx); err != nil {
		return err
	}

	if v.PlateNumber == "" {
		return platform.NewValidation("plate number is required").
			WithDetail("field", "plateNumber")
	}

	if v.Brand == "" {
		return platform.NewValidation("brand is required").
			WithDetail("field", "brand")
	}

	if v.Year != 0 && (v.Year < 1900 || v.Year > 2100) {
		return platform.NewValidation("invalid manufacture year").
			WithDetail("field", "year")
	}

	return nil
}
