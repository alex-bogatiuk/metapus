package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://metapus:metapus@localhost:5432/metapus?sslmode=disable")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `SELECT id, email FROM users ORDER BY email`)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	fmt.Println("Users:")
	for rows.Next() {
		var id, email string
		rows.Scan(&id, &email)
		fmt.Printf("  %s  %s\n", id, email)
	}

	// Check FK constraint
	rows2, err := pool.Query(ctx, `
		SELECT tc.constraint_name, kcu.column_name, ccu.table_name AS foreign_table
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name
		JOIN information_schema.constraint_column_usage ccu ON tc.constraint_name = ccu.constraint_name
		WHERE tc.table_name = 'doc_goods_receipts' AND tc.constraint_type = 'FOREIGN KEY'
		AND kcu.column_name = 'created_by'
	`)
	if err != nil {
		fmt.Printf("FK error: %v\n", err)
		return
	}
	defer rows2.Close()
	for rows2.Next() {
		var name, col, ftable string
		rows2.Scan(&name, &col, &ftable)
		fmt.Printf("FK: %s (%s) -> %s\n", name, col, ftable)
	}
}
