package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, "postgres://metapus:metapus@localhost:5432/mt_default?sslmode=disable")
	if err != nil { fmt.Println("ERROR:", err); return }
	defer conn.Close(ctx)

	fmt.Println("=== WALLET DETAILS ===")
	rows, _ := conn.Query(ctx, `SELECT id, address, status, tier, allocation_mode, network_id, is_active FROM cat_wallets ORDER BY id`)
	defer rows.Close()
	for rows.Next() {
		var id, address, status, tier, allocMode, networkId string
		var isActive bool
		_ = rows.Scan(&id, &address, &status, &tier, &allocMode, &networkId, &isActive)
		fmt.Printf("  id=%s addr=%s..%s status=%-8s tier=%-6s alloc=%-10s net=%s active=%v\n",
			id[:12], address[:6], address[len(address)-4:], status, tier, allocMode, networkId[:12], isActive)
	}
	rows.Close()

	// Check token -> network mapping
	fmt.Println("\n=== TOKEN -> NETWORK ===")
	rows2, _ := conn.Query(ctx, `SELECT t.id, t.code, t.name, t.network_id, n.name as net_name FROM cat_tokens t LEFT JOIN cat_blockchain_networks n ON n.id = t.network_id`)
	defer rows2.Close()
	for rows2.Next() {
		var tid, tcode, tname, netId, netName string
		_ = rows2.Scan(&tid, &tcode, &tname, &netId, &netName)
		fmt.Printf("  token=%s code=%-12s name=%-25s network=%s (%s)\n", tid[:12], tcode, tname, netId[:12], netName)
	}
}
