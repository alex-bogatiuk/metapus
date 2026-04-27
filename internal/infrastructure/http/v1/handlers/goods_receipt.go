package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// GoodsReceiptHandler handles HTTP requests for GoodsReceipt documents.
// Standard CRUD/posting methods are handled by BaseDocumentHandler via ResolveRefs callback.
// Only entity-specific methods (Copy, UpdateAndRepost) are overridden.
type GoodsReceiptHandler struct {
	*BaseDocumentHandler[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]
	service            domain.DocumentService[*goods_receipt.GoodsReceipt]
	printHandler       *DocumentPrintHandler[*goods_receipt.GoodsReceipt]
	relatedDocsHandler *RelatedDocumentsHandler
}

// resolveGoodsReceiptRefs batch-resolves all reference IDs for a list of GoodsReceipt documents.
// Returns an opaque DocRefsBag for use by MapToDTOWithRefs.
func resolveGoodsReceiptRefs(ctx context.Context, docs ...*goods_receipt.GoodsReceipt) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectGoodsReceiptRefs(resolver, doc)
	}

	pool := tenant.MustGetPool(ctx)
	refs, err := resolver.Resolve(ctx, pool)
	if err != nil {
		return nil, err
	}
	currencyRefs, err := resolver.ResolveCurrencies(ctx, pool)
	if err != nil {
		return nil, err
	}
	return &dto.DocRefsBag{Refs: refs, CurrencyRefs: currencyRefs}, nil
}

// NewGoodsReceiptHandler creates a new goods receipt handler.
// Accepts domain.DocumentService interface — can be a concrete service or a decorated wrapper.
func NewGoodsReceiptHandler(
	base *BaseHandler,
	service domain.DocumentService[*goods_receipt.GoodsReceipt],
	printRegistry *printing.PrintFormRegistry,
	printRenderer *printing.Renderer,
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *GoodsReceiptHandler {
	cfg := BaseDocumentHandlerConfig[*goods_receipt.GoodsReceipt, dto.CreateGoodsReceiptRequest, dto.UpdateGoodsReceiptRequest]{
		Service:    service,
		EntityName: "goods_receipt",
		MapCreateDTO: func(req dto.CreateGoodsReceiptRequest) *goods_receipt.GoodsReceipt {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateGoodsReceiptRequest, existing *goods_receipt.GoodsReceipt) *goods_receipt.GoodsReceipt {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *goods_receipt.GoodsReceipt) any {
			return dto.FromGoodsReceipt(entity, nil)
		},
		IsPostImmediately: func(req dto.CreateGoodsReceiptRequest) bool {
			return req.PostImmediately
		},
		ResolveRefs: resolveGoodsReceiptRefs,
		MapToDTOWithRefs: func(entity *goods_receipt.GoodsReceipt, refs any) any {
			bag := refs.(*dto.DocRefsBag)
			return dto.FromGoodsReceipt(entity, bag.Refs, bag.CurrencyRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &GoodsReceiptHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}

	if printRegistry != nil && printRenderer != nil {
		h.printHandler = NewDocumentPrintHandler(base, DocumentPrintHandlerConfig[*goods_receipt.GoodsReceipt]{
			Service:     service,
			EntityName:  "goods_receipt",
			DocType:     "goods_receipt",
			Registry:    printRegistry,
			Renderer:    printRenderer,
			ResolveRefs: resolveGoodsReceiptRefs,
			BuildPrintData: func(entity *goods_receipt.GoodsReceipt, refs any, showPrices bool) *printing.PrintData {
				var resp *dto.GoodsReceiptResponse
				if bag, ok := refs.(*dto.DocRefsBag); ok {
					resp = dto.FromGoodsReceipt(entity, bag.Refs, bag.CurrencyRefs)
				} else {
					resp = dto.FromGoodsReceipt(entity, nil)
				}
				dp := 2
				symbol := ""
				if resp.Currency != nil {
					dp = resp.Currency.DecimalPlaces
					symbol = resp.Currency.Symbol
				}
				return &printing.PrintData{
					FormLabel:      "Поступление товаров",
					ShowPrices:     showPrices,
					DecimalPlaces:  dp,
					CurrencySymbol: symbol,
					Doc:            resp,
					Table:          buildGoodsReceiptTable(resp, dp, symbol, showPrices),
				}
			},
		})
	}

	// Related documents (optional)
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "GoodsReceipt")
	}

	return h
}

// GetRelatedDocuments handles GET /document/goods-receipt/:id/related-documents.
// Implements DocumentRelatedDocsHandler interface (auto-registered by RegisterDocumentRoutes).
func (h *GoodsReceiptHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}

// Print handles GET /document/goods-receipt/:id/print — renders a printable HTML form.
func (h *GoodsReceiptHandler) Print(c *gin.Context) {
	if h.printHandler == nil {
		h.Error(c, apperror.NewNotFound("print service", "not configured"))
		return
	}
	h.printHandler.Print(c)
}

// ListPrintForms handles GET /document/goods-receipt/print-forms.
// Implements DocumentPrintFormsListHandler (auto-registered by RegisterDocumentRoutes).
func (h *GoodsReceiptHandler) ListPrintForms(c *gin.Context) {
	if h.printHandler == nil {
		c.JSON(http.StatusOK, []printing.PrintFormSummary{})
		return
	}
	h.printHandler.ListPrintForms(c)
}

// UpdateAndRepost handles PUT /document/goods-receipt/:id/repost — atomic update + re-post.
// Accepts the same body as Update. The document is updated and re-posted in a single transaction.
func (h *GoodsReceiptHandler) UpdateAndRepost(c *gin.Context) {
	ctx := c.Request.Context()
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.UpdateGoodsReceiptRequest
	if !h.BindJSON(c, &req) {
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	req.ApplyTo(doc)

	if err := h.service.UpdateAndRepost(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	refs, _ := resolveGoodsReceiptRefs(ctx, doc)
	var response any
	if bag, ok := refs.(*dto.DocRefsBag); ok {
		response = dto.FromGoodsReceipt(doc, bag.Refs, bag.CurrencyRefs)
	} else {
		response = dto.FromGoodsReceipt(doc, nil)
	}
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Copy handles POST /document/goods-receipt/:id/copy — with resolved references.
func (h *GoodsReceiptHandler) Copy(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	source, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	copy := goods_receipt.NewGoodsReceipt(source.OrganizationID, source.SupplierID, source.WarehouseID)
	copy.Date = time.Now()
	copy.ContractID = source.ContractID
	copy.SupplierDocNumber = source.SupplierDocNumber
	copy.IncomingNumber = source.IncomingNumber
	copy.CurrencyID = source.CurrencyID
	copy.AmountIncludesVAT = source.AmountIncludesVAT
	copy.Description = source.Description

	for _, line := range source.Lines {
		copy.AddLine(line.ProductID, line.UnitID, line.Coefficient, line.Quantity, line.UnitPrice, line.VATRateID, 0, line.DiscountPercent)
	}

	if err := h.service.Create(ctx, copy); err != nil {
		h.Error(c, err)
		return
	}

	refs, _ := resolveGoodsReceiptRefs(ctx, copy)
	var response any
	if bag, ok := refs.(*dto.DocRefsBag); ok {
		response = dto.FromGoodsReceipt(copy, bag.Refs, bag.CurrencyRefs)
	} else {
		response = dto.FromGoodsReceipt(copy, nil)
	}
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// buildGoodsReceiptTable builds a PrintTable from a GoodsReceiptResponse for XLSX/DOCX renderers.
func buildGoodsReceiptTable(resp *dto.GoodsReceiptResponse, dp int, currSymbol string, showPrices bool) *printing.PrintTable {
	t := &printing.PrintTable{
		Title:    "Поступление товаров",
		Subtitle: fmt.Sprintf("№ %s от %s", resp.Number, printing.FormatDate(resp.Date)),
	}

	// Header fields
	orgName, supName, whName, curName, contractName := "", "", "", "", ""
	if resp.Organization != nil {
		orgName = resp.Organization.Name
	}
	if resp.Supplier != nil {
		supName = resp.Supplier.Name
	}
	if resp.Warehouse != nil {
		whName = resp.Warehouse.Name
	}
	if resp.Currency != nil {
		curName = resp.Currency.Name
	}
	if resp.Contract != nil {
		contractName = resp.Contract.Name
	}

	t.HeaderRows = [][]printing.PrintHeaderField{
		{
			{Label: "Организация", Value: orgName},
			{Label: "Склад", Value: whName},
		},
		{
			{Label: "Поставщик", Value: supName},
			{Label: "Договор", Value: contractName},
		},
		{
			{Label: "Валюта", Value: curName},
		},
	}

	// Columns & rows
	if showPrices {
		t.Columns = []string{"№", "Номенклатура", "Ед.изм.", "Кол-во", "Цена", "Сумма", "НДС"}
	} else {
		t.Columns = []string{"№", "Номенклатура", "Ед.изм.", "Кол-во"}
	}

	for _, line := range resp.Lines {
		prodName, unitName := "", ""
		if line.Product != nil {
			prodName = line.Product.Name
		}
		if line.Unit != nil {
			unitName = line.Unit.Name
		}
		row := printing.PrintTableRow{}
		if showPrices {
			row.Values = []string{
				fmt.Sprintf("%d", line.LineNo),
				prodName,
				unitName,
				printing.FormatQty(line.Quantity),
				printing.FormatMoney(line.UnitPrice, dp),
				printing.FormatMoney(line.Amount, dp),
				printing.FormatMoney(line.VATAmount, dp),
			}
		} else {
			row.Values = []string{
				fmt.Sprintf("%d", line.LineNo),
				prodName,
				unitName,
				printing.FormatQty(line.Quantity),
			}
		}
		t.Rows = append(t.Rows, row)
	}

	// Totals
	if showPrices {
		t.Totals = []printing.PrintTotalLine{
			{Label: "Итого", Value: printing.FormatMoney(resp.TotalAmount, dp) + " " + currSymbol, Grand: true},
			{Label: "В том числе НДС", Value: printing.FormatMoney(resp.TotalVAT, dp) + " " + currSymbol},
		}
	}

	// Signatures (horizontal layout matching HTML print form)
	t.SignatureBlock = printing.HorizontalSignatures(
		"Сдал (поставщик)",
		"Принял",
		"Материально ответственное лицо",
	)

	return t
}
