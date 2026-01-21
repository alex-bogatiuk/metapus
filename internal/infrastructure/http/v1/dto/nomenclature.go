package dto

import (
	"github.com/shopspring/decimal"

	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/nomenclature"
)

// --- Request DTOs ---

// CreateNomenclatureRequest is the request body for creating a nomenclature item.
type CreateNomenclatureRequest struct {
	Code            string                        `json:"code"`
	Name            string                        `json:"name" binding:"required"`
	Type            nomenclature.NomenclatureType `json:"type" binding:"required"`
	Article         *string                       `json:"article"`
	Barcode         *string                       `json:"barcode"`
	BaseUnitID      *string                       `json:"baseUnitId"`
	BaseUnitName    string                        `json:"baseUnitName"`
	VATRate         nomenclature.VATRate          `json:"vatRate"`
	Weight          decimal.Decimal               `json:"weight"`
	Volume          decimal.Decimal               `json:"volume"`
	Description     *string                       `json:"description"`
	ManufacturerID  *string                       `json:"manufacturerId"`
	CountryOfOrigin *string                       `json:"countryOfOrigin"`
	IsWeighed       bool                          `json:"isWeighed"`
	TrackSerial     bool                          `json:"trackSerial"`
	TrackBatch      bool                          `json:"trackBatch"`
	ImageURL        *string                       `json:"imageUrl"`
	ParentID        *string                       `json:"parentId"`
	IsFolder        bool                          `json:"isFolder"`
	Attributes      entity.Attributes             `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateNomenclatureRequest) ToEntity() *nomenclature.Nomenclature {
	item := nomenclature.NewNomenclature(r.Code, r.Name, r.Type)
	item.Article = r.Article
	item.Barcode = r.Barcode
	item.BaseUnitID = r.BaseUnitID
	if r.VATRate != "" {
		item.VATRate = r.VATRate
	}
	item.Weight = r.Weight
	item.Volume = r.Volume
	item.Description = r.Description
	item.ManufacturerID = r.ManufacturerID
	item.CountryOfOrigin = r.CountryOfOrigin
	item.IsWeighed = r.IsWeighed
	item.TrackSerial = r.TrackSerial
	item.TrackBatch = r.TrackBatch
	item.ImageURL = r.ImageURL
	item.ParentID = r.ParentID
	item.IsFolder = r.IsFolder
	item.Attributes = r.Attributes
	return item
}

// UpdateNomenclatureRequest is the request body for updating a nomenclature item.
type UpdateNomenclatureRequest struct {
	Code            string                        `json:"code"`
	Name            string                        `json:"name" binding:"required"`
	Type            nomenclature.NomenclatureType `json:"type" binding:"required"`
	Article         *string                       `json:"article"`
	Barcode         *string                       `json:"barcode"`
	BaseUnitID      *string                       `json:"baseUnitId"`
	VATRate         nomenclature.VATRate          `json:"vatRate"`
	Weight          decimal.Decimal               `json:"weight"`
	Volume          decimal.Decimal               `json:"volume"`
	Description     *string                       `json:"description"`
	ManufacturerID  *string                       `json:"manufacturerId"`
	CountryOfOrigin *string                       `json:"countryOfOrigin"`
	IsWeighed       bool                          `json:"isWeighed"`
	TrackSerial     bool                          `json:"trackSerial"`
	TrackBatch      bool                          `json:"trackBatch"`
	ImageURL        *string                       `json:"imageUrl"`
	ParentID        *string                       `json:"parentId"`
	IsFolder        bool                          `json:"isFolder"`
	Attributes      entity.Attributes             `json:"attributes"`
	Version         int                           `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateNomenclatureRequest) ApplyTo(item *nomenclature.Nomenclature) {
	item.Code = r.Code
	item.Name = r.Name
	item.Type = r.Type
	item.Article = r.Article
	item.Barcode = r.Barcode
	item.BaseUnitID = r.BaseUnitID
	item.VATRate = r.VATRate
	item.Weight = r.Weight
	item.Volume = r.Volume
	item.Description = r.Description
	item.ManufacturerID = r.ManufacturerID
	item.CountryOfOrigin = r.CountryOfOrigin
	item.IsWeighed = r.IsWeighed
	item.TrackSerial = r.TrackSerial
	item.TrackBatch = r.TrackBatch
	item.ImageURL = r.ImageURL
	item.ParentID = r.ParentID
	item.IsFolder = r.IsFolder
	item.Attributes = r.Attributes
	item.Version = r.Version
}

// --- Response DTOs ---

// NomenclatureResponse is the response body for a nomenclature item.
type NomenclatureResponse struct {
	ID              string                        `json:"id"`
	Code            string                        `json:"code"`
	Name            string                        `json:"name"`
	Type            nomenclature.NomenclatureType `json:"type"`
	Article         *string                       `json:"article,omitempty"`
	Barcode         *string                       `json:"barcode,omitempty"`
	BaseUnitID      *string                       `json:"baseUnitId,omitempty"`
	VATRate         nomenclature.VATRate          `json:"vatRate"`
	Weight          decimal.Decimal               `json:"weight"`
	Volume          decimal.Decimal               `json:"volume"`
	Description     *string                       `json:"description,omitempty"`
	ManufacturerID  *string                       `json:"manufacturerId,omitempty"`
	CountryOfOrigin *string                       `json:"countryOfOrigin,omitempty"`
	IsWeighed       bool                          `json:"isWeighed"`
	TrackSerial     bool                          `json:"trackSerial"`
	TrackBatch      bool                          `json:"trackBatch"`
	ImageURL        *string                       `json:"imageUrl,omitempty"`
	ParentID        *string                       `json:"parentId,omitempty"`
	IsFolder        bool                          `json:"isFolder"`
	DeletionMark    bool                          `json:"deletionMark"`
	Version         int                           `json:"version"`
	Attributes      entity.Attributes             `json:"attributes,omitempty"`
}

// FromNomenclature creates response DTO from domain entity.
func FromNomenclature(item *nomenclature.Nomenclature) *NomenclatureResponse {
	return &NomenclatureResponse{
		ID:              item.ID.String(),
		Code:            item.Code,
		Name:            item.Name,
		Type:            item.Type,
		Article:         item.Article,
		Barcode:         item.Barcode,
		BaseUnitID:      item.BaseUnitID,
		VATRate:         item.VATRate,
		Weight:          item.Weight,
		Volume:          item.Volume,
		Description:     item.Description,
		ManufacturerID:  item.ManufacturerID,
		CountryOfOrigin: item.CountryOfOrigin,
		IsWeighed:       item.IsWeighed,
		TrackSerial:     item.TrackSerial,
		TrackBatch:      item.TrackBatch,
		ImageURL:        item.ImageURL,
		ParentID:        item.ParentID,
		IsFolder:        item.IsFolder,
		DeletionMark:    item.DeletionMark,
		Version:         item.Version,
		Attributes:      item.Attributes,
	}
}
