package readaccess

import (
	"context"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/security_profile"
)

type ProfileReader interface {
	GetUserProfile(context.Context, id.ID) (*security_profile.SecurityProfile, error)
}

// BuildSecurityContext has the same semantics for HTTP and scheduled jobs:
// nil profile is unassigned, but a failed lookup is never unrestricted access.
func BuildSecurityContext(ctx context.Context, profiles ProfileReader, resolvers ...security_profile.DimensionResolver) (context.Context, error) {
	user := appctx.GetUser(ctx)
	if user == nil {
		return nil, apperror.NewUnauthorized("authentication required")
	}
	uid, err := id.Parse(user.UserID)
	if err != nil {
		return nil, apperror.NewForbidden("security context is unavailable").WithCause(err)
	}
	var profile *security_profile.SecurityProfile
	if !user.IsAdmin {
		if profiles == nil {
			return nil, apperror.NewForbidden("security context is unavailable")
		}
		profile, err = profiles.GetUserProfile(ctx, uid)
		if err != nil {
			return nil, apperror.NewForbidden("security context is unavailable").WithCause(err)
		}
	}
	sc := security_profile.BuildSecurityContext(profile, user.IsAdmin)
	if !user.IsAdmin {
		for _, resolver := range resolvers {
			values, err := resolver.Resolve(ctx, uid)
			if err != nil {
				return nil, apperror.NewForbidden("security context is unavailable").WithCause(err)
			}
			if values != nil {
				sc.DataScope.SetDimension(resolver.DimensionName(), values)
			}
		}
	}
	ctx = security.WithDataScope(ctx, sc.DataScope)
	ctx = security.WithFieldPolicies(ctx, sc.FieldPolicies)
	return security.WithPolicyRules(ctx, sc.PolicyRules), nil
}

type PrincipalUsers interface {
	GetByID(context.Context, id.ID) (*auth.User, error)
	LoadRoles(context.Context, id.ID) ([]auth.Role, error)
	LoadPermissions(context.Context, id.ID) ([]string, error)
}

type PrincipalLoader struct {
	users     PrincipalUsers
	profiles  ProfileReader
	resolvers []security_profile.DimensionResolver
}

func NewPrincipalLoader(users PrincipalUsers, profiles ProfileReader, resolvers ...security_profile.DimensionResolver) *PrincipalLoader {
	return &PrincipalLoader{users, profiles, resolvers}
}

// ContextFor reloads the explicit executor and current permissions. It never
// inherits the event actor, a scheduler's privileges, or another principal's cache.
func (s *PrincipalLoader) ContextFor(ctx context.Context, uid id.ID) (context.Context, error) {
	if id.IsNil(uid) {
		return nil, apperror.NewForbidden("report executor is required")
	}
	user, err := s.users.GetByID(ctx, uid)
	if err != nil {
		return nil, apperror.NewForbidden("report executor is unavailable").WithCause(err)
	}
	if user == nil || !user.IsActive || user.DeletionMark || user.IsLocked() {
		return nil, apperror.NewForbidden("report executor is unavailable")
	}
	roles, err := s.users.LoadRoles(ctx, uid)
	if err != nil {
		return nil, apperror.NewForbidden("report executor permissions are unavailable").WithCause(err)
	}
	permissions, err := s.users.LoadPermissions(ctx, uid)
	if err != nil {
		return nil, apperror.NewForbidden("report executor permissions are unavailable").WithCause(err)
	}
	principal := &appctx.UserContext{UserID: uid.String(), TenantID: tenant.GetTenantID(ctx), Email: user.Email, IsAdmin: user.IsAdmin, Permissions: permissions}
	for _, role := range roles {
		principal.Roles = append(principal.Roles, role.Code)
	}
	return BuildSecurityContext(appctx.WithUser(ctx, principal), s.profiles, s.resolvers...)
}
