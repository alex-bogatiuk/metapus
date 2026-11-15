// integration-config imports the integration configuration of exactly one tenant
// database. Secrets are read from secret files and the chains file;
// output contains counts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/platform/secretfile"
)

var errDryRun = errors.New("dry run rollback")

func run() error {
	apply := flag.Bool("apply", false, "Commit changes (default: validate then roll back)")
	chains := flag.String("chains", "", "chains.json with networks and RPC endpoints (optional)")
	mapping := flag.String("mapping", "", "Non-secret JSON endpoint/account mapping")
	activate := flag.Bool("activate", false, "Select database RPC configuration after import")
	flag.Parse()
	dsn, err := secretfile.Get("DATABASE_URL", "")
	if err != nil || dsn == "" {
		return errors.New("configure DATABASE_URL or DATABASE_URL_FILE for exactly one tenant")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid tenant database configuration")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("tenant database unavailable")
	}
	tm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, tm)
	var cfg *chainconfig.File
	if *chains != "" {
		cfg, err = chainconfig.Load(*chains)
		if err != nil {
			return errors.New("unable to load chains configuration; check file, secret mounts and topology")
		}
	}
	bindings := map[string]crypto_integration_repo.ImportBinding{}
	if *mapping != "" {
		info, e := os.Stat(*mapping)
		if e != nil || info.Size() > 4<<20 {
			return errors.New("mapping must be a readable file under 4 MiB")
		}
		raw, e := os.ReadFile(*mapping)
		if e != nil || json.Unmarshal(raw, &bindings) != nil {
			return errors.New("invalid non-secret account mapping")
		}
	}
	var report crypto_integration_repo.ImportReport
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		var e error
		report, e = crypto_integration_repo.ImportConfiguration(ctx, cfg, bindings, *activate)
		if e != nil {
			return e
		}
		if !*apply {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Apply  bool                                 `json:"apply"`
		Report crypto_integration_repo.ImportReport `json:"report"`
	}{*apply, report})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "integration-config:", err)
		os.Exit(1)
	}
}
