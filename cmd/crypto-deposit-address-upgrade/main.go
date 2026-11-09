// Command crypto-deposit-address-upgrade previews or explicitly applies the
// additive schema upgrade without replaying baseline migrations or resetting data.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	dbassets "metapus/db"
)

func main() {
	apply := flag.Bool("apply", false, "apply the upgrade; default prints transaction SQL only")
	dsnEnv := flag.String("dsn-env", "", "environment variable containing the target tenant database DSN")
	flag.Parse()
	if err := run(*apply, *dsnEnv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(apply bool, dsnEnv string) error {
	ddl, err := dbassets.DepositAddressRotationUpgradeSQL()
	if err != nil {
		return err
	}
	if !apply {
		fmt.Print("BEGIN;\n" + ddl + "\nCOMMIT;\n")
		return nil
	}
	if dsnEnv == "" || os.Getenv(dsnEnv) == "" {
		return fmt.Errorf("-apply requires -dsn-env naming a populated environment variable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, os.Getenv(dsnEnv))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='30s'`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, ddl); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Println("Deposit address schema upgrade committed; business records and migration history preserved.")
	return nil
}
