package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable")
	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}
	defer conn.Close(ctx)

	merchantID := "019e0879-ce5b-774f-93ec-b613e24ba32d"
	tokenID := "019e084d-cb57-7478-b159-0f7cb430d6c8" // USDT-TRC20
	walletID := "019e17af-2308-768f-a26e-0d40b7be2ad5" // first pool wallet (leased)

	// Get the wallet ID of the leased one
	var wid string
	err = conn.QueryRow(ctx, `SELECT id FROM cat_wallets WHERE status = 'leased' LIMIT 1`).Scan(&wid)
	if err == nil {
		walletID = wid
	}

	rng := rand.New(rand.NewSource(42))
	now := time.Now().UTC()

	const insertSQL = `
		INSERT INTO doc_crypto_invoices (
			id, deletion_mark, version, number, date, posted, posted_version,
			merchant_id, token_id, wallet_id,
			expected_amount, received_amount, overpaid_amount,
			status, expires_at, order_id, description,
			created_at, updated_at, created_by, updated_by
		) VALUES (
			$1, false, 1, $2, $3, false, 0,
			$4, $5, $6,
			$7, $8, 0,
			$9, $10, $11, $12,
			$3, $3, 'c0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001'
		)`

	inserted := 0
	// Generate 25 confirmed invoices over last 30 days (varied amounts)
	for day := 0; day < 30; day++ {
		// Some days have 0 invoices, some 1, some 2-3
		count := rng.Intn(3) // 0, 1, or 2 invoices per day
		if day%3 == 0 {
			count = 2 // every 3rd day: 2 invoices
		}
		if day%7 == 0 {
			count = 3 // every week: 3 invoices
		}

		for i := 0; i < count; i++ {
			id := uuid.New().String()
			invoiceDate := now.AddDate(0, 0, -day).Add(time.Duration(rng.Intn(12)) * time.Hour)
			number := fmt.Sprintf("CI-SEED-%03d", day*3+i+1)
			// Random amounts: 0.5 to 50 USDT (500_000 to 50_000_000 minor units)
			amount := int64(500000 + rng.Intn(49500000))
			orderID := fmt.Sprintf("SEED-ORDER-%03d-%d", day, i)
			expiresAt := invoiceDate.Add(60 * time.Minute)

			_, err := conn.Exec(ctx, insertSQL,
				id, number, invoiceDate,
				merchantID, tokenID, walletID,
				amount, amount, // received = expected (fully paid)
				"confirmed", expiresAt, orderID, fmt.Sprintf("Seed invoice day-%d #%d", day, i),
			)
			if err != nil {
				fmt.Printf("  ERROR day %d: %v\n", day, err)
				continue
			}
			inserted++
		}
	}

	// Also add a few "created" (pending) and "expired" for variety
	for i := 0; i < 5; i++ {
		id := uuid.New().String()
		invoiceDate := now.AddDate(0, 0, -rng.Intn(10))
		number := fmt.Sprintf("CI-SEED-P%03d", i+1)
		amount := int64(1000000 + rng.Intn(5000000))
		orderID := fmt.Sprintf("SEED-PENDING-%d", i)
		status := "created"
		if i >= 3 {
			status = "expired"
		}
		expiresAt := invoiceDate.Add(60 * time.Minute)

		conn.Exec(ctx, insertSQL,
			id, number, invoiceDate,
			merchantID, tokenID, walletID,
			amount, int64(0), // received = 0
			status, expiresAt, orderID, fmt.Sprintf("Seed pending/expired #%d", i),
		)
		inserted++
	}

	fmt.Printf("✅ Inserted %d seed invoices for chart data\n", inserted)

	// Verify chart data
	fmt.Println("\n=== Chart data (last 30 days) ===")
	rows, _ := conn.Query(ctx, `
		SELECT DATE_TRUNC('day', created_at)::date AS day,
			COUNT(*) AS cnt,
			COALESCE(SUM(received_amount) FILTER (WHERE status = 'confirmed'), 0) AS deposits
		FROM doc_crypto_invoices
		WHERE merchant_id = $1 AND created_at >= NOW() - INTERVAL '30 days' AND _deleted_at IS NULL
		GROUP BY day ORDER BY day
	`, merchantID)
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		var cnt int
		var deposits int64
		_ = rows.Scan(&day, &cnt, &deposits)
		bar := ""
		for j := int64(0); j < deposits/1000000; j++ {
			bar += "█"
		}
		fmt.Printf("  %s  cnt=%d  deposits=%12d  %s\n", day.Format("2006-01-02"), cnt, deposits, bar)
	}
}
