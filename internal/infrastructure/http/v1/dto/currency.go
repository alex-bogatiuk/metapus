package dto

import (
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/entity"
	"metapus/internal/domain/catalogs/currency"
)

// --- Request DTOs ---

// CreateCurrencyRequest is the request body for creating a currency.
type CreateCurrencyRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	ISOCode        *string           `json:"isoCode" binding:"required"`
	ISONumericCode *string           `json:"isoNumericCode"`
	Symbol         *string           `json:"symbol" binding:"required"`
	DecimalPlaces  int               `json:"decimalPlaces"`
	IsBase         bool              `json:"isBase"`
	Country        *string           `json:"country"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
}

// ToEntity converts DTO to domain entity.
func (r *CreateCurrencyRequest) ToEntity() *currency.Currency {
	c := currency.NewCurrency(r.Code, r.Name, r.ISOCode, r.Symbol)
	c.ISONumericCode = r.ISONumericCode

	if r.DecimalPlaces >= 0 {
		c.DecimalPlaces = r.DecimalPlaces
	}
	c.IsBase = r.IsBase
	c.Country = r.Country
	c.ParentID = r.ParentID
	c.IsFolder = r.IsFolder
	c.Attributes = r.Attributes
	return c
}

// UpdateCurrencyRequest is the request body for updating a currency.
type UpdateCurrencyRequest struct {
	Code           string            `json:"code"`
	Name           string            `json:"name" binding:"required"`
	ISOCode        *string           `json:"isoCode" binding:"required"`
	ISONumericCode *string           `json:"isoNumericCode"`
	Symbol         *string           `json:"symbol" binding:"required"`
	DecimalPlaces  int               `json:"decimalPlaces"`
	IsBase         bool              `json:"isBase"`
	Country        *string           `json:"country"`
	ParentID       *string           `json:"parentId"`
	IsFolder       bool              `json:"isFolder"`
	Attributes     entity.Attributes `json:"attributes"`
	Version        int               `json:"version" binding:"required"`
}

// ApplyTo applies update DTO to existing entity.
func (r *UpdateCurrencyRequest) ApplyTo(c *currency.Currency) {
	c.Code = r.Code
	c.Name = r.Name
	c.ISOCode = r.ISOCode
	c.ISONumericCode = r.ISONumericCode
	c.Symbol = r.Symbol
	c.DecimalPlaces = r.DecimalPlaces
	c.IsBase = r.IsBase
	c.Country = r.Country
	c.ParentID = r.ParentID
	c.IsFolder = r.IsFolder
	c.Attributes = r.Attributes
	c.Version = r.Version
}

// UpdateExchangeRateRequest is the request body for updating exchange rate.
type UpdateExchangeRateRequest struct {
	Rate decimal.Decimal `json:"rate" binding:"required"`
	Date time.Time       `json:"date"`
}

// --- Response DTOs ---

// CurrencyResponse is the response body for a currency.
type CurrencyResponse struct {
	ID             string            `json:"id"`
	Code           string            `json:"code"`
	Name           string            `json:"name"`
	ISOCode        *string           `json:"isoCode"`
	ISONumericCode *string           `json:"isoNumericCode,omitempty"`
	Symbol         *string           `json:"symbol"`
	DecimalPlaces  int               `json:"decimalPlaces"`
	IsBase         bool              `json:"isBase"`
	Country        *string           `json:"country,omitempty"`
	ParentID       *string           `json:"parentId,omitempty"`
	IsFolder       bool              `json:"isFolder"`
	DeletionMark   bool              `json:"deletionMark"`
	Version        int               `json:"version"`
	Attributes     entity.Attributes `json:"attributes,omitempty"`
}

// FromCurrency creates response DTO from domain entity.
func FromCurrency(c *currency.Currency) *CurrencyResponse {
	return &CurrencyResponse{
		ID:             c.ID.String(),
		Code:           c.Code,
		Name:           c.Name,
		ISOCode:        c.ISOCode,
		ISONumericCode: c.ISONumericCode,
		Symbol:         c.Symbol,
		DecimalPlaces:  c.DecimalPlaces,
		IsBase:         c.IsBase,
		Country:        c.Country,
		ParentID:       c.ParentID,
		IsFolder:       c.IsFolder,
		DeletionMark:   c.DeletionMark,
		Version:        c.Version,
		Attributes:     c.Attributes,
	}
}
