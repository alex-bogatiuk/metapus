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

	fmt.Println("=== MERCHANT USERS (cat_merchant_users) ===")
	rows, _ := conn.Query(ctx, `
		SELECT mu.user_id, mu.merchant_id, mu.role, u.email, u.first_name, u.last_name
		FROM cat_merchant_users mu
		JOIN auth_users u ON u.id = mu.user_id
		ORDER BY mu.merchant_id, mu.role
	`)
	defer rows.Close()
	for rows.Next() {
		var userId, merchantId, email string
		var role int
		var firstName, lastName *string
		_ = rows.Scan(&userId, &merchantId, &role, &email, &firstName, &lastName)
		fn := ""
		if firstName != nil { fn = *firstName }
		ln := ""
		if lastName != nil { ln = *lastName }
		fmt.Printf("  user=%s email=%-30s name=%-20s merchant=%s role=%d\n", userId[:12], email, fn+" "+ln, merchantId[:12], role)
	}
	rows.Close()

	fmt.Println("\n=== ALL USERS ===")
	rows2, _ := conn.Query(ctx, `SELECT id, email, first_name, last_name, is_active FROM auth_users ORDER BY email`)
	defer rows2.Close()
	for rows2.Next() {
		var id, email string
		var firstName, lastName *string
		var isActive bool
		_ = rows2.Scan(&id, &email, &firstName, &lastName, &isActive)
		fn := ""
		if firstName != nil { fn = *firstName }
		ln := ""
		if lastName != nil { ln = *lastName }
		fmt.Printf("  id=%s email=%-30s name=%-20s active=%v\n", id[:12], email, fn+" "+ln, isActive)
	}
}
