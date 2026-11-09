import * as wireSchemas from "@/lib/generated/http-contract-schemas"
import {
    tokenResponseSchema,
    authUserSchema,
    loginResponseSchema,
    authLoginResponseSchema,
    mfaEnrollConfirmResponseSchema,
    adminUserListSchema,
    roleListSchema,
    permissionListSchema,
    effectiveAccessSchema,
} from "@/lib/schemas/auth"
import { apiFetch, checkedJSON } from "./core"

const messageSchema = wireSchemas.AuthMessageResponseSchema

export const authApi = {
    login: async (data: import("@/types/auth").LoginRequest) =>
        checkedJSON("/auth/login", authLoginResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.LoginRequestSchema.parse(data)),
        }),
    verifyMFA: async (data: import("@/types/auth").MFAVerifyRequest) =>
        checkedJSON("/auth/mfa/verify", loginResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.MFAVerifyRequestSchema.parse(data)),
        }),
    startMFAEnrollment: async (data: import("@/types/auth").MFAEnrollStartRequest) =>
        checkedJSON("/auth/mfa/enroll/start", wireSchemas.MFAEnrollStartResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.MFAEnrollStartRequestSchema.parse(data)),
        }),
    startSessionMFAEnrollment: () =>
        checkedJSON("/auth/mfa/enroll/start-session", wireSchemas.MFAEnrollStartResponseSchema, {
            method: "POST",
        }),
    confirmMFAEnrollment: async (data: import("@/types/auth").MFAEnrollConfirmRequest) =>
        checkedJSON("/auth/mfa/enroll/confirm", mfaEnrollConfirmResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.MFAEnrollConfirmRequestSchema.parse(data)),
        }),
    useMFABackupCode: async (data: import("@/types/auth").MFABackupCodeRequest) =>
        checkedJSON("/auth/mfa/backup-code", loginResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.MFABackupCodeRequestSchema.parse(data)),
        }),
    regenerateMFABackupCodes: () =>
        checkedJSON("/auth/mfa/backup-codes/regenerate", wireSchemas.MFABackupCodesResponseSchema, {
            method: "POST",
        }),
    resetMFA: async (data: import("@/types/auth").MFAResetRequest) =>
        checkedJSON("/auth/mfa/reset", messageSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.MFAResetRequestSchema.parse(data)),
        }),
    stepUp: async (data: import("@/types/auth").StepUpRequest) =>
        checkedJSON("/auth/step-up", wireSchemas.StepUpResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.StepUpRequestSchema.parse(data)),
        }),
    register: async (data: import("@/types/auth").RegisterRequest) =>
        checkedJSON("/auth/register", authUserSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.RegisterRequestSchema.parse(data)),
        }),
    refresh: async (data: import("@/types/auth").RefreshTokenRequest) =>
        checkedJSON("/auth/refresh", tokenResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.RefreshTokenRequestSchema.parse(data)),
        }),
    logout: () =>
        apiFetch<void>("/auth/logout", { method: "POST" }),
    me: () =>
        checkedJSON("/auth/me", authUserSchema),
    permissions: () =>
        checkedJSON("/auth/me/permissions", wireSchemas.PermissionCodesResponseSchema),
    webSocketTicket: () =>
        checkedJSON("/auth/ws-ticket", wireSchemas.WebSocketTicketResponseSchema, { method: "POST" }),
    assignRole: async (data: { userId: string; roleCode: string }) =>
        checkedJSON("/auth/assign-role", messageSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.AssignRoleRequestSchema.parse(data)),
        }),
    revokeRole: async (data: { userId: string; roleCode: string }) =>
        checkedJSON("/auth/revoke-role", messageSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.AssignRoleRequestSchema.parse(data)),
        }),
}

export const usersApi = {
    list: (search?: string) =>
        checkedJSON(`/auth/users${search ? `?search=${encodeURIComponent(search)}` : ""}`, adminUserListSchema),
    get: (id: string) =>
        checkedJSON(`/auth/users/${id}`, authUserSchema),
    create: async (data: import("@/types/security").CreateUserAdminRequest) =>
        checkedJSON("/auth/users", authUserSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.CreateUserAdminRequestSchema.parse(data)),
        }),
    update: async (id: string, data: import("@/types/security").UpdateUserRequest) =>
        checkedJSON(`/auth/users/${id}`, authUserSchema, {
            method: "PUT",
            body: JSON.stringify(wireSchemas.UpdateUserRequestSchema.parse(data)),
        }),
    effectiveAccess: (id: string) =>
        checkedJSON(`/auth/users/${id}/effective-access`, effectiveAccessSchema),
    impersonate: (id: string) =>
        checkedJSON(`/auth/users/${id}/impersonate`, loginResponseSchema, {
            method: "POST",
        }),
}

export const rolesApi = {
    list: () =>
        checkedJSON("/auth/roles", roleListSchema),
    get: (id: string) =>
        checkedJSON(`/auth/roles/${id}`, wireSchemas.RoleResponseSchema),
    create: async (data: import("@/types/security").CreateRoleRequest) =>
        checkedJSON("/auth/roles", wireSchemas.RoleResponseSchema, {
            method: "POST",
            body: JSON.stringify(wireSchemas.CreateRoleRequestSchema.parse(data)),
        }),
    update: async (id: string, data: import("@/types/security").UpdateRoleRequest) =>
        checkedJSON(`/auth/roles/${id}`, wireSchemas.RoleResponseSchema, {
            method: "PUT",
            body: JSON.stringify(wireSchemas.UpdateRoleRequestSchema.parse(data)),
        }),
    delete: (id: string) =>
        checkedJSON(`/auth/roles/${id}`, wireSchemas.DeleteRoleResponseSchema, {
            method: "DELETE",
        }),
    getPermissions: (roleId: string) =>
        checkedJSON(`/auth/roles/${roleId}/permissions`, permissionListSchema),
    setPermissions: async (roleId: string, permissionIds: string[]) =>
        checkedJSON(`/auth/roles/${roleId}/permissions`, messageSchema, {
            method: "PUT",
            body: JSON.stringify(wireSchemas.SetRolePermissionsRequestSchema.parse({ permissionIds })),
        }),
}

export const permissionsApi = {
    list: () =>
        checkedJSON("/auth/permissions", permissionListSchema),
}
