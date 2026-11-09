import { z } from "zod"
import * as wireSchemas from "@/lib/generated/http-contract-schemas"
import type { JsonObject } from "@/lib/json-value"
import {
    securityProfileSchema,
    securityProfileListSchema,
    policyRuleSchema,
    createSecurityProfileSchema,
    updateSecurityProfileSchema,
    createPolicyRuleSchema,
    updatePolicyRuleSchema,
} from "@/lib/schemas/security"
import { apiFetch, assertUuidPathParam, checkedJSON } from "./core"

export const securityApi = {
    profiles: {
        list: () =>
            checkedJSON("/security/profiles", securityProfileListSchema),
        get: (id: string) =>
            checkedJSON(`/security/profiles/${id}`, securityProfileSchema),
        create: async (data: import("@/types/security").CreateSecurityProfileRequest) =>
            checkedJSON("/security/profiles", securityProfileSchema, {
                method: "POST",
                body: JSON.stringify(createSecurityProfileSchema.parse(data)),
            }),
        update: async (id: string, data: import("@/types/security").UpdateSecurityProfileRequest) =>
            checkedJSON(`/security/profiles/${id}`, securityProfileSchema, {
                method: "PUT",
                body: JSON.stringify(updateSecurityProfileSchema.parse(data)),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/security/profiles/${id}`, { method: "DELETE" }),
        listUsers: (profileId: string) =>
            checkedJSON(`/security/profiles/${profileId}/users`, wireSchemas.ProfileUserListResponseSchema),
        assignUser: (profileId: string, userId: string) => {
            const safeProfileId = assertUuidPathParam(profileId, "profileId")
            const safeUserId = assertUuidPathParam(userId, "userId")
            return apiFetch<void>(`/security/profiles/${safeProfileId}/users`, {
                method: "POST",
                body: JSON.stringify({ userId: safeUserId }),
            })
        },
        removeUser: (profileId: string, userId: string) => {
            const safeProfileId = assertUuidPathParam(profileId, "profileId")
            const safeUserId = assertUuidPathParam(userId, "userId")
            return apiFetch<void>(`/security/profiles/${safeProfileId}/users/${safeUserId}`, {
                method: "DELETE",
            })
        },
        auditHistory: (profileId: string, limit?: number) =>
            checkedJSON(`/security/profiles/${profileId}/audit${limit ? `?limit=${limit}` : ""}`, wireSchemas.SecurityAuditListResponseSchema),
    },
    rules: {
        list: (profileId: string) =>
            checkedJSON(`/security/profiles/${profileId}/rules`, z.array(policyRuleSchema)),
        create: async (profileId: string, data: import("@/types/security").CreatePolicyRuleRequest) =>
            checkedJSON(`/security/profiles/${profileId}/rules`, policyRuleSchema, {
                method: "POST",
                body: JSON.stringify(createPolicyRuleSchema.parse(data)),
            }),
        update: async (profileId: string, ruleId: string, data: import("@/types/security").UpdatePolicyRuleRequest) =>
            checkedJSON(`/security/profiles/${profileId}/rules/${ruleId}`, policyRuleSchema, {
                method: "PUT",
                body: JSON.stringify(updatePolicyRuleSchema.parse(data)),
            }),
        delete: (profileId: string, ruleId: string) =>
            apiFetch<void>(`/security/profiles/${profileId}/rules/${ruleId}`, {
                method: "DELETE",
            }),
        validate: async (expression: string) =>
            checkedJSON("/security/rules/validate", wireSchemas.ValidateExpressionResponseSchema, {
                method: "POST",
                body: JSON.stringify(wireSchemas.ValidateExpressionRequestSchema.parse({ expression })),
            }),
        test: async (expression: string, doc?: JsonObject, action?: string) =>
            checkedJSON("/security/rules/test", wireSchemas.TestExpressionResponseSchema, {
                method: "POST",
                body: JSON.stringify(wireSchemas.TestExpressionRequestSchema.parse({ expression, doc, action })),
            }),
    },
}
