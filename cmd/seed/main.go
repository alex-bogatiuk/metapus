// Package main provides a CLI tool for seeding the database with initial data.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	appCrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/platform/secretfile"
	"metapus/pkg/logger"
)

const (
	generatedCounterpartyCount = 300
)

func main() {
	log, err := logger.New(logger.Config{
		Level:       "info",
		Development: true,
	})
	if err != nil {
		fmt.Printf("failed to create logger: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Connect to database
	dbURL, configErr := secretfile.Get("DATABASE_URL", "")
	if configErr != nil {
		log.Fatalw("failed to load DATABASE_URL", "error", configErr)
	}
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	poolCfg := postgres.DefaultPoolConfig(dbURL)
	pool, err := postgres.NewPool(ctx, poolCfg)
	if err != nil {
		log.Fatalw("failed to connect to database", "error", err)
	}
	defer pool.Close()

	log.Info("connected to database")

	// Seed admin user
	adminUserID, err := seedAdminUser(ctx, pool, log)
	if err != nil {
		log.Fatalw("failed to seed admin user", "error", err)
	}

	// Seed demo data if requested
	if os.Getenv("SEED_DEMO_DATA") == "true" {
		if err := seedTenantRegistry(ctx, dbURL, log); err != nil {
			log.Warnw("failed to seed tenant registry", "error", err)
		}
		if err := seedDemoData(ctx, pool, log, adminUserID); err != nil {
			log.Fatalw("failed to seed demo data", "error", err)
		}
	}

	log.Info("seeding completed successfully")
}

func seedAdminUser(ctx context.Context, pool *postgres.Pool, log *logger.Logger) (id.ID, error) {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "admin@metapus.io"
	}

	adminPassword, err := secretfile.Get("ADMIN_PASSWORD", "")
	if err != nil {
		return id.Nil(), fmt.Errorf("load ADMIN_PASSWORD: %w", err)
	}
	if adminPassword == "" {
		return id.Nil(), fmt.Errorf("ADMIN_PASSWORD is required")
	}

	// Check if admin already exists
	var existingID id.ID
	err = pool.QueryRow(ctx,
		`SELECT id FROM auth_users WHERE email = $1 AND NOT deletion_mark`,
		adminEmail,
	).Scan(&existingID)
	if err == nil {
		log.Infow("admin user already exists", "email", adminEmail, "user_id", existingID)
		return existingID, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), fmt.Errorf("check admin exists: %w", err)
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return id.Nil(), fmt.Errorf("hash password: %w", err)
	}

	userID := id.New()
	now := time.Now()

	// Create admin user
	_, err = pool.Exec(ctx, `
		INSERT INTO auth_users (
			id, email, password_hash, first_name, last_name,
			is_active, is_admin, email_verified, email_verified_at, version
		)
		VALUES ($1, $2, $3, 'System', 'Admin', true, true, true, $4, 1)
	`, userID, adminEmail, string(passwordHash), now)
	if err != nil {
		return id.Nil(), fmt.Errorf("insert admin user: %w", err)
	}

	// Assign admin role
	var adminRoleID id.ID
	err = pool.QueryRow(ctx,
		`SELECT id FROM auth_roles WHERE code = 'admin'`,
	).Scan(&adminRoleID)
	if err != nil {
		log.Warnw("admin role not found, skipping role assignment", "error", err)
	} else {
		_, err = pool.Exec(ctx, `
			INSERT INTO auth_user_roles (user_id, role_id, granted_by)
			VALUES ($1, $2, NULL)
			ON CONFLICT (user_id, role_id) DO NOTHING
		`, userID, adminRoleID)
		if err != nil {
			log.Warnw("failed to assign admin role", "error", err)
		}
	}

	log.Infow("admin user created",
		"email", adminEmail,
		"user_id", userID,
	)

	return userID, nil
}

func seedDemoData(ctx context.Context, pool *postgres.Pool, log *logger.Logger, adminUserID id.ID) error {
	log.Info("seeding demo data...")

	// 1. Seed Organization (Root entity)
	// Required for documents in later stages
	orgID := id.New()
	orgCode := "ORG-001"
	commandTag, err := pool.Exec(ctx, `
		INSERT INTO cat_organizations (id, code, name, full_name, inn, kpp, legal_address, version, deletion_mark, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
	`, orgID, orgCode, "ООО Ромашка", "Общество с ограниченной ответственностью 'Ромашка'", "7700000001", "770001001", "г. Москва, ул. Ленина, 1")
	if err != nil {
		log.Warnw("failed to seed organization", "error", err)
	}

	orgAvailable := err == nil
	if orgAvailable && commandTag.RowsAffected() == 0 {
		err = pool.Pool.QueryRow(ctx, `
			SELECT id FROM cat_organizations WHERE code = $1 AND deletion_mark = FALSE
		`, orgCode).Scan(&orgID)
		if err != nil {
			log.Warnw("failed to fetch existing organization", "code", orgCode, "error", err)
			orgAvailable = false
		}
	}

	// Organization access is now managed via Security Profiles (RLS dimensions).
	// Admin user bypasses RLS via IsAdmin flag, no explicit org link needed.

	// 2. Seed Currencies
	currencies := []struct {
		name            string
		isoCode         string
		symbol          string
		decimalPlaces   int
		minorMultiplier int64
		isBase          bool
	}{
		{"Российский рубль", "RUB", "₽", 2, 100, true},
		{"Доллар США", "USD", "$", 2, 100, false},
		{"Евро", "EUR", "€", 2, 100, false},
		{"Tether (USDT)", "USDT", "₮", 6, 1000000, false},
		{"TRON", "TRX", "TRX", 6, 1000000, false},
		{"Bitcoin", "BTC", "₿", 8, 100000000, false},
		{"Ethereum", "ETH", "Ξ", 18, 1000000000000000000, false},
	}

	currencyIDs := make(map[string]id.ID)

	for _, c := range currencies {
		currID := id.New()
		commandTag, err := pool.Exec(ctx, `
			INSERT INTO cat_currencies (
				id, code, name, iso_code, symbol,
				decimal_places, minor_multiplier, is_base,
				version, deletion_mark, attributes
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, currID, c.isoCode, c.name, c.isoCode, c.symbol, c.decimalPlaces, c.minorMultiplier, c.isBase)
		if err != nil {
			log.Warnw("failed to seed currency", "name", c.name, "error", err)
			continue
		}

		if commandTag.RowsAffected() == 0 {
			err = pool.QueryRow(ctx, `
				SELECT id FROM cat_currencies WHERE code = $1 AND deletion_mark = FALSE
			`, c.isoCode).Scan(&currID)
			if err != nil {
				log.Warnw("failed to fetch existing currency id", "iso", c.isoCode, "error", err)
				continue
			}
		}

		currencyIDs[c.isoCode] = currID
	}

	// 3b. Seed Rate Sources (cat_rate_sources)
	rateSourceCoinGeckoID := id.New()
	rateSourceManualID := id.New()
	{
		rateSources := []struct {
			id         id.ID
			code       string
			name       string
			sourceType string
			baseURL    string
			priority   int
		}{
			{rateSourceCoinGeckoID, "coingecko", "CoinGecko", "coingecko", "https://api.coingecko.com/api/v3", 10},
			{rateSourceManualID, "manual", "Ручной ввод", "manual", "", 100},
		}

		for _, rs := range rateSources {
			var baseURL any
			if rs.baseURL != "" {
				baseURL = rs.baseURL
			}
			_, err := pool.Exec(ctx, `
				INSERT INTO cat_rate_sources (
					id, code, name, source_type, base_url, priority, is_active,
					version, deletion_mark, attributes
				)
				VALUES ($1, $2, $3, $4, $5, $6, TRUE, 1, FALSE, '{}')
				ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
			`, rs.id, rs.code, rs.name, rs.sourceType, baseURL, rs.priority)
			if err != nil {
				log.Warnw("failed to seed rate source", "code", rs.code, "error", err)
			}
		}
		// Resolve existing IDs if conflict
		_ = pool.QueryRow(ctx, `SELECT id FROM cat_rate_sources WHERE code = 'coingecko' AND deletion_mark = FALSE`).Scan(&rateSourceCoinGeckoID)
		_ = pool.QueryRow(ctx, `SELECT id FROM cat_rate_sources WHERE code = 'manual' AND deletion_mark = FALSE`).Scan(&rateSourceManualID)
		log.Info("rate sources seeded (coingecko, manual)")
	}

	// 3c. Seed Rate Source Mappings (reg_rate_source_mappings)
	{
		usdID, hasUSD := currencyIDs["USD"]
		if !hasUSD {
			_ = pool.QueryRow(ctx, `SELECT id FROM cat_currencies WHERE code = 'USD' AND deletion_mark = FALSE`).Scan(&usdID)
		}
		if !id.IsNil(usdID) {
			mappings := []struct {
				currencyISO string
				externalID  string // CoinGecko coin ID
			}{
				{"USDT", "tether"},
				{"BTC", "bitcoin"},
				{"ETH", "ethereum"},
			}

			for _, m := range mappings {
				currID, ok := currencyIDs[m.currencyISO]
				if !ok {
					continue
				}
				_, err := pool.Exec(ctx, `
					INSERT INTO reg_rate_source_mappings (currency_id, base_currency_id, rate_source_id, external_id, is_active)
					VALUES ($1, $2, $3, $4, TRUE)
					ON CONFLICT (currency_id, base_currency_id, rate_source_id) DO UPDATE SET external_id = EXCLUDED.external_id
				`, currID, usdID, rateSourceCoinGeckoID, m.externalID)
				if err != nil {
					log.Warnw("failed to seed rate source mapping", "currency", m.currencyISO, "error", err)
				}
			}
			log.Info("rate source mappings seeded (USDT→tether, BTC→bitcoin, ETH→ethereum)")
		}
	}

	// 3d. Update organization base_currency_id
	if orgAvailable && !id.IsNil(orgID) {
		if rubID, ok := currencyIDs["RUB"]; ok {
			_, err := pool.Exec(ctx, `
				UPDATE cat_organizations SET base_currency_id = $1 WHERE id = $2
			`, rubID, orgID)
			if err != nil {
				log.Warnw("failed to set organization base currency", "error", err)
			}
		}
	}

	// 4. Seed Counterparties
	counterpartyIDs, err := seedGeneratedCounterparties(ctx, pool, log)
	if err != nil {
		return err
	}

	// 6. Seed Contracts
	contracts := []struct {
		name            string
		counterpartKey  string // key in counterpartyIDs map
		contractType    string // supply, sale, other
		currencyISOCode string // key in currencyIDs map
	}{
		{"Договор процессинга", "supplier", "supply", "RUB"},
		{"Договор эквайринга", "customer", "sale", "RUB"},
		{"Договор обслуживания (USD)", "both", "other", "USD"},
	}

	for i, ct := range contracts {
		ctID := id.New()
		code := fmt.Sprintf("CTR-%03d", i+1)

		cpID, cpOk := counterpartyIDs[ct.counterpartKey]
		if !cpOk {
			log.Warnw("skipping contract: counterparty not found", "key", ct.counterpartKey)
			continue
		}

		var currIDValue any
		if cID, ok := currencyIDs[ct.currencyISOCode]; ok {
			currIDValue = cID
		}

		_, err := pool.Exec(ctx, `
			INSERT INTO cat_contracts (
				id, code, name, counterparty_id, type, currency_id,
				payment_term_days, version, deletion_mark, attributes
			)
			VALUES ($1, $2, $3, $4, $5, $6, 30, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, ctID, code, ct.name, cpID, ct.contractType, currIDValue)
		if err != nil {
			log.Warnw("failed to seed contract", "name", ct.name, "error", err)
		}
	}

	// 5. Seed Crypto Processing data (Networks, Tokens, Merchants, Wallets)
	err = seedCryptoData(ctx, pool, log, currencyIDs)
	if err != nil {
		return err
	}

	// 9. Seed Automation data (TG Account, Channel, Overpayment Rule)
	if err := seedAutomationData(ctx, pool, log); err != nil {
		return err
	}

	log.Info("demo data seeded successfully")
	return nil
}

func seedGeneratedCounterparties(ctx context.Context, pool *postgres.Pool, log *logger.Logger) (map[string]id.ID, error) {
	typesList := []string{"supplier", "customer", "both"}
	companyPrefixes := []string{"Альфа", "Бета", "Вектор", "Гарант", "Профи", "Север", "Восток", "Глобал", "Оптима", "Премьер"}
	companyDomains := []string{"Снабжение", "Трейд", "Логистик", "Поставка", "Ресурс", "Комплект", "Маркет", "Сервис", "Инвест", "Партнёр", "Транс", "Авиа"}
	companyRegions := []string{"Столица", "Волга", "Урал", "Сибирь", "Дальний Восток", "Северный Кавказ", "Юг", "Запад", "Восток", "Центр"}
	surnames := []string{"Иванов", "Петров", "Сидоров", "Смирнов", "Кузнецов", "Попов", "Соколов", "Лебедев", "Новиков", "Фёдоров", "Козлов", "Морозов", "Волков", "Алексеев", "Семёнов", "Егоров", "Павлов", "Ковалёв", "Орлов"}
	firstNames := []string{"Иван", "Алексей", "Дмитрий", "Сергей", "Андрей", "Павел", "Николай", "Роман", "Егор", "Максим", "Владимир", "Михаил", "Александр", "Евгений", "Виктор", "Олег", "Игорь", "Денис", "Антон", "Кирилл"}
	middleNames := []string{"Иванович", "Петрович", "Алексеевич", "Сергеевич", "Андреевич", "Павлович", "Николаевич", "Романович", "Егорович", "Максимович"}
	cities := []string{"Москва", "Санкт-Петербург", "Бобруйск", "Казань", "Екатеринбург", "Новосибирск", "Самара", "Нижний Новгород", "Челябинск", "Краснодар", "Ростов-на-Дону", "Минск", "Брест", "Витебск", "Гомель", "Гродно", "Могилёв"}
	counterpartyIDs := make(map[string]id.ID, len(typesList))

	// Collect data for all counterparties first, then batch-insert via pgx.Batch.
	// This sends all INSERTs in a single network round-trip (1 instead of 300).
	type cpRow struct {
		id    id.ID
		code  string
		ctype string
	}
	rows := make([]cpRow, 0, generatedCounterpartyCount)
	batch := &pgx.Batch{}

	for i := 1; i <= generatedCounterpartyCount; i++ {
		cpID := id.New()
		code := fmt.Sprintf("CP-GEN-%03d", i)
		ctype := typesList[(i-1)%len(typesList)]
		mode := (i - 1) % 4
		city := cities[(i-1)%len(cities)]
		surname := surnames[(i-1)%len(surnames)]
		firstName := firstNames[((i-1)/len(surnames))%len(firstNames)]
		middleName := middleNames[((i-1)/(len(surnames)*len(firstNames)))%len(middleNames)]

		legalForm := "company"
		name := ""
		fullName := ""
		inn := ""
		var kpp any
		var ogrn any
		contactPerson := any(fmt.Sprintf("%s %s", surname, firstName))

		switch mode {
		case 0, 1:
			prefix := companyPrefixes[(i-1)%len(companyPrefixes)]
			domain := companyDomains[((i-1)/len(companyPrefixes))%len(companyDomains)]
			region := companyRegions[((i-1)/(len(companyPrefixes)*len(companyDomains)))%len(companyRegions)]
			baseName := fmt.Sprintf("%s %s %s", prefix, domain, region)
			if mode == 0 {
				name = fmt.Sprintf("ООО \"%s\"", baseName)
				fullName = fmt.Sprintf("Общество с ограниченной ответственностью \"%s\"", baseName)
			} else {
				name = fmt.Sprintf("АО \"%s\"", baseName)
				fullName = fmt.Sprintf("Акционерное общество \"%s\"", baseName)
			}
			inn = fmt.Sprintf("77%08d", i)
			kpp = fmt.Sprintf("%04d%05d", 7700+((i-1)%200), (i*37)%100000)
			ogrn = fmt.Sprintf("10%011d", i)
		case 2:
			legalForm = "sole_trader"
			name = fmt.Sprintf("ИП %s %s. %s.", surname, string([]rune(firstName)[0]), string([]rune(middleName)[0]))
			fullName = fmt.Sprintf("Индивидуальный предприниматель %s %s %s", surname, firstName, middleName)
			inn = fmt.Sprintf("77%010d", i)
			ogrn = fmt.Sprintf("30%013d", i)
		case 3:
			legalForm = "individual"
			name = fmt.Sprintf("%s %s %s", surname, firstName, middleName)
			fullName = name
			inn = fmt.Sprintf("50%010d", i)
			contactPerson = any(name)
		}

		email := fmt.Sprintf("cp%03d@seed.metapus.io", i)
		phone := fmt.Sprintf("+7 (9%02d) %03d-%02d-%02d", 10+((i-1)%90), (i*37)%1000, (i*13)%100, (i*29)%100)
		legalAddress := fmt.Sprintf("г. %s, ул. %s, д. %d", city, companyDomains[(i-1)%len(companyDomains)], (i%120)+1)
		actualAddress := fmt.Sprintf("г. %s, пр-т %s, д. %d", city, companyPrefixes[(i-1)%len(companyPrefixes)], (i%90)+1)

		batch.Queue(`
			INSERT INTO cat_counterparties (
				id, code, name, type, legal_form, inn, kpp, ogrn, full_name,
				legal_address, actual_address, phone, email, contact_person,
				version, deletion_mark, attributes
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, cpID, code, name, ctype, legalForm, inn, kpp, ogrn, fullName, legalAddress, actualAddress, phone, email, contactPerson)

		rows = append(rows, cpRow{id: cpID, code: code, ctype: ctype})
	}

	// Execute entire batch in a single network round-trip.
	results := pool.SendBatch(ctx, batch)
	for _, row := range rows {
		commandTag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return nil, fmt.Errorf("insert counterparty %s: %w", row.code, err)
		}
		if commandTag.RowsAffected() > 0 {
			counterpartyIDs[row.ctype] = row.id
		}
	}
	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("close counterparty batch: %w", err)
	}

	// For rows that were skipped (ON CONFLICT), fetch existing IDs in one query.
	for _, tp := range typesList {
		if _, ok := counterpartyIDs[tp]; !ok {
			// Find any generated counterparty of this type.
			var cpID id.ID
			err := pool.QueryRow(ctx, `
				SELECT id FROM cat_counterparties
				WHERE code LIKE 'CP-GEN-%' AND type = $1 AND deletion_mark = FALSE
				LIMIT 1
			`, tp).Scan(&cpID)
			if err != nil {
				return nil, fmt.Errorf("fetch existing counterparty type %s: %w", tp, err)
			}
			counterpartyIDs[tp] = cpID
		}
	}

	log.Infow("counterparties seeded (batch)", "count", generatedCounterpartyCount)
	return counterpartyIDs, nil
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
			confirmations_needed, explorer_url, is_active,
			version, deletion_mark, attributes
		) VALUES ($1, $2, $3, 'tron', $4, $5, $6, $7, $8, $9, true, 1, false, '{}')
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

func seedTenantRegistry(ctx context.Context, dbURL string, log *logger.Logger) error {
	metaDSN, err := secretfile.Get("META_DATABASE_URL", "")
	if err != nil {
		return fmt.Errorf("load META_DATABASE_URL: %w", err)
	}
	if metaDSN == "" {
		log.Warn("META_DATABASE_URL is not set; skipping tenant registry seed")
		return nil
	}

	metaPool, err := postgres.OpenPool(ctx, metaDSN)
	if err != nil {
		return fmt.Errorf("connect meta database: %w", err)
	}
	defer metaPool.Close()

	if err := metaPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping meta database: %w", err)
	}

	tenantSlug := os.Getenv("TENANT_SLUG")
	if tenantSlug == "" {
		tenantSlug = "demo"
	}

	tenantName := os.Getenv("TENANT_NAME")
	if tenantName == "" {
		tenantName = "Demo Tenant"
	}

	tenantPlan := os.Getenv("TENANT_PLAN")
	if tenantPlan == "" {
		tenantPlan = string(tenant.PlanStandard)
	}

	dbConfig, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return fmt.Errorf("parse tenant database url: %w", err)
	}

	dbHost := dbConfig.ConnConfig.Host
	if dbHost == "" {
		dbHost = "localhost"
	}

	dbPort := int(dbConfig.ConnConfig.Port)
	if dbPort == 0 {
		dbPort = 5432
	}

	dbName := dbConfig.ConnConfig.Database
	if dbName == "" {
		dbName = "metapus"
	}

	var existingID, existingSlug string
	err = metaPool.QueryRow(ctx, `
		SELECT id, slug
		FROM tenants
		WHERE db_name = $1
	`, dbName).Scan(&existingID, &existingSlug)
	if err == nil {
		log.Infow(
			"tenant database already exists in registry",
			"slug", existingSlug,
			"tenant_id", existingID,
			"database", dbName,
		)
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check tenant database exists: %w", err)
	}

	err = metaPool.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = $1`, tenantSlug).Scan(&existingID)
	if err == nil {
		log.Infow("tenant already exists in registry", "slug", tenantSlug, "tenant_id", existingID)
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check tenant exists: %w", err)
	}

	registry := tenantdb.NewPostgresRegistry(metaPool)
	newTenant := &tenant.Tenant{
		Slug:        tenantSlug,
		DisplayName: tenantName,
		DBName:      dbName,
		DBHost:      dbHost,
		DBPort:      dbPort,
		Status:      tenant.StatusActive,
		Plan:        tenant.Plan(tenantPlan),
		Settings:    map[string]any{},
	}

	if err := registry.Create(ctx, newTenant); err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}

	log.Infow("tenant seeded in registry", "slug", tenantSlug, "tenant_id", newTenant.ID)
	return nil
}
