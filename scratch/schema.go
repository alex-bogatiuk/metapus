package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, _ := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable")
	defer conn.Close(ctx)

	fmt.Println("=== doc_crypto_invoices columns ===")
	rows, _ := conn.Query(ctx, `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'doc_crypto_invoices'
		ORDER BY ordinal_position
	`)
	defer rows.Close()
	for rows.Next() {
		var col, dtype string
		_ = rows.Scan(&col, &dtype)
		fmt.Printf("  %-30s %s\n", col, dtype)
	}
	rows.Close()

	fmt.Println("\n=== cat_tokens columns ===")
	rows2, _ := conn.Query(ctx, `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'cat_tokens'
		ORDER BY ordinal_position
	`)
	defer rows2.Close()
	for rows2.Next() {
		var col, dtype string
		_ = rows2.Scan(&col, &dtype)
		fmt.Printf("  %-30s %s\n", col, dtype)
	}
}
