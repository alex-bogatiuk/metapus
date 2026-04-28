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
	CounterpartyID          string                  `json:"counterpartyId" binding:"required"`
	ContractID          *string                 `json:"contractId,omitempty"`
	WarehouseID         string                  `json:"warehouseId" binding:"required"`
	CustomerOrderNumber string                  `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	CurrencyID          string                  `json:"currencyId,omitempty"`
	AmountIncludesVAT   bool                    `json:"amountIncludesVat"`
	Description         string                  `json:"description,omitempty"`
	BasisType           string                  `json:"basisType,omitempty"`
	BasisID             *string                 `json:"basisId,omitempty"`
	Lines               []GoodsIssueLineRequest `json:"lines" binding:"required,min=1,dive"`
	PostImmediately     bool                    `json:"postImmediately,omitempty"`
}

type GoodsIssueLineRequest struct {
	NomenclatureID       string           `json:"nomenclatureId" binding:"required"`
	UnitID          string           `json:"unitId" binding:"required"`
	Coefficient     decimal.Decimal  `json:"coefficient"`
	Quantity        types.Quantity   `json:"quantity" binding:"required,gt=0"`
	UnitPrice       types.MinorUnits `json:"unitPrice" binding:"required,gte=0"`
	VATRateID       string           `json:"vatRateId" binding:"required"`
	VATPercent      int              `json:"vatPercent"`
	DiscountPercent decimal.Decimal  `json:"discountPercent"`
}

func (r *CreateGoodsIssueRequest) ToEntity() *goods_issue.GoodsIssue {
	customerID, _ := id.Parse(r.CounterpartyID)
	warehouseID, _ := id.Parse(r.WarehouseID)

	orgID, _ := id.Parse(r.OrganizationID)
	doc := goods_issue.NewGoodsIssue(orgID, customerID, warehouseID)
	doc.Number = r.Number
	doc.Date = r.Date
	doc.CustomerOrderNumber = r.CustomerOrderNumber
	doc.CustomerOrderDate = r.CustomerOrderDate
	doc.AmountIncludesVAT = r.AmountIncludesVAT
	doc.Description = r.Description
	doc.BasisType = r.BasisType

	if r.BasisID != nil {
		basisID, _ := id.Parse(*r.BasisID)
		doc.BasisID = &basisID
	}

	if r.ContractID != nil {
		contractID, _ := id.Parse(*r.ContractID)
		doc.ContractID = &contractID
	}

	if r.CurrencyID != "" {
		currencyID, _ := id.Parse(r.CurrencyID)
		doc.CurrencyID = currencyID
	}

	for _, line := range r.Lines {
		nomenclatureID, _ := id.Parse(line.NomenclatureID)
		unitID, _ := id.Parse(line.UnitID)
		vatRateID, _ := id.Parse(line.VATRateID)
		coefficient := line.Coefficient
		if coefficient.IsZero() {
			coefficient = decimal.NewFromInt(1)
		}
		doc.AddLine(nomenclatureID, unitID, coefficient, line.Quantity, line.UnitPrice, vatRateID, line.VATPercent, line.DiscountPercent)
	}

	return doc
}

type UpdateGoodsIssueRequest struct {
	Version             int                     `json:"version" binding:"required,min=1"`
	Number              *string                 `json:"number,omitempty"`
	Date                *time.Time              `json:"date,omitempty"`
	OrganizationID      *string                 `json:"organizationId,omitempty"`
	CounterpartyID          *string                 `json:"counterpartyId,omitempty"`
	ContractID          *string                 `json:"contractId,omitempty"`
	WarehouseID         *string                 `json:"warehouseId,omitempty"`
	CustomerOrderNumber *string                 `json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time              `json:"customerOrderDate,omitempty"`
	CurrencyID          *string                 `json:"currencyId,omitempty"`
	AmountIncludesVAT   *bool                   `json:"amountIncludesVat,omitempty"`
	Description         *string                 `json:"description,omitempty"`
	BasisType           *string                 `json:"basisType,omitempty"`
	BasisID             *string                 `json:"basisId,omitempty"`
	Lines               []GoodsIssueLineRequest `json:"lines,omitempty"`
}

// ApplyTo applies updates to an existing entity.
// Sets the client-provided version on the entity so the repo performs
// WHERE version = $client_version for optimistic locking.
func (r *UpdateGoodsIssueRequest) ApplyTo(doc *goods_issue.GoodsIssue) {
	doc.SetVersion(r.Version)
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
	if r.CounterpartyID != nil {
		customerID, _ := id.Parse(*r.CounterpartyID)
		doc.CounterpartyID = customerID
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
	if r.BasisType != nil {
		doc.BasisType = *r.BasisType
	}
	if r.BasisID != nil {
		basisID, _ := id.Parse(*r.BasisID)
		doc.BasisID = &basisID
	}

	if r.Lines != nil {
		doc.Lines = make([]goods_issue.GoodsIssueLine, 0, len(r.Lines))
		for _, line := range r.Lines {
			nomenclatureID, _ := id.Parse(line.NomenclatureID)
			unitID, _ := id.Parse(line.UnitID)
			vatRateID, _ := id.Parse(line.VATRateID)
			coefficient := line.Coefficient
			if coefficient.IsZero() {
				coefficient = decimal.NewFromInt(1)
			}
			doc.AddLine(nomenclatureID, unitID, coefficient, line.Quantity, line.UnitPrice, vatRateID, line.VATPercent, line.DiscountPercent)
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
	CounterpartyID          string                   `json:"counterpartyId"`
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
	BasisType           string                   `json:"basisType,omitempty"`
	BasisID             *string                  `json:"basisId,omitempty"`
	Lines               []GoodsIssueLineResponse `json:"lines,omitempty"`
	Version             int                      `json:"version"`
	DeletionMark        bool                     `json:"deletionMark"`
	CreatedAt           time.Time                `json:"createdAt"`
	UpdatedAt           time.Time                `json:"updatedAt"`

	// Resolved reference display names (populated by handler, not stored in DB)
	Organization  *postgres.RefDisplay         `json:"organization,omitempty"`
	Counterparty  *postgres.RefDisplay         `json:"counterparty,omitempty"`
	Contract      *postgres.RefDisplay         `json:"contract,omitempty"`
	Warehouse     *postgres.RefDisplay         `json:"warehouse,omitempty"`
	Currency      *postgres.CurrencyRefDisplay `json:"currency,omitempty"`
	CreatedByUser *postgres.RefDisplay         `json:"createdByUser,omitempty"`
	UpdatedByUser *postgres.RefDisplay         `json:"updatedByUser,omitempty"`
}

type GoodsIssueLineResponse struct {
	LineID          string           `json:"lineId"`
	LineNo          int              `json:"lineNo"`
	NomenclatureID       string           `json:"nomenclatureId"`
	UnitID          string           `json:"unitId"`
	Coefficient     decimal.Decimal  `json:"coefficient"`
	Quantity        types.Quantity   `json:"quantity"`
	UnitPrice       types.MinorUnits `json:"unitPrice"`
	DiscountPercent decimal.Decimal  `json:"discountPercent"`
	DiscountAmount  types.MinorUnits `json:"discountAmount"`
	VATRateID       string           `json:"vatRateId"`
	VATPercent      int              `json:"vatPercent"`
	VATAmount       types.MinorUnits `json:"vatAmount"`
	Amount          types.MinorUnits `json:"amount"`

	// Resolved reference display names
	Nomenclature *postgres.RefDisplay `json:"nomenclature,omitempty"`
	Unit    *postgres.RefDisplay `json:"unit,omitempty"`
	VATRate *postgres.RefDisplay `json:"vatRate,omitempty"`
}

// CollectGoodsIssueRefs registers all reference IDs from a GoodsIssue
// into the resolver for batch resolution.
func CollectGoodsIssueRefs(resolver *postgres.ReferenceResolver, doc *goods_issue.GoodsIssue) {
	resolver.Add(TableOrganizations, doc.OrganizationID)
	resolver.Add(TableCounterparties, doc.CounterpartyID)
	resolver.AddPtr(TableContracts, doc.ContractID)
	resolver.Add(TableWarehouses, doc.WarehouseID)
	resolver.Add(TableCurrencies, doc.CurrencyID)
	resolver.Add(TableUsers, doc.CreatedBy)
	resolver.Add(TableUsers, doc.UpdatedBy)

	for _, line := range doc.Lines {
		resolver.Add(TableNomenclature, line.NomenclatureID)
		resolver.Add(TableUnits, line.UnitID)
		resolver.Add(TableVATRates, line.VATRateID)
	}
}

// FromGoodsIssue converts domain entity to response DTO.
// Pass nil for refs if reference resolution is not needed.
// Optional currencyRefs provides enriched currency display (decimalPlaces, symbol).
func FromGoodsIssue(doc *goods_issue.GoodsIssue, refs postgres.ResolvedRefs, currencyRefs ...postgres.ResolvedCurrencyRefs) *GoodsIssueResponse {
	resp := &GoodsIssueResponse{
		ID:                  doc.ID.String(),
		Number:              doc.Number,
		Date:                doc.Date,
		Posted:              doc.Posted,
		PostedVersion:       doc.PostedVersion,
		OrganizationID:      doc.OrganizationID.String(),
		CounterpartyID:          doc.CounterpartyID.String(),
		WarehouseID:         doc.WarehouseID.String(),
		CustomerOrderNumber: doc.CustomerOrderNumber,
		CustomerOrderDate:   doc.CustomerOrderDate,
		CurrencyID:          doc.CurrencyID.String(),
		AmountIncludesVAT:   doc.AmountIncludesVAT,
		TotalQuantity:       doc.TotalQuantity,
		TotalAmount:         doc.TotalAmount,
		TotalVAT:            doc.TotalVAT,
		Description:         doc.Description,
		BasisType:           doc.BasisType,
		Version:             doc.Version,
		DeletionMark:        doc.DeletionMark,
		CreatedAt:           doc.CreatedAt,
		UpdatedAt:           doc.UpdatedAt,
	}

	if doc.ContractID != nil {
		s := doc.ContractID.String()
		resp.ContractID = &s
	}

	if doc.BasisID != nil {
		s := doc.BasisID.String()
		resp.BasisID = &s
	}

	// Populate resolved reference display names
	resolved := refs
	if resolved != nil {
		org := resolved.Get(TableOrganizations, doc.OrganizationID)
		resp.Organization = &org
		cust := resolved.Get(TableCounterparties, doc.CounterpartyID)
		resp.Counterparty = &cust
		wh := resolved.Get(TableWarehouses, doc.WarehouseID)
		resp.Warehouse = &wh
		if len(currencyRefs) > 0 && currencyRefs[0] != nil {
			cr := currencyRefs[0].Get(doc.CurrencyID)
			resp.Currency = &cr
		} else {
			generic := resolved.Get(TableCurrencies, doc.CurrencyID)
			resp.Currency = &postgres.CurrencyRefDisplay{ID: generic.ID, Name: generic.Name, DecimalPlaces: 2}
		}
		resp.Contract = resolved.GetPtr(TableContracts, doc.ContractID)

		createdBy := doc.CreatedBy
		updatedBy := doc.UpdatedBy
		resp.CreatedByUser = resolved.GetPtr(TableUsers, &createdBy)
		resp.UpdatedByUser = resolved.GetPtr(TableUsers, &updatedBy)
	}

	resp.Lines = make([]GoodsIssueLineResponse, len(doc.Lines))
	for i, line := range doc.Lines {
		lineResp := GoodsIssueLineResponse{
			LineID:          line.LineID.String(),
			LineNo:          line.LineNo,
			NomenclatureID:       line.NomenclatureID.String(),
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
			prod := resolved.Get(TableNomenclature, line.NomenclatureID)
			lineResp.Nomenclature = &prod
			unit := resolved.Get(TableUnits, line.UnitID)
			lineResp.Unit = &unit
			vr := resolved.Get(TableVATRates, line.VATRateID)
			lineResp.VATRate = &vr
		}

		resp.Lines[i] = lineResp
	}

	return resp
}

type GoodsIssueListResponse struct {
	Items      []*GoodsIssueResponse `json:"items"`
	TotalCount int                   `json:"totalCount"`
	Limit      int                   `json:"limit"`
	Offset     int                   `json:"offset"`
}
