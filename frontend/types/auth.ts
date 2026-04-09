/**
 * Auth types – mirrors backend auth DTOs.
 * See: internal/infrastructure/http/v1/dto/auth.go
 */

// ── Request DTOs ────────────────────────────────────────────────────────

export interface LoginRequest {
  email: string
  password: string
}

export interface RegisterRequest {
  email: string
  password: string
  firstName?: string
  lastName?: string
}

export interface RefreshTokenRequest {
  refreshToken: string
}

// ── Response DTOs ───────────────────────────────────────────────────────

export interface TokenResponse {
  accessToken: string
  refreshToken: string
  expiresAt: string
  tokenType: string
}

export interface AuthRoleResponse {
  id: string
  code: string
  name: string
  description?: string
  isSystem: boolean
}

export interface AuthUserResponse {
  id: string
  email: string
  firstName?: string
  lastName?: string
  fullName: string
  isActive: boolean
  isAdmin: boolean
  emailVerified: boolean
  roles?: AuthRoleResponse[]
  createdAt: string
}

export interface LoginResponse {
  tokens: TokenResponse
  user: AuthUserResponse
}
