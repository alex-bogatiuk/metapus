package domain

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// ParentAccessor is an interface for entities that support hierarchy.
// Used by HierarchyValidator to access parent-related fields.
type ParentAccessor interface {
	GetID() id.ID
	GetParentID() *id.ID
	GetIsFolder() bool
}

// HierarchyValidator validates hierarchy constraints for catalog entities.
// Checks: cycle detection, depth limits, parent-must-be-folder.
type HierarchyValidator struct {
	meta entity.CatalogMeta
}

// NewHierarchyValidator creates a new validator for the given catalog metadata.
func NewHierarchyValidator(meta entity.CatalogMeta) *HierarchyValidator {
	return &HierarchyValidator{meta: meta}
}

// ValidateHierarchy validates hierarchy constraints for an entity.
// Should be called during Create and Update operations.
// parentAccessor is the entity being validated.
// getByID is a function to fetch entities by ID (injected to avoid repo dependency).
func (v *HierarchyValidator) ValidateHierarchy(
	ctx context.Context,
	accessor ParentAccessor,
	getByID func(ctx context.Context, id id.ID) (ParentAccessor, error),
) error {
	if !v.meta.Hierarchical {
		// For flat catalogs, parent should be nil
		if accessor.GetParentID() != nil && !id.IsNil(*accessor.GetParentID()) {
			return apperror.NewValidation("flat catalog does not support hierarchy").
				WithDetail("field", "parentId")
		}
		return nil
	}

	parentID := accessor.GetParentID()
	if parentID == nil || id.IsNil(*parentID) {
		// Root element — always valid
		return nil
	}

	// 1. Check parent exists and is a folder (if required)
	parent, err := getByID(ctx, *parentID)
	if err != nil {
		if apperror.IsNotFound(err) {
			return apperror.NewValidation("parent not found").
				WithDetail("field", "parentId").
				WithDetail("parentId", parentID.String())
		}
		return fmt.Errorf("get parent: %w", err)
	}

	// 2. Check parent is folder (for GroupsAndItems mode)
	if v.meta.HierarchyType == entity.HierarchyGroupsAndItems && v.meta.FolderAsParentOnly {
		if !parent.GetIsFolder() {
			return apperror.NewValidation("parent must be a folder (group)").
				WithDetail("field", "parentId").
				WithDetail("parentId", parentID.String())
		}
	}

	// 3. Cycle detection: walk up the parent chain
	entityID := accessor.GetID()
	if err := v.detectCycle(ctx, entityID, *parentID, getByID); err != nil {
		return err
	}

	// 4. Depth limit check
	if v.meta.MaxDepth > 0 {
		depth, err := v.calculateDepth(ctx, *parentID, getByID)
		if err != nil {
			return fmt.Errorf("calculate depth: %w", err)
		}
		if depth+1 > v.meta.MaxDepth {
			return apperror.NewValidation(
				fmt.Sprintf("maximum nesting depth exceeded (max: %d)", v.meta.MaxDepth),
			).WithDetail("field", "parentId").
				WithDetail("maxDepth", v.meta.MaxDepth)
		}
	}

	return nil
}

// detectCycle walks up the parent chain to detect cycles.
func (v *HierarchyValidator) detectCycle(
	ctx context.Context,
	entityID id.ID,
	parentID id.ID,
	getByID func(ctx context.Context, id id.ID) (ParentAccessor, error),
) error {
	visited := map[id.ID]struct{}{entityID: {}}
	currentID := parentID

	// Walk up the tree (max 1000 iterations as safety net)
	for i := 0; i < 1000; i++ {
		if _, seen := visited[currentID]; seen {
			return apperror.NewValidation("cycle detected in hierarchy").
				WithDetail("field", "parentId").
				WithDetail("parentId", parentID.String())
		}
		visited[currentID] = struct{}{}

		parent, err := getByID(ctx, currentID)
		if err != nil {
			if apperror.IsNotFound(err) {
				// Reached a broken reference — no cycle
				return nil
			}
			return fmt.Errorf("get parent for cycle check: %w", err)
		}

		nextParent := parent.GetParentID()
		if nextParent == nil || id.IsNil(*nextParent) {
			// Reached root — no cycle
			return nil
		}
		currentID = *nextParent
	}

	return apperror.NewValidation("hierarchy too deep (possible cycle)").
		WithDetail("field", "parentId")
}

// calculateDepth returns how deep an entity is relative to root.
func (v *HierarchyValidator) calculateDepth(
	ctx context.Context,
	parentID id.ID,
	getByID func(ctx context.Context, id id.ID) (ParentAccessor, error),
) (int, error) {
	depth := 1
	currentID := parentID

	for i := 0; i < 1000; i++ {
		parent, err := getByID(ctx, currentID)
		if err != nil {
			if apperror.IsNotFound(err) {
				return depth, nil
			}
			return 0, err
		}

		nextParent := parent.GetParentID()
		if nextParent == nil || id.IsNil(*nextParent) {
			return depth, nil
		}
		depth++
		currentID = *nextParent
	}

	return depth, nil
}
