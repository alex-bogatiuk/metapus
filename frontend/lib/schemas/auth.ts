import { z } from "zod"
import * as Wire from "@/lib/generated/http-contract-schemas"
import type { AuthLoginResponse, LoginResponse, MFAEnrollConfirmResponse } from "@/types/auth"
import type * as Contract from "@/types/generated/http-contracts"

export const authCacheScopeSchema = Wire.AuthCacheScopeSchema.and(z.object({
  tenantId: z.string().min(1),
  principalId: z.string().uuid(),
  sessionId: z.string().uuid(),
  userVersion: z.string().regex(/^[1-9][0-9]*$/),
  policyVersion: z.string().regex(/^[1-9][0-9]*$/),
}))

export const tokenResponseSchema = Wire.TokenResponseSchema.and(z.object({
  accessToken: z.string().min(1), expiresAt: z.string().datetime({ offset: true }),
  tokenType: z.string().min(1), cacheScope: authCacheScopeSchema,
}))

export const authUserSchema = Wire.UserResponseSchema.and(z.object({
  id: z.string().uuid(), email: z.string(), firstName: z.string().optional(), lastName: z.string().optional(),
  fullName: z.string(), isActive: z.boolean(), isAdmin: z.boolean(), emailVerified: z.boolean(),
  roles: z.array(z.object({ id: z.string(), code: z.string(), name: z.string(), description: z.string().optional(), isSystem: z.boolean() })).optional(),
  merchantIds: z.array(z.string()).optional(), mfaEnabled: z.boolean(), mfaPending: z.boolean(), mfaRequired: z.boolean(),
  mfaGraceEndsAt: z.string().optional(), mfaVerifiedAt: z.string().optional(), createdAt: z.string(),
}))

export const loginResponseSchema: z.ZodType<LoginResponse> = Wire.LoginResponseSchema.and(z.object({ tokens: tokenResponseSchema, user: authUserSchema }))
export const authLoginResponseSchema: z.ZodType<AuthLoginResponse> = z.union([
  loginResponseSchema.and(z.object({ status: z.never().optional() })),
  Wire.MFAChallengeResponseSchema.and(z.object({ status: z.enum(["mfa_required", "mfa_enrollment_required"]), user: authUserSchema.optional(), tokens: z.never().optional() })),
])
export const mfaEnrollConfirmResponseSchema: z.ZodType<MFAEnrollConfirmResponse> = Wire.MFAEnrollConfirmResponseSchema.and(z.object({ tokens: tokenResponseSchema, user: authUserSchema }))

export const adminUserListSchema = Wire.AdminUserListResponseSchema.pipe(z.object({ items: z.array(authUserSchema), total: z.number().int() }))
export const roleListSchema: z.ZodType<{ items: Contract.RoleResponse[] }> = Wire.RoleListResponseSchema.and(z.object({ items: z.array(Wire.RoleResponseSchema) }))
export const permissionListSchema: z.ZodType<{ items: Contract.PermissionResponse[] }> = Wire.PermissionListResponseSchema.and(z.object({ items: z.array(Wire.PermissionResponseSchema) }))
export const effectiveAccessSchema = Wire.EffectiveAccessResponseSchema.and(z.object({ user: authUserSchema }))

export const persistedAuthSchema = z.object({
  tokens: tokenResponseSchema.nullable(), user: authUserSchema.nullable(), isAuthenticated: z.boolean(),
  cacheScope: authCacheScopeSchema.nullable(),
})
