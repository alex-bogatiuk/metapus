// Package platform defines the stable Extension API for Metapus.
// Client extensions import this package to register custom catalogs, documents,
// registers, visitors, recorders, and hooks.
//
// Versioning: ExtensionAPIVersion follows semver. A major bump means
// one or more required interfaces changed signature. Minor bumps add
// new optional interfaces (checked via type assertion — no breakage).
package platform

// ExtensionAPIVersion is the semantic version of the Extension API contract.
// Client extensions can assert compatibility at init-time.
const ExtensionAPIVersion = "1.0.0"
