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
    
    rows, err := conn.Query(ctx, "SELECT id, code, name FROM cat_tokens WHERE deletion_mark = FALSE ORDER BY code")
    if err != nil {
        fmt.Println("QUERY ERROR:", err)
        return
    }
    defer rows.Close()
    for rows.Next() {
        var id, code, name string
        _ = rows.Scan(&id, &code, &name)
        fmt.Printf("ID=%s  CODE=%s  NAME=%s\n", id, code, name)
    }
}
