"use client"

import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode, type RefObject } from "react"
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

/** Height of the pointed button, px (h-7). */
const TAG_HEIGHT = 28
/** How far the "<" point reaches left of the button's body, px. */
const TAG_POINT = 11

/**
 * Outline of a tag-shaped button: a "<" point on the left and a rounded body. Every corner
 * is rounded, including the tip and the two joins between point and body, so it reads as one
 * smooth shape. Built from straight edges and quadratic curves with the corners as controls.
 */
export function tagPath(width: number, height = TAG_HEIGHT, point = TAG_POINT): string {
  const mid = height / 2
  const edge = Math.hypot(point, mid)
  // Unit vector from the tip up to the top join.
  const ux = point / edge
  const uy = mid / edge
  const tip = 3 // rounding along the edges at the tip
  const join = 4 // rounding at the joins of point and body
  const body = 7 // corner radius on the right
  const n = (value: number) => Number(value.toFixed(2))
  return [
    `M ${n(ux * tip)} ${n(mid - uy * tip)}`,
    `L ${n(point - ux * join)} ${n(uy * join)}`,
    `Q ${point} 0 ${point + join} 0`,
    `L ${width - body} 0`,
    `Q ${width} 0 ${width} ${body}`,
    `L ${width} ${height - body}`,
    `Q ${width} ${height} ${width - body} ${height}`,
    `L ${point + join} ${height}`,
    `Q ${point} ${height} ${n(point - ux * join)} ${n(height - uy * join)}`,
    `L ${n(ux * tip)} ${n(mid + uy * tip)}`,
    `Q 0 ${mid} ${n(ux * tip)} ${n(mid - uy * tip)}`,
    "Z",
  ].join(" ")
}

/**
 * A compact save button for the rail, shaped like a tag whose "<" point aims at the form it saves.
 * The shape is one SVG path drawn behind the content, so fill, hover, shadow and the focus ring
 * apply to the whole outline at once; it follows the button's width as the label changes.
 */
export const FloatingSaveButton = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement>>(
  function FloatingSaveButton({ className, children, type = "button", ...props }, ref) {
    const own = useRef<HTMLButtonElement>(null)
    useImperativeHandle(ref, () => own.current as HTMLButtonElement)
    const [width, setWidth] = useState(0)
    useLayoutEffect(() => {
      const button = own.current
      if (!button) return
      setWidth(button.offsetWidth)
      const observer = new ResizeObserver(() => setWidth(button.offsetWidth))
      observer.observe(button)
      return () => observer.disconnect()
    }, [])
    return (
      <button
        ref={own}
        type={type}
        className={cn(
          "group/tag relative isolate inline-flex h-7 items-center gap-1.5 whitespace-nowrap pl-[17px] pr-2.5 text-[13px] font-medium text-primary-foreground outline-none",
          "disabled:pointer-events-none disabled:opacity-50 [&>svg:not([data-tag-shape])]:pointer-events-none [&>svg:not([data-tag-shape])]:size-3.5 [&>svg:not([data-tag-shape])]:shrink-0",
          className,
        )}
        {...props}
      >
        {width > 0 ? (
          <svg
            data-tag-shape
            aria-hidden
            width={width}
            height={TAG_HEIGHT}
            viewBox={`0 0 ${width} ${TAG_HEIGHT}`}
            className="pointer-events-none absolute left-0 top-0 -z-10 overflow-visible drop-shadow-md"
          >
            <path
              d={tagPath(width)}
              strokeWidth={3}
              strokeLinejoin="round"
              className="fill-primary stroke-transparent transition-[fill,stroke] duration-150 group-hover/tag:fill-primary/90 group-focus-visible/tag:stroke-ring/40"
            />
          </svg>
        ) : null}
        {children}
      </button>
    )
  },
)
