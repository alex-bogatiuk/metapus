package handlers

import (
	"testing"

	"metapus/internal/core/id"

	"github.com/stretchr/testify/assert"
)

func TestBuildTreeFromNodes_EmptyList(t *testing.T) {
	result := BuildTreeFromNodes(nil)
	assert.Empty(t, result)

	result = BuildTreeFromNodes([]*TreeNode{})
	assert.Empty(t, result)
}

func TestBuildTreeFromNodes_SingleRoot(t *testing.T) {
	rootID := id.New()

	nodes := []*TreeNode{
		{Data: "root", ID: rootID, ParentID: nil, IsFolder: true},
	}

	tree := BuildTreeFromNodes(nodes)

	assert.Len(t, tree, 1)
	assert.Equal(t, "root", tree[0].Data)
	assert.Empty(t, tree[0].Children)
}

func TestBuildTreeFromNodes_MultipleRoots(t *testing.T) {
	id1 := id.New()
	id2 := id.New()

	nodes := []*TreeNode{
		{Data: "root1", ID: id1, ParentID: nil, IsFolder: true},
		{Data: "root2", ID: id2, ParentID: nil, IsFolder: false},
	}

	tree := BuildTreeFromNodes(nodes)

	assert.Len(t, tree, 2)
	assert.Equal(t, "root1", tree[0].Data)
	assert.Equal(t, "root2", tree[1].Data)
}

func TestBuildTreeFromNodes_NestedTree(t *testing.T) {
	rootID := id.New()
	childID := id.New()
	grandchildID := id.New()

	nodes := []*TreeNode{
		{Data: "root", ID: rootID, ParentID: nil, IsFolder: true},
		{Data: "child", ID: childID, ParentID: &rootID, IsFolder: true},
		{Data: "grandchild", ID: grandchildID, ParentID: &childID, IsFolder: false},
	}

	tree := BuildTreeFromNodes(nodes)

	assert.Len(t, tree, 1)
	assert.Equal(t, "root", tree[0].Data)

	assert.Len(t, tree[0].Children, 1)
	assert.Equal(t, "child", tree[0].Children[0].Data)

	assert.Len(t, tree[0].Children[0].Children, 1)
	assert.Equal(t, "grandchild", tree[0].Children[0].Children[0].Data)
	assert.Empty(t, tree[0].Children[0].Children[0].Children)
}

func TestBuildTreeFromNodes_OrphanedNodes(t *testing.T) {
	rootID := id.New()
	orphanParentID := id.New() // does not exist in the list

	nodes := []*TreeNode{
		{Data: "root", ID: rootID, ParentID: nil, IsFolder: true},
		{Data: "orphan", ID: id.New(), ParentID: &orphanParentID, IsFolder: false},
	}

	tree := BuildTreeFromNodes(nodes)

	// Orphan should be treated as root
	assert.Len(t, tree, 2)
	assert.Equal(t, "root", tree[0].Data)
	assert.Equal(t, "orphan", tree[1].Data)
}

func TestBuildTreeFromNodes_MultipleChildrenPerParent(t *testing.T) {
	parentID := id.New()

	nodes := []*TreeNode{
		{Data: "parent", ID: parentID, ParentID: nil, IsFolder: true},
		{Data: "child1", ID: id.New(), ParentID: &parentID, IsFolder: false},
		{Data: "child2", ID: id.New(), ParentID: &parentID, IsFolder: false},
		{Data: "child3", ID: id.New(), ParentID: &parentID, IsFolder: true},
	}

	tree := BuildTreeFromNodes(nodes)

	assert.Len(t, tree, 1)
	assert.Equal(t, "parent", tree[0].Data)
	assert.Len(t, tree[0].Children, 3)
	assert.Equal(t, "child1", tree[0].Children[0].Data)
	assert.Equal(t, "child2", tree[0].Children[1].Data)
	assert.Equal(t, "child3", tree[0].Children[2].Data)
}
