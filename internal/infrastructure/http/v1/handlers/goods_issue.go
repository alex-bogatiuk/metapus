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
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// GoodsIssueHandler handles HTTP requests for GoodsIssue documents.
// Standard CRUD/posting methods are handled by BaseDocumentHandler via ResolveRefs callback.
// Only entity-specific methods (Copy, UpdateAndRepost) are overridden.
type GoodsIssueHandler struct {
	*BaseDocumentHandler[*goods_issue.GoodsIssue, dto.CreateGoodsIssueRequest, dto.UpdateGoodsIssueRequest]
	service            domain.DocumentService[*goods_issue.GoodsIssue]
	printHandler       *DocumentPrintHandler[*goods_issue.GoodsIssue]
	relatedDocsHandler *RelatedDocumentsHandler
}

// resolveGoodsIssueRefs batch-resolves all reference IDs for a list of GoodsIssue documents.
// Returns an opaque DocRefsBag for use by MapToDTOWithRefs.
func resolveGoodsIssueRefs(ctx context.Context, docs ...*goods_issue.GoodsIssue) (any, error) {
	resolver := postgres.NewReferenceResolver()
	for _, doc := range docs {
		dto.CollectGoodsIssueRefs(resolver, doc)
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

// NewGoodsIssueHandler creates a new goods issue handler.
// Accepts domain.DocumentService interface — can be a concrete service or a decorated wrapper.
func NewGoodsIssueHandler(
	base *BaseHandler,
	service domain.DocumentService[*goods_issue.GoodsIssue],
	printRegistry *printing.PrintFormRegistry,
	printRenderer *printing.Renderer,
	relatedDocFinder domain.RelatedDocFinder,
	movementProviders []entity.MovementProvider,
	movementRefResolver domain.RefResolver,
	settingsRepo settings.Repository,
) *GoodsIssueHandler {
	cfg := BaseDocumentHandlerConfig[*goods_issue.GoodsIssue, dto.CreateGoodsIssueRequest, dto.UpdateGoodsIssueRequest]{
		Service:    service,
		EntityName: "goods_issue",
		MapCreateDTO: func(req dto.CreateGoodsIssueRequest) *goods_issue.GoodsIssue {
			return req.ToEntity()
		},
		MapUpdateDTO: func(req dto.UpdateGoodsIssueRequest, existing *goods_issue.GoodsIssue) *goods_issue.GoodsIssue {
			req.ApplyTo(existing)
			return existing
		},
		MapToDTO: func(entity *goods_issue.GoodsIssue) any {
			return dto.FromGoodsIssue(entity, nil)
		},
		IsPostImmediately: func(req dto.CreateGoodsIssueRequest) bool {
			return req.PostImmediately
		},
		ResolveRefs: resolveGoodsIssueRefs,
		MapToDTOWithRefs: func(entity *goods_issue.GoodsIssue, refs any) any {
			bag := refs.(*dto.DocRefsBag)
			return dto.FromGoodsIssue(entity, bag.Refs, bag.CurrencyRefs)
		},
		MovementProviders:   movementProviders,
		MovementRefResolver: movementRefResolver,
		SettingsRepo:        settingsRepo,
	}

	h := &GoodsIssueHandler{
		BaseDocumentHandler: NewBaseDocumentHandler(base, cfg),
		service:             service,
	}

	if printRegistry != nil && printRenderer != nil {
		h.printHandler = NewDocumentPrintHandler(base, DocumentPrintHandlerConfig[*goods_issue.GoodsIssue]{
			Service:     service,
			EntityName:  "goods_issue",
			DocType:     "goods_issue",
			Registry:    printRegistry,
			Renderer:    printRenderer,
			ResolveRefs: resolveGoodsIssueRefs,
			BuildPrintData: func(entity *goods_issue.GoodsIssue, refs any, showPrices bool) *printing.PrintData {
				var resp *dto.GoodsIssueResponse
				if bag, ok := refs.(*dto.DocRefsBag); ok {
					resp = dto.FromGoodsIssue(entity, bag.Refs, bag.CurrencyRefs)
				} else {
					resp = dto.FromGoodsIssue(entity, nil)
				}
				dp := 2
				symbol := ""
				if resp.Currency != nil {
					dp = resp.Currency.DecimalPlaces
					symbol = resp.Currency.Symbol
				}
				return &printing.PrintData{
					FormLabel:      "Реализация товаров",
					ShowPrices:     showPrices,
					DecimalPlaces:  dp,
					CurrencySymbol: symbol,
					Doc:            resp,
					Table:          buildGoodsIssueTable(resp, dp, symbol, showPrices),
				}
			},
		})
	}

	// Related documents (optional)
	if relatedDocFinder != nil {
		h.relatedDocsHandler = NewRelatedDocumentsHandler(relatedDocFinder, "GoodsIssue")
	}

	return h
}

// GetRelatedDocuments handles GET /document/goods-issue/:id/related-documents.
// Implements DocumentRelatedDocsHandler interface (auto-registered by RegisterDocumentRoutes).
func (h *GoodsIssueHandler) GetRelatedDocuments(c *gin.Context) {
	if h.relatedDocsHandler == nil {
		c.JSON(http.StatusOK, gin.H{"groups": []any{}})
		return
	}
	h.relatedDocsHandler.GetRelatedDocuments(c)
}

// Print handles GET /document/goods-issue/:id/print — renders a printable HTML form.
func (h *GoodsIssueHandler) Print(c *gin.Context) {
	if h.printHandler == nil {
		h.Error(c, apperror.NewNotFound("print service", "not configured"))
		return
	}
	h.printHandler.Print(c)
}

// UpdateAndRepost handles PUT /document/goods-issue/:id/repost — atomic update + re-post.
// Accepts the same body as Update. The document is updated and re-posted in a single transaction.
func (h *GoodsIssueHandler) UpdateAndRepost(c *gin.Context) {
	ctx := c.Request.Context()
	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.UpdateGoodsIssueRequest
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

	refs, _ := resolveGoodsIssueRefs(ctx, doc)
	var response any
	if bag, ok := refs.(*dto.DocRefsBag); ok {
		response = dto.FromGoodsIssue(doc, bag.Refs, bag.CurrencyRefs)
	} else {
		response = dto.FromGoodsIssue(doc, nil)
	}
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Copy handles POST /document/goods-issue/:id/copy — with resolved references.
func (h *GoodsIssueHandler) Copy(c *gin.Context) {
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

	copy := goods_issue.NewGoodsIssue(source.OrganizationID, source.CustomerID, source.WarehouseID)
	copy.Date = time.Now()
	copy.ContractID = source.ContractID
	copy.CustomerOrderNumber = source.CustomerOrderNumber
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

	refs, _ := resolveGoodsIssueRefs(ctx, copy)
	var response any
	if bag, ok := refs.(*dto.DocRefsBag); ok {
		response = dto.FromGoodsIssue(copy, bag.Refs, bag.CurrencyRefs)
	} else {
		response = dto.FromGoodsIssue(copy, nil)
	}
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// buildGoodsIssueTable builds a PrintTable from a GoodsIssueResponse for XLSX/DOCX renderers.
func buildGoodsIssueTable(resp *dto.GoodsIssueResponse, dp int, currSymbol string, showPrices bool) *printing.PrintTable {
	t := &printing.PrintTable{
		Title:    "Реализация товаров",
		Subtitle: fmt.Sprintf("№ %s от %s", resp.Number, printing.FormatDate(resp.Date)),
	}

	// Header fields
	orgName, custName, whName, curName, contractName := "", "", "", "", ""
	if resp.Organization != nil {
		orgName = resp.Organization.Name
	}
	if resp.Customer != nil {
		custName = resp.Customer.Name
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
			{Label: "Организация:", Value: orgName},
			{Label: "Склад:", Value: whName},
		},
		{
			{Label: "Покупатель:", Value: custName},
			{Label: "Договор:", Value: contractName},
		},
		{
			{Label: "Валюта:", Value: curName},
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
			{Label: "Итого:", Value: printing.FormatMoney(resp.TotalAmount, dp) + " " + currSymbol, Grand: true},
			{Label: "В том числе НДС:", Value: printing.FormatMoney(resp.TotalVAT, dp) + " " + currSymbol},
		}
	}

	// Signatures (horizontal layout matching HTML print form)
	t.SignatureBlock = printing.HorizontalSignatures(
		"Отпустил:",
		"Получил (покупатель):",
		"Материально-ответственное лицо:",
	)

	return t
}
