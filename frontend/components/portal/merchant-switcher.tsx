"use client"

import { useState } from "react"
import { Check, ChevronsUpDown, Store } from "lucide-react"

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { usePortalStore } from "@/stores/usePortalStore"

export function MerchantSwitcher() {
  const [open, setOpen] = useState(false)
  const { activeMerchantId, merchants, setActiveMerchant } = usePortalStore()

  const activeMerchant = merchants.find((m) => m.id === activeMerchantId)

  // Single merchant — no need for a switcher
  if (merchants.length <= 1) {
    return (
      <div className="flex items-center gap-2 text-sm font-medium">
        <Store className="size-4 text-muted-foreground" />
        <span>{activeMerchant?.name ?? "Мерчант"}</span>
        {activeMerchant?.code && (
          <span className="text-xs text-muted-foreground">
            ({activeMerchant.code})
          </span>
        )}
      </div>
    )
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-[260px] justify-between font-normal"
        >
          <div className="flex items-center gap-2 min-w-0">
            <Store className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate">
              {activeMerchant?.name ?? "Выберите мерчанта"}
            </span>
          </div>
          <ChevronsUpDown className="ml-2 size-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[260px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Поиск мерчанта..." />
          <CommandList>
            <CommandEmpty>Не найдено</CommandEmpty>
            <CommandGroup>
              {merchants.map((m) => (
                <CommandItem
                  key={m.id}
                  value={m.name}
                  onSelect={() => {
                    setActiveMerchant(m.id)
                    setOpen(false)
                  }}
                >
                  <Check
                    className={cn(
                      "mr-2 size-4",
                      activeMerchantId === m.id ? "opacity-100" : "opacity-0"
                    )}
                  />
                  <div className="min-w-0">
                    <div className="truncate">{m.name}</div>
                    <div className="text-xs text-muted-foreground">{m.code}</div>
                  </div>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
