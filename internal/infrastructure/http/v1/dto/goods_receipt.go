package dto

import (
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/storage/postgres"
)

// --- Request DTOs ---

// CreateGoodsReceiptRequest represents a request to create a goods receipt.
type CreateGoodsReceiptRequest struct {
	Number            string                    `json:"number,omitempty"`
	Date              time.Time                 `json:"date" binding:"required"`
	OrganizationID    string                    `json:"organizationId" binding:"required"`
	SupplierID        string                    `json:"supplierId" binding:"required"`
	ContractID        *string                   `json:"contractId,omitempty"`
	WarehouseID       string                    `json:"warehouseId" binding:"required"`
	SupplierDocNumber string                    `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                `json:"supplierDocDate,omitempty"`
	IncomingNumber    *string                   `json:"incomingNumber,omitempty"`
	CurrencyID        string                    `json:"currencyId,omitempty"`
	AmountIncludesVAT bool                      `json:"amountIncludesVat"`
	Description       string                    `json:"description,omitempty"`
	Lines             []GoodsReceiptLineRequest `json:"lines" binding:"required,min=1,dive"`
	PostImmediately   bool                      `json:"postImmediately,omitempty"`
}

// GoodsReceiptLineRequest represents a line in create/update request.
type GoodsReceiptLineRequest struct {
	ProductID       string           `json:"productId" binding:"required"`
	UnitID          string           `json:"unitId" binding:"required"`
	Coefficient     decimal.Decimal  `json:"coefficient"`
	Quantity        types.Quantity   `json:"quantity" binding:"required,gt=0"`
	UnitPrice       types.MinorUnits `json:"unitPrice" binding:"required,gte=0"`
	VATRateID       string           `json:"vatRateId" binding:"required"`
	VATPercent      int              `json:"vatPercent"`
	DiscountPercent decimal.Decimal  `json:"discountPercent"`
}

// ToEntity converts request to domain entity.
func (r *CreateGoodsReceiptRequest) ToEntity() *goods_receipt.GoodsReceipt {
	supplierID, _ := id.Parse(r.SupplierID)
	warehouseID, _ := id.Parse(r.WarehouseID)

	orgID, _ := id.Parse(r.OrganizationID)
	doc := goods_receipt.NewGoodsReceipt(orgID, supplierID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.SupplierDocNumber = r.SupplierDocNumber
	doc.SupplierDocDate = r.SupplierDocDate
	doc.IncomingNumber = r.IncomingNumber
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

// UpdateGoodsReceiptRequest represents a request to update a goods receipt.
type UpdateGoodsReceiptRequest struct {
	Number            *string                   `json:"number,omitempty"`
	Date              *time.Time                `json:"date,omitempty"`
	OrganizationID    *string                   `json:"organizationId,omitempty"`
	SupplierID        *string                   `json:"supplierId,omitempty"`
	ContractID        *string                   `json:"contractId,omitempty"`
	WarehouseID       *string                   `json:"warehouseId,omitempty"`
	SupplierDocNumber *string                   `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                `json:"supplierDocDate,omitempty"`
	IncomingNumber    *string                   `json:"incomingNumber,omitempty"`
	CurrencyID        *string                   `json:"currencyId,omitempty"`
	AmountIncludesVAT *bool                     `json:"amountIncludesVat,omitempty"`
	Description       *string                   `json:"description,omitempty"`
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
		orgID, _ := id.Parse(*r.OrganizationID)
		doc.OrganizationID = orgID
	}
	if r.SupplierID != nil {
		supplierID, _ := id.Parse(*r.SupplierID)
		doc.SupplierID = supplierID
	}
	if r.ContractID != nil {
		contractID, _ := id.Parse(*r.ContractID)
		doc.ContractID = &contractID
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
	if r.IncomingNumber != nil {
		doc.IncomingNumber = r.IncomingNumber
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

	// If lines are provided, rebuild them
	if r.Lines != nil {
		doc.Lines = make([]goods_receipt.GoodsReceiptLine, 0, len(r.Lines))
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

// GoodsReceiptResponse represents a goods receipt in API responses.
type GoodsReceiptResponse struct {
	ID                string                     `json:"id"`
	Number            string                     `json:"number"`
	Date              time.Time                  `json:"date"`
	Posted            bool                       `json:"posted"`
	PostedVersion     int                        `json:"postedVersion,omitempty"`
	OrganizationID    string                     `json:"organizationId"`
	SupplierID        string                     `json:"supplierId"`
	ContractID        *string                    `json:"contractId,omitempty"`
	WarehouseID       string                     `json:"warehouseId"`
	SupplierDocNumber string                     `json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time                 `json:"supplierDocDate,omitempty"`
	IncomingNumber    *string                    `json:"incomingNumber,omitempty"`
	CurrencyID        string                     `json:"currencyId"`
	AmountIncludesVAT bool                       `json:"amountIncludesVat"`
	TotalQuantity     types.Quantity             `json:"totalQuantity"`
	TotalAmount       types.MinorUnits           `json:"totalAmount"`
	TotalVAT          types.MinorUnits           `json:"totalVat"`
	Description       string                     `json:"description,omitempty"`
	Lines             []GoodsReceiptLineResponse `json:"lines,omitempty"`
	DeletionMark      bool                       `json:"deletionMark,omitempty"`
	CreatedAt         time.Time                  `json:"createdAt"`
	UpdatedAt         time.Time                  `json:"updatedAt"`

	// Resolved reference display names (populated by handler, not stored in DB)
	Organization *postgres.RefDisplay `json:"organization,omitempty"`
	Supplier     *postgres.RefDisplay `json:"supplier,omitempty"`
	Contract     *postgres.RefDisplay `json:"contract,omitempty"`
	Warehouse    *postgres.RefDisplay `json:"warehouse,omitempty"`
	Currency     *postgres.RefDisplay `json:"currency,omitempty"`
}

// GoodsReceiptLineResponse represents a line in API responses.
type GoodsReceiptLineResponse struct {
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

// Table name constants for reference resolution.
const (
	TableOrganizations  = "cat_organizations"
	TableCounterparties = "cat_counterparties"
	TableContracts      = "cat_contracts"
	TableWarehouses     = "cat_warehouses"
	TableCurrencies     = "cat_currencies"
	TableNomenclature   = "cat_nomenclature"
	TableUnits          = "cat_units"
	TableVATRates       = "cat_vat_rates"
)

// CollectGoodsReceiptRefs registers all reference IDs from a GoodsReceipt
// into the resolver for batch resolution.
func CollectGoodsReceiptRefs(resolver *postgres.ReferenceResolver, doc *goods_receipt.GoodsReceipt) {
	resolver.Add(TableOrganizations, doc.OrganizationID)
	resolver.Add(TableCounterparties, doc.SupplierID)
	resolver.AddPtr(TableContracts, doc.ContractID)
	resolver.Add(TableWarehouses, doc.WarehouseID)
	resolver.Add(TableCurrencies, doc.CurrencyID)

	for _, line := range doc.Lines {
		resolver.Add(TableNomenclature, line.ProductID)
		resolver.Add(TableUnits, line.UnitID)
		resolver.Add(TableVATRates, line.VATRateID)
	}
}

// FromGoodsReceipt converts domain entity to response DTO.
// Pass nil for refs if reference resolution is not needed (e.g., create response).
func FromGoodsReceipt(doc *goods_receipt.GoodsReceipt, refs ...postgres.ResolvedRefs) *GoodsReceiptResponse {
	var resolved postgres.ResolvedRefs
	if len(refs) > 0 {
		resolved = refs[0]
	}
	resp := &GoodsReceiptResponse{
		ID:                doc.ID.String(),
		Number:            doc.Number,
		Date:              doc.Date,
		Posted:            doc.Posted,
		OrganizationID:    doc.OrganizationID.String(),
		SupplierID:        doc.SupplierID.String(),
		WarehouseID:       doc.WarehouseID.String(),
		SupplierDocNumber: doc.SupplierDocNumber,
		SupplierDocDate:   doc.SupplierDocDate,
		IncomingNumber:    doc.IncomingNumber,
		CurrencyID:        doc.CurrencyID.String(),
		AmountIncludesVAT: doc.AmountIncludesVAT,
		TotalQuantity:     doc.TotalQuantity,
		TotalAmount:       doc.TotalAmount,
		TotalVAT:          doc.TotalVAT,
		Description:       doc.Description,
		DeletionMark:      doc.DeletionMark,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}

	if doc.ContractID != nil {
		s := doc.ContractID.String()
		resp.ContractID = &s
	}

	// Populate resolved reference display names
	if resolved != nil {
		org := resolved.Get(TableOrganizations, doc.OrganizationID)
		resp.Organization = &org
		sup := resolved.Get(TableCounterparties, doc.SupplierID)
		resp.Supplier = &sup
		wh := resolved.Get(TableWarehouses, doc.WarehouseID)
		resp.Warehouse = &wh
		cur := resolved.Get(TableCurrencies, doc.CurrencyID)
		resp.Currency = &cur
		resp.Contract = resolved.GetPtr(TableContracts, doc.ContractID)
	}

	resp.Lines = make([]GoodsReceiptLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		lineResp := GoodsReceiptLineResponse{
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

		if resolved != nil {
			prod := resolved.Get(TableNomenclature, line.ProductID)
			lineResp.Product = &prod
			unit := resolved.Get(TableUnits, line.UnitID)
			lineResp.Unit = &unit
			vr := resolved.Get(TableVATRates, line.VATRateID)
			lineResp.VATRate = &vr
		}

		resp.Lines[i] = lineResp
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
