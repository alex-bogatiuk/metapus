package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// BaseHandler provides common handler utilities.
type BaseHandler struct{}

// NewBaseHandler creates a new base handler.
func NewBaseHandler() *BaseHandler {
	return &BaseHandler{}
}

// BindJSON binds and validates JSON request body.
func (h *BaseHandler) BindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		h.Error(c, apperror.NewValidation("invalid request body").WithDetail("error", err.Error()))
		return false
	}
	return true
}

// BindQuery binds and validates query parameters.
func (h *BaseHandler) BindQuery(c *gin.Context, obj any) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		h.Error(c, apperror.NewValidation("invalid query parameters").WithDetail("error", err.Error()))
		return false
	}
	return true
}

// Error processes error and sends appropriate response.
func (h *BaseHandler) Error(c *gin.Context, err error) {
	h.HandleError(c, err)
}

// HandleError registers error on Gin context and aborts request.
// Actual JSON response is produced by middleware.ErrorHandler (single source of truth).
func (h *BaseHandler) HandleError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// ParseIntQuery parses integer query parameter with default value.
func (h *BaseHandler) ParseIntQuery(c *gin.Context, key string, defaultVal int) int {
	val := c.Query(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return parsed
}

// GetTenantID extracts tenant ID from request context.
func (h *BaseHandler) GetTenantID(c *gin.Context) string {
	return appctx.GetTenantID(c.Request.Context())
}

// GetUserID extracts user ID from request context.
func (h *BaseHandler) GetUserID(c *gin.Context) string {
	userCtx := appctx.GetUserContext(c.Request.Context())
	if userCtx == nil {
		return ""
	}
	return userCtx.UserID
}

// CompleteIdempotency marks idempotency key as completed with the same HTTP semantics
// (status code + content type + body) for correct replay.
func (h *BaseHandler) CompleteIdempotency(c *gin.Context, statusCode int, contentType string, response any) {
	if key, exists := c.Get("idempotency_key"); exists {
		if store, ok := c.Get("idempotency_store"); ok {
			_ = store.(*postgres.IdempotencyStore).CompleteKey(c.Request.Context(), key.(string), statusCode, contentType, response)
		}
	}
}

// Created sends 201 response with ID.
func (h *BaseHandler) Created(c *gin.Context, id string) {
	response := dto.IDResponse{ID: id}
	h.CompleteIdempotency(c, http.StatusCreated, "application/json", response)
	c.JSON(http.StatusCreated, response)
}

// OK sends 200 response with data.
func (h *BaseHandler) OK(c *gin.Context, data any) {
	h.CompleteIdempotency(c, http.StatusOK, "application/json", data)
	c.JSON(http.StatusOK, data)
}

// NoContent sends 204 response.
func (h *BaseHandler) NoContent(c *gin.Context) {
	// 204 must replay as 204 with empty body.
	h.CompleteIdempotency(c, http.StatusNoContent, "", nil)
	c.Status(http.StatusNoContent)
}

// Success sends success response.
func (h *BaseHandler) Success(c *gin.Context, message string) {
	response := dto.SuccessResponse{Success: true, Message: message}
	h.CompleteIdempotency(c, http.StatusOK, "application/json", response)
	c.JSON(http.StatusOK, response)
}
