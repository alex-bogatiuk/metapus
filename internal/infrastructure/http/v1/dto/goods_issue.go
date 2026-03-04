package dto

import (
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

type CreateGoodsIssueRequest struct {
	Number              string                  `json:"number,omitempty"`
	Date                time.Time               `json:"date" binding:"required"`
	OrganizationID      string                  `json:"organizationId" binding:"required"`
	CustomerID          string                  `json:"customerId" binding:"required"`
	ContractID          *string                 `json:"contractId,omitempty"`
	WarehouseID         string                  `json:"warehouseId" binding:"required"`
	CustomerOrderNumber string                  `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	CurrencyID          string                  `json:"currencyId,omitempty"`
	AmountIncludesVAT   bool                    `json:"amountIncludesVat"`
	Description         string                  `json:"description,omitempty"`
	Lines               []GoodsIssueLineRequest `json:"lines" binding:"required,min=1,dive"`
	PostImmediately     bool                    `json:"postImmediately,omitempty"`
}

type GoodsIssueLineRequest struct {
	ProductID       string           `json:"productId" binding:"required"`
	UnitID          string           `json:"unitId" binding:"required"`
	Coefficient     decimal.Decimal  `json:"coefficient"`
	Quantity        types.Quantity   `json:"quantity" binding:"required,gt=0"`
	UnitPrice       types.MinorUnits `json:"unitPrice" binding:"required,gte=0"`
	VATRateID       string           `json:"vatRateId" binding:"required"`
	VATPercent      int              `json:"vatPercent"`
	DiscountPercent decimal.Decimal  `json:"discountPercent"`
}

func (r *CreateGoodsIssueRequest) ToEntity() *goods_issue.GoodsIssue {
	customerID, _ := id.Parse(r.CustomerID)
	warehouseID, _ := id.Parse(r.WarehouseID)

	orgID, _ := id.Parse(r.OrganizationID)
	doc := goods_issue.NewGoodsIssue(orgID, customerID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.CustomerOrderNumber = r.CustomerOrderNumber
	doc.CustomerOrderDate = r.CustomerOrderDate
	doc.AmountIncludesVAT = r.AmountIncludesVAT
	doc.Description = r.Description

	if r.ContractID != nil {
		contractID, _ := id.Parse(*r.ContractID)
		doc.ContractID = &contractID
	}

	if r.CurrencyID != "" {
		currencyID, _ := id.Parse(r.CurrencyID)
		doc.CurrencyID = currencyID
	}

	for _, line := range r.Lines {
		productID, _ := id.Parse(line.ProductID)
		unitID, _ := id.Parse(line.UnitID)
		vatRateID, _ := id.Parse(line.VATRateID)
		coefficient := line.Coefficient
		if coefficient.IsZero() {
			coefficient = decimal.NewFromInt(1)
		}
		doc.AddLine(productID, unitID, coefficient, line.Quantity, line.UnitPrice, vatRateID, line.VATPercent, line.DiscountPercent)
	}

	return doc
}

type UpdateGoodsIssueRequest struct {
	Number              *string                 `json:"number,omitempty"`
	Date                *time.Time              `json:"date,omitempty"`
	OrganizationID      *string                 `json:"organizationId,omitempty"`
	CustomerID          *string                 `json:"customerId,omitempty"`
	ContractID          *string                 `json:"contractId,omitempty"`
	WarehouseID         *string                 `json:"warehouseId,omitempty"`
	CustomerOrderNumber *string                 `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	CurrencyID          *string                 `json:"currencyId,omitempty"`
	AmountIncludesVAT   *bool                   `json:"amountIncludesVat,omitempty"`
	Description         *string                 `json:"description,omitempty"`
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
		orgID, _ := id.Parse(*r.OrganizationID)
		doc.OrganizationID = orgID
	}
	if r.CustomerID != nil {
		customerID, _ := id.Parse(*r.CustomerID)
		doc.CustomerID = customerID
	}
	if r.ContractID != nil {
		contractID, _ := id.Parse(*r.ContractID)
		doc.ContractID = &contractID
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
	if r.CurrencyID != nil {
		currencyID, _ := id.Parse(*r.CurrencyID)
		doc.CurrencyID = currencyID
	}
	if r.AmountIncludesVAT != nil {
		doc.AmountIncludesVAT = *r.AmountIncludesVAT
	}
	if r.Description != nil {
		doc.Description = *r.Description
	}

	if r.Lines != nil {
		doc.Lines = make([]goods_issue.GoodsIssueLine, 0, len(r.Lines))
		for _, line := range r.Lines {
			productID, _ := id.Parse(line.ProductID)
			unitID, _ := id.Parse(line.UnitID)
			vatRateID, _ := id.Parse(line.VATRateID)
			coefficient := line.Coefficient
			if coefficient.IsZero() {
				coefficient = decimal.NewFromInt(1)
			}
			doc.AddLine(productID, unitID, coefficient, line.Quantity, line.UnitPrice, vatRateID, line.VATPercent, line.DiscountPercent)
		}
	}
}

// --- Response DTOs ---

// CollectGoodsIssueRefs registers all reference IDs from a GoodsIssue
// into the resolver for batch resolution.
func CollectGoodsIssueRefs(resolver *postgres.ReferenceResolver, doc *goods_issue.GoodsIssue) {
	resolver.Add(TableOrganizations, doc.OrganizationID)
	resolver.Add(TableCounterparties, doc.CustomerID)
	resolver.AddPtr(TableContracts, doc.ContractID)
	resolver.Add(TableWarehouses, doc.WarehouseID)
	resolver.Add(TableCurrencies, doc.CurrencyID)

	for _, line := range doc.Lines {
		resolver.Add(TableNomenclature, line.ProductID)
		resolver.Add(TableUnits, line.UnitID)
		resolver.Add(TableVATRates, line.VATRateID)
	}
}

type GoodsIssueResponse struct {
	ID                  string                   `json:"id"`
	Number              string                   `json:"number"`
	Date                time.Time                `json:"date"`
	Posted              bool                     `json:"posted"`
	PostedVersion       int                      `json:"postedVersion,omitempty"`
	OrganizationID      string                   `json:"organizationId"`
	CustomerID          string                   `json:"customerId"`
	ContractID          *string                  `json:"contractId,omitempty"`
	WarehouseID         string                   `json:"warehouseId"`
	CustomerOrderNumber string                   `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time               `json:"customerOrderDate,omitempty"`
	CurrencyID          string                   `json:"currencyId"`
	AmountIncludesVAT   bool                     `json:"amountIncludesVat"`
	TotalQuantity       types.Quantity           `json:"totalQuantity"`
	TotalAmount         types.MinorUnits         `json:"totalAmount"`
	TotalVAT            types.MinorUnits         `json:"totalVat"`
	Description         string                   `json:"description,omitempty"`
	Lines               []GoodsIssueLineResponse `json:"lines,omitempty"`
	DeletionMark        bool                     `json:"deletionMark,omitempty"`
	CreatedAt           time.Time                `json:"createdAt"`
	UpdatedAt           time.Time                `json:"updatedAt"`

	// Resolved reference display names (populated by handler, not stored in DB)
	Organization *postgres.RefDisplay `json:"organization,omitempty"`
	Customer     *postgres.RefDisplay `json:"customer,omitempty"`
	Contract     *postgres.RefDisplay `json:"contract,omitempty"`
	Warehouse    *postgres.RefDisplay `json:"warehouse,omitempty"`
	Currency     *postgres.RefDisplay `json:"currency,omitempty"`
}

type GoodsIssueLineResponse struct {
	LineID          string           `json:"lineId"`
	LineNo          int              `json:"lineNo"`
	ProductID       string           `json:"productId"`
	UnitID          string           `json:"unitId"`
	Coefficient     decimal.Decimal  `json:"coefficient"`
	Quantity        types.Quantity   `json:"quantity"`
	UnitPrice       types.MinorUnits `json:"unitPrice"`
	DiscountPercent decimal.Decimal  `json:"discountPercent"`
	DiscountAmount  types.MinorUnits `json:"discountAmount"`
	VATRateID       string           `json:"vatRateId"`
	VATAmount       types.MinorUnits `json:"vatAmount"`
	Amount          types.MinorUnits `json:"amount"`

	// Resolved reference display names
	Product *postgres.RefDisplay `json:"product,omitempty"`
	Unit    *postgres.RefDisplay `json:"unit,omitempty"`
	VATRate *postgres.RefDisplay `json:"vatRate,omitempty"`
}

// FromGoodsIssue converts domain entity to response DTO.
// Pass nil for refs if reference resolution is not needed (e.g., create response).
func FromGoodsIssue(doc *goods_issue.GoodsIssue, refs postgres.ResolvedRefs) *GoodsIssueResponse {
	resp := &GoodsIssueResponse{
		ID:                  doc.ID.String(),
		Number:              doc.Number,
		Date:                doc.Date,
		Posted:              doc.Posted,
		PostedVersion:       doc.PostedVersion,
		OrganizationID:      doc.OrganizationID.String(),
		CustomerID:          doc.CustomerID.String(),
		WarehouseID:         doc.WarehouseID.String(),
		CustomerOrderNumber: doc.CustomerOrderNumber,
		CustomerOrderDate:   doc.CustomerOrderDate,
		CurrencyID:          doc.CurrencyID.String(),
		AmountIncludesVAT:   doc.AmountIncludesVAT,
		TotalQuantity:       doc.TotalQuantity,
		TotalAmount:         doc.TotalAmount,
		TotalVAT:            doc.TotalVAT,
		Description:         doc.Description,
		DeletionMark:        doc.DeletionMark,
		CreatedAt:           doc.CreatedAt,
		UpdatedAt:           doc.UpdatedAt,
	}

	if refs != nil {
		resp.Organization = refs.GetPtr(TableOrganizations, &doc.OrganizationID)
		resp.Customer = refs.GetPtr(TableCounterparties, &doc.CustomerID)
		resp.Contract = refs.GetPtr(TableContracts, doc.ContractID)
		resp.Warehouse = refs.GetPtr(TableWarehouses, &doc.WarehouseID)
		resp.Currency = refs.GetPtr(TableCurrencies, &doc.CurrencyID)
	}

	if doc.ContractID != nil {
		s := doc.ContractID.String()
		resp.ContractID = &s
	}

	resp.Lines = make([]GoodsIssueLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		resp.Lines[i] = GoodsIssueLineResponse{
			LineID:          line.LineID.String(),
			LineNo:          line.LineNo,
			ProductID:       line.ProductID.String(),
			UnitID:          line.UnitID.String(),
			Coefficient:     line.Coefficient,
			Quantity:        line.Quantity,
			UnitPrice:       line.UnitPrice,
			DiscountPercent: line.DiscountPercent,
			DiscountAmount:  line.DiscountAmount,
			VATRateID:       line.VATRateID.String(),
			VATAmount:       line.VATAmount,
			Amount:          line.Amount,
		}

		if refs != nil {
			resp.Lines[i].Product = refs.GetPtr(TableNomenclature, &line.ProductID)
			resp.Lines[i].Unit = refs.GetPtr(TableUnits, &line.UnitID)
			resp.Lines[i].VATRate = refs.GetPtr(TableVATRates, &line.VATRateID)
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
