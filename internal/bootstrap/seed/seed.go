// Package seed is the platform's seed command, run for one configuration
// (cmd/seed for the empty one, a configuration's own command for it). It
// creates the administrator and, with SEED_DEMO_DATA=true, the platform's
// demo data, then hands the tenant database to the configuration's Seed.
package seed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"metapus/internal/configuration"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/demoseed"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/platform/secretfile"
	"metapus/pkg/logger"
)

const (
	generatedCounterpartyCount = 300
)

// Main seeds the tenant database at DATABASE_URL for cfg.
func Main(cfg configuration.Configuration) {
	if err := configuration.CheckAPIVersion(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

	env := configuration.SeedEnv{Logger: log, Pool: pool, AdminUserID: adminUserID}
	// Seed demo data if requested
	if os.Getenv("SEED_DEMO_DATA") == "true" {
		env.Demo = true
		if err := seedTenantRegistry(ctx, dbURL, cfg.Name(), log); err != nil {
			log.Warnw("failed to seed tenant registry", "error", err)
		}
		if env.Currencies, env.RateSources, err = seedDemoData(ctx, pool, log, adminUserID); err != nil {
			log.Fatalw("failed to seed demo data", "error", err)
		}
	}
	if err := cfg.Seed(ctx, env); err != nil {
		log.Fatalw("failed to seed the configuration's data", "configuration", cfg.Name(), "error", err)
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

// seedDemoData seeds the platform's demo data and returns the IDs of the
// seeded currencies by ISO code and of the rate sources by code.
func seedDemoData(ctx context.Context, pool *postgres.Pool, log *logger.Logger, adminUserID id.ID) (map[string]id.ID, map[string]id.ID, error) {
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
	}

	currencyIDs := make(map[string]id.ID)

	for _, c := range currencies {
		currID, err := demoseed.EnsureCurrency(ctx, pool, demoseed.Currency{
			ISOCode: c.isoCode, Name: c.name, Symbol: c.symbol,
			DecimalPlaces: c.decimalPlaces, MinorMultiplier: c.minorMultiplier, IsBase: c.isBase,
		})
		if err != nil {
			log.Warnw("failed to seed currency", "name", c.name, "error", err)
			continue
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
		return nil, nil, err
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

	log.Info("demo data seeded successfully")
	return currencyIDs, map[string]id.ID{"coingecko": rateSourceCoinGeckoID, "manual": rateSourceManualID}, nil
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

func seedTenantRegistry(ctx context.Context, dbURL, configurationName string, log *logger.Logger) error {
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
		Configuration: configurationName,
		Slug:          tenantSlug,
		DisplayName:   tenantName,
		DBName:        dbName,
		DBHost:        dbHost,
		DBPort:        dbPort,
		Status:        tenant.StatusActive,
		Plan:          tenant.Plan(tenantPlan),
		Settings:      map[string]any{},
	}

	if err := registry.Create(ctx, newTenant); err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}

	log.Infow("tenant seeded in registry", "slug", tenantSlug, "tenant_id", newTenant.ID)
	return nil
}
