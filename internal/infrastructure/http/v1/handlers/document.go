package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	domainFilter "metapus/internal/domain/filter"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/dto"
)

// BaseDocumentHandler provides generic HTTP handlers for document entities.
// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
type BaseDocumentHandler[T any, CreateDTO any, UpdateDTO any] struct {
	*BaseHandler
	service    domain.DocumentService[T]
	entityName string

	// Mapper functions
	mapCreateDTO      func(dto CreateDTO) T
	mapUpdateDTO      func(dto UpdateDTO, existing T) T
	mapToDTO          func(entity T) any
	isPostImmediately func(dto CreateDTO) bool

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed.
	resolveRefs func(ctx context.Context, entities ...T) (any, error)

	// mapToDTOWithRefs is an enhanced mapper that receives the resolved refs bag.
	// Used instead of mapToDTO when resolveRefs is configured.
	mapToDTOWithRefs func(entity T, refs any) any

	// Movement providers for the document
	movementProviders    []entity.MovementProvider
	movementRefResolver  domain.RefResolver

	// settingsRepo reads tenant-level settings (batch concurrency, etc.).
	// If nil, default values are used.
	settingsRepo settings.Repository
}

// BaseDocumentHandlerConfig configures the document handler.
type BaseDocumentHandlerConfig[T any, CreateDTO any, UpdateDTO any] struct {
	Service           domain.DocumentService[T]
	EntityName        string
	MapCreateDTO      func(dto CreateDTO) T
	MapUpdateDTO      func(dto UpdateDTO, existing T) T
	MapToDTO          func(entity T) any
	IsPostImmediately func(dto CreateDTO) bool

	// ResolveRefs batch-resolves FK → display names. Returns an opaque refs bag.
	// If nil, no resolution is performed. Called before FLS masking and DTO mapping.
	ResolveRefs func(ctx context.Context, entities ...T) (any, error)

	// MapToDTOWithRefs is an enhanced mapper that receives the resolved refs bag.
	// Used instead of MapToDTO when ResolveRefs is configured.
	MapToDTOWithRefs func(entity T, refs any) any

	// MovementProviders allow the handler to resolve cross-register movements
	MovementProviders   []entity.MovementProvider
	MovementRefResolver domain.RefResolver

	// SettingsRepo reads tenant-level settings for batch concurrency.
	// If nil, default values (5) are used.
	SettingsRepo settings.Repository
}

// NewBaseDocumentHandler creates a new base document handler.
func NewBaseDocumentHandler[T any, CreateDTO any, UpdateDTO any](
	base *BaseHandler,
	cfg BaseDocumentHandlerConfig[T, CreateDTO, UpdateDTO],
) *BaseDocumentHandler[T, CreateDTO, UpdateDTO] {
	return &BaseDocumentHandler[T, CreateDTO, UpdateDTO]{
		BaseHandler:       base,
		service:           cfg.Service,
		entityName:        cfg.EntityName,
		mapCreateDTO:      cfg.MapCreateDTO,
		mapUpdateDTO:      cfg.MapUpdateDTO,
		mapToDTO:          cfg.MapToDTO,
		isPostImmediately: cfg.IsPostImmediately,
		resolveRefs:       cfg.ResolveRefs,
		mapToDTOWithRefs:  cfg.MapToDTOWithRefs,
		movementProviders:   cfg.MovementProviders,
		movementRefResolver: cfg.MovementRefResolver,
		settingsRepo:        cfg.SettingsRepo,
	}
}

// toDTO maps entity to DTO using the appropriate mapper.
// If refs is non-nil and mapToDTOWithRefs is configured, uses the enhanced mapper.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) toDTO(entity T, refs any) any {
	if h.mapToDTOWithRefs != nil && refs != nil {
		return h.mapToDTOWithRefs(entity, refs)
	}
	return h.mapToDTO(entity)
}

// applyFLSRead applies field-level security masking for read operations.
// Masks restricted fields on the domain entity before DTO mapping.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) applyFLSRead(c *gin.Context, entity any) {
	policy := security.GetFieldPolicy(c.Request.Context(), h.entityName, "read")
	if policy == nil {
		return
	}
	security.MaskForRead(entity, policy)
}

// Get handles GET /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Get(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, err = h.resolveRefs(ctx, doc)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	c.JSON(http.StatusOK, h.toDTO(doc, refs))
}

// GetMovements fetches movements for this document across all configured MovementProviders.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) GetMovements(c *gin.Context) {
	if len(h.movementProviders) == 0 {
		c.JSON(http.StatusOK, gin.H{"movements": []entity.DocumentMovement{}, "count": 0})
		return
	}

	ctx := c.Request.Context()
	docIDStr := c.Param("id")
	docID, err := id.Parse(docIDStr)
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid document ID"))
		return
	}

	var allMovements []entity.DocumentMovement

	// Extract movements from every configured provider
	for _, provider := range h.movementProviders {
		moves, err := provider.GetDocumentMovements(ctx, docID)
		if err != nil {
			h.Error(c, err)
			return
		}
		allMovements = append(allMovements, moves...)
	}

	// Batch-resolve ref-type fields to human-readable names
	if h.movementRefResolver != nil {
		enrichMovementRefs(ctx, allMovements, h.movementRefResolver)
	}

	c.JSON(http.StatusOK, gin.H{
		"movements": allMovements,
		"count":     len(allMovements),
	})
}

// Create handles POST /{entity}
// Supports postImmediately flag in DTO (requires DTO to implement interface or check manually?)
// The original handler checked `req.PostImmediately`. This is tricky for generic handler.
// We can define an interface for CreateRequest or check via type assertion/reflection, OR
// pass a function to extract this flag?
// Or we can assume CreateDTO is struct and we can't easily access field without reflection/interface.
// Let's make it simple: BaseDocumentHandler Create just calls Create.
// If we need PostImmediately behavior, we might need a specific Hook or Config?
// Note: GoodsReceipt and GoodsIssue BOTH have PostImmediately.
// Let's add a `IsPostImmediately(CreateDTO) bool` function to config?
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	// In Database-per-Tenant, no tenantID needed (isolation is physical)
	doc := h.mapCreateDTO(req)

	// Set created_by/updated_by is done in specific handler usually?
	// Or we can expect service to handle it from context strings?
	// Existing handlers set it explicitly:
	// if userID := h.GetUserID(c); userID != "" { doc.CreatedBy = userID ... }
	// We should probably add a Hook for "OnBeforeCreate"?
	// Or we can set it if T has setters?
	// T is `any`. Accessing fields is hard.
	// Maybe `MapCreateDTO` should handle setting UserID?
	// `MapCreateDTO` signature: `func(dto CreateDTO, tenantID string) T`. It doesn't get UserID.
	// We might need to change `MapCreateDTO` to accept context or UserID?
	// Or `BaseDocumentHandler` creates the doc, then we have a `Enrich(doc, c)` hook?

	// For now, let's stick to the core logic.
	// PostImmediately logic is also specific.
	// Maybe `Create` in BaseDocumentHandler is too ambitious to fully generalize without more hooks?
	// Let's try to keep it simple. If Create logic differs, we can override it in struct embedding.
	// BUT the goal IS to generalize Create.

	// Let's add `SetUserID(T, string)` to config?
	// And `IsPostImmediately(CreateDTO) bool` to config.

	if h.isPostImmediately != nil && h.isPostImmediately(req) {
		if err := h.service.PostAndSave(ctx, doc); err != nil {
			h.Error(c, err)
			return
		}
	} else {
		if err := h.service.Create(ctx, doc); err != nil {
			h.Error(c, err)
			return
		}
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// Update handles PUT /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Update(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req UpdateDTO
	if !h.BindJSON(c, &req) {
		return
	}

	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	doc = h.mapUpdateDTO(req, doc)

	// UserID updating?

	if err := h.service.Update(ctx, doc); err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Delete handles DELETE /{entity}/:id
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Delete(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Post handles POST /{entity}/:id/post
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Post(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Post(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// Unpost handles POST /{entity}/:id/unpost
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) Unpost(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	if err := h.service.Unpost(ctx, docID); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// SetDeletionMark handles POST /{entity}/:id/deletion-mark
// Sets or clears the deletion mark. If the document is posted and we're marking it for deletion,
// the service will unpost it first (1C-style behavior: unpost + mark in one transaction).
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) SetDeletionMark(c *gin.Context) {
	ctx := c.Request.Context()

	docID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id format"))
		return
	}

	var req dto.SetDeletionMarkRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := h.service.SetDeletionMark(ctx, docID, req.Marked); err != nil {
		h.Error(c, err)
		return
	}

	// Return updated document
	doc, err := h.service.GetByID(ctx, docID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, doc)
	}

	// FLS: mask restricted fields before DTO mapping
	h.applyFLSRead(c, doc)

	response := h.toDTO(doc, refs)
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}

// List handles GET /{entity} — list with filtering and pagination.
// Uses the universal filter engine via ParseListFilter.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) List(c *gin.Context) {
	ctx := c.Request.Context()

	filter, err := h.ParseListFilter(c, "-date")
	if err != nil {
		h.Error(c, err)
		return
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references for all items in batch (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, err = h.resolveRefs(ctx, result.Items...)
		if err != nil {
			h.Error(c, err)
			return
		}
	}

	// Map entities to DTOs (with FLS masking)
	items := make([]any, len(result.Items))
	for i, item := range result.Items {
		h.applyFLSRead(c, item)
		items[i] = h.toDTO(item, refs)
	}

	c.JSON(http.StatusOK, dto.CursorListResponse{
		Items:       items,
		NextCursor:  result.NextCursor,
		PrevCursor:  result.PrevCursor,
		HasMore:     result.HasMore,
		HasPrev:     result.HasPrev,
		TargetIndex: result.TargetIndex,
		TotalCount:  result.TotalCount,
	})
}

// ExportList handles POST /{entity}/export-list — exports the current list view to XLSX.
// Reuses the same List pipeline (filters, sorting, RLS, FLS, FK resolution)
// but without pagination (capped at ExportMaxRows).
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) ExportList(c *gin.Context) {
	ctx := c.Request.Context()

	req, filter, err := parseExportRequest(c)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Documents default to -date sort
	if req.OrderBy == "" && filter.OrderBy == "name" {
		filter.OrderBy = "-date"
	}

	result, err := h.service.List(ctx, filter)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Resolve FK references for all items in batch (if configured)
	var refs any
	if h.resolveRefs != nil {
		refs, _ = h.resolveRefs(ctx, result.Items...)
	}

	// Map entities to DTOs (with FLS masking)
	dtoItems := make([]any, len(result.Items))
	for i, item := range result.Items {
		h.applyFLSRead(c, item)
		dtoItems[i] = h.toDTO(item, refs)
	}

	writeExportXLSX(c, h.entityName, req, dtoItems)
}

// ── Batch Operations ────────────────────────────────────────────────────

// batchActionRequest is the DTO for batch document operations.
type batchActionRequest struct {
	IDs    []string `json:"ids" binding:"required,min=1,max=500"`
	Action string   `json:"action" binding:"required,oneof=post unpost setDeletionMark clearDeletionMark"`
}

// batchActionResult describes the outcome for a single document in a batch.
type batchActionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// batchActionResponse is the response for batch operations.
type batchActionResponse struct {
	Results []batchActionResult `json:"results"`
	Total   int                 `json:"total"`
	Success int                 `json:"success"`
	Failed  int                 `json:"failed"`
}

// ── Worker Pool for Concurrent Batch Processing ─────────────────────────

// defaultBatchConcurrency is used when settings are unavailable.
const defaultBatchConcurrency = 5

// maxConnsPerTenant must match tenant.Manager config. Used for clamping.
const maxConnsPerTenant = 10

// getBatchConcurrency reads the configured concurrency from tenant settings.
// Falls back to defaultBatchConcurrency on any error or if settingsRepo is nil.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) getBatchConcurrency(ctx context.Context) int {
	if h.settingsRepo == nil {
		return defaultBatchConcurrency
	}
	s, err := h.settingsRepo.Get(ctx)
	if err != nil || s.Performance.BatchConcurrency == 0 {
		return defaultBatchConcurrency
	}
	return settings.ClampBatchConcurrency(s.Performance.BatchConcurrency, maxConnsPerTenant)
}

// batchWorkerResult is sent from worker goroutines back to the main goroutine.
type batchWorkerResult struct {
	idx int    // original position in the input slice (for ordered results)
	id  id.ID  // document ID
	err error  // nil on success
}

// executeBatchConcurrent fans out document processing to a bounded worker pool.
// Results are streamed back via the returned channel in completion order.
// The channel is closed after all documents are processed or ctx is cancelled.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) executeBatchConcurrent(
	ctx context.Context, ids []id.ID, action string, concurrency int,
) <-chan batchWorkerResult {
	results := make(chan batchWorkerResult, concurrency*2)
	sem := make(chan struct{}, concurrency)

	go func() {
		defer close(results)
		var wg sync.WaitGroup

		for i, docID := range ids {
			// Stop launching new workers if ctx is cancelled
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case sem <- struct{}{}: // acquire worker slot
			}

			wg.Add(1)
			go func(idx int, did id.ID) {
				defer func() {
					<-sem // release worker slot
					wg.Done()
				}()
				err := h.executeAction(ctx, did, action)
				results <- batchWorkerResult{idx: idx, id: did, err: err}
			}(i, docID)
		}

		wg.Wait()
	}()

	return results
}

// BatchAction handles POST /{entity}/batch-action
//
// Processes each document independently (partial mode):
//   - One failure does not roll back others
//   - Returns per-item results for the client to display
//   - Permission checks are performed per-action inside the service layer
//   - Documents are processed concurrently via worker pool
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) BatchAction(c *gin.Context) {
	ctx := c.Request.Context()

	var req batchActionRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Pre-validate IDs and build typed slice
	parsedIDs := make([]id.ID, 0, len(req.IDs))
	invalidResults := make([]batchActionResult, 0)
	for _, rawID := range req.IDs {
		docID, err := id.Parse(rawID)
		if err != nil {
			invalidResults = append(invalidResults, batchActionResult{ID: rawID, Error: "invalid id format"})
			continue
		}
		parsedIDs = append(parsedIDs, docID)
	}

	// Process valid IDs concurrently
	results := make([]batchActionResult, len(parsedIDs))
	successCount := 0

	for r := range h.executeBatchConcurrent(ctx, parsedIDs, req.Action, h.getBatchConcurrency(ctx)) {
		results[r.idx] = batchActionResult{
			ID:      r.id.String(),
			Success: r.err == nil,
		}
		if r.err != nil {
			results[r.idx].Error = r.err.Error()
		} else {
			successCount++
		}
	}

	// Merge invalid + valid results
	allResults := append(invalidResults, results...)
	c.JSON(http.StatusOK, batchActionResponse{
		Results: allResults,
		Total:   len(allResults),
		Success: successCount,
		Failed:  len(allResults) - successCount,
	})
}

// ── Batch Action By Filter ──────────────────────────────────────────────

// DefaultBatchFilterLimit is the default safety limit for filter-based batch operations.
// Can be overridden per-handler via SetBatchFilterLimit.
const DefaultBatchFilterLimit = 100000

// batchActionByFilterRequest is the DTO for filter-based batch operations.
// Instead of specifying IDs explicitly, the client sends the same filter
// used by the list view. The server resolves matching IDs.
type batchActionByFilterRequest struct {
	Filter         json.RawMessage `json:"filter"`                    // JSON-encoded []filter.Item
	Action         string          `json:"action" binding:"required,oneof=post unpost setDeletionMark clearDeletionMark"`
	ExcludeIDs     []string        `json:"excludeIds"`                // IDs to skip (user manually unchecked)
	IncludeDeleted bool            `json:"includeDeleted"`            // match current list view
	OrderBy        string          `json:"orderBy"`                   // current sort (for filter consistency)
	Search         string          `json:"search"`                    // current search text
}

// BatchActionByFilter handles POST /{entity}/batch-action-by-filter
//
// Virtual "select all" workflow:
//  1. Client clicks "Select all N by filter" → sends current filters + action
//  2. Server resolves matching IDs via ListIDs (SELECT id WHERE ...)
//  3. Removes excludeIDs (user manually unchecked individual items)
//  4. Processes each document independently (partial mode, same as BatchAction)
//
// If the client sends Accept: text/event-stream, the handler streams progress
// via Server-Sent Events (SSE). Otherwise, returns a single JSON response.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) BatchActionByFilter(c *gin.Context) {
	ctx := c.Request.Context()

	var req batchActionByFilterRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Build ListFilter from request
	listFilter := domain.DefaultListFilter()
	listFilter.IncludeDeleted = req.IncludeDeleted
	listFilter.Search = req.Search
	listFilter.OrderBy = req.OrderBy
	if listFilter.OrderBy == "" {
		listFilter.OrderBy = "-date"
	}

	// Parse advanced filters from JSON
	if len(req.Filter) > 0 && string(req.Filter) != "null" {
		var advFilters []domainFilter.Item
		if err := json.Unmarshal(req.Filter, &advFilters); err != nil {
			h.Error(c, apperror.NewValidation("invalid filter format").
				WithDetail("error", err.Error()))
			return
		}
		if err := domainFilter.ValidateItems(advFilters); err != nil {
			h.Error(c, apperror.NewValidation("invalid filter").
				WithDetail("error", err.Error()))
			return
		}
		listFilter.AdvancedFilters = advFilters
	}

	// Inject RLS DataScope from context
	listFilter.DataScope = security.GetDataScope(ctx)

	// Resolve matching IDs via service (applies RLS)
	ids, err := h.service.ListIDs(ctx, listFilter, DefaultBatchFilterLimit)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Remove excluded IDs (user manually unchecked)
	if len(req.ExcludeIDs) > 0 {
		excludeSet := make(map[string]struct{}, len(req.ExcludeIDs))
		for _, eid := range req.ExcludeIDs {
			excludeSet[eid] = struct{}{}
		}
		filtered := ids[:0]
		for _, docID := range ids {
			if _, excluded := excludeSet[docID.String()]; !excluded {
				filtered = append(filtered, docID)
			}
		}
		ids = filtered
	}

	// Dispatch based on Accept header
	if c.GetHeader("Accept") == "text/event-stream" {
		h.streamBatchAction(c, ids, req.Action)
		return
	}

	// Sync JSON mode (backward compatible)
	h.syncBatchAction(c, ids, req.Action)
}

// syncBatchAction processes all documents concurrently and returns a single JSON response.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) syncBatchAction(
	c *gin.Context, ids []id.ID, action string,
) {
	ctx := c.Request.Context()
	results := make([]batchActionResult, len(ids))
	successCount := 0

	for r := range h.executeBatchConcurrent(ctx, ids, action, h.getBatchConcurrency(ctx)) {
		results[r.idx] = batchActionResult{
			ID:      r.id.String(),
			Success: r.err == nil,
		}
		if r.err != nil {
			results[r.idx].Error = r.err.Error()
		} else {
			successCount++
		}
	}

	c.JSON(http.StatusOK, batchActionResponse{
		Results: results,
		Total:   len(results),
		Success: successCount,
		Failed:  len(results) - successCount,
	})
}

// ── SSE Streaming ───────────────────────────────────────────────────────

const sseProgressInterval = 50 // emit progress event every N processed documents

// sseEvent represents a Server-Sent Event for batch progress.
type sseEvent struct {
	Type      string `json:"type"`                // started | progress | completed | cancelled
	Processed int    `json:"processed,omitempty"` // documents processed so far
	Success   int    `json:"success,omitempty"`   // successful operations
	Failed    int    `json:"failed,omitempty"`    // failed operations
	Total     int    `json:"total"`               // total documents to process
}

// streamBatchAction processes documents and streams SSE progress events.
// Supports cancellation via client disconnect (ctx.Done).
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) streamBatchAction(
	c *gin.Context, ids []id.ID, action string,
) {
	ctx := c.Request.Context()

	// Extend write deadline for SSE — the default server WriteTimeout (30s) is
	// too short for long-running batch operations. Go 1.20+ ResponseController
	// overrides the deadline per-request without affecting other handlers.
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Time{}) // no deadline for streaming

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable nginx buffering
	c.Status(http.StatusOK)

	total := len(ids)
	processed, success, failed := 0, 0, 0

	// Send "started" event
	writeSSE(c, sseEvent{Type: "started", Total: total})

	// Consume results from the concurrent worker pool.
	// All SSE writes happen here (main goroutine) — no concurrent writes.
	for r := range h.executeBatchConcurrent(ctx, ids, action, h.getBatchConcurrency(ctx)) {
		processed++
		if r.err != nil {
			failed++
		} else {
			success++
		}

		// Emit progress every N docs or at the end
		if processed%sseProgressInterval == 0 || processed == total {
			writeSSE(c, sseEvent{
				Type:      "progress",
				Processed: processed,
				Success:   success,
				Failed:    failed,
				Total:     total,
			})
		}
	}

	// Determine final event type
	select {
	case <-ctx.Done():
		writeSSE(c, sseEvent{
			Type:      "cancelled",
			Processed: processed,
			Success:   success,
			Failed:    failed,
			Total:     total,
		})
	default:
		writeSSE(c, sseEvent{
			Type:      "completed",
			Processed: processed,
			Success:   success,
			Failed:    failed,
			Total:     total,
		})
	}
}

// writeSSE writes a single SSE event to the response stream and flushes.
func writeSSE(c *gin.Context, event sseEvent) {
	data, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
}

// executeAction runs a single batch action on one document.
func (h *BaseDocumentHandler[T, CreateDTO, UpdateDTO]) executeAction(
	ctx context.Context, docID id.ID, action string,
) error {
	switch action {
	case "post":
		return h.service.Post(ctx, docID)
	case "unpost":
		return h.service.Unpost(ctx, docID)
	case "setDeletionMark":
		return h.service.SetDeletionMark(ctx, docID, true)
	case "clearDeletionMark":
		return h.service.SetDeletionMark(ctx, docID, false)
	default:
		return apperror.NewValidation("unknown action: " + action)
	}
}

