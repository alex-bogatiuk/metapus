// Package httpvalue holds wire values of the HTTP API.
package httpvalue

import (
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/httpvalue"
)

type (
	// BusinessReference permits omission, while rejecting explicit null, empty
	// strings and non-string values at the HTTP boundary.
	BusinessReference = core.BusinessReference
	// StringDictionary distinguishes omission from {} and rejects null,
	// including null values inside the dictionary, at the strict HTTP boundary.
	StringDictionary = core.StringDictionary
)
