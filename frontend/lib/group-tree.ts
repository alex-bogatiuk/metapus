/**
 * The group tree of a hierarchical catalog (1C "Группы" in a selection form),
 * built from its folders: each folder under its parent, siblings by name.
 */

export interface GroupFolder {
    id: string
    name: string
    parentId: string | null
}

export interface GroupNode extends GroupFolder {
    children: GroupNode[]
}

/** A node as drawn: its depth and whether it has children. */
export interface VisibleGroup {
    node: GroupNode
    depth: number
    hasChildren: boolean
}

/**
 * Nests the folders by parent. A folder whose parent is not among them is a
 * root (the parent is outside what the user may see, or not a folder).
 */
export function buildGroupTree(folders: readonly GroupFolder[]): GroupNode[] {
    const nodes = new Map(folders.map((folder) => [folder.id, { ...folder, children: [] as GroupNode[] }]))
    const roots: GroupNode[] = []
    for (const node of nodes.values()) {
        const parent = node.parentId ? nodes.get(node.parentId) : undefined
        if (parent && parent !== node) parent.children.push(node)
        else roots.push(node)
    }
    const byName = (a: GroupNode, b: GroupNode) => a.name.localeCompare(b.name, "ru")
    const sort = (list: GroupNode[]) => {
        list.sort(byName)
        list.forEach((node) => sort(node.children))
    }
    sort(roots)
    return roots
}

/** The nodes drawn with the given groups expanded, depth-first. */
export function visibleGroups(roots: readonly GroupNode[], expanded: ReadonlySet<string>): VisibleGroup[] {
    const out: VisibleGroup[] = []
    const walk = (nodes: readonly GroupNode[], depth: number) => {
        for (const node of nodes) {
            out.push({ node, depth, hasChildren: node.children.length > 0 })
            if (expanded.has(node.id)) walk(node.children, depth + 1)
        }
    }
    walk(roots, 0)
    return out
}

/** The group and its ancestors, root first: what to expand to show it. */
export function groupPath(folders: readonly GroupFolder[], id: string): string[] {
    const byId = new Map(folders.map((folder) => [folder.id, folder]))
    const path: string[] = []
    let current = byId.get(id)
    while (current && !path.includes(current.id)) {
        path.unshift(current.id)
        current = current.parentId ? byId.get(current.parentId) : undefined
    }
    return path
}
