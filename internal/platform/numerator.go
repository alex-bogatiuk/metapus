package platform

// Re-export numerator types for client extensions.

import "metapus/internal/core/numerator"

// Generator is the code/number generator interface.
type Generator = numerator.Generator

// NumeratorConfig configures number generation for an entity.
type NumeratorConfig = numerator.Config

// DefaultNumeratorConfig creates a standard numerator config with prefix.
var DefaultNumeratorConfig = numerator.DefaultConfig
