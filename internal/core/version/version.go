// Package version provides compile-time and schema version tracking.
// ExpectedSchemaVersion is the platform migration number this binary expects
// all tenant databases to be at. It is exposed by the system version endpoint
// and used to detect schema drift.
package version

// ExpectedSchemaVersion is the platform's schema version: its highest tenant
// migration (db/migrations). A configuration's migrations (db/configuration/
// <name>) and the extension modules' have version histories of their own and
// do not move it; version_test.go guards drift.
const ExpectedSchemaVersion = 8

// CompatibleSchema returns true when the tenant's platform schema is at the
// expected version. Compatibility requires an exact match.
func CompatibleSchema(tenantSchemaVersion int) bool {
	return tenantSchemaVersion == ExpectedSchemaVersion
}
