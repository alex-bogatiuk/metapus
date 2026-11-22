// Package demoseed writes the platform's demo data a configuration's seed
// builds on — currencies, rate mappings and a Telegram notification — so the
// seed names none of the platform's tables or secrets.
package demoseed

import (
	"context"

	"metapus/internal/core/id"
	core "metapus/internal/infrastructure/demoseed"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

type (
	// Currency is a demo currency; its code is its ISO code.
	Currency = core.Currency
	// RateMapping names a currency's rate at a rate source by the source's
	// own identifier.
	RateMapping = core.RateMapping
	// Notification is an automation rule that sends a text message when an
	// entity event matches its CEL condition.
	Notification = core.Notification
)

// EnsureCurrency adds the currency unless one with its code exists, and
// returns the ID of the one stored.
func EnsureCurrency(ctx context.Context, pool *postgres.Pool, c Currency) (id.ID, error) {
	return core.EnsureCurrency(ctx, pool, c)
}

// MapRate stores the active mapping, replacing the external identifier of
// an existing one.
func MapRate(ctx context.Context, pool *postgres.Pool, m RateMapping) error {
	return core.MapRate(ctx, pool, m)
}

// TelegramNotification sends the notification to the demo Telegram chat
// configured by AUTOMATION_TG_BOT_TOKEN and AUTOMATION_TG_CHAT_ID; without
// them or the platform key it seeds nothing.
func TelegramNotification(ctx context.Context, pool *postgres.Pool, log *logger.Logger, n Notification) error {
	return core.TelegramNotification(ctx, pool, log, n)
}
