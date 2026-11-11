// Package dbassets embeds PostgreSQL migrations for binaries started outside the repository root.
package dbassets

import "embed"

// Files contains both tenant and meta-database migrations.
//
//go:embed migrations/*.sql meta/*.sql
var Files embed.FS
