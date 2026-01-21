package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	
	appctx "metapus/internal/core/context"
	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/storage/postgres"
)

const HeaderIdempotencyKey = "X-Idempotency-Key"
const maxIdempotencyBodyBytes = 1 << 20 // 1 MiB

// Idempotency middleware protects against duplicate requests.
// Used for POST/PUT/PATCH operations that should be idempotent.
func Idempotency(store *postgres.IdempotencyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only apply to mutating methods
		if c.Request.Method != http.MethodPost && 
		   c.Request.Method != http.MethodPut && 
		   c.Request.Method != http.MethodPatch {
			c.Next()
			return
		}
		
		// Get idempotency key
		key := c.GetHeader(HeaderIdempotencyKey)
		if key == "" {
			c.Next()
			return
		}
		
		// Get user context
		userID := ""
		if user := appctx.GetUser(c.Request.Context()); user != nil {
			userID = user.UserID
		}
		
		// Hash request body
		limited := io.LimitReader(c.Request.Body, maxIdempotencyBodyBytes+1)
		body, _ := io.ReadAll(limited)
		if len(body) > maxIdempotencyBodyBytes {
			appErr := apperror.NewValidation("request body too large for idempotency")
			appErr.HTTPStatus = http.StatusRequestEntityTooLarge
			_ = c.Error(appErr.WithDetail("max_bytes", maxIdempotencyBodyBytes))
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		hash := sha256.Sum256(body)
		requestHash := hex.EncodeToString(hash[:])
		
		// Operation name from path
		operation := c.Request.Method + " " + c.FullPath()
		
		// Try to acquire key
		replay, err := store.AcquireKey(c.Request.Context(), key, userID, operation, requestHash)
		if err != nil {
			if appErr, ok := apperror.AsAppError(err); ok {
				_ = c.Error(appErr)
				c.Abort()
				return
			}
			_ = c.Error(apperror.NewInternal(err).WithDetail("component", "idempotency"))
			c.Abort()
			return
		}
		
		// Return cached response if exists
		if replay != nil {
			c.Data(replay.StatusCode, replay.ContentType, replay.Body)
			c.Abort()
			return
		}
		
		// Store key for completion
		c.Set("idempotency_key", key)
		c.Set("idempotency_store", store)
		
		c.Next()
	}
}
