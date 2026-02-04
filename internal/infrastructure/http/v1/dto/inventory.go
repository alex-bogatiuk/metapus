package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/inventory"
)

// --- Request DTOs ---

type CreateInventoryRequest struct {
	Number         string    `json:"number,omitempty"`
	Date           time.Time `json:"date" binding:"required"`
	OrganizationID string    `json:"organizationId" binding:"required"`
	WarehouseID    string    `json:"warehouseId" binding:"required"`
	StartDate      time.Time `json:"startDate" binding:"required"`
	ResponsibleID  *string   `json:"responsibleId,omitempty"`
	Description    string    `json:"description,omitempty"`
}

func (r *CreateInventoryRequest) ToEntity() *inventory.Inventory {
	warehouseID, _ := id.Parse(r.WarehouseID)

	doc := inventory.NewInventory(r.OrganizationID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.StartDate = r.StartDate
	doc.Description = r.Description

	if r.ResponsibleID != nil {
		respID, _ := id.Parse(*r.ResponsibleID)
		doc.ResponsibleID = &respID
	}

	return doc
}

type UpdateInventoryRequest struct {
	Number         *string    `json:"number,omitempty"`
	Date           *time.Time `json:"date,omitempty"`
	OrganizationID *string    `json:"organizationId,omitempty"`
	WarehouseID    *string    `json:"warehouseId,omitempty"`
	StartDate      *time.Time `json:"startDate,omitempty"`
	ResponsibleID  *string    `json:"responsibleId,omitempty"`
	Description    *string    `json:"description,omitempty"`
}

func (r *UpdateInventoryRequest) ApplyTo(doc *inventory.Inventory) {
	if r.Number != nil {
		doc.Number = *r.Number
	}
	if r.Date != nil {
		doc.Date = *r.Date
	}
	if r.OrganizationID != nil {
		doc.OrganizationID = *r.OrganizationID
	}
	if r.WarehouseID != nil {
		warehouseID, _ := id.Parse(*r.WarehouseID)
		doc.WarehouseID = warehouseID
	}
	if r.StartDate != nil {
		doc.StartDate = *r.StartDate
	}
	if r.ResponsibleID != nil {
		respID, _ := id.Parse(*r.ResponsibleID)
		doc.ResponsibleID = &respID
	}
	if r.Description != nil {
		doc.Description = *r.Description
	}
}

type RecordCountRequest struct {
	LineNo         int     `json:"lineNo" binding:"required,min=1"`
	ActualQuantity float64 `json:"actualQuantity" binding:"required,gte=0"`
}

// --- Response DTOs ---

type InventoryResponse struct {
	ID                    string                  `json:"id"`
	Number                string                  `json:"number"`
	Date                  time.Time               `json:"date"`
	Posted                bool                    `json:"posted"`
	PostedVersion         int                     `json:"postedVersion,omitempty"`
	OrganizationID        string                  `json:"organizationId"`
	WarehouseID           string                  `json:"warehouseId"`
	Status                string                  `json:"status"`
	StartDate             time.Time               `json:"startDate"`
	EndDate               *time.Time              `json:"endDate,omitempty"`
	ResponsibleID         *string                 `json:"responsibleId,omitempty"`
	TotalBookQuantity     float64                 `json:"totalBookQuantity"`
	TotalActualQuantity   float64                 `json:"totalActualQuantity"`
	TotalSurplusQuantity  float64                 `json:"totalSurplusQuantity"`
	TotalShortageQuantity float64                 `json:"totalShortageQuantity"`
	Description           string                  `json:"description,omitempty"`
	Lines                 []InventoryLineResponse `json:"lines,omitempty"`
	DeletionMark          bool                    `json:"deletionMark,omitempty"`
	CreatedAt             time.Time               `json:"createdAt"`
	UpdatedAt             time.Time               `json:"updatedAt"`
}

type InventoryLineResponse struct {
	LineID          string     `json:"lineId"`
	LineNo          int        `json:"lineNo"`
	ProductID       string     `json:"productId"`
	BookQuantity    float64    `json:"bookQuantity"`
	ActualQuantity  *float64   `json:"actualQuantity,omitempty"`
	Deviation       float64    `json:"deviation"`
	UnitPrice       int64      `json:"unitPrice"`
	DeviationAmount int64      `json:"deviationAmount"`
	Counted         bool       `json:"counted"`
	CountedAt       *time.Time `json:"countedAt,omitempty"`
	CountedBy       *string    `json:"countedBy,omitempty"`
}

func FromInventory(doc *inventory.Inventory) *InventoryResponse {
	resp := &InventoryResponse{
		ID:                    doc.ID.String(),
		Number:                doc.Number,
		Date:                  doc.Date,
		Posted:                doc.Posted,
		PostedVersion:         doc.PostedVersion,
		OrganizationID:        doc.OrganizationID,
		WarehouseID:           doc.WarehouseID.String(),
		Status:                string(doc.Status),
		StartDate:             doc.StartDate,
		EndDate:               doc.EndDate,
		TotalBookQuantity:     doc.TotalBookQuantity,
		TotalActualQuantity:   doc.TotalActualQuantity,
		TotalSurplusQuantity:  doc.TotalSurplusQuantity,
		TotalShortageQuantity: doc.TotalShortageQuantity,
		Description:           doc.Description,
		DeletionMark:          doc.DeletionMark,
		CreatedAt:             doc.CreatedAt,
		UpdatedAt:             doc.UpdatedAt,
	}

	if doc.ResponsibleID != nil {
		respIDStr := doc.ResponsibleID.String()
		resp.ResponsibleID = &respIDStr
	}

	resp.Lines = make([]InventoryLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		resp.Lines[i] = InventoryLineResponse{
			LineID:          line.LineID.String(),
			LineNo:          line.LineNo,
			ProductID:       line.ProductID.String(),
			BookQuantity:    line.BookQuantity,
			ActualQuantity:  line.ActualQuantity,
			Deviation:       line.Deviation,
			UnitPrice:       line.UnitPrice,
			DeviationAmount: line.DeviationAmount,
			Counted:         line.Counted,
			CountedAt:       line.CountedAt,
			CountedBy:       line.CountedBy,
		}
	}

	return resp
}

type InventoryListResponse struct {
	Items      []*InventoryResponse `json:"items"`
	TotalCount int                  `json:"totalCount"`
	Limit      int                  `json:"limit"`
	Offset     int                  `json:"offset"`
}

type ComparisonResponse struct {
	InventoryID    string           `json:"inventoryId"`
	WarehouseID    string           `json:"warehouseId"`
	Status         string           `json:"status"`
	Items          []ComparisonItem `json:"items"`
	TotalBookQty   float64          `json:"totalBookQty"`
	TotalActualQty float64          `json:"totalActualQty"`
	TotalSurplus   float64          `json:"totalSurplus"`
	TotalShortage  float64          `json:"totalShortage"`
}

type ComparisonItem struct {
	LineNo          int     `json:"lineNo"`
	ProductID       string  `json:"productId"`
	BookQuantity    float64 `json:"bookQuantity"`
	ActualQuantity  float64 `json:"actualQuantity"`
	Deviation       float64 `json:"deviation"`
	DeviationAmount int64   `json:"deviationAmount"`
	Counted         bool    `json:"counted"`
}

func FromComparison(c *inventory.ComparisonResult) *ComparisonResponse {
	resp := &ComparisonResponse{
		InventoryID:    c.InventoryID.String(),
		WarehouseID:    c.WarehouseID.String(),
		Status:         string(c.Status),
		TotalBookQty:   c.TotalBookQty,
		TotalActualQty: c.TotalActualQty,
		TotalSurplus:   c.TotalSurplus,
		TotalShortage:  c.TotalShortage,
	}

	resp.Items = make([]ComparisonItem, len(c.Items))
	for i, item := range c.Items {
		resp.Items[i] = ComparisonItem{
			LineNo:          item.LineNo,
			ProductID:       item.ProductID.String(),
			BookQuantity:    item.BookQuantity,
			ActualQuantity:  item.ActualQuantity,
			Deviation:       item.Deviation,
			DeviationAmount: item.DeviationAmount,
			Counted:         item.Counted,
		}
	}

	return resp
}
