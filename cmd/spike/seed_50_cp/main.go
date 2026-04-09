package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"metapus/internal/core/id"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://metapus:metapus@localhost:5432/metapus?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	types := []string{"customer", "supplier", "both"}
	legalForms := []string{"company", "individual"}

	for i := 1; i <= 50; i++ {
		cpID := id.New()
		code := fmt.Sprintf("CP-GEN-%03d", i)
		name := fmt.Sprintf("Сгенерированный Контрагент %d", i)
		fullName := fmt.Sprintf("Общество с ограниченной ответственностью 'Сгенерированный Контрагент %d'", i)
		ctype := types[i%len(types)]
		legalForm := legalForms[i%len(legalForms)]
		inn := fmt.Sprintf("77%08d", i)

		_, err := pool.Exec(ctx, `
			INSERT INTO cat_counterparties (id, code, name, type, legal_form, inn, full_name, version, deletion_mark, attributes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 1, false, '{}')
			ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING
		`, cpID, code, name, ctype, legalForm, inn, fullName)

		if err != nil {
			log.Printf("Failed to insert %s: %v", code, err)
		} else {
			fmt.Printf("Inserted %s - %s\n", code, name)
		}
	}

	fmt.Println("Finished generating 50 counterparties.")
}
