// Package main provides a CLI tool for seeding the database with initial data.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
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
	if err := seedAdminUser(ctx, pool, log); err != nil {
		log.Fatalw("failed to seed admin user", "error", err)
	}

	// Seed demo data if requested
	if os.Getenv("SEED_DEMO_DATA") == "true" {
		if err := seedDemoData(ctx, pool, log); err != nil {
			log.Fatalw("failed to seed demo data", "error", err)
		}
	}

	log.Info("seeding completed successfully")
}

func seedAdminUser(ctx context.Context, pool *postgres.Pool, log *logger.Logger) error {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "admin@metapus.io"
	}

	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "Admin123!"
	}

	// Check if admin already exists
	var exists bool
	err := pool.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1 AND deletion_mark = FALSE)`,
		adminEmail,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check admin exists: %w", err)
	}

	if exists {
		log.Infow("admin user already exists", "email", adminEmail)
		return nil
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	userID := id.New()
	now := time.Now()

	// Create admin user
	_, err = pool.Pool.Exec(ctx, `
		INSERT INTO users (
			id, email, password_hash, first_name, last_name, 
			is_active, is_admin, email_verified, created_at, updated_at,
			version, deletion_mark, attributes
		)
		VALUES ($1, $2, $3, 'System', 'Admin', true, true, true, $4, $4, 1, false, '{}')
	`, userID, adminEmail, string(passwordHash), now)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	// Assign admin role
	var adminRoleID id.ID
	err = pool.Pool.QueryRow(ctx,
		`SELECT id FROM roles WHERE code = 'admin'`,
	).Scan(&adminRoleID)
	if err != nil {
		log.Warnw("admin role not found, skipping role assignment", "error", err)
	} else {
		_, err = pool.Pool.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, granted_at)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, userID, adminRoleID, now)
		if err != nil {
			log.Warnw("failed to assign admin role", "error", err)
		}
	}

	log.Infow("admin user created",
		"email", adminEmail,
		"user_id", userID,
	)

	return nil
}

func seedDemoData(ctx context.Context, pool *postgres.Pool, log *logger.Logger) error {
	log.Info("seeding demo data...")

	// 1. Seed Organization (Root entity)
	// Required for documents in later stages
	orgID := id.New()
	orgCode := "ORG-001"
	_, err := pool.Pool.Exec(ctx, `
		INSERT INTO cat_organizations (id, code, name, full_name, inn, kpp, legal_address, version, deletion_mark, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
		ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
	`, orgID, orgCode, "ООО Ромашка", "Общество с ограниченной ответственностью 'Ромашка'", "7700000001", "770001001", "г. Москва, ул. Ленина, 1")
	if err != nil {
		log.Warnw("failed to seed organization", "error", err)
	}

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
		commandTag, err := pool.Pool.Exec(ctx, `
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
			err = pool.Pool.QueryRow(ctx, `
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
		name    string
		isoCode string
		symbol  string
		isBase  bool
	}{
		{"Российский рубль", "RUB", "₽", true},
		{"Доллар США", "USD", "$", false},
		{"Евро", "EUR", "€", false},
	}

	for _, c := range currencies {
		currID := id.New()
		_, err := pool.Pool.Exec(ctx, `
			INSERT INTO cat_currencies (id, code, name, iso_code, symbol, is_base, version, deletion_mark, attributes)
			VALUES ($1, $2, $3, $4, $5, $6, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, currID, c.isoCode, c.name, c.isoCode, c.symbol, c.isBase)
		if err != nil {
			log.Warnw("failed to seed currency", "name", c.name, "error", err)
		}
	}

	// 4. Seed Warehouses
	// Теперь мы поддерживаем несколько складов, как в ERP-системах.
	warehouses := []struct {
		name      string
		address   string
		wType     string
		isDefault bool // Новое поле
	}{
		{"Основной склад", "г. Москва, ул. Складская, д. 1", "main", true},
		{"Розничный магазин", "г. Москва, ул. Торговая, д. 5", "retail", false},
		{"Транзитный склад", "Виртуальный", "transit", false},
	}

	for i, w := range warehouses {
		whID := id.New()
		code := fmt.Sprintf("WH-%03d", i+1)
		_, err := pool.Pool.Exec(ctx, `
            INSERT INTO cat_warehouses (id, code, name, address, type, organization_id, is_default, version, deletion_mark, attributes)
            VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
            ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
        `, whID, code, w.name, w.address, w.wType, orgID, w.isDefault)

		// Особая обработка для is_default:
		// Если мы пытаемся вставить второй default (вдруг), база отстрелит ошибку 23505
		// В реальном коде тут нужна логика "снять флаг с других", но для сидера достаточно просто не падать.
		if err != nil {
			// Игнорируем ошибку уникальности default, если вдруг запускаем сидер повторно с другими данными
			log.Warnw("failed to seed warehouse", "name", w.name, "error", err)
		}
	}

	// 5. Seed Counterparties
	counterparties := []struct {
		name      string
		ctype     string // customer, supplier, both
		legalForm string // company, individual
		inn       string
	}{
		{"ООО 'Поставщик Плюс'", "supplier", "company", "7707083893"},
		{"ООО 'Закупщик'", "customer", "company", "7710140679"},
		{"ИП Иванов И.И.", "both", "individual", "772300001234"},
	}

	for i, cp := range counterparties {
		cpID := id.New()
		code := fmt.Sprintf("CP-%03d", i+1)
		_, err := pool.Pool.Exec(ctx, `
			INSERT INTO cat_counterparties (id, code, name, type, legal_form, inn, full_name, version, deletion_mark, attributes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, cpID, code, cp.name, cp.ctype, cp.legalForm, cp.inn, cp.name)
		if err != nil {
			log.Warnw("failed to seed counterparty", "name", cp.name, "error", err)
		}
	}

	// 6. Seed Nomenclature
	products := []struct {
		name       string
		article    string
		barcode    string
		ntype      string // goods, service
		unitSymbol string
	}{
		{"Бумага офисная А4", "PAP-A4", "4600000000001", "goods", "уп"},
		{"Ручка шариковая синяя", "PEN-BLU", "4600000000002", "goods", "шт"},
		{"Степлер настольный", "STP-001", "4600000000003", "goods", "шт"},
		{"Скрепки 28мм (100шт)", "CLP-028", "4600000000004", "goods", "уп"},
		{"Папка-регистратор", "FOL-REG", "4600000000005", "goods", "шт"},
		{"Доставка груза", "DELIVERY", "", "service", "шт"}, // Example service
	}

	for i, p := range products {
		prodID := id.New()
		code := fmt.Sprintf("NM-%05d", i+1)

		// Find Unit ID
		unitID, ok := unitIDs[p.unitSymbol]
		if !ok {
			// Fallback to 'piece' if specific unit not found
			unitID = unitIDs["шт"]
		}

		_, err := pool.Pool.Exec(ctx, `
			INSERT INTO cat_nomenclature (id, code, name, type, article, barcode, base_unit_id, vat_rate, version, deletion_mark, attributes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '20', 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, prodID, code, p.name, p.ntype, p.article, p.barcode, unitID)

		if err != nil {
			log.Warnw("failed to seed product", "name", p.name, "error", err)
		}
	}

	log.Info("demo data seeded successfully")
	return nil
}
