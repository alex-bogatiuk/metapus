package platform

import "metapus/internal/metadata"

// ---------------------------------------------------------------------------
// Optional interfaces — implement to provide richer metadata.
// Adding a new optional interface is a MINOR version bump (no breakage).
// The router checks for these via type assertion:
//
//	if p, ok := factory.(platform.Presentable); ok { ... }
// ---------------------------------------------------------------------------

// Presentable provides rich display names for the UI.
type Presentable interface {
	EntityPresentation() metadata.Presentation
}

// Inspectable provides a zero-value struct for metadata.Inspect().
type Inspectable interface {
	EntityStruct() interface{}
}

// Labeled provides a human-readable entity label.
type Labeled interface {
	EntityLabel() string
}

// ReferenceProvider declares which reference types this catalog satisfies.
// E.g. ["supplier", "customer"] for Counterparty.
type ReferenceProvider interface {
	ReferenceTypes() []string
}
