// Package version provides compile-time and schema version tracking.
// ExpectedSchemaVersion is the migration number this binary expects all tenant databases to be at.
// It is exposed by the system version endpoint and used to detect schema drift.
package version

// ExpectedSchemaVersion is the highest goose migration number shipped with this binary.
// It MUST match the highest tenant migration version (db/migrations and
// db/configuration/*); version_test.go guards drift.
const ExpectedSchemaVersion = 9

// CompatibleSchema returns true when the tenant's schema is at the expected version.
// In future this could allow a supported range, but for now compatibility requires an exact match.
func CompatibleSchema(tenantSchemaVersion int) bool {
	return tenantSchemaVersion == ExpectedSchemaVersion
}
