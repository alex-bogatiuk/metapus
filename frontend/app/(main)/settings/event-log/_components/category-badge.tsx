import { Badge } from "@/components/ui/badge"
import type { EventCategory } from "@/types/event-log"
import { EVENT_CATEGORIES } from "@/types/event-log"

export function CategoryBadge({ category }: { category: EventCategory }) {
  const label = EVENT_CATEGORIES.find((c) => c.value === category)?.label ?? category
  return <Badge variant="secondary">{label}</Badge>
}
