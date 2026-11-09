package automation

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
)

// RoleRepository is a minimal interface for resolving roles.
// Subset of auth.RoleRepository — avoids importing auth domain.
type RoleRepository interface {
	GetByCode(ctx context.Context, code string) (RoleBrief, error)
	ListUserIDsByRoleID(ctx context.Context, roleID id.ID) ([]id.ID, error)
}

// RoleBrief is a lightweight role view for the automation package.
type RoleBrief struct {
	ID   id.ID
	Code string
	Name string
}

// roleUserResolver implements UserResolver using RoleRepository.
type roleUserResolver struct {
	roleRepo RoleRepository
}

// NewRoleUserResolver creates a UserResolver backed by a RoleRepository.
func NewRoleUserResolver(repo RoleRepository) UserResolver {
	return &roleUserResolver{roleRepo: repo}
}

// ResolveUserIDsByRole resolves role code → role ID → user IDs.
func (r *roleUserResolver) ResolveUserIDsByRole(ctx context.Context, roleCode string) ([]id.ID, error) {
	role, err := r.roleRepo.GetByCode(ctx, roleCode)
	if err != nil {
		return nil, fmt.Errorf("get role by code %q: %w", roleCode, err)
	}

	userIDs, err := r.roleRepo.ListUserIDsByRoleID(ctx, role.ID)
	if err != nil {
		return nil, fmt.Errorf("list users for role %q: %w", roleCode, err)
	}

	return userIDs, nil
}
