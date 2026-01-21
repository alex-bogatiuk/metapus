// Package dto provides data transfer objects for HTTP API.
package dto

import (
	"time"

	"metapus/internal/domain/auth"
)

// --- Request DTOs ---

// RegisterRequest for user registration.
type RegisterRequest struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
}

// ToAuthRequest converts to domain request.
func (r *RegisterRequest) ToAuthRequest() auth.RegisterRequest {
	return auth.RegisterRequest{
		Email:     r.Email,
		Password:  r.Password,
		FirstName: r.FirstName,
		LastName:  r.LastName,
	}
}

// LoginRequest for user login.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// ToCredentials converts to domain credentials.
func (r *LoginRequest) ToCredentials() auth.Credentials {
	return auth.Credentials{
		Email:    r.Email,
		Password: r.Password,
	}
}

// RefreshTokenRequest for token refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// AssignRoleRequest for assigning role to user.
type AssignRoleRequest struct {
	UserID   string `json:"userId" binding:"required,uuid"`
	RoleCode string `json:"roleCode" binding:"required"`
}

// --- Response DTOs ---

// TokenResponse represents token pair response.
type TokenResponse struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	TokenType    string    `json:"tokenType"`
}

// FromTokenPair creates response from domain token pair.
func FromTokenPair(tp *auth.TokenPair) *TokenResponse {
	return &TokenResponse{
		AccessToken:  tp.AccessToken,
		RefreshToken: tp.RefreshToken,
		ExpiresAt:    tp.ExpiresAt,
		TokenType:    tp.TokenType,
	}
}

// UserResponse represents user in API response.
type UserResponse struct {
	ID            string       `json:"id"`
	Email         string       `json:"email"`
	FirstName     string       `json:"firstName,omitempty"`
	LastName      string       `json:"lastName,omitempty"`
	FullName      string       `json:"fullName"`
	IsActive      bool         `json:"isActive"`
	IsAdmin       bool         `json:"isAdmin"`
	EmailVerified bool         `json:"emailVerified"`
	Roles         []RoleResponse `json:"roles,omitempty"`
	CreatedAt     time.Time    `json:"createdAt"`
}

// FromUser creates response from domain user.
func FromUser(u *auth.User) *UserResponse {
	roles := make([]RoleResponse, len(u.Roles))
	for i, r := range u.Roles {
		roles[i] = *FromRole(&r)
	}

	return &UserResponse{
		ID:            u.ID.String(),
		Email:         u.Email,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		FullName:      u.FullName(),
		IsActive:      u.IsActive,
		IsAdmin:       u.IsAdmin,
		EmailVerified: u.EmailVerified,
		Roles:         roles,
		CreatedAt:     u.CreatedAt,
	}
}

// RoleResponse represents role in API response.
type RoleResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsSystem    bool   `json:"isSystem"`
}

// FromRole creates response from domain role.
func FromRole(r *auth.Role) *RoleResponse {
	return &RoleResponse{
		ID:          r.ID.String(),
		Code:        r.Code,
		Name:        r.Name,
		Description: r.Description,
		IsSystem:    r.IsSystem,
	}
}

// PermissionResponse represents permission in API response.
type PermissionResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
}

// FromPermission creates response from domain permission.
func FromPermission(p *auth.Permission) *PermissionResponse {
	return &PermissionResponse{
		ID:          p.ID.String(),
		Code:        p.Code,
		Name:        p.Name,
		Description: p.Description,
		Resource:    p.Resource,
		Action:      p.Action,
	}
}

// LoginResponse includes tokens and user info.
type LoginResponse struct {
	Tokens *TokenResponse `json:"tokens"`
	User   *UserResponse  `json:"user"`
}
