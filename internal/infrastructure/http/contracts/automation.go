package contracts

import (
	"metapus/internal/domain/automations"
	"metapus/internal/infrastructure/http/v1/dto"
)

func automationContracts() []Contract {
	return []Contract{
		Request[automations.CreateAccountRequest]("CreateAutomationAccountRequest"),
		Request[automations.UpdateAccountRequest]("UpdateAutomationAccountRequest"),
		Request[dto.UpdateAutomationCredentialsRequest]("UpdateAutomationCredentialsRequest"),
		Response[automations.Account]("AutomationAccountResponse"),
		Response[[]automations.Account]("AutomationAccountListResponse"),
		Request[automations.CreateChannelRequest]("CreateAutomationChannelRequest"),
		Request[automations.UpdateChannelRequest]("UpdateAutomationChannelRequest"),
		Response[automations.Channel]("AutomationChannelResponse"),
		Response[[]automations.Channel]("AutomationChannelListResponse"),
		Request[automations.CreateRuleRequest]("CreateAutomationRuleRequest"),
		Request[automations.UpdateRuleRequest]("UpdateAutomationRuleRequest"),
		Request[automations.TestRuleRequest]("TestAutomationRuleRequest"),
		Response[automations.TestRuleResponse]("TestAutomationRuleResponse"),
		Response[automations.Rule]("AutomationRuleResponse"),
		Response[[]automations.Rule]("AutomationRuleListResponse"),
		Response[dto.AutomationToggleResponse]("AutomationToggleResponse"),
		Response[automations.HistoryEntry]("AutomationHistoryResponse"),
		Response[dto.AutomationHistoryListResponse]("AutomationHistoryListResponse"),
		Response[automations.HistoryStats]("AutomationHistoryStatsResponse"),
		Response[dto.AutomationReplayResponse]("AutomationReplayResponse"),
		Response[dto.AutomationBatchReplayResponse]("AutomationBatchReplayResponse"),
		Response[dto.AutomationEnumItem]("AutomationEnumItem"),
		Response[dto.AutomationMetadataResponse]("AutomationMetadataResponse"),
		Response[dto.AutomationDatasetSummary]("AutomationDatasetSummary"),
		Response[[]dto.AutomationDatasetSummary]("AutomationDatasetsResponse"),
	}
}

func automationRoutes() []RouteContract {
	routes := []RouteContract{}
	add := func(method, path, request, response string) {
		routes = append(routes, RouteContract{Scope: "api", Method: method, Path: "/system/" + path, Request: request, Response: response})
	}
	for _, entity := range []struct{ path, name string }{
		{"automation-accounts", "AutomationAccount"},
		{"automation-channels", "AutomationChannel"},
		{"automation-rules", "AutomationRule"},
	} {
		add("GET", entity.path, "", entity.name+"ListResponse")
		add("GET", entity.path+"/:id", "", entity.name+"Response")
		add("POST", entity.path, "Create"+entity.name+"Request", "IDResponse")
		add("PUT", entity.path+"/:id", "Update"+entity.name+"Request", entity.name+"Response")
		add("DELETE", entity.path+"/:id", "", "")
	}
	add("PUT", "automation-accounts/:id/credentials", "UpdateAutomationCredentialsRequest", "")
	add("POST", "automation-accounts/:id/test", "", "SuccessResponse")
	add("POST", "automation-rules/:id/toggle", "", "AutomationToggleResponse")
	add("POST", "automation-rules/test", "TestAutomationRuleRequest", "TestAutomationRuleResponse")
	add("GET", "automation-history", "", "AutomationHistoryListResponse")
	add("GET", "automation-history/stats", "", "AutomationHistoryStatsResponse")
	add("GET", "automation-history/:id", "", "AutomationHistoryResponse")
	add("POST", "automation-history/:id/replay", "", "AutomationReplayResponse")
	add("POST", "automation-history/batch-replay", "", "AutomationBatchReplayResponse")
	add("GET", "automation-meta", "", "AutomationMetadataResponse")
	add("GET", "automation-meta/datasets", "", "AutomationDatasetsResponse")
	return routes
}
