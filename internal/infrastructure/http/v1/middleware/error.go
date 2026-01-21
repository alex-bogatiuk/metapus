package middleware

import (
	"github.com/gin-gonic/gin"
	
	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

// ErrorHandler middleware transforms errors into consistent JSON responses.
// Hides internal errors from clients while logging full details.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		
		// Check for errors
		if len(c.Errors) == 0 {
			return
		}
		
		// Get last error
		err := c.Errors.Last().Err

		// If response already written by handler, do not override it.
		if c.Writer.Written() {
			return
		}
		
		// Try to extract AppError
		if appErr, ok := apperror.AsAppError(err); ok {
			// Log internal error if present
			if appErr.Err != nil {
				logger.Error(c.Request.Context(), "request error",
					"code", appErr.Code,
					"cause", appErr.Err,
				)
			}
			
			body := gin.H{
				"code":    appErr.Code,
				"message": appErr.Message,
				"details": appErr.Details,
			}

			// Mark idempotency as failed with the exact response we return (best-effort).
			if key, exists := c.Get("idempotency_key"); exists {
				if store, ok := c.Get("idempotency_store"); ok {
					if s, ok := store.(*postgres.IdempotencyStore); ok && s != nil {
						_ = s.FailKey(c.Request.Context(), key.(string), appErr.HTTPStatus, "application/json", body)
					}
				}
			}

			c.JSON(appErr.HTTPStatus, body)
			return
		}
		
		// Unknown error - log and return generic message
		logger.Error(c.Request.Context(), "unhandled error",
			"error", err,
		)
		
		body := gin.H{
			"code":    apperror.CodeInternal,
			"message": "Internal server error",
			"details": map[string]any{
				"request_id": c.GetString("request_id"),
			},
		}

		// Mark idempotency as failed with the exact response we return (best-effort).
		if key, exists := c.Get("idempotency_key"); exists {
			if store, ok := c.Get("idempotency_store"); ok {
				if s, ok := store.(*postgres.IdempotencyStore); ok && s != nil {
					_ = s.FailKey(c.Request.Context(), key.(string), 500, "application/json", body)
				}
			}
		}

		c.JSON(500, body)
	}
}
