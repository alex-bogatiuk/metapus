/**
 * Security types for security profile management.
 * Mirrors backend SecurityProfile domain model + DTOs.
 */

// ── Field Policy ──────────────────────────────────────────────────────────

export interface FieldPolicyItem {
  entityName: string
  action: "read" | "write"
  allowedFields: string[]
  tableParts?: Record<string, string[]>
}

// ── CEL Policy Rule ───────────────────────────────────────────────────────

export interface PolicyRuleResponse {
  id: string
  profileId: string
  name: string
  description?: string
  entityName: string
  actions: string[]
  expression: string
  effect: "deny" | "allow"
  priority: number
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface CreatePolicyRuleRequest {
  name: string
  description?: string
  entityName: string
  actions: string[]
  expression: string
  effect: "deny" | "allow"
  priority?: number
  enabled?: boolean
}

export interface UpdatePolicyRuleRequest {
  name?: string
  description?: string
  entityName?: string
  actions?: string[]
  expression?: string
  effect?: "deny" | "allow"
  priority?: number
  enabled?: boolean
}

// ── Security Profile ──────────────────────────────────────────────────────

export interface SecurityProfileResponse {
  id: string
  code: string
  name: string
  description?: string
  isSystem: boolean
  createdAt: string
  updatedAt: string
  dimensions?: Record<string, string[]>
  fieldPolicies?: FieldPolicyItem[]
  policyRules?: PolicyRuleResponse[]
  userCount: number
}

export interface CreateSecurityProfileRequest {
  code: string
  name: string
  description?: string
  dimensions?: Record<string, string[]>
  fieldPolicies?: FieldPolicyItem[]
}

export interface UpdateSecurityProfileRequest {
  code?: string
  name?: string
  description?: string
  dimensions?: Record<string, string[]>
  fieldPolicies?: FieldPolicyItem[]
}

export interface AssignProfileUserRequest {
  userId: string
}

// ── CEL Validation ────────────────────────────────────────────────────────

export interface ValidateExpressionResponse {
  valid: boolean
  error?: string
}

// ── Users (from auth) ─────────────────────────────────────────────────────

export interface UserResponse {
  id: string
  email: string
  firstName?: string
  lastName?: string
  fullName: string
  isActive: boolean
  isAdmin: boolean
  emailVerified: boolean
  roles?: RoleResponse[]
  securityProfile?: SecurityProfileBrief
  createdAt: string
}

export interface SecurityProfileBrief {
  id: string
  code: string
  name: string
}

export interface UpdateUserRequest {
  firstName?: string
  lastName?: string
  isActive?: boolean
  isAdmin?: boolean
}

export interface CreateUserAdminRequest {
  email: string
  password: string
  firstName?: string
  lastName?: string
  roleCodes?: string[]
}

export interface ProfileUserItem {
  id: string
  email: string
  fullName: string
}

export interface RoleResponse {
  id: string
  code: string
  name: string
  description?: string
  isSystem: boolean
}

export interface PermissionResponse {
  id: string
  code: string
  name: string
  description?: string
  resource: string
  action: string
}

// ── CEL Sandbox ──────────────────────────────────────────────────────────

export interface TestExpressionResponse {
  result: boolean
  error?: string
  elapsed: string
}

// ── Audit Log ────────────────────────────────────────────────────────────

export interface AuditEntryResponse {
  id: string
  action: string
  userId?: string
  userEmail?: string
  changes?: Record<string, { old?: unknown; new?: unknown } | unknown>
  createdAt: string
}

// ── Effective Access ─────────────────────────────────────────────────────

export interface EffectiveAccessResponse {
  user: UserResponse
  permissions: string[]
  rlsDimensions?: Record<string, { id: string; name?: string }[]>
  flsPolicies?: { entityName: string; action: string; hiddenFields?: string[] }[]
  celRules?: { name: string; entityName: string; effect: string; expression: string; priority: number }[]
}
