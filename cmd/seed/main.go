// Package main provides a CLI tool for seeding the database with initial data.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	appCrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

const (
	generatedCounterpartyCount = 300
	generatedNomenclatureCount = 300
	generatedGoodsReceiptCount = 2000

	// Crypto document generation counts — enough for dashboard charts.
	_generatedCryptoInvoiceCount    = 500
	_generatedCryptoWithdrawalCount = 80
	_cryptoDocBatchSize             = 100
)

type generatedCounterparty struct {
	ID   id.ID
	Name string
	Type string
}

type generatedProduct struct {
	ID     id.ID
	Name   string
	UnitID id.ID
}

type generatedWarehouse struct {
	ID   id.ID
	Name string
}

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
	dbURL := os.Getenv("DATABASE_URL")
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

	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "Admin123!"
	}

	// Check if admin already exists
	var existingID id.ID
	err := pool.QueryRow(ctx,
		`SELECT id FROM users WHERE email = $1 AND NOT deletion_mark`,
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
		INSERT INTO users (
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
		`SELECT id FROM roles WHERE code = 'admin'`,
	).Scan(&adminRoleID)
	if err != nil {
		log.Warnw("admin role not found, skipping role assignment", "error", err)
	} else {
		_, err = pool.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, granted_by)
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

	// 2. Seed Units
	// We need to capture IDs to use them in Nomenclature
	type unitSeed struct {
		name   string
		symbol string
		uType  string // piece, weight, length, etc.
	}

	units := []unitSeed{
		{"Штука", "шт", "piece"},
		{"Килограмм", "кг", "weight"},
		{"Литр", "л", "volume"},
		{"Метр", "м", "length"},
		{"Упаковка", "уп", "pack"},
	}

	// Map symbol -> UUID for nomenclature reference
	unitIDs := make(map[string]id.ID)

	for _, u := range units {
		uid := id.New()
		// Try to insert
		commandTag, err := pool.Exec(ctx, `
			INSERT INTO cat_units (id, code, name, symbol, type, is_base, conversion_factor, version, deletion_mark, attributes)
			VALUES ($1, $2, $3, $4, $5, true, 1, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, uid, u.symbol, u.name, u.symbol, u.uType)

		if err != nil {
			log.Warnw("failed to seed unit", "name", u.name, "error", err)
			continue
		}

		// If inserted, use new ID. If conflict, we need to fetch existing ID.
		if commandTag.RowsAffected() == 0 {
			err = pool.QueryRow(ctx, `
				SELECT id FROM cat_units 
				WHERE code = $1 AND deletion_mark = FALSE
			`, u.symbol).Scan(&uid)
			if err != nil {
				log.Warnw("failed to fetch existing unit id", "symbol", u.symbol, "error", err)
				continue
			}
		}

		unitIDs[u.symbol] = uid
	}

	// 3. Seed Currencies
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
		{"Bitcoin", "BTC", "₿", 8, 100000000, false},
		{"Ethereum", "ETH", "Ξ", 9, 1000000000, false},
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

	// 3a. Update organization base_currency_id
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

	// 4. Seed Warehouses
	// We now support multiple warehouses, just like in ERP systems.
	warehouses := []struct {
		name      string
		address   string
		wType     string
		isDefault bool // New field
	}{
		{"Основной склад", "г. Москва, ул. Складская, д. 1", "main", true},
		{"Розничный магазин", "г. Москва, ул. Торговая, д. 5", "retail", false},
		{"Транзитный склад", "Виртуальный", "transit", false},
	}

	for i, w := range warehouses {
		whID := id.New()
		code := fmt.Sprintf("WH-%03d", i+1)
		var orgIDValue interface{}
		if orgAvailable && !id.IsNil(orgID) {
			orgIDValue = orgID
		}
		_, err := pool.Exec(ctx, `
            INSERT INTO cat_warehouses (id, code, name, address, type, organization_id, is_default, version, deletion_mark, attributes)
            VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
            ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
        `, whID, code, w.name, w.address, w.wType, orgIDValue, w.isDefault)

		// Special handling for is_default:
		// If we try to insert a second default (by chance), the DB will throw error 23505
		// In production code, we'd need logic to unset others, but for the seeder, skipping is enough.
		if err != nil {
			// Ignore uniqueness error for default if the seeder is re-run with different data
			log.Warnw("failed to seed warehouse", "name", w.name, "error", err)
		}
	}

	// 5. Seed Counterparties
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
		{"Договор поставки канцтоваров", "supplier", "supply", "RUB"},
		{"Договор продажи (розница)", "customer", "sale", "RUB"},
		{"Договор (USD)", "both", "other", "USD"},
	}

	for i, ct := range contracts {
		ctID := id.New()
		code := fmt.Sprintf("CTR-%03d", i+1)

		cpID, cpOk := counterpartyIDs[ct.counterpartKey]
		if !cpOk {
			log.Warnw("skipping contract: counterparty not found", "key", ct.counterpartKey)
			continue
		}

		var currIDValue interface{}
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

	// 7. Seed Nomenclature
	// Fetch default VAT rate (VAT 20%) — seeded by migration 00016_cat_vat_rates
	var defaultVatRateID id.ID
	err = pool.QueryRow(ctx, `
		SELECT id FROM cat_vat_rates WHERE code = 'VR-001' AND deletion_mark = FALSE
	`).Scan(&defaultVatRateID)
	if err != nil {
		log.Warnw("failed to fetch default VAT rate, nomenclature will have NULL default_vat_rate_id", "error", err)
	}

	if id.IsNil(defaultVatRateID) {
		return fmt.Errorf("default VAT rate VR-001 not found")
	}

	if err := seedGeneratedNomenclature(ctx, pool, log, unitIDs, defaultVatRateID); err != nil {
		return err
	}

	rubID, ok := currencyIDs["RUB"]
	if !ok || id.IsNil(rubID) {
		return fmt.Errorf("RUB currency not found")
	}
	if !orgAvailable || id.IsNil(orgID) {
		return fmt.Errorf("organization ORG-001 not found")
	}
	if err := seedGeneratedGoodsReceipts(ctx, pool, log, adminUserID, orgID, rubID, defaultVatRateID); err != nil {
		return err
	}

	// 8. Seed Crypto Processing data (Networks, Tokens, Merchants, Wallets)
	cryptoRefs, err := seedCryptoData(ctx, pool, log)
	if err != nil {
		return err
	}

	// 8a. Seed Crypto Documents (Invoices, Payments, Withdrawals) for dashboard charts
	if err := seedCryptoDocuments(ctx, pool, log, cryptoRefs); err != nil {
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

func seedGeneratedNomenclature(ctx context.Context, pool *postgres.Pool, log *logger.Logger, unitIDs map[string]id.ID, defaultVatRateID id.ID) error {
	templates := []struct {
		name       string
		unitSymbol string
	}{
		{name: "Бумага офисная А4", unitSymbol: "уп"},
		{name: "Ручка шариковая", unitSymbol: "шт"},
		{name: "Маркер перманентный", unitSymbol: "шт"},
		{name: "Папка-регистратор", unitSymbol: "шт"},
		{name: "Картридж лазерный", unitSymbol: "шт"},
		{name: "Кабель силовой", unitSymbol: "м"},
		{name: "Лампа светодиодная", unitSymbol: "шт"},
		{name: "Перчатки рабочие", unitSymbol: "уп"},
		{name: "Клей монтажный", unitSymbol: "шт"},
		{name: "Розетка электрическая", unitSymbol: "шт"},
		{name: "Выключатель одноклавишный", unitSymbol: "шт"},
		{name: "Труба полипропиленовая", unitSymbol: "м"},
		{name: "Смеситель кухонный", unitSymbol: "шт"},
		{name: "Шуруп универсальный", unitSymbol: "уп"},
		{name: "Краска интерьерная", unitSymbol: "л"},
		{name: "Герметик санитарный", unitSymbol: "шт"},
		{name: "Насос циркуляционный", unitSymbol: "шт"},
		{name: "Автоматический выключатель", unitSymbol: "шт"},
		{name: "Коврик диэлектрический", unitSymbol: "шт"},
		{name: "Лента изоляционная", unitSymbol: "шт"},
		{name: "Пакет полиэтиленовый", unitSymbol: "шт"},
		{name: "Ведро строительное", unitSymbol: "шт"},
		{name: "Уровень строительный", unitSymbol: "шт"},
		{name: "Мастерок", unitSymbol: "шт"},
		{name: "Шпатель малярный", unitSymbol: "шт"},
		{name: "Мастика битумная", unitSymbol: "кг"},
		{name: "Масло моторное", unitSymbol: "л"},
		{name: "Лом", unitSymbol: "шт"},
		{name: "Молоток", unitSymbol: "шт"},
		{name: "Отвертка", unitSymbol: "шт"},
		{name: "Плоскогубцы", unitSymbol: "шт"},
		{name: "Кусачки", unitSymbol: "шт"},
		{name: "Ключ гаечный", unitSymbol: "шт"},
		{name: "Ключ разводной", unitSymbol: "шт"},
		{name: "Ключ торцевой", unitSymbol: "шт"},
		{name: "Ключ накидной", unitSymbol: "шт"},
		{name: "Ключ рожковый", unitSymbol: "шт"},
		{name: "Ключ комбинированный", unitSymbol: "шт"},
		{name: "Ключ трубный", unitSymbol: "шт"},
		{name: "Ключ разводной", unitSymbol: "шт"},
		{name: "Ключ торцевой", unitSymbol: "шт"},
		{name: "Ключ накидной", unitSymbol: "шт"},
		{name: "Ключ рожковый", unitSymbol: "шт"},
		{name: "Ключ комбинированный", unitSymbol: "шт"},
		{name: "Ключ трубный", unitSymbol: "шт"},
	}
	brands := []string{"NordLine", "Volta", "OfficePro", "StroyMax", "PrimeTech"}
	series := []string{"Базовая серия", "Проф серия", "Комфорт серия"}
	countries := []string{"RU", "BY", "KZ", "CN", "TR"}

	// Batch-insert all nomenclature via pgx.Batch (single network round-trip).
	batch := &pgx.Batch{}

	for i := 1; i <= generatedNomenclatureCount; i++ {
		prodID := id.New()
		code := fmt.Sprintf("NM-GEN-%03d", i)
		template := templates[(i-1)%len(templates)]
		brand := brands[((i-1)/len(templates))%len(brands)]
		serie := series[((i-1)/(len(templates)*len(brands)))%len(series)]
		name := fmt.Sprintf("%s %s %s", template.name, brand, serie)
		article := fmt.Sprintf("ART-%03d-%02d", i, ((i-1)%97)+1)
		barcode := fmt.Sprintf("469%010d", i)
		unitID, ok := unitIDs[template.unitSymbol]
		if !ok {
			unitID = unitIDs["шт"]
		}
		description := fmt.Sprintf("%s, торговая марка %s", template.name, brand)
		country := countries[(i-1)%len(countries)]
		isWeighed := template.unitSymbol == "кг" || template.unitSymbol == "л"
		trackBatch := template.unitSymbol == "л" || template.unitSymbol == "уп"

		batch.Queue(`
			INSERT INTO cat_nomenclatures (
				id, code, name, type, article, barcode, base_unit_id, default_vat_rate_id,
				description, country_of_origin, is_weighed, track_batch,
				version, deletion_mark, attributes
			)
			VALUES ($1, $2, $3, 'goods', $4, $5, $6, $7, $8, $9, $10, $11, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, prodID, code, name, article, barcode, unitID, defaultVatRateID, description, country, isWeighed, trackBatch)
	}

	// Execute entire batch in a single network round-trip.
	results := pool.SendBatch(ctx, batch)
	for i := 1; i <= generatedNomenclatureCount; i++ {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("insert nomenclature NM-GEN-%03d: %w", i, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close nomenclature batch: %w", err)
	}

	log.Infow("nomenclature seeded (batch)", "count", generatedNomenclatureCount)
	return nil
}

// goodsReceiptBatchSize controls how many documents are inserted per transaction batch.
// Each batch uses CopyFromSlice for headers and lines — dramatically reducing round-trips.
const goodsReceiptBatchSize = 100

func seedGeneratedGoodsReceipts(ctx context.Context, pool *postgres.Pool, log *logger.Logger, adminUserID, orgID, currencyID, defaultVatRateID id.ID) error {
	existingNumbers, err := loadExistingSeededGoodsReceiptNumbers(ctx, pool)
	if err != nil {
		return err
	}
	if len(existingNumbers) >= generatedGoodsReceiptCount {
		log.Infow("goods receipts already seeded", "count", len(existingNumbers))
		return nil
	}

	suppliers, err := loadGeneratedSuppliers(ctx, pool)
	if err != nil {
		return err
	}
	if len(suppliers) == 0 {
		return fmt.Errorf("no generated suppliers found")
	}

	products, err := loadGeneratedProducts(ctx, pool)
	if err != nil {
		return err
	}
	if len(products) == 0 {
		return fmt.Errorf("no generated nomenclature found")
	}

	warehouses, err := loadWarehouses(ctx, pool)
	if err != nil {
		return err
	}
	if len(warehouses) == 0 {
		return fmt.Errorf("no warehouses found")
	}

	txm := postgres.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rng := rand.New(rand.NewSource(20260306))
	created := 0

	// Build all documents in memory first, then flush in batches.
	var pendingDocs []*goods_receipt.GoodsReceipt

	for i := 1; i <= generatedGoodsReceiptCount; i++ {
		number := fmt.Sprintf("GR-SEED-%05d", i)
		if existingNumbers[number] {
			continue
		}

		supplier := suppliers[rng.Intn(len(suppliers))]
		warehouse := warehouses[rng.Intn(len(warehouses))]
		docDate := time.Now().UTC().AddDate(0, 0, -rng.Intn(540))
		supplierDocDate := docDate.AddDate(0, 0, -rng.Intn(10))
		incomingNumber := fmt.Sprintf("IN-SEED-%05d", i)

		builder := goods_receipt.NewBuilder(orgID, supplier.ID, warehouse.ID).
			WithNumber(number).
			WithDate(docDate).
			WithCurrency(currencyID).
			WithSupplierDoc(fmt.Sprintf("SUP-%04d-%05d", rng.Intn(9000)+1000, i), &supplierDocDate).
			WithIncomingNumber(incomingNumber).
			WithCreatedBy(adminUserID).
			WithDescription(fmt.Sprintf("Поступление на %s от %s", warehouse.Name, supplier.Name))

		lineCount := 2 + rng.Intn(5)
		usedProducts := make(map[string]struct{}, lineCount)
		for lineNo := 0; lineNo < lineCount; lineNo++ {
			product := products[rng.Intn(len(products))]
			for len(usedProducts) < len(products) {
				if _, exists := usedProducts[product.ID.String()]; !exists {
					break
				}
				product = products[rng.Intn(len(products))]
			}
			usedProducts[product.ID.String()] = struct{}{}

			quantity := 1 + rng.Intn(48)
			unitPrice := types.MinorUnits(500 + rng.Intn(149500))
			builder.AddLine(product.ID, product.UnitID, quantity, unitPrice, defaultVatRateID, 20)
		}

		doc := builder.MustBuild()
		if err := doc.Validate(ctx); err != nil {
			return fmt.Errorf("validate goods receipt %s: %w", number, err)
		}

		pendingDocs = append(pendingDocs, doc)

		// Flush batch when full.
		if len(pendingDocs) >= goodsReceiptBatchSize {
			if err := flushGoodsReceiptBatch(ctx, txm, pendingDocs); err != nil {
				return err
			}
			created += len(pendingDocs)
			pendingDocs = pendingDocs[:0]

			if created%500 == 0 {
				log.Infow("goods receipts seeding progress", "created", created, "target", generatedGoodsReceiptCount)
			}
		}
	}

	// Flush remaining.
	if len(pendingDocs) > 0 {
		if err := flushGoodsReceiptBatch(ctx, txm, pendingDocs); err != nil {
			return err
		}
		created += len(pendingDocs)
	}

	log.Infow("goods receipts seeded (batch)", "created", created, "target", generatedGoodsReceiptCount)
	return nil
}

// flushGoodsReceiptBatch inserts a batch of documents and their lines
// in a single transaction using CopyFromSlice (COPY protocol).
// This replaces N individual transactions with 1, and N individual INSERTs
// with 2 COPY operations (headers + lines).
func flushGoodsReceiptBatch(ctx context.Context, txm *postgres.TxManager, docs []*goods_receipt.GoodsReceipt) error {
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		inserter := postgres.NewBatchInserter(txm)

		// 1. COPY document headers
		headerCols := []string{
			"id", "number", "date", "posted", "posted_version",
			"organization_id", "basis_type", "basis_id", "description",
			"counterparty_id", "contract_id", "warehouse_id",
			"supplier_doc_number", "supplier_doc_date", "incoming_number",
			"currency_id", "amount_includes_vat",
			"total_quantity", "total_amount", "total_vat",
			"deletion_mark", "version", "attributes",
			"created_at", "updated_at", "created_by", "updated_by",
		}
		headerRows := make([][]any, 0, len(docs))
		for _, doc := range docs {
			headerRows = append(headerRows, []any{
				doc.ID, doc.Number, doc.Date, doc.Posted, doc.PostedVersion,
				doc.OrganizationID, doc.BasisType, doc.BasisID, doc.Description,
				doc.CounterpartyID, doc.ContractID, doc.WarehouseID,
				doc.SupplierDocNumber, doc.SupplierDocDate, doc.IncomingNumber,
				doc.CurrencyID, doc.AmountIncludesVAT,
				doc.TotalQuantity, doc.TotalAmount, doc.TotalVAT,
				doc.DeletionMark, doc.Version, doc.Attributes,
				doc.CreatedAt, doc.UpdatedAt, doc.CreatedBy, doc.UpdatedBy,
			})
		}
		if _, err := inserter.CopyFromSlice(txCtx, "doc_goods_receipts", headerCols, headerRows); err != nil {
			return fmt.Errorf("copy goods receipt headers: %w", err)
		}

		// 2. COPY all lines from all documents in the batch.
		lineCols := []string{
			"line_id", "document_id", "line_no", "nomenclature_id",
			"unit_id", "coefficient",
			"quantity", "unit_price",
			"discount_percent", "discount_amount",
			"vat_rate_id", "vat_percent", "vat_amount", "amount",
		}
		// Pre-count total lines for allocation.
		totalLines := 0
		for _, doc := range docs {
			totalLines += len(doc.Lines)
		}
		lineRows := make([][]any, 0, totalLines)
		for _, doc := range docs {
			for _, line := range doc.Lines {
				lineRows = append(lineRows, []any{
					line.LineID, doc.ID, line.LineNo, line.NomenclatureID,
					line.UnitID, line.Coefficient,
					line.Quantity, line.UnitPrice,
					line.DiscountPercent, line.DiscountAmount,
					line.VATRateID, line.VATPercent, line.VATAmount, line.Amount,
				})
			}
		}
		if _, err := inserter.CopyFromSlice(txCtx, "doc_goods_receipt_lines", lineCols, lineRows); err != nil {
			return fmt.Errorf("copy goods receipt lines: %w", err)
		}

		return nil
	})
}

func loadExistingSeededGoodsReceiptNumbers(ctx context.Context, pool *postgres.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `
		SELECT number FROM doc_goods_receipts WHERE number LIKE 'GR-SEED-%'
	`)
	if err != nil {
		return nil, fmt.Errorf("query existing goods receipts: %w", err)
	}
	defer rows.Close()

	numbers := make(map[string]bool)
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, fmt.Errorf("scan existing goods receipt number: %w", err)
		}
		numbers[number] = true
	}

	return numbers, rows.Err()
}

func loadGeneratedSuppliers(ctx context.Context, pool *postgres.Pool) ([]generatedCounterparty, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, type
		FROM cat_counterparties
		WHERE code LIKE 'CP-GEN-%' AND deletion_mark = FALSE AND type IN ('supplier', 'both')
		ORDER BY code
	`)
	if err != nil {
		return nil, fmt.Errorf("query generated suppliers: %w", err)
	}
	defer rows.Close()

	items := make([]generatedCounterparty, 0, generatedCounterpartyCount)
	for rows.Next() {
		var item generatedCounterparty
		if err := rows.Scan(&item.ID, &item.Name, &item.Type); err != nil {
			return nil, fmt.Errorf("scan generated supplier: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func loadGeneratedProducts(ctx context.Context, pool *postgres.Pool) ([]generatedProduct, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, base_unit_id
		FROM cat_nomenclatures
		WHERE code LIKE 'NM-GEN-%' AND deletion_mark = FALSE
		ORDER BY code
	`)
	if err != nil {
		return nil, fmt.Errorf("query generated nomenclature: %w", err)
	}
	defer rows.Close()

	items := make([]generatedProduct, 0, generatedNomenclatureCount)
	for rows.Next() {
		var item generatedProduct
		if err := rows.Scan(&item.ID, &item.Name, &item.UnitID); err != nil {
			return nil, fmt.Errorf("scan generated nomenclature: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func loadWarehouses(ctx context.Context, pool *postgres.Pool) ([]generatedWarehouse, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name
		FROM cat_warehouses
		WHERE deletion_mark = FALSE
		ORDER BY code
	`)
	if err != nil {
		return nil, fmt.Errorf("query warehouses: %w", err)
	}
	defer rows.Close()

	items := make([]generatedWarehouse, 0, 8)
	for rows.Next() {
		var item generatedWarehouse
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, fmt.Errorf("scan warehouse: %w", err)
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// cryptoRefs holds IDs from seeded crypto catalog data, used by seedCryptoDocuments.
type cryptoRefs struct {
	merchantIDs []id.ID // all active merchants
	tokenID     id.ID
	walletIDs   []id.ID // pool wallets (used for invoices)
	hotWalletID id.ID   // hot wallet (used for withdrawals)
}

// seedCryptoData creates the crypto processing reference data:
// 1 blockchain network (TRON Shasta) → 1 token (USDT-TRC20) → 3 merchants → 7 wallets.
// Returns cryptoRefs so seedCryptoDocuments can create invoices/payments/withdrawals.
func seedCryptoData(ctx context.Context, pool *postgres.Pool, log *logger.Logger) (*cryptoRefs, error) {
	log.Info("seeding crypto processing data...")

	// ── Blockchain Network: TRON Shasta Testnet ────────────────────────
	networkID := id.New()
	networkCode := "TRON-SHASTA"

	commandTag, err := pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks (
			id, code, name, chain_id,
			native_token_symbol, native_decimals, block_time_seconds,
			confirmations_needed, explorer_url, is_active,
			version, deletion_mark, attributes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
	`, networkID, networkCode, "TRON Shasta Testnet", "shasta",
		"TRX", 6, 3, 19, "https://shasta.tronscan.org")
	if err != nil {
		return nil, fmt.Errorf("seed blockchain network: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		err = pool.QueryRow(ctx, `
			SELECT id FROM cat_blockchain_networks WHERE code = $1 AND deletion_mark = FALSE
		`, networkCode).Scan(&networkID)
		if err != nil {
			return nil, fmt.Errorf("fetch existing network: %w", err)
		}
	}

	// ── Token: USDT-TRC20 ──────────────────────────────────────────────
	tokenID := id.New()
	tokenCode := "USDT-TRC20"

	commandTag, err = pool.Exec(ctx, `
		INSERT INTO cat_tokens (
			id, code, name, symbol, network_id,
			contract_address, decimal_places, token_standard, is_active,
			sweep_threshold, sweep_max_age_hours,
			version, deletion_mark, attributes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9, $10, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
	`, tokenID, tokenCode, "Tether USD (TRC-20)", "USDT", networkID,
		"TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs", 6, "TRC-20",
		"10000000", 1) // sweep_threshold=10 USDT (minor units), max_age=1 hour (for testing)
	if err != nil {
		return nil, fmt.Errorf("seed token: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		err = pool.QueryRow(ctx, `
			SELECT id FROM cat_tokens WHERE code = $1 AND deletion_mark = FALSE
		`, tokenCode).Scan(&tokenID)
		if err != nil {
			return nil, fmt.Errorf("fetch existing token: %w", err)
		}
	}

	// ── Merchants: 3 total for chart diversity ──────────────────────────
	type merchantSeed struct {
		code           string
		name           string
		legalName      string
		webhookURL     string
		commissionRate int // basis points
	}

	merchants := []merchantSeed{
		{"MERCHANT-001", "Test Merchant (Dev)", "Test Merchant LLC", "https://webhook.site/test", 150},
		{"MERCHANT-002", "CryptoGate", "CryptoGate Inc.", "https://webhook.site/cryptogate", 200},
		{"MERCHANT-003", "BlockPay Express", "BlockPay Express Ltd.", "https://webhook.site/blockpay", 100},
	}

	merchantIDs := make([]id.ID, 0, len(merchants))
	for _, m := range merchants {
		mID := id.New()
		ct, mErr := pool.Exec(ctx, `
			INSERT INTO cat_merchants (
				id, code, name, legal_name,
				webhook_url, commission_rate, kyb_status, is_active,
				version, deletion_mark, attributes
			) VALUES ($1, $2, $3, $4, $5, $6, 'approved', true, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, mID, m.code, m.name, m.legalName, m.webhookURL, m.commissionRate)
		if mErr != nil {
			return nil, fmt.Errorf("seed merchant %s: %w", m.code, mErr)
		}
		if ct.RowsAffected() == 0 {
			mErr = pool.QueryRow(ctx, `
				SELECT id FROM cat_merchants WHERE code = $1 AND deletion_mark = FALSE
			`, m.code).Scan(&mID)
			if mErr != nil {
				return nil, fmt.Errorf("fetch existing merchant %s: %w", m.code, mErr)
			}
		}
		merchantIDs = append(merchantIDs, mID)
	}

	// ── Wallets: 1 Hot + 6 Pool ─────────────────────────────────────────
	type walletSeed struct {
		code    string
		name    string
		address string
		path    string
		tier    string // "pool", "hot", "warm", "cold"
		status  string // "free", "leased", "sweep_pending", "frozen"
	}

	wallets := []walletSeed{
		{"HOT-TRON-001", "TRON Hot Wallet", "TYDzsYUEgfGRreSR7oqKMo7pqdXxnPH1Hh", "m/44'/195'/0'/0/0", "hot", "leased"},
		{"POOL-TRON-001", "Pool Wallet #1", "TVgY6mWpDGGCtPRBxuMSjitVHfPkpJuVRG", "m/44'/195'/0'/0/1", "pool", "free"},
		{"POOL-TRON-002", "Pool Wallet #2", "TMXMyg87BiHCVfkwvVj3T32SWDSuRQqsPx", "m/44'/195'/0'/0/2", "pool", "free"},
		{"POOL-TRON-003", "Pool Wallet #3", "TN3W4H6rK2ce4vX9YnFQHwKENnHjoxb3m9", "m/44'/195'/0'/0/3", "pool", "free"},
		{"POOL-TRON-004", "Pool Wallet #4", "TSyBKGSVUfJ1Gb4LH4C8u5V8fX9J3wZZp1", "m/44'/195'/0'/0/4", "pool", "free"},
		{"POOL-TRON-005", "Pool Wallet #5", "TKVBfMrcQZGYPKzqKjZHEMNuQ5gJdvUXs4", "m/44'/195'/0'/0/5", "pool", "free"},
		{"POOL-TRON-006", "Pool Wallet #6", "TPjhHRPyQ7e8w7JfP3GxBYNYKqWfMr2wAx", "m/44'/195'/0'/0/6", "pool", "free"},
	}

	walletIDs := make([]id.ID, 0, len(wallets))
	var hotWalletID id.ID

	batch := &pgx.Batch{}
	for _, w := range wallets {
		wID := id.New()
		batch.Queue(`
			INSERT INTO cat_wallets (
				id, code, name, address, network_id,
				derivation_path, tier, status, allocation_mode,
				version, deletion_mark, attributes
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transient', 1, false, '{}')
			ON CONFLICT DO NOTHING
		`, wID, w.code, w.name, w.address, networkID, w.path, w.tier, w.status)

		if w.tier == "hot" {
			hotWalletID = wID
		} else {
			walletIDs = append(walletIDs, wID)
		}
	}

	results := pool.SendBatch(ctx, batch)
	for _, w := range wallets {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return nil, fmt.Errorf("seed wallet %s: %w", w.code, err)
		}
	}
	if err := results.Close(); err != nil {
		return nil, fmt.Errorf("close wallet batch: %w", err)
	}

	// If wallets already existed (ON CONFLICT), fetch IDs from DB.
	if id.IsNil(hotWalletID) {
		err = pool.QueryRow(ctx, `
			SELECT id FROM cat_wallets WHERE code = 'HOT-TRON-001' AND deletion_mark = FALSE
		`).Scan(&hotWalletID)
		if err != nil {
			return nil, fmt.Errorf("fetch hot wallet: %w", err)
		}
	}
	if len(walletIDs) == 0 {
		rows, rErr := pool.Query(ctx, `
			SELECT id FROM cat_wallets WHERE tier = 'pool' AND deletion_mark = FALSE ORDER BY code
		`)
		if rErr != nil {
			return nil, fmt.Errorf("query pool wallets: %w", rErr)
		}
		defer rows.Close()
		for rows.Next() {
			var wID id.ID
			if sErr := rows.Scan(&wID); sErr != nil {
				return nil, fmt.Errorf("scan pool wallet: %w", sErr)
			}
			walletIDs = append(walletIDs, wID)
		}
		if rows.Err() != nil {
			return nil, fmt.Errorf("iterate pool wallets: %w", rows.Err())
		}
	}

	log.Infow("crypto data seeded",
		"network", networkCode,
		"token", tokenCode,
		"merchants", len(merchantIDs),
		"wallets", len(walletIDs)+1,
	)
	return &cryptoRefs{
		merchantIDs: merchantIDs,
		tokenID:     tokenID,
		walletIDs:   walletIDs,
		hotWalletID: hotWalletID,
	}, nil
}

// seedAutomationData creates the automation reference data:
// 1 Telegram Account → 1 Channel → 1 Rule (overpayment notification) → 1 Subscriber.
func seedAutomationData(ctx context.Context, pool *postgres.Pool, log *logger.Logger) error {
	log.Info("seeding automation data...")

	encKey := os.Getenv("AUTOMATION_ENCRYPTION_KEY")
	if encKey == "" {
		log.Warn("AUTOMATION_ENCRYPTION_KEY not set; skipping automation seed")
		return nil
	}

	botToken := os.Getenv("AUTOMATION_TG_BOT_TOKEN")
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
		`doc.status == "overpaid"`,
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
	metaDSN := os.Getenv("META_DATABASE_URL")
	if metaDSN == "" {
		log.Warn("META_DATABASE_URL is not set; skipping tenant registry seed")
		return nil
	}

	metaPool, err := pgxpool.New(ctx, metaDSN)
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

	var existingID string
	err = metaPool.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = $1`, tenantSlug).Scan(&existingID)
	if err == nil {
		log.Infow("tenant already exists in registry", "slug", tenantSlug, "tenant_id", existingID)
		return nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check tenant exists: %w", err)
	}

	registry := tenant.NewPostgresRegistry(metaPool)
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

// ═══════════════════════════════════════════════════════════════════════════════
// Crypto Documents Seed (Invoices, Payments, Withdrawals)
// ═══════════════════════════════════════════════════════════════════════════════

// seedCryptoDocuments generates realistic crypto document data spanning 6 months
// for dashboard charts. Creates invoices → payments (linked) → withdrawals.
func seedCryptoDocuments(ctx context.Context, pool *postgres.Pool, log *logger.Logger, refs *cryptoRefs) error {
	if refs == nil || len(refs.merchantIDs) == 0 || len(refs.walletIDs) == 0 {
		log.Warn("skipping crypto documents: no crypto refs available")
		return nil
	}

	// Check if already seeded.
	var existingCount int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM doc_crypto_invoices WHERE number LIKE 'CI-SEED-%'
	`).Scan(&existingCount)
	if err != nil {
		return fmt.Errorf("check existing crypto invoices: %w", err)
	}
	if existingCount >= _generatedCryptoInvoiceCount {
		log.Infow("crypto documents already seeded", "invoices", existingCount)
		return nil
	}

	log.Info("seeding crypto documents for dashboard charts...")

	rng := rand.New(rand.NewSource(20260512))
	now := time.Now().UTC()

	// ── Phase 1: Generate Invoices ──────────────────────────────────────
	type invoiceSeed struct {
		id             id.ID
		number         string
		date           time.Time
		merchantID     id.ID
		walletID       id.ID
		expectedAmount int64
		receivedAmount int64
		overpaidAmount int64
		status         string
		expiresAt      time.Time
		externalID     string
		orderID        string
	}

	invoices := make([]invoiceSeed, 0, _generatedCryptoInvoiceCount)

	// Status distribution weights for realistic data:
	// 60% confirmed, 15% expired, 10% paid, 5% partially_paid, 5% created, 3% overpaid, 2% cancelled
	statusWeights := []struct {
		status string
		weight int
	}{
		{"confirmed", 60},
		{"expired", 15},
		{"paid", 10},
		{"partially_paid", 5},
		{"created", 5},
		{"overpaid", 3},
		{"cancelled", 2},
	}
	statusPool := make([]string, 0, 100)
	for _, sw := range statusWeights {
		for range sw.weight {
			statusPool = append(statusPool, sw.status)
		}
	}

	for i := 1; i <= _generatedCryptoInvoiceCount; i++ {
		invID := id.New()
		number := fmt.Sprintf("CI-SEED-%05d", i)

		// Spread documents over 6 months with slight concentration toward recent dates.
		daysAgo := rng.Intn(180)
		hoursOffset := rng.Intn(24)
		minutesOffset := rng.Intn(60)
		docDate := now.AddDate(0, 0, -daysAgo).Add(-time.Duration(hoursOffset)*time.Hour - time.Duration(minutesOffset)*time.Minute)

		merchantID := refs.merchantIDs[rng.Intn(len(refs.merchantIDs))]
		walletID := refs.walletIDs[rng.Intn(len(refs.walletIDs))]

		// Amount 1 – 5000 USDT (in minor units, 6 decimals: 1 USDT = 1_000_000)
		expectedAmount := int64(1_000_000 + rng.Intn(4_999_000_000)) // 1 to 5000 USDT

		status := statusPool[rng.Intn(len(statusPool))]
		expiresAt := docDate.Add(30 * time.Minute)

		var receivedAmount, overpaidAmount int64
		switch status {
		case "confirmed", "paid":
			receivedAmount = expectedAmount
		case "partially_paid":
			// 20-80% of expected
			receivedAmount = expectedAmount * int64(20+rng.Intn(60)) / 100
		case "overpaid":
			excess := int64(100_000 + rng.Intn(500_000)) // 0.1-0.6 USDT overpayment
			receivedAmount = expectedAmount + excess
			overpaidAmount = excess
		case "expired", "cancelled", "created":
			receivedAmount = 0
		}

		invoices = append(invoices, invoiceSeed{
			id:             invID,
			number:         number,
			date:           docDate,
			merchantID:     merchantID,
			walletID:       walletID,
			expectedAmount: expectedAmount,
			receivedAmount: receivedAmount,
			overpaidAmount: overpaidAmount,
			status:         status,
			expiresAt:      expiresAt,
			externalID:     "ext-" + strconv.Itoa(i),
			orderID:        "ORD-" + strconv.Itoa(10000+i),
		})
	}

	// Batch-insert invoices.
	invoiceCreated := 0
	for batchStart := 0; batchStart < len(invoices); batchStart += _cryptoDocBatchSize {
		batchEnd := batchStart + _cryptoDocBatchSize
		if batchEnd > len(invoices) {
			batchEnd = len(invoices)
		}
		chunk := invoices[batchStart:batchEnd]

		batch := &pgx.Batch{}
		for _, inv := range chunk {
			batch.Queue(`
				INSERT INTO doc_crypto_invoices (
					id, number, date, posted, posted_version,
					basis_type, description,
					merchant_id, token_id, wallet_id,
					expected_amount, received_amount, overpaid_amount,
					status, expires_at, callback_url, external_id, order_id, customer_email,
					deletion_mark, version, attributes,
					created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, $5,
					'', $6,
					$7, $8, $9,
					$10, $11, $12,
					$13, $14, '', $15, $16, '',
					false, 1, '{}',
					$3, $3
				)
				ON CONFLICT (number) DO NOTHING
			`,
				inv.id, inv.number, inv.date,
				inv.status == "confirmed", // posted only if confirmed
				boolToInt(inv.status == "confirmed"),
				"Seed crypto invoice "+inv.number,
				inv.merchantID, refs.tokenID, inv.walletID,
				inv.expectedAmount, inv.receivedAmount, inv.overpaidAmount,
				inv.status, inv.expiresAt, inv.externalID, inv.orderID,
			)
		}

		results := pool.SendBatch(ctx, batch)
		for _, inv := range chunk {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert crypto invoice %s: %w", inv.number, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close crypto invoice batch: %w", err)
		}
		invoiceCreated += len(chunk)
		if invoiceCreated%200 == 0 {
			log.Infow("crypto invoices seeding progress", "created", invoiceCreated, "target", _generatedCryptoInvoiceCount)
		}
	}

	log.Infow("crypto invoices seeded", "count", invoiceCreated)

	// ── Phase 2: Generate Payments (linked to confirmed/paid/overpaid invoices) ───
	type paymentSeed struct {
		id            id.ID
		number        string
		date          time.Time
		invoiceID     id.ID
		merchantID    id.ID
		walletID      id.ID
		amount        int64
		txHash        string
		fromAddress   string
		blockNumber   int64
		confirmations int
		requiredConfs int
		status        string
		networkFee    int64
		detectedAt    time.Time
		confirmedAt   *time.Time
	}

	payments := make([]paymentSeed, 0, len(invoices))
	paymentIdx := 0

	// Fake sender addresses for variety.
	senders := []string{
		"TJCnKsPa7y5okkXvQAidZBzqx3QyQ6sxMW",
		"TX1Kh4JCBbhLNmJrKPbFVq6YdXnUAP7Yeo",
		"TLkqHjNkfGEdQzJkLzTd4s1j5kB2V1JdUQ",
		"TVy5VNw7X1FKRmJH6zCqYhHqR9Xt8UBqpR",
		"TGd4rFJBLN5SNB7rVJ1LFJg6bPDRfLHJQ3",
		"TAahcmJeDLiTZyCwXyBocPhKR2YNqk7Zac",
		"TYASr5UV6HEcXatwdFQfmLVUqQQQMUxHLS",
		"TNaRAoLUyYEV2uF7GUrzSjRQTU8v5ZJ5VR",
	}

	for _, inv := range invoices {
		if inv.receivedAmount <= 0 {
			continue
		}

		paymentIdx++
		pID := id.New()
		detectedAt := inv.date.Add(time.Duration(1+rng.Intn(25)) * time.Minute)
		blockNum := int64(50_000_000 + rng.Intn(10_000_000))
		confirmedAt := detectedAt.Add(time.Duration(3*19) * time.Second) // 19 confirmations × 3s block time
		fee := int64(100_000 + rng.Intn(500_000))                       // 0.1-0.6 TRX network fee

		status := "confirmed"
		confs := 19
		if inv.status == "partially_paid" {
			status = "confirming"
			confs = 5 + rng.Intn(10)
		}

		payments = append(payments, paymentSeed{
			id:            pID,
			number:        fmt.Sprintf("CP-SEED-%05d", paymentIdx),
			date:          detectedAt,
			invoiceID:     inv.id,
			merchantID:    inv.merchantID,
			walletID:      inv.walletID,
			amount:        inv.receivedAmount,
			txHash:        generateFakeTxHash(rng, paymentIdx),
			fromAddress:   senders[rng.Intn(len(senders))],
			blockNumber:   blockNum,
			confirmations: confs,
			requiredConfs: 19,
			status:        status,
			networkFee:    fee,
			detectedAt:    detectedAt,
			confirmedAt:   &confirmedAt,
		})
	}

	// Batch-insert payments.
	paymentCreated := 0
	for batchStart := 0; batchStart < len(payments); batchStart += _cryptoDocBatchSize {
		batchEnd := batchStart + _cryptoDocBatchSize
		if batchEnd > len(payments) {
			batchEnd = len(payments)
		}
		chunk := payments[batchStart:batchEnd]

		batch := &pgx.Batch{}
		for _, p := range chunk {
			batch.Queue(`
				INSERT INTO doc_crypto_payments (
					id, number, date, posted, posted_version,
					basis_type, basis_id, description,
					invoice_id, merchant_id, token_id, wallet_id,
					tx_hash, from_address, amount,
					block_number, confirmations, required_confs,
					status, network_fee, detected_at, confirmed_at,
					deletion_mark, version, attributes,
					created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, $5,
					'CryptoInvoice', $6, $7,
					$6, $8, $9, $10,
					$11, $12, $13,
					$14, $15, $16,
					$17, $18, $19, $20,
					false, 1, '{}',
					$3, $3
				)
				ON CONFLICT DO NOTHING
			`,
				p.id, p.number, p.date,
				p.status == "confirmed", // posted only if confirmed
				boolToInt(p.status == "confirmed"),
				p.invoiceID,
				"Seed crypto payment "+p.number,
				p.merchantID, refs.tokenID, p.walletID,
				p.txHash, p.fromAddress, p.amount,
				p.blockNumber, p.confirmations, p.requiredConfs,
				p.status, p.networkFee, p.detectedAt, p.confirmedAt,
			)
		}

		results := pool.SendBatch(ctx, batch)
		for _, p := range chunk {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert crypto payment %s: %w", p.number, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close crypto payment batch: %w", err)
		}
		paymentCreated += len(chunk)
	}

	log.Infow("crypto payments seeded", "count", paymentCreated)

	// ── Phase 3: Generate Withdrawals ───────────────────────────────────
	type withdrawalSeed struct {
		id             id.ID
		number         string
		date           time.Time
		merchantID     id.ID
		sourceWalletID id.ID
		destAddress    string
		amount         int64
		networkFee     int64
		txHash         string
		status         string
	}

	withdrawalStatuses := []string{
		"confirmed", "confirmed", "confirmed", "confirmed", "confirmed", // 62.5%
		"broadcast",                                   // 12.5%
		"created", "created",                          // 25%
	}

	destAddresses := []string{
		"TUxqPKkAv2efsUYBhUYhvYPdR9QsvkGBnF",
		"TMwFHYXLJaRUPeW6421aqXL4ZEzPRFGkGT",
		"TW3kJrEvg9FhbhU7q3VQtC5EL9SDB1d2FE",
		"TJvKqL4VKVbz2rE6ixfL1h2mSxQGhE3D4F",
	}

	withdrawals := make([]withdrawalSeed, 0, _generatedCryptoWithdrawalCount)
	for i := 1; i <= _generatedCryptoWithdrawalCount; i++ {
		wID := id.New()
		daysAgo := rng.Intn(180)
		docDate := now.AddDate(0, 0, -daysAgo).Add(-time.Duration(rng.Intn(24)) * time.Hour)
		merchantID := refs.merchantIDs[rng.Intn(len(refs.merchantIDs))]
		amount := int64(5_000_000 + rng.Intn(495_000_000)) // 5-500 USDT
		fee := int64(1_000_000 + rng.Intn(2_000_000))       // 1-3 TRX
		status := withdrawalStatuses[rng.Intn(len(withdrawalStatuses))]

		txHash := ""
		if status == "confirmed" || status == "broadcast" {
			txHash = generateFakeTxHash(rng, 10000+i)
		}

		withdrawals = append(withdrawals, withdrawalSeed{
			id:             wID,
			number:         fmt.Sprintf("CW-SEED-%05d", i),
			date:           docDate,
			merchantID:     merchantID,
			sourceWalletID: refs.hotWalletID,
			destAddress:    destAddresses[rng.Intn(len(destAddresses))],
			amount:         amount,
			networkFee:     fee,
			txHash:         txHash,
			status:         status,
		})
	}

	// Batch-insert withdrawals.
	withdrawalCreated := 0
	for batchStart := 0; batchStart < len(withdrawals); batchStart += _cryptoDocBatchSize {
		batchEnd := batchStart + _cryptoDocBatchSize
		if batchEnd > len(withdrawals) {
			batchEnd = len(withdrawals)
		}
		chunk := withdrawals[batchStart:batchEnd]

		batch := &pgx.Batch{}
		for _, w := range chunk {
			batch.Queue(`
				INSERT INTO doc_crypto_withdrawals (
					id, number, date, posted, posted_version,
					basis_type, description,
					merchant_id, token_id, source_wallet_id,
					dest_address, amount, network_fee, tx_hash, status,
					deletion_mark, version, attributes,
					created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, $5,
					'', $6,
					$7, $8, $9,
					$10, $11, $12, $13, $14,
					false, 1, '{}',
					$3, $3
				)
				ON CONFLICT DO NOTHING
			`,
				w.id, w.number, w.date,
				w.status == "confirmed",
				boolToInt(w.status == "confirmed"),
				"Seed withdrawal "+w.number,
				w.merchantID, refs.tokenID, w.sourceWalletID,
				w.destAddress, w.amount, w.networkFee, w.txHash, w.status,
			)
		}

		results := pool.SendBatch(ctx, batch)
		for _, w := range chunk {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert crypto withdrawal %s: %w", w.number, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close crypto withdrawal batch: %w", err)
		}
		withdrawalCreated += len(chunk)
	}

	log.Infow("crypto withdrawals seeded", "count", withdrawalCreated)

	log.Infow("crypto documents seeded",
		"invoices", invoiceCreated,
		"payments", paymentCreated,
		"withdrawals", withdrawalCreated,
	)
	return nil
}

// boolToInt converts a boolean to 0 or 1 (for posted_version).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// generateFakeTxHash creates a deterministic fake TRON tx hash for seeding.
func generateFakeTxHash(rng *rand.Rand, idx int) string {
	const hexChars = "0123456789abcdef"
	buf := make([]byte, 64)
	for i := range buf {
		buf[i] = hexChars[rng.Intn(len(hexChars))]
	}
	// Embed index to guarantee uniqueness.
	suffix := strconv.Itoa(idx)
	copy(buf[64-len(suffix):], suffix)
	return string(buf)
}
