package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()

	// Check tenant DB - list ALL tables to find user storage
	conn, _ := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable")
	defer conn.Close(ctx)

	fmt.Println("=== ALL TABLES IN mt_default ===")
	rows, _ := conn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	defer rows.Close()
	for rows.Next() {
		var t string
		_ = rows.Scan(&t)
		fmt.Println("  " + t)
	}
	rows.Close()

	// Also check meta DB for users
	fmt.Println("\n=== META DB TABLES ===")
	metaConn, _ := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable")
	defer metaConn.Close(ctx)
	
	rows2, _ := metaConn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	defer rows2.Close()
	for rows2.Next() {
		var t string
		_ = rows2.Scan(&t)
		fmt.Println("  " + t)
	}
}
