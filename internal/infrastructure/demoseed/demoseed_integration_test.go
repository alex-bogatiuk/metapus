//go:build integration

package demoseed_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/demoseed"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/secretbox"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

func TestDemoSeedIsIdempotent(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := tenant.Context(appctx.UserContext{})
	pool := &postgres.Pool{Pool: tenant.Pool}
	log := logger.Default()

	usd := demoseed.Currency{ISOCode: "USD", Name: "Доллар США", Symbol: "$", DecimalPlaces: 2, MinorMultiplier: 100}
	usdID, err := demoseed.EnsureCurrency(ctx, pool, usd)
	require.NoError(t, err)
	again, err := demoseed.EnsureCurrency(ctx, pool, usd)
	require.NoError(t, err)
	require.Equal(t, usdID, again)
	btcID, err := demoseed.EnsureCurrency(ctx, pool, demoseed.Currency{ISOCode: "BTC", Name: "Bitcoin", Symbol: "₿", DecimalPlaces: 8, MinorMultiplier: 100000000})
	require.NoError(t, err)

	sourceID := id.New()
	_, err = tenant.Pool.Exec(ctx, `INSERT INTO cat_rate_sources(id,code,name,source_type,priority) VALUES($1,'coingecko','CoinGecko','coingecko',10)`, sourceID)
	require.NoError(t, err)
	mapping := demoseed.RateMapping{CurrencyID: btcID, BaseCurrencyID: usdID, RateSourceID: sourceID, ExternalID: "btc"}
	require.NoError(t, demoseed.MapRate(ctx, pool, mapping))
	mapping.ExternalID = "bitcoin"
	require.NoError(t, demoseed.MapRate(ctx, pool, mapping))
	var external string
	require.NoError(t, tenant.Pool.QueryRow(ctx, `SELECT external_id FROM reg_rate_source_mappings WHERE currency_id=$1`, btcID).Scan(&external))
	require.Equal(t, "bitcoin", external)

	notification := demoseed.Notification{
		Name: "Тестовое уведомление", Description: "Проверка сида", EventType: "updated",
		TargetEntities: []string{"currency"}, Condition: `doc.code == "BTC"`, Severity: "info", Message: "Валюта {{.doc.code}}",
	}
	count := func(table string) int {
		var n int
		require.NoError(t, tenant.Pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n))
		return n
	}

	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "")
	t.Setenv("AUTOMATION_TG_BOT_TOKEN", "123:token")
	t.Setenv("AUTOMATION_TG_CHAT_ID", `-100"42`)
	require.NoError(t, demoseed.TelegramNotification(ctx, pool, log, notification))
	require.Zero(t, count("sys_automation_accounts"))

	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "short")
	require.Error(t, demoseed.TelegramNotification(ctx, pool, log, notification))

	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	for range 2 {
		require.NoError(t, demoseed.TelegramNotification(ctx, pool, log, notification))
	}
	for _, table := range []string{"sys_automation_accounts", "sys_automation_channels", "sys_automation_rules", "sys_automation_subscribers"} {
		require.Equal(t, 1, count(table), table)
	}
	var sealed []byte
	var chatID string
	require.NoError(t, tenant.Pool.QueryRow(ctx, `SELECT a.credentials_enc, c.destination->>'chat_id' FROM sys_automation_accounts a JOIN sys_automation_channels c ON c.account_id=a.id`).Scan(&sealed, &chatID))
	token, err := secretbox.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "123:token", string(token))
	require.Equal(t, `-100"42`, chatID)
}
