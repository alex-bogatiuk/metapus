package content

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration"
	appCrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/platform/secretfile"
	"metapus/pkg/logger"
)

// Seed adds the crypto demo data after the platform's: crypto currencies and
// their CoinGecko rates, the TRON Nile network with its tokens, a merchant
// with wallets and fees, and the Telegram notification of overpaid
// invoices. Without env.Demo it seeds nothing.
func (cryptoConfiguration) Seed(ctx context.Context, env configuration.SeedEnv) error {
	if !env.Demo {
		return nil
	}
	currencyIDs := seedCryptoCurrencies(ctx, env)
	if err := seedCryptoData(ctx, env.Pool, env.Logger, currencyIDs); err != nil {
		return err
	}
	return seedAutomationData(ctx, env.Pool, env.Logger)
}

// seedCryptoCurrencies adds the crypto currencies and maps them to the
// CoinGecko rate source the platform seeded. It returns every currency's ID
// by ISO code, the platform's included.
func seedCryptoCurrencies(ctx context.Context, env configuration.SeedEnv) map[string]id.ID {
	pool, log := env.Pool, env.Logger
	currencyIDs := make(map[string]id.ID, len(env.Currencies)+4)
	for code, currencyID := range env.Currencies {
		currencyIDs[code] = currencyID
	}
	currencies := []struct {
		name            string
		isoCode         string
		symbol          string
		decimalPlaces   int
		minorMultiplier int64
		coinGeckoID     string
	}{
		{"Tether (USDT)", "USDT", "₮", 6, 1000000, "tether"},
		{"TRON", "TRX", "TRX", 6, 1000000, ""},
		{"Bitcoin", "BTC", "₿", 8, 100000000, "bitcoin"},
		{"Ethereum", "ETH", "Ξ", 18, 1000000000000000000, "ethereum"},
	}
	usdID, hasUSD := currencyIDs["USD"]
	coinGeckoID, hasCoinGecko := env.RateSources["coingecko"]
	for _, c := range currencies {
		currID := id.New()
		tag, err := pool.Exec(ctx, `
			INSERT INTO cat_currencies (
				id, code, name, iso_code, symbol,
				decimal_places, minor_multiplier, is_base,
				version, deletion_mark, attributes
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, currID, c.isoCode, c.name, c.isoCode, c.symbol, c.decimalPlaces, c.minorMultiplier)
		if err == nil && tag.RowsAffected() == 0 {
			err = pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE code = $1 AND deletion_mark = FALSE`, c.isoCode).Scan(&currID)
		}
		if err != nil {
			log.Warnw("failed to seed currency", "name", c.name, "error", err)
			continue
		}
		currencyIDs[c.isoCode] = currID
		if c.coinGeckoID == "" || !hasUSD || !hasCoinGecko {
			continue
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO reg_rate_source_mappings (currency_id, base_currency_id, rate_source_id, external_id, is_active)
			VALUES ($1, $2, $3, $4, TRUE)
			ON CONFLICT (currency_id, base_currency_id, rate_source_id) DO UPDATE SET external_id = EXCLUDED.external_id
		`, currID, usdID, coinGeckoID, c.coinGeckoID); err != nil {
			log.Warnw("failed to seed rate source mapping", "currency", c.isoCode, "error", err)
		}
	}
	log.Info("crypto currencies seeded (USDT, TRX, BTC, ETH) with CoinGecko rates for USDT, BTC and ETH")
	return currencyIDs
}

func seedCryptoData(ctx context.Context, pool *postgres.Pool, log *logger.Logger, currencyIDs map[string]id.ID) error {
	log.Info("seeding crypto processing data...")

	// ── Blockchain Network: TRON Nile Testnet ──────────────────────────
	networkID := id.New()
	networkCode := "TRON-NILE"

	err := pool.QueryRow(ctx, `
		INSERT INTO cat_blockchain_networks (
			id, code, name, chain_family, network_identifier,
			native_token_symbol, native_decimals, block_time_seconds,
			confirmations_needed, explorer_url, color, is_active,
			version, deletion_mark, attributes
		) VALUES ($1, $2, $3, 'tron', $4, $5, $6, $7, $8, $9, 'red', true, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO UPDATE SET
			name = EXCLUDED.name,
			chain_family = EXCLUDED.chain_family,
			network_identifier = EXCLUDED.network_identifier,
			native_token_symbol = EXCLUDED.native_token_symbol,
			native_decimals = EXCLUDED.native_decimals,
			block_time_seconds = EXCLUDED.block_time_seconds,
			confirmations_needed = EXCLUDED.confirmations_needed,
			explorer_url = EXCLUDED.explorer_url,
			is_active = TRUE,
			updated_at = NOW(),
			version = cat_blockchain_networks.version + 1
		RETURNING id
	`, networkID, networkCode, "TRON Nile Testnet", "nile",
		"TRX", 6, 3, 19, "https://nile.tronscan.org/#/transaction/{txHash}").Scan(&networkID)
	if err != nil {
		return fmt.Errorf("seed blockchain network: %w", err)
	}

	// ── Token: USDT-TRC20 ──────────────────────────────────────────────
	tokenID := id.New()
	tokenCode := "USDT-TRC20"

	// Link token to USDT currency for balance → fiat conversion.
	var usdtCurrencyIDVal any
	usdtCurrencyID := currencyIDs["USDT"]
	if !id.IsNil(usdtCurrencyID) {
		usdtCurrencyIDVal = usdtCurrencyID
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO cat_tokens (
			id, code, name, symbol, network_id,
			contract_address, decimal_places, token_standard, is_active,
			sweep_threshold, sweep_max_age_hours, sweep_retain_amount,
			currency_id,
			version, deletion_mark, attributes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9, $10, $11, $12, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO UPDATE SET
			name = EXCLUDED.name,
			symbol = EXCLUDED.symbol,
			network_id = EXCLUDED.network_id,
			contract_address = EXCLUDED.contract_address,
			decimal_places = EXCLUDED.decimal_places,
			token_standard = EXCLUDED.token_standard,
			is_active = TRUE,
			sweep_threshold = EXCLUDED.sweep_threshold,
			sweep_max_age_hours = EXCLUDED.sweep_max_age_hours,
			sweep_retain_amount = EXCLUDED.sweep_retain_amount,
			currency_id = EXCLUDED.currency_id,
			updated_at = NOW(),
			version = cat_tokens.version + 1
		RETURNING id
	`, tokenID, tokenCode, "Tether USD (TRC-20)", "USDT", networkID,
		"TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf", 6, "TRC-20",
		"10000000", 1, "1", usdtCurrencyIDVal).Scan(&tokenID) // reserve=1 raw unit (0.000001 USDT)
	if err != nil {
		return fmt.Errorf("seed token: %w", err)
	}

	// ── Native Token: TRX on TRON Nile ─────────────────────────────────
	nativeTokenID := id.New()
	nativeTokenCode := "TRX-NILE"
	var trxCurrencyIDVal any
	trxCurrencyID := currencyIDs["TRX"]
	if !id.IsNil(trxCurrencyID) {
		trxCurrencyIDVal = trxCurrencyID
	}

	err = pool.QueryRow(ctx, `
		SELECT id
		FROM cat_tokens
		WHERE network_id = $1
		  AND token_standard = 'native'
		  AND deletion_mark = FALSE
		ORDER BY id
		LIMIT 1
	`, networkID).Scan(&nativeTokenID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("fetch native token: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = pool.QueryRow(ctx, `
			INSERT INTO cat_tokens (
				id, code, name, symbol, network_id,
				contract_address, decimal_places, token_standard, is_active,
				sweep_threshold, sweep_max_age_hours,
				currency_id,
				version, deletion_mark, attributes
			) VALUES ($1, $2, $3, 'TRX', $4, '', 6, 'native', true, 0, 0, $5, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO UPDATE SET
				name = EXCLUDED.name,
				symbol = EXCLUDED.symbol,
				network_id = EXCLUDED.network_id,
				contract_address = '',
				decimal_places = EXCLUDED.decimal_places,
				token_standard = 'native',
				is_active = TRUE,
				sweep_threshold = 0,
				sweep_max_age_hours = 0,
				currency_id = EXCLUDED.currency_id,
				updated_at = NOW(),
				version = cat_tokens.version + 1
			RETURNING id
		`, nativeTokenID, nativeTokenCode, "TRON (Nile)", networkID, trxCurrencyIDVal).Scan(&nativeTokenID)
		if err != nil {
			return fmt.Errorf("seed native token: %w", err)
		}
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE cat_tokens
			SET code = $2,
			    name = $3,
			    symbol = 'TRX',
			    network_id = $4,
			    contract_address = '',
			    decimal_places = 6,
			    token_standard = 'native',
			    is_active = TRUE,
			    sweep_threshold = 0,
			    sweep_max_age_hours = 0,
			    currency_id = $5,
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1
			  AND deletion_mark = FALSE
		`, nativeTokenID, nativeTokenCode, "TRON (Nile)", networkID, trxCurrencyIDVal)
		if err != nil {
			return fmt.Errorf("repair native token: %w", err)
		}
	}

	// ── Merchants: 3 total for chart diversity ──────────────────────────
	type merchantSeed struct {
		code       string
		name       string
		legalName  string
		webhookURL string
		feeBP      int // processing fee in basis points (for fee schedule)
	}

	merchants := []merchantSeed{
		{"MERCHANT-001", "Test Merchant (Dev)", "Test Merchant LLC", "https://webhook.site/test", 150},
		{"MERCHANT-002", "CryptoGate", "CryptoGate Inc.", "https://webhook.site/cryptogate", 200},
		{"MERCHANT-003", "BlockPay Express", "BlockPay Express Ltd.", "https://webhook.site/blockpay", 100},
	}

	merchantIDs := make([]id.ID, 0, len(merchants))
	merchantFeeBP := make(map[string]int, len(merchants))
	for _, m := range merchants {
		mID := id.New()
		ct, mErr := pool.Exec(ctx, `
			INSERT INTO cat_merchants (
				id, code, name, legal_name,
				webhook_url, kyb_status, is_active,
				version, deletion_mark, attributes
			) VALUES ($1, $2, $3, $4, $5, 'approved', true, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, mID, m.code, m.name, m.legalName, m.webhookURL)
		if mErr != nil {
			return fmt.Errorf("seed merchant %s: %w", m.code, mErr)
		}
		if ct.RowsAffected() == 0 {
			mErr = pool.QueryRow(ctx, `
				SELECT id FROM cat_merchants WHERE code = $1 AND deletion_mark = FALSE
			`, m.code).Scan(&mID)
			if mErr != nil {
				return fmt.Errorf("fetch existing merchant %s: %w", m.code, mErr)
			}
		}
		merchantIDs = append(merchantIDs, mID)
		merchantFeeBP[mID.String()] = m.feeBP
	}

	// ── Wallets: 1 Hot + 6 Pool ─────────────────────────────────────────
	// Addresses are valid Nile dev placeholders for a fresh dev database.
	// crypto-setup.ps1 derives and rewrites them from the configured Vault HD seed.
	type walletSeed struct {
		code                  string
		name                  string
		address               string
		path                  string
		tier                  string // "pool", "hot", "warm", "cold"
		walletRole            string // "merchant_pool", "liquidity_hot", "network_fee", "treasury"
		status                string // "free", "leased", "sweep_pending", "frozen"
		chainActivationStatus string
	}

	wallets := []walletSeed{
		{"HOT-TRON-001", "TRON Hot Wallet", "TMXMyg87BiHCVfkwvVj3T32SWDSuRQqsPx", "m/44'/195'/0'/0/0", "hot", "liquidity_hot", "leased", "active"},
		{"FEE-TRON-001", "TRON Network Fee Wallet", "TRBA21s1LL7weu4ufowAvXL2mpTxLoifU9", "m/44'/195'/0'/0/1", "hot", "network_fee", "free", "active"},
		{"POOL-TRON-001", "Pool Wallet #1", "TVgY6mWpDGGCtPRBxuMSjitVHfPkpJuVRG", "m/44'/195'/0'/0/2", "pool", "merchant_pool", "free", "unknown"},
		{"POOL-TRON-002", "Pool Wallet #2", "TYhJ3xwXK7MKurXVdocvKGNMojgkAzVkJM", "m/44'/195'/0'/0/3", "pool", "merchant_pool", "free", "unknown"},
		{"POOL-TRON-003", "Pool Wallet #3", "TFPDi8prFckqBNNTdPk85gk5K3GBkww82V", "m/44'/195'/0'/0/4", "pool", "merchant_pool", "free", "unknown"},
		{"POOL-TRON-004", "Pool Wallet #4", "TUhYxEYr7L7UPX5ibcdQ4B6YANdzzCgLBn", "m/44'/195'/0'/0/5", "pool", "merchant_pool", "free", "unknown"},
		{"POOL-TRON-005", "Pool Wallet #5", "TFvAAZ3EhZbcbC1ZhFFM29GSv2BtzE9vMM", "m/44'/195'/0'/0/6", "pool", "merchant_pool", "free", "unknown"},
		{"POOL-TRON-006", "Pool Wallet #6", "TSn7XYrg1ZEFmaFf6ukW4MNwRR6NvD3fkV", "m/44'/195'/0'/0/7", "pool", "merchant_pool", "free", "unknown"},
	}

	batch := &pgx.Batch{}
	for _, w := range wallets {
		wID := id.New()
		batch.Queue(`
			INSERT INTO cat_wallets (
				id, code, name, address, network_id,
				derivation_path, tier, wallet_role, status, allocation_mode,
				chain_activation_status,
				version, deletion_mark, attributes
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, 'transient',
				$10,
				1, false, '{}'
			)
			ON CONFLICT DO NOTHING
		`, wID, w.code, w.name, w.address, networkID, w.path, w.tier, w.walletRole, w.status, w.chainActivationStatus)
	}

	results := pool.SendBatch(ctx, batch)
	for _, w := range wallets {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("seed wallet %s: %w", w.code, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close wallet batch: %w", err)
	}

	// ── Fee Schedule: global default + per-merchant overrides ───────────
	// Global default: 1% processing fee (100 bp)
	_, fErr := pool.Exec(ctx, `
		INSERT INTO reg_fee_schedule (
			merchant_id, token_id, direction,
			fixed_fee, percent_bp, min_fee, max_fee
		) VALUES (NULL, $1, 'processing', 0, 100, 0, 0)
		ON CONFLICT (COALESCE(merchant_id, '00000000-0000-0000-0000-000000000000'::UUID), token_id, direction) DO NOTHING
	`, tokenID)
	if fErr != nil {
		return fmt.Errorf("seed global fee schedule: %w", fErr)
	}

	// Per-merchant overrides: each merchant has its own processing rate
	for _, mID := range merchantIDs {
		bp := merchantFeeBP[mID.String()]
		_, fErr = pool.Exec(ctx, `
			INSERT INTO reg_fee_schedule (
				merchant_id, token_id, direction,
				fixed_fee, percent_bp, min_fee, max_fee
			) VALUES ($1, $2, 'processing', 0, $3, 0, 0)
			ON CONFLICT (COALESCE(merchant_id, '00000000-0000-0000-0000-000000000000'::UUID), token_id, direction) DO NOTHING
		`, mID, tokenID, bp)
		if fErr != nil {
			return fmt.Errorf("seed merchant fee schedule: %w", fErr)
		}
	}

	log.Infow("crypto data seeded",
		"network", networkCode,
		"token", tokenCode,
		"merchants", len(merchantIDs),
		"wallets", len(wallets),
		"feeSchedules", len(merchantIDs)+1,
	)
	return nil
}

// seedAutomationData creates the automation reference data:
// 1 Telegram Account → 1 Channel → 1 Rule (overpayment notification) → 1 Subscriber.
func seedAutomationData(ctx context.Context, pool *postgres.Pool, log *logger.Logger) error {
	log.Info("seeding automation data...")

	encKey, err := secretfile.Get("AUTOMATION_ENCRYPTION_KEY", "")
	if err != nil {
		return fmt.Errorf("load AUTOMATION_ENCRYPTION_KEY: %w", err)
	}
	if encKey == "" {
		log.Warn("AUTOMATION_ENCRYPTION_KEY not set; skipping automation seed")
		return nil
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

	// ── Account: Telegram Bot ────────────────────────────────────────────
	accountID := id.New()
	accountName := "Metapus TG Bot"

	// Encrypt bot token
	credEnc, err := appCrypto.Encrypt([]byte(botToken), []byte(encKey))
	if err != nil {
		return fmt.Errorf("encrypt bot token: %w", err)
	}

	commandTag, err := pool.Exec(ctx, `
		INSERT INTO sys_automation_accounts (
			id, name, account_type, config, credentials_enc,
			is_active, status, version, deletion_mark
		) VALUES ($1, $2, 'telegram', '{}', $3, true, 'active', 1, false)
		ON CONFLICT DO NOTHING
	`, accountID, accountName, credEnc)
	if err != nil {
		return fmt.Errorf("seed automation account: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		// Account already exists — fetch by name+type for idempotency
		err = pool.QueryRow(ctx, `
			SELECT id FROM sys_automation_accounts
			WHERE name = $1 AND account_type = 'telegram' AND deletion_mark = FALSE
			LIMIT 1
		`, accountName).Scan(&accountID)
		if err != nil {
			log.Warnw("automation account exists but could not fetch ID", "error", err)
			return nil
		}
	}

	// ── Channel: Telegram Chat ──────────────────────────────────────────
	channelID := id.New()
	channelName := "Metapus TG Chat"
	destination := fmt.Sprintf(`{"chat_id": "%s"}`, chatID)

	commandTag, err = pool.Exec(ctx, `
		INSERT INTO sys_automation_channels (
			id, name, account_id, destination,
			is_active, version, deletion_mark
		) VALUES ($1, $2, $3, $4::jsonb, true, 1, false)
		ON CONFLICT DO NOTHING
	`, channelID, channelName, accountID, destination)
	if err != nil {
		return fmt.Errorf("seed automation channel: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		err = pool.QueryRow(ctx, `
			SELECT id FROM sys_automation_channels
			WHERE name = $1 AND account_id = $2 AND deletion_mark = FALSE
			LIMIT 1
		`, channelName, accountID).Scan(&channelID)
		if err != nil {
			log.Warnw("automation channel exists but could not fetch ID", "error", err)
			return nil
		}
	}

	// ── Rule: Overpayment Notification ──────────────────────────────────
	ruleID := id.New()
	ruleName := "Уведомление о переплате"

	commandTag, err = pool.Exec(ctx, `
		INSERT INTO sys_automation_rules (
			id, name, description,
			trigger_type, event_type, target_entities,
			condition_cel,
			reaction_type, notif_severity, message_format, action_template,
			priority, max_retries, cooldown_seconds,
			is_active, version, deletion_mark
		) VALUES (
			$1, $2, $3,
			'entity_event', 'updated', ARRAY['crypto_invoice'],
			$4,
			'notify', 'warning', 'text', $5,
			50, 3, 0,
			true, 1, false
		)
		ON CONFLICT DO NOTHING
	`, ruleID,
		ruleName,
		"Автоматическое уведомление при переплате по крипто-инвойсу",
		`doc.overpaidAmount != "0"`,
		"⚠️ Переплата по инвойсу {{.doc.number}}!\nПолучено: {{.doc.receivedAmount}}, Ожидалось: {{.doc.expectedAmount}}.\nПереплата: {{.doc.overpaidAmount}}",
	)
	if err != nil {
		return fmt.Errorf("seed automation rule: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		err = pool.QueryRow(ctx, `
			SELECT id FROM sys_automation_rules
			WHERE name = $1 AND deletion_mark = FALSE
			LIMIT 1
		`, ruleName).Scan(&ruleID)
		if err != nil {
			log.Warnw("automation rule exists but could not fetch ID", "error", err)
			return nil
		}
	}

	// ── Subscriber: Channel → Rule ──────────────────────────────────────
	_, err = pool.Exec(ctx, `
		INSERT INTO sys_automation_subscribers (
			id, rule_id, subscriber_type, channel_id, delivery_method, idx
		) VALUES ($1, $2, 'channel', $3, 'push', 1)
		ON CONFLICT DO NOTHING
	`, id.New(), ruleID, channelID)
	if err != nil {
		return fmt.Errorf("seed automation subscriber: %w", err)
	}

	log.Infow("automation data seeded",
		"account", accountName,
		"channel", channelName,
		"rule", ruleName,
	)
	return nil
}
