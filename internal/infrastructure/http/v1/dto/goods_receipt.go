package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/goods_receipt"
)

// --- Request DTOs ---

// CreateGoodsReceiptRequest represents a request to create a goods receipt.
type CreateGoodsReceiptRequest struct {
	Number            string                    `json:"number,omitempty"`
	Date              time.Time                 `json:"date" binding:"required"`
	OrganizationID    string                    `json:"organizationId" binding:"required"`
	SupplierID        string                    `json:"supplierId" binding:"required"`
	WarehouseID       string                    `json:"warehouseId" binding:"required"`
	SupplierDocNumber string                    `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                `json:"supplierDocDate,omitempty"`
	Currency          string                    `json:"currency,omitempty"`
	Comment           string                    `json:"comment,omitempty"`
	Lines             []GoodsReceiptLineRequest `json:"lines" binding:"required,min=1,dive"`
	PostImmediately   bool                      `json:"postImmediately,omitempty"`
}

// GoodsReceiptLineRequest represents a line in create/update request.
type GoodsReceiptLineRequest struct {
	ProductID string  `json:"productId" binding:"required"`
	Quantity  float64 `json:"quantity" binding:"required,gt=0"`
	UnitPrice int64   `json:"unitPrice" binding:"required,gte=0"`
	VATRate   string  `json:"vatRate,omitempty"`
}

// ToEntity converts request to domain entity.
func (r *CreateGoodsReceiptRequest) ToEntity() *goods_receipt.GoodsReceipt {
	supplierID, _ := id.Parse(r.SupplierID)
	warehouseID, _ := id.Parse(r.WarehouseID)

	doc := goods_receipt.NewGoodsReceipt(r.OrganizationID, supplierID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.SupplierDocNumber = r.SupplierDocNumber
	doc.SupplierDocDate = r.SupplierDocDate
	doc.Comment = r.Comment

	if r.Currency != "" {
		doc.Currency = r.Currency
	}

	for _, line := range r.Lines {
		productID, _ := id.Parse(line.ProductID)
		vatRate := line.VATRate
		if vatRate == "" {
			vatRate = "20"
		}
		doc.AddLine(productID, line.Quantity, line.UnitPrice, vatRate)

	}

	return doc
}

// UpdateGoodsReceiptRequest represents a request to update a goods receipt.
type UpdateGoodsReceiptRequest struct {
	Number            *string                   `json:"number,omitempty"`
	Date              *time.Time                `json:"date,omitempty"`
	OrganizationID    *string                   `json:"organizationId,omitempty"`
	SupplierID        *string                   `json:"supplierId,omitempty"`
	WarehouseID       *string                   `json:"warehouseId,omitempty"`
	SupplierDocNumber *string                   `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                `json:"supplierDocDate,omitempty"`
	Currency          *string                   `json:"currency,omitempty"`
	Comment           *string                   `json:"comment,omitempty"`
	Lines             []GoodsReceiptLineRequest `json:"lines,omitempty"`
}

// ApplyTo applies updates to an existing entity.
func (r *UpdateGoodsReceiptRequest) ApplyTo(doc *goods_receipt.GoodsReceipt) {
	if r.Number != nil {
		doc.Number = *r.Number
	}
	if r.Date != nil {
		doc.Date = *r.Date
	}
	if r.OrganizationID != nil {
		doc.OrganizationID = *r.OrganizationID
	}
	if r.SupplierID != nil {
		supplierID, _ := id.Parse(*r.SupplierID)
		doc.SupplierID = supplierID
	}
	if r.WarehouseID != nil {
		warehouseID, _ := id.Parse(*r.WarehouseID)
		doc.WarehouseID = warehouseID
	}
	if r.SupplierDocNumber != nil {
		doc.SupplierDocNumber = *r.SupplierDocNumber
	}
	if r.SupplierDocDate != nil {
		doc.SupplierDocDate = r.SupplierDocDate
	}
	if r.Currency != nil {
		doc.Currency = *r.Currency
	}
	if r.Comment != nil {
		doc.Comment = *r.Comment
	}

	// If lines are provided, rebuild them
	if r.Lines != nil {
		doc.Lines = make([]goods_receipt.GoodsReceiptLine, 0, len(r.Lines))
		for _, line := range r.Lines {
			productID, _ := id.Parse(line.ProductID)
			vatRate := line.VATRate
			if vatRate == "" {
				vatRate = "20"
			}
			doc.AddLine(productID, line.Quantity, line.UnitPrice, vatRate)

		}
	}
}

// --- Response DTOs ---

// GoodsReceiptResponse represents a goods receipt in API responses.
type GoodsReceiptResponse struct {
	ID                string                     `json:"id"`
	Number            string                     `json:"number"`
	Date              time.Time                  `json:"date"`
	Posted            bool                       `json:"posted"`
	PostedVersion     int                        `json:"postedVersion,omitempty"`
	OrganizationID    string                     `json:"organizationId"`
	SupplierID        string                     `json:"supplierId"`
	WarehouseID       string                     `json:"warehouseId"`
	SupplierDocNumber string                     `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                 `json:"supplierDocDate,omitempty"`
	Currency          string                     `json:"currency"`
	TotalQuantity     float64                    `json:"totalQuantity"`
	TotalAmount       int64                      `json:"totalAmount"`
	TotalVAT          int64                      `json:"totalVat"`
	Comment           string                     `json:"comment,omitempty"`
	Lines             []GoodsReceiptLineResponse `json:"lines,omitempty"`
	DeletionMark      bool                       `json:"deletionMark,omitempty"`
	CreatedAt         time.Time                  `json:"createdAt"`
	UpdatedAt         time.Time                  `json:"updatedAt"`
}

// GoodsReceiptLineResponse represents a line in API responses.
type GoodsReceiptLineResponse struct {
	LineID    string  `json:"lineId"`
	LineNo    int     `json:"lineNo"`
	ProductID string  `json:"productId"`
	Quantity  float64 `json:"quantity"`
	UnitPrice int64   `json:"unitPrice"`
	VATRate   string  `json:"vatRate"`
	VATAmount int64   `json:"vatAmount"`
	Amount    int64   `json:"amount"`
}

// FromGoodsReceipt converts domain entity to response DTO.
func FromGoodsReceipt(doc *goods_receipt.GoodsReceipt) *GoodsReceiptResponse {
	resp := &GoodsReceiptResponse{
		ID:                doc.ID.String(),
		Number:            doc.Number,
		Date:              doc.Date,
		Posted:            doc.Posted,
		OrganizationID:    doc.OrganizationID,
		SupplierID:        doc.SupplierID.String(),
		WarehouseID:       doc.WarehouseID.String(),
		SupplierDocNumber: doc.SupplierDocNumber,
		SupplierDocDate:   doc.SupplierDocDate,
		Currency:          doc.Currency,
		TotalQuantity:     doc.TotalQuantity,
		TotalAmount:       doc.TotalAmount,
		TotalVAT:          doc.TotalVAT,
		Comment:           doc.Comment,
		DeletionMark:      doc.DeletionMark,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}

	resp.Lines = make([]GoodsReceiptLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		resp.Lines[i] = GoodsReceiptLineResponse{
			LineID:    line.LineID.String(),
			LineNo:    line.LineNo,
			ProductID: line.ProductID.String(),
			Quantity:  line.Quantity,
			UnitPrice: line.UnitPrice,
			VATRate:   line.VATRate,
			VATAmount: line.VATAmount,
			Amount:    line.Amount,
		}
	}

	return resp
}

// GoodsReceiptListResponse represents a list of goods receipts.
type GoodsReceiptListResponse struct {
	Items      []*GoodsReceiptResponse `json:"items"`
	TotalCount int                     `json:"totalCount"`
	Limit      int                     `json:"limit"`
	Offset     int                     `json:"offset"`
}
