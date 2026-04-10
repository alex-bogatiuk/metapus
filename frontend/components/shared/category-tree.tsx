"use client"

/**
 * CategoryTree — collapsible category tree sidebar for the product picker.
 *
 * Renders a hierarchical tree of nomenclature folders (isFolder: true) fetched
 * from `api.nomenclature.tree()`. Allows filtering products by parentId.
 *
 * Extracted from the prototype's CategoryRow component, adapted to use
 * real NomenclatureResponse data (isFolder, parentId) instead of mock Category type.
 *
 * UX patterns:
 *   - 1С: tree of nomenclature groups in the picker sidebar
 *   - SAP: left-panel category filter in ValueHelp
 */

import { useState, useEffect, useMemo, useCallback } from "react"
import { ChevronRight, ChevronDown, FolderOpen, Folder } from "lucide-react"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import type { NomenclatureResponse } from "@/types/catalog"

// ── Types ───────────────────────────────────────────────────────────────

interface TreeNode {
    id: string
    name: string
    parentId: string | null
    children: TreeNode[]
}

interface CategoryTreeProps {
    /** Currently selected category ID ("all" for root) */
    selectedId: string
    /** Callback when category is selected */
    onSelect: (id: string) => void
    /** Additional CSS classes */
    className?: string
}

// ── Build tree from flat nomenclature list ───────────────────────────────

function buildTree(items: NomenclatureResponse[]): TreeNode[] {
    const folders = items.filter((i) => i.isFolder && !i.deletionMark)
    const nodeMap = new Map<string, TreeNode>()

    // Create nodes
    for (const f of folders) {
        nodeMap.set(f.id, { id: f.id, name: f.name, parentId: f.parentId ?? null, children: [] })
    }

    // Build hierarchy
    const roots: TreeNode[] = []
    for (const node of nodeMap.values()) {
        if (node.parentId && nodeMap.has(node.parentId)) {
            nodeMap.get(node.parentId)!.children.push(node)
        } else {
            roots.push(node)
        }
    }

    // Sort children by name
    function sortChildren(nodes: TreeNode[]) {
        nodes.sort((a, b) => a.name.localeCompare(b.name, "ru"))
        for (const n of nodes) sortChildren(n.children)
    }
    sortChildren(roots)

    return roots
}

// ── Module-level cache ──────────────────────────────────────────────────
let _treeCache: TreeNode[] | null = null

// ── Component ───────────────────────────────────────────────────────────

export function CategoryTree({ selectedId, onSelect, className }: CategoryTreeProps) {
    const [tree, setTree] = useState<TreeNode[]>(_treeCache ?? [])
    const [loading, setLoading] = useState(!_treeCache)
    const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())

    // Fetch tree once
    useEffect(() => {
        // If cache exists, state was already initialized from it
        if (_treeCache) return

        let cancelled = false

        apiFetch<NomenclatureResponse[]>("/catalog/nomenclature/tree")
            .then((data) => {
                if (cancelled) return
                const built = buildTree(data)
                _treeCache = built
                setTree(built)

                // Auto-expand first level
                const firstLevelIds = new Set(built.map((n) => n.id))
                setExpandedIds(firstLevelIds)
            })
            .catch(() => {
                if (cancelled) return
                setTree([])
            })
            .finally(() => {
                if (!cancelled) setLoading(false)
            })

        return () => { cancelled = true }
    }, [])

    const toggleExpand = useCallback((id: string) => {
        setExpandedIds((prev) => {
            const next = new Set(prev)
            if (next.has(id)) {
                next.delete(id)
            } else {
                next.add(id)
            }
            return next
        })
    }, [])

    const isAllSelected = selectedId === "all"

    return (
        <div className={cn("flex flex-col", className)}>
            <div className="flex-none px-3 py-2 border-b bg-muted/30">
                <span className="text-xs font-medium text-muted-foreground">Категории</span>
            </div>
            <ScrollArea className="flex-1">
                <div className="py-1">
                    {/* "All" root node */}
                    <div
                        className={cn(
                            "flex items-center gap-1.5 px-3 py-1.5 cursor-pointer text-xs hover:bg-muted/50 transition-colors",
                            isAllSelected && "bg-primary/10 text-primary font-medium",
                        )}
                        onClick={() => onSelect("all")}
                    >
                        <FolderOpen className="h-3.5 w-3.5 shrink-0" />
                        <span className="truncate flex-1">Все товары</span>
                    </div>

                    {loading ? (
                        <div className="px-3 py-4 text-xs text-muted-foreground text-center">
                            Загрузка…
                        </div>
                    ) : (
                        tree.map((node) => (
                            <CategoryTreeNode
                                key={node.id}
                                node={node}
                                level={1}
                                selectedId={selectedId}
                                expandedIds={expandedIds}
                                onSelect={onSelect}
                                onToggle={toggleExpand}
                            />
                        ))
                    )}
                </div>
            </ScrollArea>
        </div>
    )
}

// ── Tree Node (recursive) ───────────────────────────────────────────────

function CategoryTreeNode({
    node,
    level,
    selectedId,
    expandedIds,
    onSelect,
    onToggle,
}: {
    node: TreeNode
    level: number
    selectedId: string
    expandedIds: Set<string>
    onSelect: (id: string) => void
    onToggle: (id: string) => void
}) {
    const hasChildren = node.children.length > 0
    const isExpanded = expandedIds.has(node.id)
    const isSelected = selectedId === node.id

    return (
        <>
            <div
                className={cn(
                    "flex items-center gap-1 px-2 py-1 cursor-pointer text-xs hover:bg-muted/50 transition-colors",
                    isSelected && "bg-primary/10 text-primary font-medium",
                )}
                style={{ paddingLeft: `${level * 12 + 8}px` }}
                onClick={() => onSelect(node.id)}
            >
                {hasChildren ? (
                    <button
                        onClick={(e) => {
                            e.stopPropagation()
                            onToggle(node.id)
                        }}
                        className="p-0.5 hover:bg-muted rounded -ml-1 shrink-0"
                    >
                        {isExpanded ? (
                            <ChevronDown className="h-3 w-3" />
                        ) : (
                            <ChevronRight className="h-3 w-3" />
                        )}
                    </button>
                ) : (
                    <span className="w-4 shrink-0" />
                )}
                {isExpanded ? (
                    <FolderOpen className="h-3 w-3 shrink-0 text-muted-foreground" />
                ) : (
                    <Folder className="h-3 w-3 shrink-0 text-muted-foreground" />
                )}
                <span className="truncate flex-1">{node.name}</span>
            </div>
            {hasChildren && isExpanded && node.children.map((child) => (
                <CategoryTreeNode
                    key={child.id}
                    node={child}
                    level={level + 1}
                    selectedId={selectedId}
                    expandedIds={expandedIds}
                    onSelect={onSelect}
                    onToggle={onToggle}
                />
            ))}
        </>
    )
}
