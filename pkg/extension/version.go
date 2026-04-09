// Package extension is the PUBLIC Extension SDK for Metapus.
// Client extensions import this package (not internal/) to register
// custom catalogs, documents, registers, visitors, and recorders.
//
// Versioning: ExtensionAPIVersion follows semver. A major bump means
// one or more required interfaces changed signature.
package extension

// ExtensionAPIVersion is the semantic version of the Extension API contract.
const ExtensionAPIVersion = "1.0.0"
