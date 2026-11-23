package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	corejson "github.com/alex-bogatiuk/metapus/internal/core/json"
)

// RegisterJSONCodecs installs the product JSON v2 semantics for PostgreSQL
// json/jsonb values. It lives in infrastructure because pgx is an adapter,
// not part of the core serialization contract.
func RegisterJSONCodecs(m *pgtype.Map) {
	m.RegisterType(&pgtype.Type{
		Name:  "json",
		OID:   pgtype.JSONOID,
		Codec: &pgtype.JSONCodec{Marshal: corejson.Marshal, Unmarshal: corejson.Unmarshal},
	})
	m.RegisterType(&pgtype.Type{
		Name:  "jsonb",
		OID:   pgtype.JSONBOID,
		Codec: &pgtype.JSONBCodec{Marshal: corejson.Marshal, Unmarshal: corejson.Unmarshal},
	})
}

// ConfigurePool composes JSON codec registration with an existing
// AfterConnect hook. Call it for every pgxpool.Config used by product code.
func ConfigurePool(config *pgxpool.Config) {
	previous := config.AfterConnect
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if previous != nil {
			if err := previous(ctx, conn); err != nil {
				return err
			}
		}
		RegisterJSONCodecs(conn.TypeMap())
		return nil
	}
}

// OpenPool is pgxpool.New with the mandatory Metapus JSON codecs installed.
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DSN: %w", err)
	}
	ConfigurePool(config)
	return pgxpool.NewWithConfig(ctx, config)
}
