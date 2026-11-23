package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// StepUpChecker verifies fresh sensitive-action confirmations.
type StepUpChecker interface {
	HasFreshStepUp(ctx context.Context, sessionID, userID id.ID, tenantID, actionCode string, now time.Time) (bool, error)
}

func adminIPAllowed(clientIP string, allowedCIDRs []netip.Prefix) bool {
	if len(allowedCIDRs) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range allowedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// AdminGuard applies admin CIDR and MFA controls to authenticated admin principals.
func AdminGuard(
	mode auth.AdminSecurityMode,
	allowedCIDRs []netip.Prefix,
	mfaMode auth.MFAMode,
	mfaRepo auth.MFARepository,
	writer DirectEventWriter,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if !appctx.IsAdminPrincipal(user) {
			c.Next()
			return
		}

		if err := enforceAdminCIDR(c, user, mode, allowedCIDRs, writer); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if err := enforceAdminMFA(c, user, mfaMode, mfaRepo, writer); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireSystemAdmin fences emergency business-data actions. Keep this check
// in addition to RBAC so a security_admin cannot acquire emergency accounting
// controls merely by being inside the broader admin security boundary.
func RequireSystemAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !appctx.IsSystemAdmin(appctx.GetUser(c.Request.Context())) {
			_ = c.Error(apperror.NewForbidden("system administrator access is required"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func enforceAdminCIDR(c *gin.Context, user *appctx.UserContext, mode auth.AdminSecurityMode, allowedCIDRs []netip.Prefix, writer DirectEventWriter) error {
	if mode == auth.AdminSecurityOff || adminIPAllowed(c.ClientIP(), allowedCIDRs) {
		return nil
	}

	eventType := eventlog.EventSecurityAdminIPWarn
	severity := eventlog.SeverityWarning
	if mode == auth.AdminSecurityEnforce {
		eventType = eventlog.EventSecurityAdminIPDenied
		severity = eventlog.SeverityCritical
	}
	emitAdminSecurityEvent(c, writer, eventType, severity,
		fmt.Sprintf("Admin route access from non-allowlisted IP: %s", user.Email),
		map[string]any{
			"path":   c.Request.URL.Path,
			"method": c.Request.Method,
			"email":  user.Email,
			"ip":     c.ClientIP(),
			"mode":   string(mode),
		},
	)
	if mode == auth.AdminSecurityEnforce {
		return apperror.NewAdminIPDenied()
	}
	return nil
}

func enforceAdminMFA(c *gin.Context, user *appctx.UserContext, mode auth.MFAMode, repo auth.MFARepository, writer DirectEventWriter) error {
	if mode == auth.MFAModeOff || isAuthContinuationPath(c.Request.URL.Path) {
		return nil
	}
	if user.MFAVerifiedAt != nil && !user.MFAPending {
		return nil
	}
	if repo == nil {
		return apperror.NewInternal(fmt.Errorf("mfa repository is not configured"))
	}
	userID, err := id.Parse(user.UserID)
	if err != nil {
		return apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}

	rec, err := repo.GetTOTP(c.Request.Context(), userID)
	if err != nil {
		if apperror.IsNotFound(err) {
			if mode == auth.MFAModeOptional {
				return nil
			}
			emitMFARequired(c, writer, user, "enrollment")
			return apperror.NewMFAEnrollmentRequired("MFA enrollment is required")
		}
		return apperror.NewInternal(err)
	}
	if rec.Enabled {
		emitMFARequired(c, writer, user, "verify")
		return apperror.NewStepUpRequired("auth.session_mfa")
	}
	if mode == auth.MFAModeRequired && (rec.RequiredAfter == nil || !time.Now().UTC().Before(*rec.RequiredAfter)) {
		emitMFARequired(c, writer, user, "enrollment")
		return apperror.NewMFAEnrollmentRequired("MFA enrollment is required")
	}
	return nil
}

func isAuthContinuationPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth/")
}

func emitMFARequired(c *gin.Context, writer DirectEventWriter, user *appctx.UserContext, reason string) {
	emitAdminSecurityEvent(c, writer, eventlog.EventSecurityMFARequired, eventlog.SeverityWarning,
		fmt.Sprintf("MFA required for admin route access: %s", user.Email),
		map[string]any{
			"path":   c.Request.URL.Path,
			"method": c.Request.Method,
			"userId": user.UserID,
			"reason": reason,
		},
	)
}

// SensitiveActionGuard requires a fresh step-up confirmation for one action code.
func SensitiveActionGuard(checker StepUpChecker, actionCode string, writer DirectEventWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := appctx.GetUser(c.Request.Context())
		if user == nil {
			_ = c.Error(apperror.NewUnauthorized("authentication required"))
			c.Abort()
			return
		}
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if checker == nil {
			_ = c.Error(apperror.NewInternal(fmt.Errorf("step-up checker is not configured")))
			c.Abort()
			return
		}
		userID, err := id.Parse(user.UserID)
		if err != nil {
			_ = c.Error(apperror.NewUnauthorized("invalid user identity").WithCause(err))
			c.Abort()
			return
		}
		sessionID, err := id.Parse(user.SessionID)
		if err != nil || id.IsNil(sessionID) {
			_ = c.Error(apperror.NewUnauthorized("invalid session"))
			c.Abort()
			return
		}
		tenantID := tenant.GetTenantID(c.Request.Context())
		ok, err := checker.HasFreshStepUp(c.Request.Context(), sessionID, userID, tenantID, actionCode, time.Now().UTC())
		if err != nil {
			_ = c.Error(apperror.NewInternal(err))
			c.Abort()
			return
		}
		if !ok {
			emitAdminSecurityEvent(c, writer, eventlog.EventSecurityStepUpRequired, eventlog.SeverityWarning,
				fmt.Sprintf("Step-up required for %s", actionCode),
				map[string]any{
					"actionCode": actionCode,
					"path":       c.Request.URL.Path,
					"method":     c.Request.Method,
					"userId":     user.UserID,
				},
			)
			_ = c.Error(apperror.NewStepUpRequired(actionCode))
			c.Abort()
			return
		}
		c.Next()
	}
}

func emitAdminSecurityEvent(c *gin.Context, writer DirectEventWriter, eventType eventlog.EventType, severity eventlog.Severity, message string, details map[string]any) {
	if writer == nil {
		return
	}
	pool, err := tenantdb.GetPool(c.Request.Context())
	if err != nil {
		return
	}
	user := appctx.GetUser(c.Request.Context())
	event := eventlog.Event{
		Category:  eventlog.CategorySecurity,
		Severity:  severity,
		EventType: eventType,
		Source:    "api",
		ClientIP:  c.ClientIP(),
		Message:   message,
		Details:   details,
	}
	if user != nil {
		event.UserID = user.UserID
		event.SessionID = user.SessionID
	}
	if err := writer.WriteDirect(c.Request.Context(), pool, event); err != nil {
		logger.Warn(c.Request.Context(), "eventlog: failed to write admin security event", "eventType", eventType, "error", err)
	}
}
