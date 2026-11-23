package domain

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// MarkedObjectRef identifies one object by its registry entity name and ID.
type MarkedObjectRef struct {
	EntityName string `json:"entityName" binding:"required"`
	EntityID   id.ID  `json:"entityId"   binding:"required"`
}

// DeletionStatus is the outcome a deletion plan predicts for one marked object.
type DeletionStatus string

const (
	// DeletionReady: nothing references the object.
	DeletionReady DeletionStatus = "ready"
	// DeletionWithGroup: only objects deleted in the same plan reference it.
	DeletionWithGroup DeletionStatus = "withGroup"
	// DeletionBlocked: something that stays in the database references it.
	DeletionBlocked DeletionStatus = "blocked"
	// DeletionExcluded: the object is marked but was not selected.
	DeletionExcluded DeletionStatus = "excluded"
)

// BlockReason explains why a selected object cannot be deleted.
type BlockReason string

const (
	// BlockLiveReference: an object without a deletion mark references it.
	BlockLiveReference BlockReason = "liveReference"
	// BlockNotSelected: a marked object that is not selected references it.
	BlockNotSelected BlockReason = "notSelected"
	// BlockChain: a selected object that cannot be deleted references it.
	BlockChain BlockReason = "chain"
	// BlockCycle: objects of different tables reference each other, so no
	// deletion order satisfies the foreign keys.
	BlockCycle BlockReason = "cycle"
)

// Source kinds of an incoming reference.
const (
	SourceKindCatalog  = "catalog"
	SourceKindDocument = "document"
	SourceKindRegister = "register"
	// SourceKindSystem is a table outside the metadata registry that holds a
	// foreign key to the object. Such rows can never be marked for deletion.
	SourceKindSystem = "system"
)

// IncomingReference is one object (or, for system tables, one group of rows)
// that references a marked object.
type IncomingReference struct {
	SourceEntityName string `json:"sourceEntityName,omitzero"`
	SourceEntityType string `json:"sourceEntityType"`
	SourceID         id.ID  `json:"sourceId,omitzero"`
	SourceField      string `json:"sourceField"`
	Presentation     string `json:"presentation"`
	// SourceMarked is true when the source carries a deletion mark itself.
	SourceMarked bool `json:"sourceMarked"`
	// SourcePosted is true for a posted document: it has to be unposted
	// before it can be marked.
	SourcePosted bool `json:"sourcePosted"`
	// SourceStatus is the planned status of a marked source.
	SourceStatus DeletionStatus `json:"sourceStatus,omitzero"`
	// Count is the number of rows the entry stands for; above 1 only for
	// system tables, whose rows are not individual objects.
	Count int `json:"count"`
}

// MarkedObject is one deletion-marked object together with what references it.
type MarkedObject struct {
	EntityName   string         `json:"entityName"`
	EntityType   string         `json:"entityType"` // "catalog" | "document"
	EntityID     id.ID          `json:"entityId"`
	Presentation string         `json:"presentation"`
	Status       DeletionStatus `json:"status"`
	Reason       BlockReason    `json:"reason,omitzero"`
	// BlockedBy is the reference that decided a blocked status.
	BlockedBy *IncomingReference `json:"blockedBy,omitzero"`
	// RefCount counts every referencing object (or system row).
	RefCount int `json:"refCount"`
	// LiveRefCount counts referencing objects that carry no deletion mark.
	LiveRefCount int `json:"liveRefCount"`
	// References lists every marked source and a sample of live ones.
	References []IncomingReference `json:"references"`
	// ReferencesTruncated is true when live references were sampled.
	ReferencesTruncated bool `json:"referencesTruncated"`
}

// DeletionPlan is the predicted result of deleting the selected marked objects.
type DeletionPlan struct {
	Items     []MarkedObject `json:"items"`
	Total     int            `json:"total"`
	Selected  int            `json:"selected"`
	Deletable int            `json:"deletable"`
	Blocked   int            `json:"blocked"`
	// Truncated is true when an entity had more marked objects than one scan
	// returns; the rest appear after the listed ones are deleted.
	Truncated bool `json:"truncated"`
}

// DeletionOutcome is what happened to one object during deletion.
type DeletionOutcome string

const (
	DeletionOutcomeDeleted DeletionOutcome = "deleted"
	DeletionOutcomeSkipped DeletionOutcome = "skipped"
	DeletionOutcomeFailed  DeletionOutcome = "failed"
)

// DeletedObjectResult reports the outcome for one requested object.
type DeletedObjectResult struct {
	EntityName   string          `json:"entityName"`
	EntityID     id.ID           `json:"entityId"`
	Presentation string          `json:"presentation"`
	Outcome      DeletionOutcome `json:"outcome"`
	Reason       BlockReason     `json:"reason,omitzero"`
	// Message is a user-facing explanation for skipped and failed objects.
	Message string `json:"message,omitzero"`
}

// DeleteMarkedResult is the result of executing a deletion plan.
type DeleteMarkedResult struct {
	Deleted int                   `json:"deleted"`
	Skipped int                   `json:"skipped"`
	Errors  int                   `json:"errors"`
	Items   []DeletedObjectResult `json:"items"`
}

// MarkedObjectsProcessor plans and performs physical deletion of
// deletion-marked objects. Analogous to 1C's "Удаление помеченных объектов".
type MarkedObjectsProcessor interface {
	// PlanDeletion lists every marked object with its incoming references and
	// the status it would get if the selection were deleted. A nil selection
	// selects every marked object.
	PlanDeletion(ctx context.Context, selection []MarkedObjectRef) (DeletionPlan, error)

	// DeleteMarked re-plans the selection under row locks and deletes every
	// object the plan allows, referencing objects first.
	DeleteMarked(ctx context.Context, items []MarkedObjectRef) (DeleteMarkedResult, error)
}
