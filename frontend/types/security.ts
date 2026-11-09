import type { fieldPolicySchema, policyRuleSchema, SecurityProfile } from "@/lib/schemas/security"
import type * as Wire from "@/types/generated/http-contracts"
import type { z } from "zod"
/**
 * Security types for security profile management.
 * Mirrors backend SecurityProfile domain model + DTOs.
 */

// ── Field Policy ──────────────────────────────────────────────────────────

export type FieldPolicyItem = z.infer<typeof fieldPolicySchema>

// ── CEL Policy Rule ───────────────────────────────────────────────────────

export type PolicyRuleResponse = z.infer<typeof policyRuleSchema>

export type CreatePolicyRuleRequest = Wire.CreatePolicyRuleRequest

export type UpdatePolicyRuleRequest = Wire.UpdatePolicyRuleRequest

// ── Security Profile ──────────────────────────────────────────────────────

export type SecurityProfileResponse = SecurityProfile

export type CreateSecurityProfileRequest = Wire.CreateSecurityProfileRequest

export type UpdateSecurityProfileRequest = Wire.UpdateSecurityProfileRequest

export type AssignProfileUserRequest = Wire.AssignProfileRequest

// ── CEL Validation ────────────────────────────────────────────────────────

export type ValidateExpressionResponse = Wire.ValidateExpressionResponse

// ── Users (from auth) ─────────────────────────────────────────────────────

export type UserResponse = Wire.UserResponse

export type SecurityProfileBrief = Wire.SecurityProfileBrief

export type UpdateUserRequest = Wire.UpdateUserRequest

export type CreateUserAdminRequest = Wire.CreateUserAdminRequest

export type ProfileUserItem = Wire.ProfileUserItem

export type RoleResponse = Wire.RoleResponse

export type CreateRoleRequest = Wire.CreateRoleRequest

export type UpdateRoleRequest = Wire.UpdateRoleRequest

export type PermissionResponse = Wire.PermissionResponse

// ── CEL Sandbox ──────────────────────────────────────────────────────────

export type TestExpressionResponse = Wire.TestExpressionResponse

// ── Audit Log ────────────────────────────────────────────────────────────

export type AuditEntryResponse = Wire.AuditEntryResponse

// ── Effective Access ─────────────────────────────────────────────────────

export type EffectiveAccessResponse = Omit<Wire.EffectiveAccessResponse, "user"> & { user: Wire.UserResponse }
