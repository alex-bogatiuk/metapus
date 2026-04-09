// Package domain provides the core business logic.
// related_documents.go — types and interface for the Related Documents feature.
// Analogous to 1C's "Структура подчиненности" / SAP's "Document Flow".
package domain

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// RelatedDocumentsRequest specifies which entity to find related documents for.
type RelatedDocumentsRequest struct {
	EntityName string // "GoodsReceipt", "GoodsIssue"
	EntityID   id.ID
}

// RelatedDocGroup is a group of related documents of the same type.
type RelatedDocGroup struct {
	EntityName   string           `json:"entityName"`   // e.g. "GoodsIssue"
	EntityType   string           `json:"entityType"`   // "document" | "catalog"
	Presentation string           `json:"presentation"` // e.g. "Реализации товаров"
	RoutePrefix  string           `json:"routePrefix"`  // e.g. "goods-issue"
	Items        []RelatedDocItem `json:"items"`
	TotalCount   int              `json:"totalCount"`
}

// RelatedDocItem is a single related document in a group.
type RelatedDocItem struct {
	ID           id.ID             `json:"id"`
	Presentation string            `json:"presentation"` // e.g. "РТ-00015  15.03.2026"
	Number       string            `json:"number"`
	Date         time.Time         `json:"date"`
	Posted       bool              `json:"posted"`
	DeletionMark bool              `json:"deletionMark"`
	Amount       int64             `json:"amount,omitempty"`
	CurrencyID   *id.ID            `json:"currencyId,omitempty"`
	PreviewData  map[string]string `json:"previewData,omitempty"` // label → resolved name (e.g. "Поставщик" → "ООО Ромашка")
}

// ── Tree structure for document subordination ───────────────────────────

// RelatedDocTreeNode represents a node in the document subordination tree.
// Analogous to 1C's "Структура подчинённости документа".
type RelatedDocTreeNode struct {
	RelatedDocItem
	EntityName  string               `json:"entityName"`  // e.g. "GoodsReceipt"
	EntityType  string               `json:"entityType"`  // "document"
	RoutePrefix string               `json:"routePrefix"` // e.g. "goods-receipt"
	IsCurrent   bool                 `json:"isCurrent"`   // true for the document user is viewing
	Children    []RelatedDocTreeNode `json:"children,omitempty"`
}

// RelatedDocumentsResult wraps the response for the related-documents endpoint.
// Contains a subordination tree (root → children) and optional flat groups for
// FK-references that are not part of the basis_type/basis_id chain.
type RelatedDocumentsResult struct {
	// Tree is the subordination tree. Root is the top-level document in the chain.
	// nil if the current document has no basis links at all.
	Tree *RelatedDocTreeNode `json:"tree,omitempty"`

	// FlatGroups contains FK-referenced documents that are NOT part of the
	// basis chain (e.g. documents referencing the current one via direct FK).
	FlatGroups []RelatedDocGroup `json:"flatGroups,omitempty"`

	// Total is the total number of documents in tree + flat groups.
	Total int `json:"total"`
}

// RelatedDocFinder searches for all documents that reference the given entity.
// Uses RefFinder + metadata.Registry to compute reverse document links.
type RelatedDocFinder interface {
	// FindRelatedDocuments returns the full subordination tree plus flat FK-references.
	FindRelatedDocuments(ctx context.Context, req RelatedDocumentsRequest) (*RelatedDocumentsResult, error)
}
