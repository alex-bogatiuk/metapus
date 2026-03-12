package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/http/v1/dto"
)

// PolicyRuleHandler handles CRUD operations for CEL policy rules.
type PolicyRuleHandler struct {
	BaseHandler
	repo         security_profile.PolicyRuleRepository
	policyEngine *security.PolicyEngine
}

// NewPolicyRuleHandler creates a new PolicyRuleHandler.
func NewPolicyRuleHandler(repo security_profile.PolicyRuleRepository, engine *security.PolicyEngine) *PolicyRuleHandler {
	return &PolicyRuleHandler{
		repo:         repo,
		policyEngine: engine,
	}
}

// Create creates a new policy rule for a profile.
// POST /api/v1/security/profiles/:profileId/rules
func (h *PolicyRuleHandler) Create(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	var req dto.CreatePolicyRuleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	rule := req.ToDomain(profileID)

	// Validate domain invariants
	if err := rule.Validate(c.Request.Context()); err != nil {
		h.Error(c, err)
		return
	}

	// Validate CEL expression compiles and returns bool
	if err := h.policyEngine.Compile(rule.Expression); err != nil {
		h.Error(c, err)
		return
	}

	if err := h.repo.Create(c.Request.Context(), rule); err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.FromPolicyRule(rule))
}

// List lists all rules for a profile.
// GET /api/v1/security/profiles/:profileId/rules
func (h *PolicyRuleHandler) List(c *gin.Context) {
	profileID, err := id.Parse(c.Param("profileId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid profileId"))
		return
	}

	rules, err := h.repo.ListByProfileID(c.Request.Context(), profileID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, dto.FromPolicyRules(rules))
}

// Get retrieves a single rule.
// GET /api/v1/security/profiles/:profileId/rules/:ruleId
func (h *PolicyRuleHandler) Get(c *gin.Context) {
	ruleID, err := id.Parse(c.Param("ruleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid ruleId"))
		return
	}

	rule, err := h.repo.GetByID(c.Request.Context(), ruleID)
	if err != nil {
		h.Error(c, err)
		return
	}

	h.OK(c, dto.FromPolicyRule(rule))
}

// Update modifies an existing rule.
// PUT /api/v1/security/profiles/:profileId/rules/:ruleId
func (h *PolicyRuleHandler) Update(c *gin.Context) {
	ruleID, err := id.Parse(c.Param("ruleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid ruleId"))
		return
	}

	var req dto.UpdatePolicyRuleRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// Fetch existing rule
	rule, err := h.repo.GetByID(c.Request.Context(), ruleID)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Apply partial updates
	req.ApplyTo(rule)

	// Validate domain invariants
	if err := rule.Validate(c.Request.Context()); err != nil {
		h.Error(c, err)
		return
	}

	// Validate CEL expression
	if err := h.policyEngine.Compile(rule.Expression); err != nil {
		h.Error(c, err)
		return
	}

	if err := h.repo.Update(c.Request.Context(), rule); err != nil {
		h.Error(c, err)
		return
	}

	// Invalidate cached program for this rule
	h.policyEngine.InvalidateCache(rule.ID.String())

	h.OK(c, dto.FromPolicyRule(rule))
}

// Delete removes a rule.
// DELETE /api/v1/security/profiles/:profileId/rules/:ruleId
func (h *PolicyRuleHandler) Delete(c *gin.Context) {
	ruleID, err := id.Parse(c.Param("ruleId"))
	if err != nil {
		h.Error(c, apperror.NewValidation("invalid ruleId"))
		return
	}

	if err := h.repo.Delete(c.Request.Context(), ruleID); err != nil {
		h.Error(c, err)
		return
	}

	// Invalidate cached program
	h.policyEngine.InvalidateCache(ruleID.String())

	h.Success(c, "rule deleted")
}

// ValidateExpression validates a CEL expression without saving.
// POST /api/v1/security/rules/validate
func (h *PolicyRuleHandler) ValidateExpression(c *gin.Context) {
	var req dto.ValidateExpressionRequest
	if !h.BindJSON(c, &req) {
		return
	}

	err := h.policyEngine.Compile(req.Expression)
	if err != nil {
		c.JSON(http.StatusOK, dto.ValidateExpressionResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dto.ValidateExpressionResponse{Valid: true})
}

// TestExpression compiles and evaluates a CEL expression against sample data.
// POST /api/v1/security/rules/test
func (h *PolicyRuleHandler) TestExpression(c *gin.Context) {
	var req dto.TestExpressionRequest
	if !h.BindJSON(c, &req) {
		return
	}

	// First validate the expression compiles
	if err := h.policyEngine.Compile(req.Expression); err != nil {
		c.JSON(http.StatusOK, dto.TestExpressionResponse{
			Result: false,
			Error:  fmt.Sprintf("compile error: %v", err),
		})
		return
	}

	// Build activation map matching CEL environment variables
	action := req.Action
	if action == "" {
		action = "read"
	}
	doc := req.Doc
	if doc == nil {
		doc = map[string]any{}
	}

	activation := map[string]any{
		"doc":    doc,
		"user":   map[string]any{"id": "", "email": "", "roles": []any{}, "orgIds": []any{}, "isAdmin": false},
		"action": action,
		"now":    time.Now().UTC(),
	}

	// Evaluate
	start := time.Now()
	result, evalErr := h.policyEngine.EvalExpression(req.Expression, activation)
	elapsed := time.Since(start)

	if evalErr != nil {
		c.JSON(http.StatusOK, dto.TestExpressionResponse{
			Result:  false,
			Error:   fmt.Sprintf("eval error: %v", evalErr),
			Elapsed: elapsed.String(),
		})
		return
	}

	c.JSON(http.StatusOK, dto.TestExpressionResponse{
		Result:  result,
		Elapsed: elapsed.String(),
	})
}
