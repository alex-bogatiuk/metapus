package platform

import (
	"metapus/internal/domain"
)

// Re-export hook types for client extensions.

// Hook is a lifecycle callback for entity operations.
type Hook[T any] = domain.Hook[T]

// HookEvent identifies the lifecycle event (BeforeCreate, AfterCreate, etc.).
type HookEvent = domain.HookEvent

// Standard hook events — re-exported for convenience.
const (
	BeforeCreate = domain.BeforeCreate
	AfterCreate  = domain.AfterCreate
	BeforeUpdate = domain.BeforeUpdate
	AfterUpdate  = domain.AfterUpdate
	BeforeDelete = domain.BeforeDelete
	AfterDelete  = domain.AfterDelete
)
