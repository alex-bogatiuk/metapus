package domain

import (
	"context"

	"metapus/internal/core/id"
)

// MarkedObject represents an entity with deletion mark, enriched with reference count.
type MarkedObject struct {
	EntityName   string `json:"entityName"`
	EntityType   string `json:"entityType"`   // "catalog" | "document"
	EntityID     id.ID  `json:"entityId"`
	Presentation string `json:"presentation"` // resolved display string
	RefCount     int    `json:"refCount"`      // number of incoming references
	CanDelete    bool   `json:"canDelete"`     // true if refCount == 0
}

// DeleteMarkedRequest specifies a single entity to delete.
type DeleteMarkedRequest struct {
	EntityName string `json:"entityName" binding:"required"`
	EntityID   id.ID  `json:"entityId"   binding:"required"`
}

// DeleteMarkedResult is the result of batch deletion.
type DeleteMarkedResult struct {
	Deleted int `json:"deleted"` // successfully deleted
	Skipped int `json:"skipped"` // skipped due to references
	Errors  int `json:"errors"`  // failed due to errors
}

// MarkedObjectsProcessor lists and deletes marked objects.
// Analogous to 1C's "Удаление помеченных объектов".
type MarkedObjectsProcessor interface {
	// ListMarkedObjects returns all deletion-marked entities with reference counts.
	ListMarkedObjects(ctx context.Context) ([]MarkedObject, error)

	// DeleteMarked permanently deletes the specified entities.
	// Only deletes entities with no incoming references.
	DeleteMarked(ctx context.Context, items []DeleteMarkedRequest) (DeleteMarkedResult, error)
}
