export interface AutomationRule {
    id: string;
    name: string;
    organizationId?: string | null;
    eventType: string;
    conditionCel?: string | null;
    actionType: string;
    actionTemplate: string;
    serviceAccountId: string;
    isActive: boolean;
    createdAt: string;
    updatedAt: string;
}

export interface CreateAutomationRuleRequest {
    name: string;
    organizationId?: string | null;
    eventType: string;
    conditionCel?: string | null;
    actionType: string;
    actionTemplate: string;
    serviceAccountId: string;
    isActive: boolean;
}

export interface UpdateAutomationRuleRequest extends CreateAutomationRuleRequest {}

export interface TestRuleRequest {
    conditionCel?: string | null;
    actionTemplate: string;
    payload: Record<string, any>;
}

export interface TestRuleResponse {
    conditionMatched: boolean;
    conditionError?: string;
    renderedPayload?: string;
    renderError?: string;
}
