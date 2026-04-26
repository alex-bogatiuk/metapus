package adapters

import (
	"context"
	"fmt"

	"metapus/internal/core/automation"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
)

// authRoleAdapter adapts auth.RoleRepository to automation.RoleRepository.
// Bridges the auth domain types to the lightweight automation.RoleBrief.
type authRoleAdapter struct {
	repo auth.RoleRepository
}

// NewAuthRoleAdapter creates an automation.RoleRepository adapter from auth.RoleRepository.
func NewAuthRoleAdapter(repo auth.RoleRepository) automation.RoleRepository {
	return &authRoleAdapter{repo: repo}
}

func (a *authRoleAdapter) GetByCode(ctx context.Context, code string) (automation.RoleBrief, error) {
	role, err := a.repo.GetByCode(ctx, code)
	if err != nil {
		return automation.RoleBrief{}, fmt.Errorf("get role by code: %w", err)
	}
	return automation.RoleBrief{
		ID:   role.ID,
		Code: role.Code,
		Name: role.Name,
	}, nil
}

func (a *authRoleAdapter) ListUserIDsByRoleID(ctx context.Context, roleID id.ID) ([]id.ID, error) {
	return a.repo.ListUserIDsByRoleID(ctx, roleID)
}
