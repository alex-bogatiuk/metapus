"use client"

import { forwardRef, useEffect, useRef, useState, type ReactNode, type RefObject } from "react"
import { Button, type ButtonProps } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/** Space kept between the floating action and the edges of the visible area, px. */
const EDGE = 8

/** The vertical span of the element that is actually on screen: clipped by the window and every clipping ancestor. */
function visibleSpan(element: HTMLElement): { top: number; bottom: number } {
  const box = element.getBoundingClientRect()
  let top = Math.max(box.top, 0)
  let bottom = Math.min(box.bottom, window.innerHeight)
  for (let parent = element.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
    if (getComputedStyle(parent).overflowY === "visible") continue
    const clip = parent.getBoundingClientRect()
    top = Math.max(top, clip.top)
    bottom = Math.min(bottom, clip.bottom)
  }
  return { top, bottom }
}

/**
 * A narrow column beside a long form that carries a copy of its save action while
 * there are unsaved changes. The copy rides at the height of the mouse pointer, kept
 * inside the visible part of the form (without a mouse it rests mid-way), so saving
 * needs no scroll to the bottom. Once the form's own save area is on screen the copy
 * steps aside. Below `xl` there is no room beside the form and the column stays hidden.
 */
export function FloatingSaveRail({
  active,
  anchorRef,
  children,
  className,
}: {
  /** The form has unsaved changes. */
  active: boolean
  /** The form's own save area: while any of it is on screen the copy is hidden. */
  anchorRef: RefObject<HTMLElement | null>
  children: ReactNode
  className?: string
}) {
  const railRef = useRef<HTMLDivElement>(null)
  const itemRef = useRef<HTMLDivElement>(null)
  const pointerY = useRef<number | null>(null)
  const [offset, setOffset] = useState<number | null>(null)
  const [anchorVisible, setAnchorVisible] = useState(false)

  useEffect(() => {
    const anchor = anchorRef.current
    if (!anchor) return
    const observer = new IntersectionObserver(([entry]) => setAnchorVisible(entry.isIntersecting))
    observer.observe(anchor)
    return () => observer.disconnect()
  }, [anchorRef])

  useEffect(() => {
    const rail = railRef.current
    if (!active || !rail) return
    let frame = 0
    const place = () => {
      frame = 0
      const box = rail.getBoundingClientRect()
      const span = visibleSpan(rail)
      const top = span.top + EDGE
      const bottom = span.bottom - EDGE
      const height = itemRef.current?.offsetHeight ?? 0
      // Too little of the form is on screen to hold the action.
      if (bottom - top < height) return setOffset(null)
      const wanted = (pointerY.current ?? (top + bottom) / 2) - height / 2
      setOffset(Math.min(Math.max(wanted, top), bottom - height) - box.top)
    }
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(place)
    }
    const follow = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") return
      pointerY.current = event.clientY
      schedule()
    }
    const resize = new ResizeObserver(schedule)
    resize.observe(rail)
    place()
    window.addEventListener("pointermove", follow, { passive: true })
    window.addEventListener("resize", schedule)
    // Scroll does not bubble; capturing on the window hears every scrolling ancestor.
    window.addEventListener("scroll", schedule, { capture: true, passive: true })
    return () => {
      cancelAnimationFrame(frame)
      resize.disconnect()
      window.removeEventListener("pointermove", follow)
      window.removeEventListener("resize", schedule)
      window.removeEventListener("scroll", schedule, { capture: true })
    }
  }, [active])

  const shown = active && !anchorVisible && offset != null

  return (
    <div ref={railRef} data-floating-save={shown ? "shown" : "hidden"} className={cn("relative hidden w-36 shrink-0 xl:block", className)}>
      <div
        ref={itemRef}
        aria-hidden={!shown}
        className={cn(
          "absolute inset-x-0 top-0 flex pl-3.5 transition-[transform,opacity,visibility] duration-150 ease-panel motion-reduce:transition-none",
          shown ? "visible opacity-100" : "invisible opacity-0",
        )}
        style={{ transform: `translateY(${offset ?? 0}px)` }}
      >
        {children}
      </div>
    </div>
  )
}

/**
 * A compact save button for the rail whose left edge is a "<" point aimed at the form it saves.
 * The point is a rotated square in the button's own colour; its diagonal equals the
 * button's height (h-7), so its corners meet the button's top and bottom edges.
 */
export const FloatingSaveButton = forwardRef<HTMLButtonElement, ButtonProps>(function FloatingSaveButton({ className, ...props }, ref) {
  return (
    <Button
      ref={ref}
      size="sm"
      className={cn(
        "relative rounded-l-none pl-1.5 shadow-none drop-shadow-md",
        // drop-shadow makes the button a stacking context, so -z-10 keeps the point under the icon yet over the button's background.
        "before:absolute before:-z-10 before:-left-px before:top-1/2 before:size-[19.8px] before:-translate-x-1/2 before:-translate-y-1/2 before:rotate-45 before:rounded-[3px] before:bg-inherit before:content-['']",
        className,
      )}
      {...props}
    />
  )
})
