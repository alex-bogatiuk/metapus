package main

import (
	"context"
	"fmt"
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

	// 1. Check wallets
	fmt.Println("=== WALLETS ===")
	rows, _ := conn.Query(ctx, `SELECT id, address, status, network_id, is_active, deletion_mark FROM cat_wallets ORDER BY status, id LIMIT 20`)
	defer rows.Close()
	for rows.Next() {
		var id, address, status, networkId string
		var isActive, deletionMark bool
		_ = rows.Scan(&id, &address, &status, &networkId, &isActive, &deletionMark)
		fmt.Printf("  id=%s addr=%s..%s status=%-10s network=%s active=%v del=%v\n", id[:8], address[:6], address[len(address)-4:], status, networkId[:8], isActive, deletionMark)
	}
	rows.Close()

	// 2. Check recent invoices wallet_id
	fmt.Println("\n=== RECENT INVOICES (wallet_id) ===")
	rows2, _ := conn.Query(ctx, `SELECT id, status, wallet_id, token_id, merchant_id FROM doc_crypto_invoices ORDER BY created_at DESC LIMIT 5`)
	defer rows2.Close()
	for rows2.Next() {
		var id, status, merchantId string
		var walletId, tokenId *string
		_ = rows2.Scan(&id, &status, &walletId, &tokenId, &merchantId)
		wid := "<nil>"
		if walletId != nil { wid = *walletId }
		tid := "<nil>"
		if tokenId != nil { tid = *tokenId }
		fmt.Printf("  invoice=%s status=%-10s wallet=%s token=%s merchant=%s\n", id[:12], status, wid, tid[:12], merchantId[:12])
	}
	rows2.Close()

	// 3. Check wallet statuses count
	fmt.Println("\n=== WALLET STATUS COUNTS ===")
	rows3, _ := conn.Query(ctx, `SELECT status, COUNT(*) FROM cat_wallets GROUP BY status`)
	defer rows3.Close()
	for rows3.Next() {
		var status string
		var count int
		_ = rows3.Scan(&status, &count)
		fmt.Printf("  %s: %d\n", status, count)
	}
}
