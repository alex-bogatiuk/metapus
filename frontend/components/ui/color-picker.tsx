"use client"

import * as React from "react"
import { Check, ChevronDown } from "lucide-react"

import { cn } from "@/lib/utils"
import { isPaletteKey, paletteColor, paletteInk, type PaletteKey } from "@/lib/palette"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

export interface ColorOption {
  value: string
  label: string
}

/** A palette swatch: a small rounded square, or a dashed outline when no colour is set. */
export function ColorSwatch({ color, className, ...props }: { color: string | null | undefined } & React.HTMLAttributes<HTMLSpanElement>) {
  const key = isPaletteKey(color) ? color : null
  return <span aria-hidden="true" {...props}
    className={cn("inline-block size-3 shrink-0 rounded-[4px]", key ? "shadow-[inset_0_0_0_1px_hsl(0_0%_0%/0.08)]" : "border border-dashed border-muted-foreground/50", className)}
    style={key ? { backgroundColor: paletteColor(key), ...props.style } : props.style} />
}

const COLUMNS = 7

export interface ColorPickerProps {
  /** Palette key, or "" for no colour. */
  value: string
  onChange: (value: string) => void
  /** Colours to offer, in order; values outside the palette are skipped. */
  options: readonly ColorOption[]
  /** Offer a "no colour" choice first; the interface then picks a colour itself. */
  allowEmpty?: boolean
  emptyLabel?: string
  id?: string
  disabled?: boolean
  readOnly?: boolean
  className?: string
  "aria-label"?: string
}

/**
 * Colour field in the shadcn style: a select-like trigger and a grid of swatches.
 * Arrow keys walk the grid, Enter or Space picks, Escape closes; the name of the
 * swatch under the pointer or focus shows below the grid.
 */
export function ColorPicker({
  value, onChange, options, allowEmpty = true, emptyLabel = "Автоматически",
  id, disabled, readOnly, className, "aria-label": ariaLabel,
}: ColorPickerProps) {
  const [open, setOpen] = React.useState(false)
  const choices = React.useMemo(() => {
    const colours = options.filter((o): o is { value: PaletteKey; label: string } => isPaletteKey(o.value))
    return allowEmpty ? [{ value: "", label: emptyLabel }, ...colours] : colours
  }, [options, allowEmpty, emptyLabel])
  const selected = choices.find(c => c.value === value) ?? null
  const selectedIndex = Math.max(0, choices.findIndex(c => c.value === value))
  const [active, setActive] = React.useState(selectedIndex)
  const cells = React.useRef<(HTMLButtonElement | null)[]>([])

  // Opening focuses the selected swatch. Done here rather than on the popover's mount, because a
  // popover reopened during its closing animation never unmounts and would keep focus on the trigger.
  React.useEffect(() => {
    if (!open) return
    const frame = requestAnimationFrame(() => cells.current[active]?.focus())
    return () => cancelAnimationFrame(frame)
    // Only on opening: later moves focus the swatch themselves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const openChange = (next: boolean) => {
    if (next && (disabled || readOnly)) return
    if (next) setActive(selectedIndex)
    setOpen(next)
  }
  const pick = (next: string) => {
    onChange(next)
    setOpen(false)
  }
  const move = (to: number) => {
    const index = Math.min(choices.length - 1, Math.max(0, to))
    setActive(index)
    cells.current[index]?.focus()
  }
  const onGridKeyDown = (event: React.KeyboardEvent) => {
    const step = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: COLUMNS, ArrowUp: -COLUMNS }[event.key]
    if (step != null) move(active + step)
    else if (event.key === "Home") move(0)
    else if (event.key === "End") move(choices.length - 1)
    else return
    event.preventDefault()
  }

  const shown = choices[active] ?? selected
  return <Popover open={open} onOpenChange={openChange}>
    <PopoverTrigger asChild>
      <button id={id} type="button" disabled={disabled} data-readonly={readOnly || undefined} aria-label={ariaLabel}
        className={cn(
          "flex h-8 w-full items-center gap-2 whitespace-nowrap rounded-md border border-input bg-field px-2.5 text-left text-sm shadow-control transition-[color,border-color,box-shadow] hover:border-muted-foreground/40 focus-visible:border-ring focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25 disabled:cursor-not-allowed disabled:opacity-50 data-[readonly]:cursor-default data-[readonly]:hover:border-input data-[state=open]:border-ring data-[state=open]:ring-[3px] data-[state=open]:ring-ring/25",
          className,
        )}>
        <ColorSwatch color={value} className="size-3.5" />
        <span className={cn("min-w-0 flex-1 truncate", !value && "text-muted-foreground")}>{selected?.label ?? value}</span>
        {!readOnly && <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
      </button>
    </PopoverTrigger>
    <PopoverContent align="start" className="w-auto rounded-lg p-2"
      onOpenAutoFocus={event => event.preventDefault()}>
      <div role="radiogroup" aria-label={ariaLabel ?? "Цвет"} onKeyDown={onGridKeyDown}
        className="grid gap-1.5" style={{ gridTemplateColumns: `repeat(${COLUMNS}, 1.75rem)` }}>
        {choices.map((choice, index) => {
          const isSelected = choice.value === value
          const key = isPaletteKey(choice.value) ? choice.value : null
          return <button key={choice.value || "empty"} ref={el => { cells.current[index] = el }}
            type="button" role="radio" aria-checked={isSelected} aria-label={choice.label} title={choice.label}
            tabIndex={index === active ? 0 : -1}
            onClick={() => pick(choice.value)}
            onFocus={() => setActive(index)}
            onPointerEnter={() => setActive(index)}
            className={cn(
              "relative grid size-7 place-items-center rounded-md outline-none transition-[transform,box-shadow] duration-150 ease-panel motion-reduce:transition-none",
              "hover:scale-110 focus-visible:scale-110 motion-reduce:hover:scale-100 motion-reduce:focus-visible:scale-100",
              "focus-visible:ring-[3px] focus-visible:ring-ring/25",
              key ? "shadow-[inset_0_0_0_1px_hsl(0_0%_0%/0.08)]" : "border border-dashed border-muted-foreground/50 text-muted-foreground",
              isSelected && "ring-2 ring-foreground/80 ring-offset-2 ring-offset-popover focus-visible:ring-2 focus-visible:ring-foreground/80",
            )}
            style={key ? { backgroundColor: paletteColor(key) } : undefined}>
            {isSelected && <Check className="size-3.5" strokeWidth={3} style={key ? { color: paletteInk(key) } : undefined} aria-hidden="true" />}
          </button>
        })}
      </div>
      <div className="mt-2 flex items-center gap-2 border-t px-0.5 pt-2 text-xs text-muted-foreground" aria-live="polite">
        <ColorSwatch color={shown?.value} />
        <span className="truncate text-foreground">{shown?.label}</span>
        {shown && shown.value === value && <span className="ml-auto">выбран</span>}
      </div>
    </PopoverContent>
  </Popover>
}
