// frontend/components/shared/favorite-button.tsx
"use client"

import { useCallback, useEffect } from "react"
import { Star } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { useFavoritesStore } from "@/stores/useFavoritesStore"

const _maxFavorites = 50

interface FavoriteButtonProps {
    entityType: string
    entityId: string
    /** Current title from loaded entity data (for self-healing cache). */
    title: string
    /** Direct URL to the entity page. */
    url: string
    /** Button size variant. */
    size?: "sm" | "default"
}

/**
 * ⭐ toggle button for adding/removing an entity from favorites.
 *
 * Self-healing: if the title from loaded data differs from the cached
 * title in favorites, it silently updates the cache.
 */
export function FavoriteButton({
    entityType,
    entityId,
    title,
    url,
    size = "sm",
}: FavoriteButtonProps) {
    const isFavorite = useFavoritesStore((s) => s.isFavorite(entityType, entityId))
    const toggleFavorite = useFavoritesStore((s) => s.toggleFavorite)
    const refreshTitle = useFavoritesStore((s) => s.refreshTitle)
    const items = useFavoritesStore((s) => s.items)

    // Self-healing: update cached title when entity data loads with a different name
    useEffect(() => {
        if (isFavorite && title) {
            refreshTitle(entityType, entityId, title)
        }
    }, [isFavorite, entityType, entityId, title, refreshTitle])

    const handleClick = useCallback(() => {
        if (!isFavorite && items.length >= _maxFavorites) {
            toast.error(`Максимум ${_maxFavorites} избранных. Удалите неактуальные.`)
            return
        }

        toggleFavorite({ entityType, entityId, title, url })

        if (isFavorite) {
            toast.success("Убрано из избранного")
        } else {
            toast.success("Добавлено в избранное")
        }
    }, [isFavorite, items.length, toggleFavorite, entityType, entityId, title, url])

    const iconSize = size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4"
    const btnSize = size === "sm" ? "h-7 w-7" : "h-8 w-8"

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    variant="ghost"
                    size="icon"
                    className={cn(
                        btnSize,
                        "transition-transform duration-150 active:scale-90",
                    )}
                    onClick={handleClick}
                >
                    <Star
                        className={cn(
                            iconSize,
                            "transition-colors duration-200",
                            isFavorite
                                ? "fill-yellow-400 text-yellow-400"
                                : "text-muted-foreground",
                        )}
                    />
                </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
                {isFavorite ? "Убрать из избранного" : "Добавить в избранное"}
            </TooltipContent>
        </Tooltip>
    )
}
