import type * as Wire from "@/types/generated/http-contracts"
/**
 * Auth types – mirrors backend auth DTOs.
 * See: internal/infrastructure/http/v1/dto/auth.go
 */

// ── Request DTOs ────────────────────────────────────────────────────────

export type LoginRequest = Wire.LoginRequest

export type RegisterRequest = Wire.RegisterRequest

export type RefreshTokenRequest = Wire.RefreshTokenRequest

// ── Response DTOs ───────────────────────────────────────────────────────

// refreshToken is now delivered via httpOnly cookie, not in JSON response.
export type AuthCacheScope = Wire.AuthCacheScope

export type TokenResponse = Wire.TokenResponse

export type AuthRoleResponse = Wire.RoleResponse

export type AuthUserResponse = Wire.UserResponse

export interface LoginResponse {
  tokens: TokenResponse
  user: AuthUserResponse
}

export type MFAChallengeStatus = "mfa_required" | "mfa_enrollment_required"

export interface MFAChallengeResponse {
  status: MFAChallengeStatus
  challengeId: string
  expiresAt: string
  user?: AuthUserResponse
}

export type AuthLoginResponse = LoginResponse | MFAChallengeResponse

export type MFAVerifyRequest = Wire.MFAVerifyRequest

export type MFAEnrollStartRequest = Wire.MFAEnrollStartRequest

export type MFAEnrollStartResponse = Wire.MFAEnrollStartResponse

export type MFAEnrollConfirmRequest = Wire.MFAEnrollConfirmRequest

export type MFAResetRequest = Wire.MFAResetRequest

export type StepUpRequest = Wire.StepUpRequest

export type StepUpResponse = Wire.StepUpResponse
