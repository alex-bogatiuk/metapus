package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/automation"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationRuleHandler handles API endpoints for automation rules.
type AutomationRuleHandler struct {
	*BaseHandler
	repo automations.Repository
}

// NewAutomationRuleHandler creates a new handler.
func NewAutomationRuleHandler(base *BaseHandler, repo automations.Repository) *AutomationRuleHandler {
	return &AutomationRuleHandler{
		BaseHandler: base,
		repo:        repo,
	}
}

// List returns all automation rules.
func (h *AutomationRuleHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	var eventType *string
	if et := c.Query("eventType"); et != "" {
		eventType = &et
	}

	rules, err := h.repo.List(ctx, eventType)
	if err != nil {
		h.Error(c, err)
		return
	}

	if rules == nil {
		rules = []automations.AutomationRule{}
	}

	h.OK(c, rules)
}

// Get returns a single automation rule.
func (h *AutomationRuleHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	ruleID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	rule, err := h.repo.GetByID(ctx, ruleID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, rule)
}

// Create handles the creation of a new automation rule.
func (h *AutomationRuleHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req automations.CreateRuleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	rule, err := h.repo.Create(ctx, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.Created(c, rule.ID.String())
}

// Update modifies an existing automation rule.
func (h *AutomationRuleHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()

	ruleID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	var req automations.UpdateRuleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	if err := req.Validate(ctx); err != nil {
		h.Error(c, err)
		return
	}

	rule, err := h.repo.Update(ctx, ruleID, req)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, rule)
}

// Delete removes an automation rule.
func (h *AutomationRuleHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	ruleID, err := id.Parse(c.Param("id"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid id parameter").WithDetail("id", c.Param("id")))
		return
	}

	if err := h.repo.Delete(ctx, ruleID); err != nil {
		h.Error(c, err)
		return
	}

	h.NoContent(c)
}

// Test evaluates a rule's condition and renders its template using provided payload.
func (h *AutomationRuleHandler) Test(c *gin.Context) {
	// 1. Bind JSON request
	var req automations.TestRuleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// 2. Create minimal engine for testing
	engine, err := automation.NewEngine(nil, nil, nil, nil, nil)
	if err != nil {
		h.Error(c, fmt.Errorf("failed to init test engine: %w", err))
		return
	}

	resp := automations.TestRuleResponse{
		ConditionMatched: true, // Default to true if no CEL condition
	}

	// Unpack variables if available
	var vars map[string]any = make(map[string]any)
	if req.Payload != nil {
		doc := req.Payload["doc"]
		action, _ := req.Payload["action"].(string)
		entityType, _ := req.Payload["entityType"].(string)
		vars = map[string]any{
			"doc":        doc,
			"action":     action,
			"entityType": entityType,
		}
	}

	// 3. Evaluate CEL
	if req.ConditionCEL != nil && *req.ConditionCEL != "" {
		matched, evalErr := engine.EvaluateCEL(*req.ConditionCEL, vars)
		if evalErr != nil {
			resp.ConditionMatched = false
			resp.ConditionError = evalErr.Error()
		} else {
			resp.ConditionMatched = matched
		}
	}

	// 4. Render Template
	if req.ActionTemplate != "" {
		rendered, renderErr := engine.RenderTemplate(req.ActionTemplate, req.Payload)
		if renderErr != nil {
			resp.RenderError = renderErr.Error()
		} else {
			resp.RenderedPayload = rendered
		}
	}

	h.OK(c, resp)
}

// RegisterRoutes registers the handlers to the Gin router group.
func (h *AutomationRuleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rules := rg.Group("/automation-rules")
	{
		rules.GET("", h.List)
		rules.POST("", h.Create)
		rules.GET("/:id", h.Get)
		rules.PUT("/:id", h.Update)
		rules.DELETE("/:id", h.Delete)
		rules.POST("/test", h.Test)
	}
}
