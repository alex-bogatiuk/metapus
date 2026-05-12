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

	fmt.Println("=== USERS ===")
	rows, _ := conn.Query(ctx, `SELECT id, email, first_name, last_name, is_active FROM users ORDER BY email`)
	defer rows.Close()
	for rows.Next() {
		var id, email string
		var firstName, lastName *string
		var isActive bool
		_ = rows.Scan(&id, &email, &firstName, &lastName, &isActive)
		fn := ""
		if firstName != nil { fn = *firstName }
		ln := ""
		if lastName != nil { ln = *lastName }
		fmt.Printf("  id=%s email=%-30s name=%-20s active=%v\n", id, email, fn+" "+ln, isActive)
	}
	rows.Close()

	fmt.Println("\n=== MERCHANT USERS (sys_merchant_users) ===")
	rows2, _ := conn.Query(ctx, `SELECT user_id, merchant_id, role FROM sys_merchant_users ORDER BY merchant_id`)
	defer rows2.Close()
	for rows2.Next() {
		var userId, merchantId string
		var role int
		_ = rows2.Scan(&userId, &merchantId, &role)
		fmt.Printf("  user=%s merchant=%s role=%d\n", userId, merchantId, role)
	}
}
