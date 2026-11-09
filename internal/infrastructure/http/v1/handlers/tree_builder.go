package handlers

import (
	"metapus/internal/core/id"
)

// TreeNode represents a node in a nested tree structure.
// Used by GetTree handler to return a frontend-friendly hierarchical response.
type TreeNode struct {
	// The original DTO (mapped from entity)
	Data any `json:"data"`

	// Extracted fields for tree building
	ID       id.ID  `json:"-"`
	ParentID *id.ID `json:"-"`
	IsFolder bool   `json:"isFolder"`

	// Children nodes
	Children []*TreeNode `json:"children"`
}

// BuildTreeFromNodes converts a flat slice of TreeNodes into a nested tree.
// Nodes must be pre-populated with Data, ID, ParentID, IsFolder.
//
// Algorithm (O(n)):
// 1. Index nodes by ID
// 2. Attach children to parents
// 3. Return root nodes (no parent or parent not in list)
func BuildTreeFromNodes(nodes []*TreeNode) []*TreeNode {
	if len(nodes) == 0 {
		return []*TreeNode{}
	}

	// Index by ID
	index := make(map[id.ID]*TreeNode, len(nodes))
	for _, node := range nodes {
		node.Children = []*TreeNode{} // ensure non-nil
		index[node.ID] = node
	}

	// Build tree
	var roots []*TreeNode
	for _, node := range nodes {
		if node.ParentID == nil || id.IsNil(*node.ParentID) {
			roots = append(roots, node)
		} else if parent, ok := index[*node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			// Orphan — parent not in result set, treat as root
			roots = append(roots, node)
		}
	}

	if roots == nil {
		roots = []*TreeNode{}
	}

	return roots
}
