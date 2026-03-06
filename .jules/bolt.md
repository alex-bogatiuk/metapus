# Bolt's Journal — Critical Learnings Only

> ⚠️ Only add entries for critical, surprising, or reusable learnings.

## 2026-03-04 - O(N²) .find() in JSX render loops is a common pattern in this codebase
**Learning:** Document form pages with tabular parts (lines) use `(doc?.lines ?? []).find(l => l.someId === line.someId)?.ref?.name` inside `.map()` to resolve display names for ReferenceField. This is O(N²) and fires on every keystroke since `lines` state changes trigger re-render.
**Action:** When adding new document forms with tabular parts, always pre-build `useMemo` lookup Maps from `doc.lines` for display name resolution. Check existing forms for the same anti-pattern.

## 2026-03-04 - Double setLines pattern in ReferenceField onChange handlers
**Learning:** The `new` goods-receipt page had a pattern where ReferenceField onChange called `updateLine(key, "fieldId", id)` followed by a second `setLines(prev => prev.map(...))` to set the display name. This caused 2 array traversals (O(2N)) where 1 suffices. Additionally, `updateLine` used stale closure (`lines.map(...)`) instead of functional form (`prev => prev.map(...)`), risking state desync.
**Action:** Always merge multi-field line updates into a single `setLines` call with functional form. All state updaters in render-loop callbacks must use `prev =>` form.

## 2026-03-04 - toLocaleString creates new Intl.NumberFormat on every call
**Learning:** `number.toLocaleString("ru-RU", opts)` internally creates a new `Intl.NumberFormat` each time. In render loops (per-line formatting of amounts), this means N allocations per render. Caching the formatter as a module-level `const` eliminates this.
**Action:** For any `toLocaleString` / `toLocaleDateString` called in hot paths (render loops, list columns), extract a module-level cached `Intl.NumberFormat` / `Intl.DateTimeFormat` constant.

## 2026-03-04 - Array.includes() in selection hooks and DataTable is O(N²)
**Learning:** `useListSelection` computed `isAllSelected`/`isIndeterminate` using `selectedIds.includes(id)` inside `.every()`/`.some()` — O(N²). The same pattern existed in `DataTable` rows: `selectedIds.includes(item.id)` per row. Both are shared components used by ALL list pages.
**Action:** Always convert selection ID arrays to `Set` via `useMemo` before using in loops. Watch for `.includes()` inside `.map()`, `.every()`, `.some()`, `.filter()` — these are hidden O(N²).

## 2026-03-04 - Debug fmt.Printf left in production backend code
**Learning:** `document_repo/base.go` had 3 `fmt.Printf("DEBUG ...")` statements inside `buildWhereConditions()` — a method called on **every** document list API request. This added I/O overhead and polluted stdout on every call.
**Action:** Before committing, grep for `fmt.Print` / `console.log` in production paths. Test files are fine, but handler/repo code should never have debug prints.

## 2026-03-04 - ReferenceField had dual useEffects causing duplicate API calls on open
**Learning:** Two separate `useEffect` hooks — one for debounced search, one for initial load — both had `open` in their dependency arrays. When the popover opened, both fired: one immediately, one after 200ms delay. Same `""` query hit the API twice per dropdown open. With 6+ ReferenceFields on a document form, that's 12+ wasted requests.
**Action:** When a component needs both "immediate action on state change" and "debounced action on input change", merge into a single effect with ref-based tracking of which transition occurred.
