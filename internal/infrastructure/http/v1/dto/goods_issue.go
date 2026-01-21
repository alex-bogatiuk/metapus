package dto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/goods_issue"
)

// --- Request DTOs ---

type CreateGoodsIssueRequest struct {
	Number              string                  `json:"number,omitempty"`
	Date                time.Time               `json:"date" binding:"required"`
	OrganizationID      string                  `json:"organizationId" binding:"required"`
	CustomerID          string                  `json:"customerId" binding:"required"`
	WarehouseID         string                  `json:"warehouseId" binding:"required"`
	CustomerOrderNumber string                  `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	Currency            string                  `json:"currency,omitempty"`
	Comment             string                  `json:"comment,omitempty"`
	Lines               []GoodsIssueLineRequest `json:"lines" binding:"required,min=1,dive"`
	PostImmediately     bool                    `json:"postImmediately,omitempty"`
}

type GoodsIssueLineRequest struct {
	ProductID string  `json:"productId" binding:"required"`
	Quantity  float64 `json:"quantity" binding:"required,gt=0"`
	UnitPrice int64   `json:"unitPrice" binding:"required,gte=0"`
	VATRate   string  `json:"vatRate,omitempty"`
}

func (r *CreateGoodsIssueRequest) ToEntity() *goods_issue.GoodsIssue {
	customerID, _ := id.Parse(r.CustomerID)
	warehouseID, _ := id.Parse(r.WarehouseID)

	doc := goods_issue.NewGoodsIssue(r.OrganizationID, customerID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.CustomerOrderNumber = r.CustomerOrderNumber
	doc.CustomerOrderDate = r.CustomerOrderDate
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

type UpdateGoodsIssueRequest struct {
	Number              *string                 `json:"number,omitempty"`
	Date                *time.Time              `json:"date,omitempty"`
	OrganizationID      *string                 `json:"organizationId,omitempty"`
	CustomerID          *string                 `json:"customerId,omitempty"`
	WarehouseID         *string                 `json:"warehouseId,omitempty"`
	CustomerOrderNumber *string                 `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	Currency            *string                 `json:"currency,omitempty"`
	Comment             *string                 `json:"comment,omitempty"`
	Lines               []GoodsIssueLineRequest `json:"lines,omitempty"`
}

func (r *UpdateGoodsIssueRequest) ApplyTo(doc *goods_issue.GoodsIssue) {
	if r.Number != nil {
		doc.Number = *r.Number
	}
	if r.Date != nil {
		doc.Date = *r.Date
	}
	if r.OrganizationID != nil {
		doc.OrganizationID = *r.OrganizationID
	}
	if r.CustomerID != nil {
		customerID, _ := id.Parse(*r.CustomerID)
		doc.CustomerID = customerID
	}
	if r.WarehouseID != nil {
		warehouseID, _ := id.Parse(*r.WarehouseID)
		doc.WarehouseID = warehouseID
	}
	if r.CustomerOrderNumber != nil {
		doc.CustomerOrderNumber = *r.CustomerOrderNumber
	}
	if r.CustomerOrderDate != nil {
		doc.CustomerOrderDate = r.CustomerOrderDate
	}
	if r.Currency != nil {
		doc.Currency = *r.Currency
	}
	if r.Comment != nil {
		doc.Comment = *r.Comment
	}

	if r.Lines != nil {
		doc.Lines = make([]goods_issue.GoodsIssueLine, 0, len(r.Lines))
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

type GoodsIssueResponse struct {
	ID                  string                   `json:"id"`
	Number              string                   `json:"number"`
	Date                time.Time                `json:"date"`
	Posted              bool                     `json:"posted"`
	PostedVersion       int                      `json:"postedVersion,omitempty"`
	OrganizationID      string                   `json:"organizationId"`
	CustomerID          string                   `json:"customerId"`
	WarehouseID         string                   `json:"warehouseId"`
	CustomerOrderNumber string                   `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time               `json:"customerOrderDate,omitempty"`
	Currency            string                   `json:"currency"`
	TotalQuantity       float64                  `json:"totalQuantity"`
	TotalAmount         int64                    `json:"totalAmount"`
	TotalVAT            int64                    `json:"totalVat"`
	Comment             string                   `json:"comment,omitempty"`
	Lines               []GoodsIssueLineResponse `json:"lines,omitempty"`
	DeletionMark        bool                     `json:"deletionMark,omitempty"`
	CreatedAt           time.Time                `json:"createdAt"`
	UpdatedAt           time.Time                `json:"updatedAt"`
}

type GoodsIssueLineResponse struct {
	LineID    string  `json:"lineId"`
	LineNo    int     `json:"lineNo"`
	ProductID string  `json:"productId"`
	Quantity  float64 `json:"quantity"`
	UnitPrice int64   `json:"unitPrice"`
	VATRate   string  `json:"vatRate"`
	VATAmount int64   `json:"vatAmount"`
	Amount    int64   `json:"amount"`
}

func FromGoodsIssue(doc *goods_issue.GoodsIssue) *GoodsIssueResponse {
	resp := &GoodsIssueResponse{
		ID:                  doc.ID.String(),
		Number:              doc.Number,
		Date:                doc.Date,
		Posted:              doc.Posted,
		PostedVersion:       doc.PostedVersion,
		OrganizationID:      doc.OrganizationID,
		CustomerID:          doc.CustomerID.String(),
		WarehouseID:         doc.WarehouseID.String(),
		CustomerOrderNumber: doc.CustomerOrderNumber,
		CustomerOrderDate:   doc.CustomerOrderDate,
		Currency:            doc.Currency,
		TotalQuantity:       doc.TotalQuantity,
		TotalAmount:         doc.TotalAmount,
		TotalVAT:            doc.TotalVAT,
		Comment:             doc.Comment,
		DeletionMark:        doc.DeletionMark,
		CreatedAt:           doc.CreatedAt,
		UpdatedAt:           doc.UpdatedAt,
	}

	resp.Lines = make([]GoodsIssueLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		resp.Lines[i] = GoodsIssueLineResponse{
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

type GoodsIssueListResponse struct {
	Items      []*GoodsIssueResponse `json:"items"`
	TotalCount int                   `json:"totalCount"`
	Limit      int                   `json:"limit"`
	Offset     int                   `json:"offset"`
}
