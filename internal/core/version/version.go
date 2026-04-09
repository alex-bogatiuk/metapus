// Package version provides compile-time and schema version tracking.
// ExpectedSchemaVersion is the migration number this binary expects all tenant databases to be at.
// It is used by the version gate (ManagerConfig.VersionGroup) and the /api/v1/system/version endpoint.
package version

// ExpectedSchemaVersion is the highest goose migration number shipped with this binary.
// It MUST be updated whenever a new migration file is added to db/migrations/.
// Current: 00020_doc_basis_fields.sql
const ExpectedSchemaVersion = 20

// CompatibleSchema returns true when the tenant's schema is at the expected version.
// In future this could allow a range (e.g. ExpectedSchemaVersion-1..ExpectedSchemaVersion),
// but for now we require an exact match in cloud mode.
func CompatibleSchema(tenantSchemaVersion int) bool {
	return tenantSchemaVersion == ExpectedSchemaVersion
}
