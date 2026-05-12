package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()

	// Check tenant DB
	conn, _ := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable")
	defer conn.Close(ctx)

	// List all tables
	fmt.Println("=== AUTH TABLES ===")
	rows, _ := conn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename LIKE 'auth_%' ORDER BY tablename`)
	defer rows.Close()
	for rows.Next() {
		var t string
		_ = rows.Scan(&t)
		fmt.Printf("  %s\n", t)
	}
	rows.Close()

	// Users
	fmt.Println("\n=== auth_users ===")
	rows2, _ := conn.Query(ctx, `SELECT id, email, first_name, last_name, is_active FROM auth_users ORDER BY email LIMIT 10`)
	defer rows2.Close()
	for rows2.Next() {
		var id, email string
		var firstName, lastName *string
		var isActive bool
		_ = rows2.Scan(&id, &email, &firstName, &lastName, &isActive)
		fn := "<nil>"
		if firstName != nil { fn = *firstName }
		ln := "<nil>"
		if lastName != nil { ln = *lastName }
		fmt.Printf("  id=%s email=%-30s name=%s %s active=%v\n", id, email, fn, ln, isActive)
	}
	rows2.Close()

	// Merchant users
	fmt.Println("\n=== cat_merchant_users ===")
	var muCount int
	conn.QueryRow(ctx, `SELECT COUNT(*) FROM cat_merchant_users`).Scan(&muCount)
	fmt.Printf("  count=%d\n", muCount)

	if muCount == 0 {
		fmt.Println("  NO MERCHANT USERS - need to create one!")
	}
}
