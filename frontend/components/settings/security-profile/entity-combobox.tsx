"use client"

import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import {
  Check,
  ChevronsUpDown
} from "lucide-react"
import { useState } from "react"

export function EntityCombobox({
  value,
  onChange,
  documents,
  catalogs,
  placeholder = "Выберите сущность…",
  withWildcard = false,
  className,
}: {
  value: string
  onChange: (v: string) => void
  documents: { value: string; label: string }[]
  catalogs: { value: string; label: string }[]
  placeholder?: string
  withWildcard?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)

  const allItems = [
    ...(withWildcard ? [{ value: "*", label: "Все сущности" }] : []),
    ...documents,
    ...catalogs,
  ]
  const selectedLabel = allItems.find((e) => e.value === value)?.label

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn("h-8 text-xs justify-between font-normal", !selectedLabel && "text-muted-foreground", className)}
        >
          <span className="truncate">{selectedLabel ?? placeholder}</span>
          <ChevronsUpDown className="ml-2 h-3 w-3 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Поиск…" className="h-8 text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-center text-xs text-muted-foreground">Не найдено</CommandEmpty>
            {withWildcard && (
              <CommandGroup>
                <CommandItem
                  value="*"
                  onSelect={() => { onChange("*"); setOpen(false) }}
                  className="text-xs"
                >
                  <Check className={cn("mr-2 h-3 w-3", value === "*" ? "opacity-100" : "opacity-0")} />
                  Все сущности
                </CommandItem>
              </CommandGroup>
            )}
            {documents.length > 0 && (
              <CommandGroup heading="Документы">
                {documents.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onChange(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    <Check className={cn("mr-2 h-3 w-3", value === e.value ? "opacity-100" : "opacity-0")} />
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {catalogs.length > 0 && (
              <CommandGroup heading="Справочники">
                {catalogs.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onChange(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    <Check className={cn("mr-2 h-3 w-3", value === e.value ? "opacity-100" : "opacity-0")} />
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
