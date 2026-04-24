package automation

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/cel-go/cel"

	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
	"metapus/pkg/logger"
)

// maxDeliveryConcurrency limits parallel adapter calls in Phase 2 (Deliver).
// Prevents overwhelming external APIs while still improving throughput over sequential.
const maxDeliveryConcurrency = 10

// DeliveryTask is the output of Phase 1 (Evaluate) and input of Phase 2 (Deliver).
// One Rule may produce multiple DeliveryTasks (one per subscriber).
type DeliveryTask struct {
	Rule            *automations.Rule
	Subscriber      automations.Subscriber
	RenderedPayload string
	EventType       string
	AggregateID     *id.ID
	AggregateName   *string
}

// Adapter defines the interface for external delivery (Telegram, Email, Webhook).
// v2: destination-aware — takes channel destination separately from account config.
type Adapter interface {
	// Deliver sends the rendered payload through the channel.
	// destination: channel-specific config (e.g. chat_id, email address, webhook URL)
	// accountConfig: account-level config (e.g. parse_mode defaults)
	// credentials: decrypted account credentials (e.g. bot token, SMTP password)
	// payload: rendered message text
	Deliver(ctx context.Context, destination map[string]any, accountConfig map[string]any, credentials []byte, payload string) error
}

// OutboxPublisher publishes events to the transactional outbox for chain reactions.
type OutboxPublisher interface {
	Publish(ctx context.Context, eventType string, payload []byte) error
}

// celCacheMaxSize is the maximum number of compiled CEL programs to cache.
const celCacheMaxSize = 512

// celCacheEntry holds a cached CEL program with LRU metadata.
type celCacheEntry struct {
	expr string
	prg  cel.Program
}

// Engine is the core automation component with two-phase processing:
//
//	Phase 1 (Evaluate): CPU-bound — CEL condition check, template rendering.
//	Phase 2 (Deliver): I/O-bound — adapter execution, history recording.
type Engine struct {
	ruleRepo    automations.RuleRepository
	historyRepo automations.HistoryRepository
	credManager automations.CredentialManager
	accountRepo automations.AccountRepository
	channelRepo automations.ChannelRepository
	adapters    map[string]Adapter
	publisher   OutboxPublisher // For chain reactions
	celEnv      *cel.Env

	// Bounded LRU cache for compiled CEL programs.
	celMu       sync.Mutex
	celLookup   map[string]*list.Element // expr → list element
	celOrder    *list.List               // front = most recently used

	// Bounded LRU cache for parsed Templates.
	tmplMu     sync.Mutex
	tmplLookup map[string]*list.Element // text → list element
	tmplOrder  *list.List
}

// tmplCacheMaxSize is the maximum number of parsed templates to cache.
const tmplCacheMaxSize = 512

// tmplCacheEntry holds a cached parsed template with LRU metadata.
type tmplCacheEntry struct {
	text string
	tmpl *template.Template
}

// NewEngine creates a new automation engine.
func NewEngine(
	ruleRepo automations.RuleRepository,
	historyRepo automations.HistoryRepository,
	credManager automations.CredentialManager,
	accountRepo automations.AccountRepository,
	channelRepo automations.ChannelRepository,
	adapters map[string]Adapter,
	publisher OutboxPublisher,
) (*Engine, error) {
	// Initialize CEL environment with standard document variables.
	env, err := cel.NewEnv(
		cel.Variable("doc", cel.DynType),
		cel.Variable("action", cel.StringType),
		cel.Variable("entityType", cel.StringType),
		cel.Variable("currency", cel.DynType),
		cel.Variable("humanAmounts", cel.DynType),
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
		channelRepo: channelRepo,
		adapters:    adapters,
		publisher:   publisher,
		celEnv:      env,
		celLookup:   make(map[string]*list.Element, celCacheMaxSize),
		celOrder:    list.New(),
		tmplLookup:  make(map[string]*list.Element, tmplCacheMaxSize),
		tmplOrder:   list.New(),
	}, nil
}

// HandleEvent is the main entry point. Called by OutboxRelay for each event.
// Orchestrates Phase 1 (Evaluate) → Phase 2 (Deliver) → Stats update.
func (e *Engine) HandleEvent(ctx context.Context, eventType string, payload map[string]any) error {
	// Phase 1: Evaluate
	tasks, err := e.Evaluate(ctx, eventType, payload)
	if err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	// Phase 2: Deliver
	e.Deliver(ctx, tasks)

	return nil
}

// DeliverReplay replays a failed message using its history entry ID.
func (e *Engine) DeliverReplay(ctx context.Context, historyID id.ID) error {
	entry, err := e.historyRepo.GetByID(ctx, historyID)
	if err != nil {
		return fmt.Errorf("failed to fetch history entry: %w", err)
	}

	if entry.RenderedPayload == nil || *entry.RenderedPayload == "" {
		return fmt.Errorf("cannot replay history entry with empty payload")
	}

	rule := &automations.Rule{
		ID:   entry.RuleID,
		Name: entry.RuleName,
	}

	subscriber := automations.Subscriber{}
	if entry.ChannelID != nil {
		subscriber.SubscriberType = automations.SubChannel
		subscriber.ChannelID = entry.ChannelID
	} else {
		return fmt.Errorf("replay is only supported for channel-based deliveries")
	}

	task := DeliveryTask{
		Rule:            rule,
		Subscriber:      subscriber,
		RenderedPayload: *entry.RenderedPayload,
		EventType:       entry.EventType,
		AggregateID:     entry.AggregateID,
		AggregateName:   entry.AggregateName,
	}

	e.Deliver(ctx, []DeliveryTask{task})
	return nil
}

// HandleScheduledRule is called directly by the Scheduler for cron-triggered rules.
// It bypasses the event-type matching (since the cron expression IS the event_type,
// not a matchable string) and directly renders the template + delivers.
func (e *Engine) HandleScheduledRule(ctx context.Context, rule automations.Rule, payload map[string]any) error {
	eventType := fmt.Sprintf("scheduled.%s", rule.ID)

	// Render template
	renderedPayload, renderErr := e.RenderTemplate(rule.ActionTemplate, payload)
	if renderErr != nil {
		logger.Error(ctx, "scheduled rule: template render failed",
			"ruleId", rule.ID, "error", renderErr)
		errMsg := renderErr.Error()
		e.recordHistory(ctx, &rule, nil, eventType, nil, automations.HistoryError, "", &errMsg)
		return fmt.Errorf("render template: %w", renderErr)
	}

	// Build delivery tasks for each subscriber
	var tasks []DeliveryTask
	for _, sub := range rule.Subscribers {
		tasks = append(tasks, DeliveryTask{
			Rule:            &rule,
			Subscriber:      sub,
			RenderedPayload: renderedPayload,
			EventType:       eventType,
		})
	}

	if len(tasks) == 0 {
		logger.Warn(ctx, "scheduled rule: no subscribers", "ruleId", rule.ID)
		return nil
	}

	// Deliver
	e.Deliver(ctx, tasks)

	return nil
}

// Evaluate is Phase 1 (CPU-bound): fetch rules, check cooldowns, evaluate CEL, render templates.
// Returns a list of DeliveryTasks ready for Phase 2.
func (e *Engine) Evaluate(ctx context.Context, eventType string, payload map[string]any) ([]DeliveryTask, error) {
	entityType, _ := payload["entityType"].(string)
	rules, err := e.ruleRepo.ListActiveByEvent(ctx, eventType, entityType)
	if err != nil {
		return nil, fmt.Errorf("fetch rules: %w", err)
	}

	if len(rules) == 0 {
		return nil, nil
	}

	// Prepare CEL variables
	doc := payload["doc"]
	action, _ := payload["action"].(string)

	vars := map[string]any{
		"doc":        doc,
		"action":     action,
		"entityType": entityType,
	}

	if curr, ok := payload["currency"]; ok {
		vars["currency"] = curr
	}
	if ha, ok := payload["humanAmounts"]; ok {
		vars["humanAmounts"] = ha
	}

	var aggregateID *id.ID
	if eIDStr, ok := payload["entityId"].(string); ok {
		parsed, parseErr := id.Parse(eIDStr)
		if parseErr == nil {
			aggregateID = &parsed
		}
	}

	var tasks []DeliveryTask

	for i := range rules {
		rule := &rules[i]

		// Cooldown check: skip if executed too recently
		if rule.CooldownSecs > 0 && rule.LastExecutedAt != nil {
			cooldownEnd := rule.LastExecutedAt.Add(time.Duration(rule.CooldownSecs) * time.Second)
			if time.Now().Before(cooldownEnd) {
				logger.Debug(ctx, "rule skipped due to cooldown", "ruleId", rule.ID, "cooldownEnds", cooldownEnd)
				e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistorySkipped, "", nil)
				continue
			}
		}

		// CEL condition evaluation
		if rule.ConditionCEL != nil && strings.TrimSpace(*rule.ConditionCEL) != "" {
			matched, evalErr := e.EvaluateCEL(*rule.ConditionCEL, vars)
			if evalErr != nil {
				logger.Error(ctx, "CEL evaluation failed", "ruleId", rule.ID, "error", evalErr)
				errMsg := evalErr.Error()
				e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistoryError, "", &errMsg)
				continue
			}
			if !matched {
				e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistoryConditionFalse, "", nil)
				continue
			}
		}

		// Template rendering
		renderedPayload, renderErr := e.RenderTemplate(rule.ActionTemplate, payload)
		if renderErr != nil {
			logger.Error(ctx, "template render failed", "ruleId", rule.ID, "error", renderErr)
			errMsg := renderErr.Error()
			e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistoryError, "", &errMsg)
			continue
		}

		// Chain reaction: publish new event instead of delivering to subscribers
		if rule.ReactionType == automations.ReactionChain {
			e.handleChainReaction(ctx, rule, eventType, aggregateID, renderedPayload)
			continue
		}

		// Generate one DeliveryTask per subscriber
		for _, sub := range rule.Subscribers {
			tasks = append(tasks, DeliveryTask{
				Rule:            rule,
				Subscriber:      sub,
				RenderedPayload: renderedPayload,
				EventType:       eventType,
				AggregateID:     aggregateID,
			})
		}
	}

	return tasks, nil
}

// deliveryCache holds preloaded data for a batch of DeliveryTasks.
// Reduces queries by deduplicating IDs. TODO: Implement ListByIDs in repos to eliminate N+1 completely.
type deliveryCache struct {
	channels    map[id.ID]*automations.Channel
	accounts    map[id.ID]*automations.Account
	credentials map[id.ID][]byte
}

// preloadDeliveryData loads all unique channels, accounts, and credentials
// needed by the task batch. Runs BEFORE the parallel Deliver loop.
func (e *Engine) preloadDeliveryData(ctx context.Context, tasks []DeliveryTask) *deliveryCache {
	cache := &deliveryCache{
		channels:    make(map[id.ID]*automations.Channel),
		accounts:    make(map[id.ID]*automations.Account),
		credentials: make(map[id.ID][]byte),
	}

	// Collect unique channel IDs
	channelIDs := make(map[id.ID]struct{})
	for _, t := range tasks {
		if t.Subscriber.SubscriberType == automations.SubChannel && t.Subscriber.ChannelID != nil {
			channelIDs[*t.Subscriber.ChannelID] = struct{}{}
		}
	}
	if len(channelIDs) == 0 {
		return cache
	}

	// Load channels → collect account IDs
	accountIDs := make(map[id.ID]struct{})
	for chID := range channelIDs {
		ch, err := e.channelRepo.GetByID(ctx, chID)
		if err != nil {
			logger.Error(ctx, "preload: channel load failed", "channelId", chID, "error", err)
			continue
		}
		cache.channels[chID] = ch
		accountIDs[ch.AccountID] = struct{}{}
	}

	// Load accounts + decrypt credentials (one per unique account)
	for accID := range accountIDs {
		acc, err := e.accountRepo.GetByID(ctx, accID)
		if err != nil {
			logger.Error(ctx, "preload: account load failed", "accountId", accID, "error", err)
			continue
		}
		cache.accounts[accID] = acc

		creds, err := e.credManager.ReadCredentials(ctx, accID)
		if err != nil {
			logger.Error(ctx, "preload: credentials load failed", "accountId", accID, "error", err)
			continue
		}
		cache.credentials[accID] = creds
	}

	return cache
}

// Deliver is Phase 2 (I/O-bound): execute adapters in parallel, record history, update stats.
// Uses bounded concurrency (maxDeliveryConcurrency) to prevent overwhelming external APIs.
func (e *Engine) Deliver(ctx context.Context, tasks []DeliveryTask) {
	// Preload channel/account/credentials to avoid N+1 in the delivery loop
	cache := e.preloadDeliveryData(ctx, tasks)

	var (
		mu         sync.Mutex
		ruleErrors = make(map[id.ID]bool) // ruleID → hadError
		wg         sync.WaitGroup
		sem        = make(chan struct{}, maxDeliveryConcurrency)
	)

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot
		go func(t DeliveryTask) {
			defer wg.Done()
			defer func() { <-sem }() // Release slot

			start := time.Now()
			var adapterErr error

			switch t.Subscriber.SubscriberType {
			case automations.SubChannel:
				adapterErr = e.deliverToChannelCached(ctx, t, cache)
			case automations.SubUser, automations.SubRole, automations.SubDocField:
				adapterErr = e.deliverInternal(ctx, t)
			default:
				errMsg := fmt.Sprintf("unknown subscriber type: %s", t.Subscriber.SubscriberType)
				logger.Error(ctx, errMsg, "ruleId", t.Rule.ID)
				adapterErr = fmt.Errorf("%s", errMsg)
			}

			durationMs := int(time.Since(start).Milliseconds())

			// Record history per delivery
			var status automations.HistoryStatus
			var errText *string
			if adapterErr != nil {
				status = automations.HistoryError
				msg := adapterErr.Error()
				errText = &msg
				mu.Lock()
				ruleErrors[t.Rule.ID] = true
				mu.Unlock()
			} else {
				status = automations.HistorySuccess
				mu.Lock()
				if _, exists := ruleErrors[t.Rule.ID]; !exists {
					ruleErrors[t.Rule.ID] = false
				}
				mu.Unlock()
			}

			e.recordHistoryWithDuration(ctx, t.Rule, &t.Subscriber, t.EventType, t.AggregateID, status, t.RenderedPayload, errText, &durationMs)
		}(task)
	}

	wg.Wait()

	// Update rule stats (sequential — small number of unique rules)
	for ruleID, hadError := range ruleErrors {
		if err := e.ruleRepo.IncrementStats(ctx, ruleID, hadError); err != nil {
			logger.Error(ctx, "failed to update rule stats", "ruleId", ruleID, "error", err)
		}
	}
}

// deliverToChannelCached uses preloaded data from deliveryCache instead of N+1 queries.
func (e *Engine) deliverToChannelCached(ctx context.Context, task DeliveryTask, cache *deliveryCache) error {
	if task.Subscriber.ChannelID == nil {
		return fmt.Errorf("channel subscriber has no channelId")
	}

	chID := *task.Subscriber.ChannelID
	channel, ok := cache.channels[chID]
	if !ok {
		return fmt.Errorf("channel %s not found in preload cache", chID)
	}

	account, ok := cache.accounts[channel.AccountID]
	if !ok {
		return fmt.Errorf("account %s not found in preload cache", channel.AccountID)
	}

	creds := cache.credentials[channel.AccountID]

	adapter, ok := e.adapters[string(account.AccountType)]
	if !ok {
		return fmt.Errorf("no adapter registered for account type %q", account.AccountType)
	}

	adapterErr := adapter.Deliver(ctx, channel.Destination, account.Config, creds, task.RenderedPayload)

	// Update account last result
	if adapterErr != nil {
		errMsg := adapterErr.Error()
		_ = e.accountRepo.UpdateLastResult(ctx, account.ID, false, &errMsg)
	} else {
		_ = e.accountRepo.UpdateLastResult(ctx, account.ID, true, nil)
	}

	return adapterErr
}

// deliverInternal handles user/role/doc_field subscribers via internal notification adapter.
func (e *Engine) deliverInternal(ctx context.Context, task DeliveryTask) error {
	adapter, ok := e.adapters["internal_notification"]
	if !ok {
		return fmt.Errorf("internal_notification adapter not registered")
	}
	return adapter.Deliver(ctx, nil, nil, nil, task.RenderedPayload)
}

// handleChainReaction publishes a new event to the outbox for each chain_rule_id.
func (e *Engine) handleChainReaction(ctx context.Context, rule *automations.Rule, eventType string, aggregateID *id.ID, renderedPayload string) {
	if e.publisher == nil {
		logger.Error(ctx, "chain reaction: no outbox publisher configured", "ruleId", rule.ID)
		return
	}

	// Optimize: Marshal the event payload once, as it's identical for all chain rules
	chainEvent := map[string]any{
		"sourceRuleId":    rule.ID.String(),
		"sourceEventType": eventType,
		"doc":             renderedPayload,
		"action":          "chain",
		"entityType":      "automation",
	}

	eventPayload, err := json.Marshal(chainEvent)
	if err != nil {
		logger.Error(ctx, "chain reaction: marshal failed", "ruleId", rule.ID, "error", err)
		return
	}

	for _, chainRuleID := range rule.ChainRuleIDs {
		// Publish as a new event that will be picked up by the target rule
		chainEventType := fmt.Sprintf("automation.chain.%s", chainRuleID)
		if pubErr := e.publisher.Publish(ctx, chainEventType, eventPayload); pubErr != nil {
			logger.Error(ctx, "chain reaction: publish failed", "ruleId", rule.ID, "chainRuleId", chainRuleID, "error", pubErr)
			errMsg := pubErr.Error()
			e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistoryError, "", &errMsg)
		} else {
			e.recordHistory(ctx, rule, nil, eventType, aggregateID, automations.HistorySuccess, renderedPayload, nil)
		}
	}
}

// recordHistory is a convenience wrapper for writing history without duration.
func (e *Engine) recordHistory(
	ctx context.Context, rule *automations.Rule, sub *automations.Subscriber,
	eventType string, aggregateID *id.ID,
	status automations.HistoryStatus, payload string, errText *string,
) {
	e.recordHistoryWithDuration(ctx, rule, sub, eventType, aggregateID, status, payload, errText, nil)
}

// recordHistoryWithDuration writes a history entry.
func (e *Engine) recordHistoryWithDuration(
	ctx context.Context, rule *automations.Rule, sub *automations.Subscriber,
	eventType string, aggregateID *id.ID,
	status automations.HistoryStatus, payload string, errText *string, durationMs *int,
) {
	if e.historyRepo == nil {
		return
	}

	entry := &automations.HistoryEntry{
		RuleID:    rule.ID,
		RuleName:  rule.Name,
		EventType: eventType,
		Status:    status,
		DurationMs: durationMs,
		ErrorText:  errText,
	}

	if aggregateID != nil {
		entry.AggregateID = aggregateID
	}
	if payload != "" {
		entry.RenderedPayload = &payload
	}
	if sub != nil && sub.ChannelID != nil {
		entry.ChannelID = sub.ChannelID
		entry.ChannelName = sub.ChannelName
	}

	if err := e.historyRepo.Create(ctx, entry); err != nil {
		logger.Error(ctx, "failed to write automation history", "ruleId", rule.ID, "error", err)
	}
}

// --- CEL & Template utilities (unchanged) ---

// EvaluateCEL evaluates a CEL expression against variables.
func (e *Engine) EvaluateCEL(expr string, vars map[string]any) (bool, error) {
	prg, err := e.getOrCompileCEL(expr)
	if err != nil {
		return false, err
	}

	out, _, err := prg.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("eval error: %w", err)
	}

	if out.Type() != cel.BoolType {
		return false, fmt.Errorf("expression must return bool, got %v", out.Type().TypeName())
	}

	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("eval returned non-bool value")
	}

	return val, nil
}

// getOrCompileCEL returns a cached or newly compiled CEL program.
// Uses a bounded LRU cache to prevent unbounded memory growth.
func (e *Engine) getOrCompileCEL(expr string) (cel.Program, error) {
	e.celMu.Lock()
	if elem, ok := e.celLookup[expr]; ok {
		e.celOrder.MoveToFront(elem)
		e.celMu.Unlock()
		return elem.Value.(*celCacheEntry).prg, nil
	}
	e.celMu.Unlock()

	// Compile outside the lock (expensive operation)
	ast, issues := e.celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile error: %w", issues.Err())
	}

	prg, err := e.celEnv.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program error: %w", err)
	}

	e.celMu.Lock()
	defer e.celMu.Unlock()

	// Double-check after reacquiring lock
	if elem, ok := e.celLookup[expr]; ok {
		e.celOrder.MoveToFront(elem)
		return elem.Value.(*celCacheEntry).prg, nil
	}

	// Evict LRU if at capacity
	if e.celOrder.Len() >= celCacheMaxSize {
		oldest := e.celOrder.Back()
		if oldest != nil {
			entry := oldest.Value.(*celCacheEntry)
			delete(e.celLookup, entry.expr)
			e.celOrder.Remove(oldest)
		}
	}

	elem := e.celOrder.PushFront(&celCacheEntry{expr: expr, prg: prg})
	e.celLookup[expr] = elem

	return prg, nil
}

// InvalidateCELCache removes a specific expression from the cache.
func (e *Engine) InvalidateCELCache(expr string) {
	e.celMu.Lock()
	defer e.celMu.Unlock()
	if elem, ok := e.celLookup[expr]; ok {
		delete(e.celLookup, expr)
		e.celOrder.Remove(elem)
	}
}

// maxTemplateOutputSize is the maximum rendered output size (64KB).
const maxTemplateOutputSize = 64 * 1024

// safeFuncMap contains whitelisted template functions.
// Deliberately excludes `call` and other potentially dangerous builtins.
var safeFuncMap = template.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
	"jsonIndent": func(v any) (string, error) {
		b, err := json.MarshalIndent(v, "", "  ")
		return string(b), err
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"trim":  strings.TrimSpace,
	"default": func(defaultVal, val any) any {
		if val == nil || val == "" {
			return defaultVal
		}
		return val
	},
	// truncate cuts string to maxLen and appends ellipsis
	"truncate": func(maxLen int, s string) string {
		if len(s) <= maxLen {
			return s
		}
		return s[:maxLen] + "…"
	},
	// money converts MinorUnits to human-readable string with proper decimal places and thousand separators.
	// Usage: {{money .doc.totalAmount .currency.decimalPlaces}}
	"money": func(rawAmount any, dp any) string {
		var minor float64
		switch n := rawAmount.(type) {
		case float64: minor = n
		case float32: minor = float64(n)
		case int: minor = float64(n)
		case int64: minor = float64(n)
		case json.Number: minor, _ = n.Float64()
		default: return fmt.Sprintf("%v", rawAmount)
		}
		
		decimalPlaces := 2 // default
		switch d := dp.(type) {
		case float64: decimalPlaces = int(d)
		case int: decimalPlaces = d
		case int64: decimalPlaces = int(d)
		case json.Number: 
			if df, err := d.Float64(); err == nil {
				decimalPlaces = int(df)
			}
		}

		f := minor / math.Pow10(decimalPlaces)

		// Format with proper decimals, then insert thousand separators
		format := fmt.Sprintf("%%.%df", decimalPlaces)
		raw := fmt.Sprintf(format, f)
		parts := strings.SplitN(raw, ".", 2)
		intPart := parts[0]
		// Insert space every 3 digits from right
		negative := ""
		if strings.HasPrefix(intPart, "-") {
			negative = "-"
			intPart = intPart[1:]
		}
		var result []byte
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result = append(result, ' ')
			}
			result = append(result, byte(c))
		}
		
		if len(parts) > 1 {
			// use comma for decimal separator
			return negative + string(result) + "," + parts[1]
		}
		return negative + string(result)
	},
	// currency (legacy) formats a number with two decimal places and thousand separators (space).
	// Example: 150000 → "150 000.00", 1234.5 → "1 234.50"
	"currency": func(v any) string {
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case float32:
			f = float64(n)
		case int:
			f = float64(n)
		case int64:
			f = float64(n)
		case json.Number:
			f, _ = n.Float64()
		default:
			return fmt.Sprintf("%v", v)
		}
		// Format with 2 decimals, then insert thousand separators
		raw := fmt.Sprintf("%.2f", f)
		parts := strings.SplitN(raw, ".", 2)
		intPart := parts[0]
		// Insert space every 3 digits from right
		negative := ""
		if strings.HasPrefix(intPart, "-") {
			negative = "-"
			intPart = intPart[1:]
		}
		var result []byte
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result = append(result, ' ')
			}
			result = append(result, byte(c))
		}
		return negative + string(result) + "." + parts[1]
	},
	// date formats a time value as DD.MM.YYYY.
	"date": func(v any) string {
		t, ok := parseTime(v)
		if !ok {
			return fmt.Sprintf("%v", v)
		}
		return t.Format("02.01.2006")
	},
	// datetime formats a time value as DD.MM.YYYY HH:MM.
	"datetime": func(v any) string {
		t, ok := parseTime(v)
		if !ok {
			return fmt.Sprintf("%v", v)
		}
		return t.Format("02.01.2006 15:04")
	},
	// number formats a numeric value with thousand separators (no decimals).
	"number": func(v any) string {
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case float32:
			f = float64(n)
		case int:
			f = float64(n)
		case int64:
			f = float64(n)
		case json.Number:
			f, _ = n.Float64()
		default:
			return fmt.Sprintf("%v", v)
		}
		intPart := fmt.Sprintf("%.0f", f)
		negative := ""
		if strings.HasPrefix(intPart, "-") {
			negative = "-"
			intPart = intPart[1:]
		}
		var result []byte
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result = append(result, ' ')
			}
			result = append(result, byte(c))
		}
		return negative + string(result)
	},
}

// parseTime attempts to parse a value as time.Time from string or time.Time.
func parseTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02 15:04:05",
			"2006-01-02",
		} {
			parsed, err := time.Parse(layout, t)
			if err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

// getOrParseTemplate returns a cached or newly parsed template.
// Uses a bounded LRU cache to prevent unbounded memory growth.
func (e *Engine) getOrParseTemplate(tmplText string) (*template.Template, error) {
	e.tmplMu.Lock()
	if elem, ok := e.tmplLookup[tmplText]; ok {
		e.tmplOrder.MoveToFront(elem)
		e.tmplMu.Unlock()
		return elem.Value.(*tmplCacheEntry).tmpl, nil
	}
	e.tmplMu.Unlock()

	// Parse outside the lock (expensive operation)
	tmpl, err := template.New("action").Option("missingkey=zero").Funcs(safeFuncMap).Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	e.tmplMu.Lock()
	defer e.tmplMu.Unlock()

	// Double-check after reacquiring lock
	if elem, ok := e.tmplLookup[tmplText]; ok {
		e.tmplOrder.MoveToFront(elem)
		return elem.Value.(*tmplCacheEntry).tmpl, nil
	}

	// Evict LRU if at capacity
	if e.tmplOrder.Len() >= tmplCacheMaxSize {
		oldest := e.tmplOrder.Back()
		if oldest != nil {
			entry := oldest.Value.(*tmplCacheEntry)
			delete(e.tmplLookup, entry.text)
			e.tmplOrder.Remove(oldest)
		}
	}

	elem := e.tmplOrder.PushFront(&tmplCacheEntry{text: tmplText, tmpl: tmpl})
	e.tmplLookup[tmplText] = elem

	return tmpl, nil
}

// RenderTemplate renders a Go text/template against data with safety limits.
// Uses a restricted FuncMap (no `call`) and enforces output size limits.
func (e *Engine) RenderTemplate(tmplText string, data map[string]any) (string, error) {
	tmpl, err := e.getOrParseTemplate(tmplText)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	if buf.Len() > maxTemplateOutputSize {
		return "", fmt.Errorf("rendered output exceeds %d bytes limit", maxTemplateOutputSize)
	}

	return buf.String(), nil
}
