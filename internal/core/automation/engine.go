package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/google/cel-go/cel"

	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
	"metapus/internal/domain/integrations"
	"metapus/pkg/logger"
)

// Engine is the core component that evaluates rules against events and executes actions.
type Engine struct {
	ruleRepo     automations.Repository
	historyRepo  automations.HistoryRepository
	credManager  integrations.CredentialManager
	accountRepo  integrations.Repository
	adapters     map[string]Adapter
	celEnv       *cel.Env
	celCache     sync.Map // string(expression) → cel.Program
}

// Adapter defines the interface for external integrations (Telegram, Webhook, etc.).
type Adapter interface {
	// Execute performs the action using the rendered payload and the service account's secret config.
	Execute(ctx context.Context, config map[string]interface{}, credentials []byte, payload string) error
}

// NewEngine creates a new automation engine.
func NewEngine(
	ruleRepo automations.Repository,
	historyRepo automations.HistoryRepository,
	credManager integrations.CredentialManager,
	accountRepo integrations.Repository,
	adapters map[string]Adapter,
) (*Engine, error) {
	// Initialize CEL environment with standard document variables.
	env, err := cel.NewEnv(
		cel.Variable("doc", cel.DynType),
		cel.Variable("action", cel.StringType),
		cel.Variable("entityType", cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL env: %w", err)
	}

	if adapters == nil {
		adapters = make(map[string]Adapter)
	}

	return &Engine{
		ruleRepo:    ruleRepo,
		historyRepo: historyRepo,
		credManager: credManager,
		accountRepo: accountRepo,
		adapters:    adapters,
		celEnv:      env,
	}, nil
}

// HandleEvent processes a single event payload. It acts as the OutboxHandler for automation.
func (e *Engine) HandleEvent(ctx context.Context, eventType string, payload map[string]any) error {
	// 1. Fetch active rules for this event type
	rules, err := e.ruleRepo.ListActiveByEventType(ctx, eventType)
	if err != nil {
		return fmt.Errorf("fetch rules: %w", err)
	}

	if len(rules) == 0 {
		return nil // Nothing to do
	}

	// Unpack common variables for CEL
	doc := payload["doc"]
	action, _ := payload["action"].(string)
	entityType, _ := payload["entityType"].(string)

	var aggregateID id.ID
	if eIDStr, ok := payload["entityId"].(string); ok {
		aggregateID, _ = id.Parse(eIDStr)
	}

	vars := map[string]any{
		"doc":        doc,
		"action":     action,
		"entityType": entityType,
	}

	// Process each rule
	for _, rule := range rules {
		// 2. Evaluate CEL condition if present
		if rule.ConditionCEL != nil && strings.TrimSpace(*rule.ConditionCEL) != "" {
			matched, evalErr := e.EvaluateCEL(*rule.ConditionCEL, vars)
			if evalErr != nil {
				logger.Error(ctx, "failed to evaluate rule condition", "ruleId", rule.ID, "error", evalErr)
				continue
			}
			if !matched {
				continue // Skip if condition is false
			}
		}

		// 3. Render the template
		renderedPayload, err := e.RenderTemplate(rule.ActionTemplate, payload)
		if err != nil {
			logger.Error(ctx, "failed to render action template", "ruleId", rule.ID, "error", err)
			continue
		}

		// 4. Retrieve Service Account & Credentials (Optional)
		var config map[string]interface{}
		var creds []byte

		if rule.ServiceAccountID != nil {
			account, err := e.accountRepo.GetByID(ctx, *rule.ServiceAccountID)
			if err != nil {
				logger.Error(ctx, "failed to load service account", "ruleId", rule.ID, "accountId", *rule.ServiceAccountID, "error", err)
				continue
			}

			if string(account.AccountType) != rule.ActionType {
				logger.Error(ctx, "rule action type mismatch with account type", "ruleId", rule.ID, "ruleAction", rule.ActionType, "accountType", account.AccountType)
				continue
			}

			creds, err = e.credManager.ReadCredentials(ctx, account.ID)
			if err != nil {
				logger.Error(ctx, "failed to read credentials for account", "accountId", account.ID, "error", err)
				continue
			}
			config = account.Config
		}

		// 5. Execute via Adapter
		adapter, ok := e.adapters[rule.ActionType]
		if !ok {
			logger.Error(ctx, "unsupported action type", "actionType", rule.ActionType, "ruleId", rule.ID)
			continue
		}

		adapterErr := adapter.Execute(ctx, config, creds, renderedPayload)
		
		// Record execution history (synchronous — within the same tx as the outbox processing)
		errMsg := ""
		if adapterErr != nil {
			errMsg = adapterErr.Error()
		}

		history := &automations.ExecutionHistory{
			RuleID:         rule.ID,
			EventType:      eventType,
			AggregateID:    aggregateID,
			Success:        adapterErr == nil,
			RequestPayload: &renderedPayload,
		}
		if errMsg != "" {
			history.ErrorMessage = &errMsg
		}

		if err := e.historyRepo.Create(ctx, history); err != nil {
			logger.Error(ctx, "failed to save execution history", "ruleId", rule.ID, "error", err)
		}

		if adapterErr != nil {
			logger.Error(ctx, "adapter execution failed", "ruleId", rule.ID, "actionType", rule.ActionType, "error", adapterErr)
			// Returning error will cause outbox message to be marked failed/retried. 
			// The history will ROLLBACK. 
			// If we want history to persist even on failure, we need a separate db tx, OR we don't return an error and just log it (meaning we give up on retries).
			// The rule says "give up on retries".
			// For Phase 3: We return nil even on adapter failure, but we recorded it in history as failure!
			// If we want retries, it's a different story. Let's return nil to mark the outbox message as successfully processed (no retries), and rely on history for observability.
		} else {
			logger.Info(ctx, "automation rule executed successfully", "ruleId", rule.ID, "eventType", eventType)
		}
	}

	return nil
}

func (e *Engine) EvaluateCEL(expr string, vars map[string]any) (bool, error) {
	// Try cached program first
	prg, err := e.getOrCompileCEL(expr)
	if err != nil {
		return false, err
	}

	// Evaluate
	out, _, err := prg.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("eval error: %w", err)
	}

	// Check result
	if out.Type() != cel.BoolType {
		return false, fmt.Errorf("expression must return bool, got %v", out.Type().TypeName())
	}

	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("eval returned non-bool value")
	}

	return val, nil
}

// getOrCompileCEL returns a cached cel.Program or compiles and caches a new one.
func (e *Engine) getOrCompileCEL(expr string) (cel.Program, error) {
	if cached, ok := e.celCache.Load(expr); ok {
		return cached.(cel.Program), nil
	}

	ast, issues := e.celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile error: %w", issues.Err())
	}

	prg, err := e.celEnv.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program error: %w", err)
	}

	e.celCache.Store(expr, prg)
	return prg, nil
}

// InvalidateCELCache removes a specific expression from the cache.
// Should be called when a rule's ConditionCEL is updated.
func (e *Engine) InvalidateCELCache(expr string) {
	e.celCache.Delete(expr)
}

func (e *Engine) RenderTemplate(tmplText string, data map[string]any) (string, error) {
	// Create template with missingkey=zero to avoid silent empty strings for nested paths
	tmpl, err := template.New("action").Option("missingkey=zero").Funcs(template.FuncMap{
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"jsonIndent": func(v any) (string, error) {
			b, err := json.MarshalIndent(v, "", "  ")
			return string(b), err
		},
	}).Parse(tmplText)

	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
