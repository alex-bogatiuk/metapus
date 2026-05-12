// Package middleware provides the RequireMerchantAccess middleware.
package middleware

import (
	"context"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
)

// MerchantAccessChecker resolves a user's role for a specific merchant.
// Extracted from MerchantUserRepository to keep middleware independent of
// the full repository interface (Interface Segregation Principle).
type MerchantAccessChecker interface {
	// GetRole returns the role a user has for a specific merchant.
	// Returns apperror.NotFound if no association exists.
	GetRole(ctx context.Context, userID, merchantID id.ID) (merchant.MerchantRole, error)
}

// RequireMerchantAccess enforces that the authenticated user has an
// association with the merchant identified by :merchantId URL param.
//
// CWE-639 fix: prevents BOLA by validating object-level ownership
// on top of the RBAC permission check done by RequirePermission.
//
// Rules:
//   - Global admins (user.IsAdmin) bypass the check.
//   - Non-admin users must have a role for this merchant with
//     privilege level ≤ maxRole (lower number = more privilege).
//   - If no association exists → 403 Forbidden.
//
// Must be placed AFTER Auth + SecurityContext middleware and BEFORE handlers.
func RequireMerchantAccess(checker MerchantAccessChecker, maxRole merchant.MerchantRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}

		// Global admins bypass merchant ownership checks.
		if user.IsAdmin {
			c.Next()
			return
		}

		// Parse :merchantId from URL path.
		rawMerchantID := c.Param("merchantId")
		if rawMerchantID == "" {
			_ = c.Error(apperror.NewValidation("merchantId path parameter is required"))
			c.Abort()
			return
		}

		merchantID, err := id.Parse(rawMerchantID)
		if err != nil {
			_ = c.Error(apperror.NewValidation("invalid merchantId"))
			c.Abort()
			return
		}

		userID, err := id.Parse(user.UserID)
		if err != nil {
			_ = c.Error(apperror.NewUnauthorized("invalid user identity"))
			c.Abort()
			return
		}

		// Check the user's role for this merchant.
		role, err := checker.GetRole(c.Request.Context(), userID, merchantID)
		if err != nil {
			// No association found → treat as forbidden, not "not found"
			// to avoid leaking merchant existence information.
			_ = c.Error(apperror.NewForbidden("access denied for this merchant"))
			c.Abort()
			return
		}

		// Role check: lower number = more privilege (Owner=1 < Manager=2 < Viewer=3).
		if role > maxRole {
			_ = c.Error(apperror.NewForbidden("insufficient merchant role"))
			c.Abort()
			return
		}

		c.Next()
	}
}
