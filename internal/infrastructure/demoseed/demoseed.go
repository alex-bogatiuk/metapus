// Package demoseed writes the platform's demo data that a configuration's
// seed builds on: currencies, their exchange rate mappings and a Telegram
// notification of a configuration's event. The platform owns these tables,
// the credential key and the bot's secrets, so a configuration's seed names
// none of them. Every function is idempotent: a repeated seed finds what the
// previous one wrote.
package demoseed

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/secretbox"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/platform/secretfile"
	"metapus/pkg/logger"
)

// Currency is a demo currency; its code is its ISO code.
type Currency struct {
	ISOCode         string
	Name            string
	Symbol          string
	DecimalPlaces   int
	MinorMultiplier int64
	IsBase          bool
}

// EnsureCurrency adds the currency unless one with its code exists, and
// returns the ID of the one stored.
func EnsureCurrency(ctx context.Context, pool *postgres.Pool, c Currency) (id.ID, error) {
	currencyID := id.New()
	tag, err := pool.Exec(ctx, `
		INSERT INTO cat_currencies (
			id, code, name, iso_code, symbol,
			decimal_places, minor_multiplier, is_base,
			version, deletion_mark, attributes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
	`, currencyID, c.ISOCode, c.Name, c.ISOCode, c.Symbol, c.DecimalPlaces, c.MinorMultiplier, c.IsBase)
	if err != nil {
		return id.Nil(), fmt.Errorf("seed currency %s: %w", c.ISOCode, err)
	}
	if tag.RowsAffected() == 0 {
		if err := pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE code = $1 AND deletion_mark = FALSE`, c.ISOCode).Scan(&currencyID); err != nil {
			return id.Nil(), fmt.Errorf("read currency %s: %w", c.ISOCode, err)
		}
	}
	return currencyID, nil
}

// RateMapping names a currency's rate at a rate source against a base
// currency by the source's own identifier, such as a CoinGecko coin ID.
type RateMapping struct {
	CurrencyID     id.ID
	BaseCurrencyID id.ID
	RateSourceID   id.ID
	ExternalID     string
}

// MapRate stores the active mapping, replacing the external identifier of
// an existing one.
func MapRate(ctx context.Context, pool *postgres.Pool, m RateMapping) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO reg_rate_source_mappings (currency_id, base_currency_id, rate_source_id, external_id, is_active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (currency_id, base_currency_id, rate_source_id) DO UPDATE SET external_id = EXCLUDED.external_id
	`, m.CurrencyID, m.BaseCurrencyID, m.RateSourceID, m.ExternalID); err != nil {
		return fmt.Errorf("seed rate source mapping: %w", err)
	}
	return nil
}

// Notification is an automation rule that sends a text message when an
// entity event matches its CEL condition.
type Notification struct {
	Name        string
	Description string
	// EventType is the entity event, such as "updated".
	EventType string
	// TargetEntities are the entity keys whose events the rule reads.
	TargetEntities []string
	Condition      string
	// Severity is "info", "warning" or "error".
	Severity string
	// Message is a Go template over the event, such as {{.doc.number}}.
	Message string
}

const (
	telegramAccountName = "Metapus TG Bot"
	telegramChannelName = "Metapus TG Chat"
)

// TelegramNotification sends the notification to the demo Telegram chat:
// it seeds the bot account (its token sealed with the platform key), the
// chat channel, the rule and the channel's subscription. Without the key,
// AUTOMATION_TG_BOT_TOKEN or AUTOMATION_TG_CHAT_ID it logs why and seeds
// nothing; a key of the wrong length is an error.
func TelegramNotification(ctx context.Context, pool *postgres.Pool, log *logger.Logger, n Notification) error {
	if err := secretbox.Ready(); errors.Is(err, secretbox.ErrNotConfigured) {
		log.Warn("platform encryption key not configured; skipping automation seed")
		return nil
	} else if err != nil {
		return fmt.Errorf("check the encryption key: %w", err)
	}
	botToken, err := secretfile.Get("AUTOMATION_TG_BOT_TOKEN", "")
	if err != nil {
		return fmt.Errorf("load AUTOMATION_TG_BOT_TOKEN: %w", err)
	}
	if botToken == "" {
		log.Warn("AUTOMATION_TG_BOT_TOKEN not set; skipping automation seed")
		return nil
	}
	chatID := os.Getenv("AUTOMATION_TG_CHAT_ID")
	if chatID == "" {
		log.Warn("AUTOMATION_TG_CHAT_ID not set; skipping automation seed")
		return nil
	}

	sealed, err := secretbox.Seal([]byte(botToken))
	if err != nil {
		return fmt.Errorf("encrypt bot token: %w", err)
	}
	// The automation tables have no natural unique keys, so each row is
	// found by what the seed names it before it is added.
	accountID, err := ensure(ctx, pool,
		`SELECT id FROM sys_automation_accounts WHERE name = $1 AND account_type = 'telegram' AND deletion_mark = FALSE ORDER BY id LIMIT 1`,
		[]any{telegramAccountName},
		`INSERT INTO sys_automation_accounts (id, name, account_type, config, credentials_enc, is_active, status, version, deletion_mark)
		 VALUES ($1, $2, 'telegram', '{}', $3, true, 'active', 1, false)`,
		telegramAccountName, sealed)
	if err != nil {
		return fmt.Errorf("seed automation account: %w", err)
	}
	channelID, err := ensure(ctx, pool,
		`SELECT id FROM sys_automation_channels WHERE name = $1 AND account_id = $2 AND deletion_mark = FALSE ORDER BY id LIMIT 1`,
		[]any{telegramChannelName, accountID},
		`INSERT INTO sys_automation_channels (id, name, account_id, destination, is_active, version, deletion_mark)
		 VALUES ($1, $2, $3, jsonb_build_object('chat_id', $4::text), true, 1, false)`,
		telegramChannelName, accountID, chatID)
	if err != nil {
		return fmt.Errorf("seed automation channel: %w", err)
	}
	ruleID, err := ensure(ctx, pool,
		`SELECT id FROM sys_automation_rules WHERE name = $1 AND deletion_mark = FALSE ORDER BY id LIMIT 1`,
		[]any{n.Name},
		`INSERT INTO sys_automation_rules (
			id, name, description, trigger_type, event_type, target_entities, condition_cel,
			reaction_type, notif_severity, message_format, action_template,
			priority, max_retries, cooldown_seconds, is_active, version, deletion_mark
		) VALUES ($1, $2, $3, 'entity_event', $4, $5, $6, 'notify', $7, 'text', $8, 50, 3, 0, true, 1, false)`,
		n.Name, n.Description, n.EventType, n.TargetEntities, n.Condition, n.Severity, n.Message)
	if err != nil {
		return fmt.Errorf("seed automation rule: %w", err)
	}
	if _, err := ensure(ctx, pool,
		`SELECT id FROM sys_automation_subscribers WHERE rule_id = $1 AND channel_id = $2 ORDER BY id LIMIT 1`,
		[]any{ruleID, channelID},
		`INSERT INTO sys_automation_subscribers (id, rule_id, subscriber_type, channel_id, delivery_method, idx)
		 VALUES ($1, $2, 'channel', $3, 'push', 1)`,
		ruleID, channelID); err != nil {
		return fmt.Errorf("seed automation subscriber: %w", err)
	}
	log.Infow("automation data seeded", "account", telegramAccountName, "channel", telegramChannelName, "rule", n.Name)
	return nil
}

// ensure returns the ID the lookup finds, or inserts a row with a new ID
// (the insert's $1, followed by its args) and returns that.
func ensure(ctx context.Context, pool *postgres.Pool, lookup string, lookupArgs []any, insert string, insertArgs ...any) (id.ID, error) {
	var found id.ID
	err := pool.QueryRow(ctx, lookup, lookupArgs...).Scan(&found)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), err
	}
	created := id.New()
	if _, err := pool.Exec(ctx, insert, append([]any{created}, insertArgs...)...); err != nil {
		return id.Nil(), err
	}
	return created, nil
}
