// Package cache provides caching infrastructure with PostgreSQL LISTEN/NOTIFY support.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/pkg/logger"
)

// SchemaCache provides thread-safe caching of metadata (custom fields, feature flags)
// with automatic invalidation via PostgreSQL LISTEN/NOTIFY.
// This eliminates TTL-based polling and provides near-realtime cache updates.
type SchemaCache struct {
	pool         *pgxpool.Pool
	mu           sync.RWMutex
	customFields map[string][]CustomFieldSchema // entityType -> fields
	featureFlags map[string]FeatureFlag         // flagName -> flag

	// Listeners for cache invalidation
	listeners   []InvalidationListener
	listenersMu sync.RWMutex

	// Lifecycle
	lifecycleMu sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	started     bool
}

// CustomFieldSchema represents a custom field definition.
type CustomFieldSchema struct {
	ID              string
	EntityType      string
	FieldName       string
	FieldType       string
	DisplayName     string
	Description     string
	IsRequired      bool
	IsIndexed       bool
	DefaultValue    any
	ValidationRules map[string]any
	ReferenceType   string
	EnumValues      []string
	SortOrder       int
	IsActive        bool
}

// FeatureFlag represents a feature flag.
type FeatureFlag struct {
	ID          string
	FlagName    string
	Description string
	IsEnabled   bool
	Variant     string
	Config      map[string]any
	ValidFrom   *time.Time
	ValidUntil  *time.Time
}

// InvalidationListener is called when cache is invalidated.
type InvalidationListener func(channel string, payload string)

// NewSchemaCache creates a new schema cache.
func NewSchemaCache(pool *pgxpool.Pool) *SchemaCache {
	return &SchemaCache{
		pool:         pool,
		customFields: make(map[string][]CustomFieldSchema),
		featureFlags: make(map[string]FeatureFlag),
	}
}

// Start begins listening for NOTIFY events and loads initial data.
func (c *SchemaCache) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	c.lifecycleMu.Lock()
	if c.started {
		c.lifecycleMu.Unlock()
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.started = true
	c.lifecycleMu.Unlock()

	// Load initial data
	if err := c.loadCustomFields(c.ctx); err != nil {
		c.Stop()
		return fmt.Errorf("load custom fields: %w", err)
	}
	if err := c.loadFeatureFlags(c.ctx); err != nil {
		c.Stop()
		return fmt.Errorf("load feature flags: %w", err)
	}

	// Start listener goroutine
	c.wg.Add(1)
	go c.listenLoop()
	logger.Info(c.ctx, "schema cache started")
	return nil
}

// Stop gracefully stops the cache listener.
func (c *SchemaCache) Stop() {
	c.lifecycleMu.Lock()
	if !c.started {
		c.lifecycleMu.Unlock()
		return
	}
	cancel := c.cancel
	c.started = false
	c.cancel = nil
	c.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	c.wg.Wait()
	logger.Info(context.Background(), "schema cache stopped")
}

// listenLoop listens for PostgreSQL NOTIFY events.
func (c *SchemaCache) listenLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Acquire dedicated connection for LISTEN
		conn, err := c.pool.Acquire(c.ctx)
		if err != nil {
			logger.Error(c.ctx, "failed to acquire connection for LISTEN", "error", err)
			time.Sleep(time.Second)
			continue
		}

		// Subscribe to channels
		_, err = conn.Exec(c.ctx, "LISTEN schema_changed; LISTEN feature_flags_changed;")
		if err != nil {
			logger.Error(c.ctx, "failed to LISTEN", "error", err)
			conn.Release()
			time.Sleep(time.Second)
			continue
		}

		logger.Info(c.ctx, "listening for schema_changed and feature_flags_changed notifications")

		// Wait for notifications
		c.waitForNotifications(conn)
		conn.Release()
	}
}

// waitForNotifications blocks waiting for NOTIFY events.
func (c *SchemaCache) waitForNotifications(conn *pgxpool.Conn) {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Wait for notification with timeout for graceful shutdown
		ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
		notification, err := conn.Conn().WaitForNotification(ctx)
		cancel()

		if err != nil {
			if c.ctx.Err() != nil {
				return // Shutting down
			}
			// Timeout is expected, continue listening
			continue
		}

		logger.Debug(c.ctx, "received notification",
			"channel", notification.Channel,
			"payload", notification.Payload)

		// Handle notification
		c.handleNotification(notification.Channel, notification.Payload)
	}
}

// handleNotification processes NOTIFY event.
func (c *SchemaCache) handleNotification(channel, payload string) {
	switch channel {
	case "schema_changed":
		// Payload format: "entityType"
		c.invalidateCustomFields(c.ctx, payload)

	case "feature_flags_changed":
		// Payload format: "flagName"
		c.invalidateFeatureFlags(c.ctx, payload)
	}

	// Notify registered listeners with panic recovery (no goroutine fan-out).
	// This keeps invalidation delivery bounded and avoids goroutine storms on bursts of NOTIFY events.
	c.listenersMu.RLock()
	for _, listener := range c.listeners {
		func(l InvalidationListener) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error(c.ctx, "listener panic recovered", "channel", channel, "panic", r)
				}
			}()
			l(channel, payload)
		}(listener)
	}
	c.listenersMu.RUnlock()
}

// invalidateCustomFields reloads custom fields for specific entity.
func (c *SchemaCache) invalidateCustomFields(ctx context.Context, payload string) {
	entityType := strings.TrimSpace(payload)
	if entityType == "" {
		// Invalid payload, reload all.
		if err := c.loadCustomFields(ctx); err != nil {
			logger.Error(ctx, "failed to reload all custom fields", "error", err)
		}
		return
	}

	if err := c.loadCustomFieldsForEntity(ctx, entityType); err != nil {
		logger.Error(ctx, "failed to reload custom fields",
			"entityType", entityType, "error", err)
	}
}

// invalidateFeatureFlags reloads feature flags.
func (c *SchemaCache) invalidateFeatureFlags(ctx context.Context, payload string) {
	// For simplicity, reload all flags
	// In production, could be more selective based on payload
	if err := c.loadFeatureFlags(ctx); err != nil {
		logger.Error(ctx, "failed to reload feature flags", "error", err)
	}
}

// loadCustomFields loads all custom field schemas from database.
func (c *SchemaCache) loadCustomFields(ctx context.Context) error {
	rows, err := c.pool.Query(ctx, `
		SELECT id, entity_type, field_name, field_type, display_name, 
			   description, is_required, is_indexed, default_value,
			   validation_rules, reference_type, enum_values, sort_order, 
			   is_active
		FROM sys_custom_field_schemas
		WHERE is_active = TRUE
		ORDER BY entity_type, sort_order
	`)
	if err != nil {
		return fmt.Errorf("query custom fields: %w", err)
	}
	defer rows.Close()

	fields := make(map[string][]CustomFieldSchema)
	for rows.Next() {
		var f CustomFieldSchema
		var defaultValue, validationRules []byte
		var enumValues []string

		err := rows.Scan(
			&f.ID, &f.EntityType, &f.FieldName, &f.FieldType, &f.DisplayName,
			&f.Description, &f.IsRequired, &f.IsIndexed, &defaultValue,
			&validationRules, &f.ReferenceType, &enumValues, &f.SortOrder,
			&f.IsActive,
		)
		if err != nil {
			return fmt.Errorf("scan custom field: %w", err)
		}

		f.EnumValues = enumValues
		if len(defaultValue) > 0 {
			var v any
			if err := json.Unmarshal(defaultValue, &v); err != nil {
				return fmt.Errorf("unmarshal custom field default_value (%s.%s): %w", f.EntityType, f.FieldName, err)
			}
			f.DefaultValue = v
		}
		if len(validationRules) > 0 {
			var m map[string]any
			if err := json.Unmarshal(validationRules, &m); err != nil {
				return fmt.Errorf("unmarshal custom field validation_rules (%s.%s): %w", f.EntityType, f.FieldName, err)
			}
			f.ValidationRules = m
		}

		fields[f.EntityType] = append(fields[f.EntityType], f)
	}

	c.mu.Lock()
	c.customFields = fields
	c.mu.Unlock()

	totalFields := 0
	for _, list := range fields {
		totalFields += len(list)
	}
	logger.Info(ctx, "loaded custom fields", "entities", len(fields), "fields", totalFields)
	return nil
}

// loadCustomFieldsForEntity loads custom fields for specific entity.
func (c *SchemaCache) loadCustomFieldsForEntity(ctx context.Context, entityType string) error {
	rows, err := c.pool.Query(ctx, `
		SELECT id, entity_type, field_name, field_type, display_name, 
			   description, is_required, is_indexed, default_value,
			   validation_rules, reference_type, enum_values, sort_order, 
			   is_active
		FROM sys_custom_field_schemas
		WHERE entity_type = $1 AND is_active = TRUE
		ORDER BY sort_order
	`, entityType)
	if err != nil {
		return fmt.Errorf("query custom fields: %w", err)
	}
	defer rows.Close()

	var fields []CustomFieldSchema
	for rows.Next() {
		var f CustomFieldSchema
		var defaultValue, validationRules []byte
		var enumValues []string

		err := rows.Scan(
			&f.ID, &f.EntityType, &f.FieldName, &f.FieldType, &f.DisplayName,
			&f.Description, &f.IsRequired, &f.IsIndexed, &defaultValue,
			&validationRules, &f.ReferenceType, &enumValues, &f.SortOrder,
			&f.IsActive,
		)
		if err != nil {
			return fmt.Errorf("scan custom field: %w", err)
		}

		f.EnumValues = enumValues
		if len(defaultValue) > 0 {
			var v any
			if err := json.Unmarshal(defaultValue, &v); err != nil {
				return fmt.Errorf("unmarshal custom field default_value (%s.%s): %w", f.EntityType, f.FieldName, err)
			}
			f.DefaultValue = v
		}
		if len(validationRules) > 0 {
			var m map[string]any
			if err := json.Unmarshal(validationRules, &m); err != nil {
				return fmt.Errorf("unmarshal custom field validation_rules (%s.%s): %w", f.EntityType, f.FieldName, err)
			}
			f.ValidationRules = m
		}
		fields = append(fields, f)
	}

	c.mu.Lock()
	c.customFields[entityType] = fields
	c.mu.Unlock()

	logger.Debug(ctx, "reloaded custom fields", "entityType", entityType, "fields", len(fields))
	return nil
}

// loadFeatureFlags loads all feature flags from database.
func (c *SchemaCache) loadFeatureFlags(ctx context.Context) error {
	rows, err := c.pool.Query(ctx, `
		SELECT id, flag_name, description, is_enabled, variant, 
			   config, valid_from, valid_until
		FROM sys_feature_flags
	`)
	if err != nil {
		return fmt.Errorf("query feature flags: %w", err)
	}
	defer rows.Close()

	flags := make(map[string]FeatureFlag)
	now := time.Now()

	for rows.Next() {
		var f FeatureFlag
		var config []byte

		err := rows.Scan(
			&f.ID, &f.FlagName, &f.Description, &f.IsEnabled, &f.Variant,
			&config, &f.ValidFrom, &f.ValidUntil,
		)
		if err != nil {
			return fmt.Errorf("scan feature flag: %w", err)
		}

		if len(config) > 0 {
			var m map[string]any
			if err := json.Unmarshal(config, &m); err != nil {
				return fmt.Errorf("unmarshal feature flag config (%s): %w", f.FlagName, err)
			}
			f.Config = m
		}

		// Check validity period
		if f.ValidFrom != nil && now.Before(*f.ValidFrom) {
			f.IsEnabled = false
		}
		if f.ValidUntil != nil && now.After(*f.ValidUntil) {
			f.IsEnabled = false
		}

		flags[f.FlagName] = f
	}

	c.mu.Lock()
	c.featureFlags = flags
	c.mu.Unlock()

	logger.Info(ctx, "loaded feature flags", "count", len(flags))
	return nil
}

// GetCustomFields returns custom fields for entity type (within tenant database).
func (c *SchemaCache) GetCustomFields(entityType string) []CustomFieldSchema {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fields := c.customFields[entityType]
	// Return a copy to prevent external mutation of internal cache state.
	return append([]CustomFieldSchema(nil), fields...)
}

// IsFeatureEnabled checks if feature flag is enabled.
func (c *SchemaCache) IsFeatureEnabled(flagName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flag, ok := c.featureFlags[flagName]
	return ok && flag.IsEnabled
}

// GetFeatureVariant returns variant for A/B test.
func (c *SchemaCache) GetFeatureVariant(flagName string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if flag, ok := c.featureFlags[flagName]; ok {
		return flag.Variant
	}
	return ""
}

// GetFeatureConfig returns a shallow copy of feature flag config (map) if present.
// It returns nil if flag is missing or has no config.
func (c *SchemaCache) GetFeatureConfig(flagName string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flag, ok := c.featureFlags[flagName]
	if !ok || len(flag.Config) == 0 {
		return nil
	}
	cfg := make(map[string]any, len(flag.Config))
	for k, v := range flag.Config {
		cfg[k] = v
	}
	return cfg
}

// OnInvalidation registers a callback for cache invalidation events.
func (c *SchemaCache) OnInvalidation(listener InvalidationListener) {
	c.listenersMu.Lock()
	c.listeners = append(c.listeners, listener)
	c.listenersMu.Unlock()
}

// Stats returns cache statistics.
type CacheStats struct {
	EntitiesCount     int
	CustomFieldsCount int
	FeatureFlagsCount int
	EntitiesCached    []string
}

// GetStats returns current cache statistics.
func (c *SchemaCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entities := make([]string, 0, len(c.customFields))
	totalFields := 0
	for k := range c.customFields {
		entities = append(entities, k)
	}
	for _, list := range c.customFields {
		totalFields += len(list)
	}

	flagCount := len(c.featureFlags)

	return CacheStats{
		EntitiesCount:     len(c.customFields),
		CustomFieldsCount: totalFields,
		FeatureFlagsCount: flagCount,
		EntitiesCached:    entities,
	}
}
