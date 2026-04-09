package vehicle

import (
	"metapus/internal/platform"
)

// --- Request DTOs ---

// CreateVehicleRequest is the request body for creating a vehicle.
type CreateVehicleRequest struct {
	Code        string  `json:"code"`
	Name        string  `json:"name" binding:"required"`
	PlateNumber string  `json:"plateNumber" binding:"required"`
	Brand       string  `json:"brand" binding:"required"`
	Model       string  `json:"model"`
	Year        int     `json:"year"`
	VIN         *string `json:"vin"`
	IsActive    bool    `json:"isActive"`
	Description *string `json:"description"`
	ParentID    *string `json:"parentId"`
	IsFolder    bool    `json:"isFolder"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateVehicleRequest) ToEntity() *Vehicle {
	v := NewVehicle(r.Code, r.Name, r.PlateNumber, r.Brand)
	v.Model = r.Model
	v.Year = r.Year
	v.VIN = r.VIN
	v.IsActive = r.IsActive
	v.Description = r.Description
	v.IsFolder = r.IsFolder
	if r.ParentID != nil && *r.ParentID != "" {
		if pid, err := platform.ParseID(*r.ParentID); err == nil {
			v.SetParent(pid)
		}
	}
	return v
}

// UpdateVehicleRequest is the request body for updating a vehicle.
type UpdateVehicleRequest struct {
	Name        *string `json:"name"`
	PlateNumber *string `json:"plateNumber"`
	Brand       *string `json:"brand"`
	Model       *string `json:"model"`
	Year        *int    `json:"year"`
	VIN         *string `json:"vin"`
	IsActive    *bool   `json:"isActive"`
	Description *string `json:"description"`
	ParentID    *string `json:"parentId"`
	IsFolder    *bool   `json:"isFolder"`
}

// ApplyTo applies the update DTO to an existing entity.
func (r *UpdateVehicleRequest) ApplyTo(v *Vehicle) {
	if r.Name != nil {
		v.Name = *r.Name
	}
	if r.PlateNumber != nil {
		v.PlateNumber = *r.PlateNumber
	}
	if r.Brand != nil {
		v.Brand = *r.Brand
	}
	if r.Model != nil {
		v.Model = *r.Model
	}
	if r.Year != nil {
		v.Year = *r.Year
	}
	if r.VIN != nil {
		v.VIN = r.VIN
	}
	if r.IsActive != nil {
		v.IsActive = *r.IsActive
	}
	if r.Description != nil {
		v.Description = r.Description
	}
	if r.IsFolder != nil {
		v.IsFolder = *r.IsFolder
	}
	if r.ParentID != nil {
		if *r.ParentID == "" {
			v.ClearParent()
		} else if pid, err := platform.ParseID(*r.ParentID); err == nil {
			v.SetParent(pid)
		}
	}
}

// --- Response DTO ---

// VehicleResponse is the API response for a vehicle.
type VehicleResponse struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	PlateNumber  string            `json:"plateNumber"`
	Brand        string            `json:"brand"`
	Model        string            `json:"model"`
	Year         int               `json:"year"`
	VIN          *string           `json:"vin,omitempty"`
	IsActive     bool              `json:"isActive"`
	Description  *string           `json:"description,omitempty"`
	ParentID     *string           `json:"parentId,omitempty"`
	IsFolder     bool              `json:"isFolder"`
	DeletionMark bool              `json:"deletionMark"`
	Attributes   platform.Attributes `json:"attributes,omitempty"`
	Version      int               `json:"version"`
}

// FromVehicle creates response DTO from domain entity.
func FromVehicle(v *Vehicle) *VehicleResponse {
	resp := &VehicleResponse{
		ID:           v.ID.String(),
		Code:         v.Code,
		Name:         v.Name,
		PlateNumber:  v.PlateNumber,
		Brand:        v.Brand,
		Model:        v.Model,
		Year:         v.Year,
		VIN:          v.VIN,
		IsActive:     v.IsActive,
		Description:  v.Description,
		IsFolder:     v.IsFolder,
		DeletionMark: v.DeletionMark,
		Attributes:   v.Attributes,
		Version:      v.Version,
	}
	if v.ParentID != nil {
		s := v.ParentID.String()
		resp.ParentID = &s
	}
	return resp
}
