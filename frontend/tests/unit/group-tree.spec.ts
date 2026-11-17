import { test, expect } from "@playwright/test"
import { buildGroupTree, groupPath, visibleGroups, type GroupFolder } from "../../lib/group-tree"

const folders: GroupFolder[] = [
    { id: "fix", name: "Крепёж", parentId: null },
    { id: "bolts", name: "Болты", parentId: "fix" },
    { id: "nails", name: "Гвозди", parentId: "fix" },
    { id: "m8", name: "М8", parentId: "bolts" },
    { id: "chem", name: "Химия", parentId: null },
    { id: "orphan", name: "Анкеры", parentId: "hidden" },
]

test("folders nest under their parents, siblings by name, orphans at the root", () => {
    const roots = buildGroupTree(folders)
    expect(roots.map((node) => node.id)).toEqual(["orphan", "fix", "chem"])
    expect(roots[1].children.map((node) => node.id)).toEqual(["bolts", "nails"])
    expect(roots[1].children[0].children.map((node) => node.id)).toEqual(["m8"])
})

test("only expanded groups show their children, with depth", () => {
    const roots = buildGroupTree(folders)
    expect(visibleGroups(roots, new Set()).map((row) => row.node.id)).toEqual(["orphan", "fix", "chem"])
    const open = visibleGroups(roots, new Set(["fix", "bolts"]))
    expect(open.map((row) => `${row.node.id}:${row.depth}:${row.hasChildren}`))
        .toEqual(["orphan:0:false", "fix:0:true", "bolts:1:true", "m8:2:false", "nails:1:false", "chem:0:false"])
})

test("a group's path runs from the root to it", () => {
    expect(groupPath(folders, "m8")).toEqual(["fix", "bolts", "m8"])
    expect(groupPath(folders, "orphan")).toEqual(["orphan"])
    expect(groupPath(folders, "missing")).toEqual([])
})
