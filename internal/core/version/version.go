// Package version provides compile-time and schema version tracking.
// ExpectedSchemaVersion is the migration number this binary expects all tenant databases to be at.
// It is exposed by the system version endpoint and used to detect schema drift.
package version

// ExpectedSchemaVersion is the schema version of the delivery: the highest
// tenant migration of the platform and its configurations (db/migrations and
// db/configuration/*) below FirstExtensionMigration. Extension modules'
// migrations do not move it; version_test.go guards drift.
const ExpectedSchemaVersion = 9

// FirstExtensionMigration is the first migration version reserved for
// extension modules. The platform and configurations number theirs below it;
// schema checks read the database's version as the highest applied one
// below it.
const FirstExtensionMigration = 10000

// CompatibleSchema returns true when the tenant's schema is at the expected version.
// In future this could allow a supported range, but for now compatibility requires an exact match.
func CompatibleSchema(tenantSchemaVersion int) bool {
	return tenantSchemaVersion == ExpectedSchemaVersion
}
