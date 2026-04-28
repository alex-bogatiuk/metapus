// frontend/components/layout/sidebar-favorites.tsx
"use client"

import { useCallback, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import {
    Star,
    GripVertical,
    Trash2,
    ChevronDown,
} from "lucide-react"
import {
    DndContext,
    closestCenter,
    PointerSensor,
    useSensor,
    useSensors,
    type DragEndEvent,
} from "@dnd-kit/core"
import {
    SortableContext,
    useSortable,
    verticalListSortingStrategy,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { restrictToVerticalAxis } from "@dnd-kit/modifiers"

import {
    SidebarGroup,
    SidebarGroupLabel,
    SidebarGroupContent,
    SidebarMenu,
    SidebarMenuItem,
    SidebarMenuButton,
} from "@/components/ui/sidebar"
import {
    ContextMenu,
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
} from "@radix-ui/react-collapsible"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { useFavoritesStore } from "@/stores/useFavoritesStore"
import { useTabsStore } from "@/stores/useTabsStore"
import type { FavoriteItem } from "@/types/user-prefs"

const _defaultVisibleCount = 10

// ── Sortable item ───────────────────────────────────────────────────────

interface SortableFavoriteItemProps {
    item: FavoriteItem
    isActive: boolean
    onNavigate: (e: React.MouseEvent, item: FavoriteItem) => void
    onRemove: (item: FavoriteItem) => void
}

function SortableFavoriteItem({
    item,
    isActive,
    onNavigate,
    onRemove,
}: SortableFavoriteItemProps) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: `${item.entityType}::${item.entityId}` })

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
    }

    return (
        <ContextMenu>
            <ContextMenuTrigger asChild>
                <SidebarMenuItem
                    ref={setNodeRef}
                    style={style}
                    className={cn(isDragging && "opacity-50")}
                >
                    <SidebarMenuButton
                        asChild
                        isActive={isActive}
                        tooltip={item.title}
                    >
                        <a
                            href={item.url}
                            onClick={(e) => onNavigate(e, item)}
                            className="group/fav"
                        >
                            <span
                                className="cursor-grab opacity-0 group-hover/fav:opacity-60 transition-opacity"
                                {...attributes}
                                {...listeners}
                            >
                                <GripVertical className="h-3 w-3" />
                            </span>
                            <Star className="h-3.5 w-3.5 shrink-0 fill-yellow-400 text-yellow-400" />
                            <span className="truncate">{item.title}</span>
                        </a>
                    </SidebarMenuButton>
                </SidebarMenuItem>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-48">
                <ContextMenuItem
                    className="gap-2 text-destructive focus:text-destructive"
                    onClick={() => onRemove(item)}
                >
                    <Trash2 className="h-4 w-4" />
                    Убрать из избранного
                </ContextMenuItem>
            </ContextMenuContent>
        </ContextMenu>
    )
}

// ── SidebarFavorites ────────────────────────────────────────────────────

interface SidebarFavoritesProps {
    currentPath: string
    onItemClick: (e: React.MouseEvent, item: { title: string; url: string }) => void
}

export function SidebarFavorites({
    currentPath,
    onItemClick,
}: SidebarFavoritesProps) {
    const items = useFavoritesStore((s) => s.items)
    const isLoaded = useFavoritesStore((s) => s.isLoaded)
    const removeFavorite = useFavoritesStore((s) => s.removeFavorite)
    const reorder = useFavoritesStore((s) => s.reorder)
    const [showAll, setShowAll] = useState(false)

    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    )

    const visibleItems = useMemo(
        () => (showAll ? items : items.slice(0, _defaultVisibleCount)),
        [items, showAll],
    )

    const sortableIds = useMemo(
        () => visibleItems.map((f) => `${f.entityType}::${f.entityId}`),
        [visibleItems],
    )

    const handleDragEnd = useCallback(
        (event: DragEndEvent) => {
            const { active, over } = event
            if (!over || active.id === over.id) return

            const fromIndex = items.findIndex(
                (f) => `${f.entityType}::${f.entityId}` === active.id,
            )
            const toIndex = items.findIndex(
                (f) => `${f.entityType}::${f.entityId}` === over.id,
            )
            if (fromIndex !== -1 && toIndex !== -1) {
                reorder(fromIndex, toIndex)
            }
        },
        [items, reorder],
    )

    const handleRemove = useCallback(
        (item: FavoriteItem) => {
            removeFavorite(item.entityType, item.entityId)
            toast.success("Убрано из избранного")
        },
        [removeFavorite],
    )

    // Don't render section if no favorites or not loaded
    if (!isLoaded || items.length === 0) return null

    return (
        <SidebarGroup>
            <SidebarGroupLabel>
                <Star className="mr-1 h-3 w-3 fill-yellow-400 text-yellow-400" />
                Избранное
            </SidebarGroupLabel>
            <SidebarGroupContent>
                <DndContext
                    sensors={sensors}
                    collisionDetection={closestCenter}
                    modifiers={[restrictToVerticalAxis]}
                    onDragEnd={handleDragEnd}
                >
                    <SortableContext
                        items={sortableIds}
                        strategy={verticalListSortingStrategy}
                    >
                        <SidebarMenu>
                            {visibleItems.map((item) => (
                                <SortableFavoriteItem
                                    key={`${item.entityType}::${item.entityId}`}
                                    item={item}
                                    isActive={currentPath.startsWith(item.url)}
                                    onNavigate={(e) => onItemClick(e, item)}
                                    onRemove={handleRemove}
                                />
                            ))}

                            {/* Show more / less toggle */}
                            {items.length > _defaultVisibleCount && (
                                <SidebarMenuItem>
                                    <SidebarMenuButton
                                        className="text-muted-foreground text-xs"
                                        onClick={() => setShowAll((prev) => !prev)}
                                    >
                                        <ChevronDown
                                            className={cn(
                                                "h-3 w-3 transition-transform duration-200",
                                                showAll && "rotate-180",
                                            )}
                                        />
                                        <span>
                                            {showAll
                                                ? "Свернуть"
                                                : `Ещё ${items.length - _defaultVisibleCount}`}
                                        </span>
                                    </SidebarMenuButton>
                                </SidebarMenuItem>
                            )}
                        </SidebarMenu>
                    </SortableContext>
                </DndContext>
            </SidebarGroupContent>
        </SidebarGroup>
    )
}
