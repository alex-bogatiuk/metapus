import { z } from "zod"
import * as Wire from "@/lib/generated/http-contract-schemas"
import type * as Contract from "@/types/generated/http-contracts"
import { constrainContract } from "@/lib/schemas/contract-constraints"

const effect = z.enum(["deny", "allow"])
const action = z.enum(["read", "write"])
export const policyRuleSchema = Wire.PolicyRuleResponseSchema.and(z.object({ effect }))
export const fieldPolicySchema = Wire.FieldPolicyResponseSchema.and(z.object({ action }))
const fieldPolicyInput = constrainContract(Wire.FieldPolicyInputSchema, z.object({ action }))
export type SecurityProfile = Omit<Contract.SecurityProfileResponse, "fieldPolicies" | "policyRules"> & {
  fieldPolicies?: z.infer<typeof fieldPolicySchema>[]
  policyRules?: z.infer<typeof policyRuleSchema>[]
}
export const securityProfileSchema: z.ZodType<SecurityProfile> = Wire.SecurityProfileResponseSchema.and(z.object({
  fieldPolicies: z.array(fieldPolicySchema).optional(), policyRules: z.array(policyRuleSchema).optional(),
}))
export const securityProfileListSchema: z.ZodType<{ items: SecurityProfile[] }> = Wire.SecurityProfileListResponseSchema.and(z.object({ items: z.array(securityProfileSchema) }))
export const createSecurityProfileSchema = constrainContract(Wire.CreateSecurityProfileRequestSchema, z.object({ fieldPolicies: z.array(fieldPolicyInput).nullish() }))
export const updateSecurityProfileSchema = constrainContract(Wire.UpdateSecurityProfileRequestSchema, z.object({ fieldPolicies: z.array(fieldPolicyInput).nullish() }))
export const createPolicyRuleSchema = constrainContract(Wire.CreatePolicyRuleRequestSchema, z.object({ effect }))
export const updatePolicyRuleSchema = constrainContract(Wire.UpdatePolicyRuleRequestSchema, z.object({ effect: effect.nullish() }))
