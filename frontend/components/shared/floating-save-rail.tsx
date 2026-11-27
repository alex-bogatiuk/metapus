"use client"

import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode, type RefObject } from "react"
import { cn } from "@/lib/utils"
import { stepSpring } from "@/lib/spring"

/** Space kept between the floating action and the edges of the visible area, px. */
const EDGE = 8
/** The pointer may roam this far above or below the button's centre without moving it, px. */
const DEAD_ZONE = 72
/** The pointer must rest this long before the button relocates to it, ms: it moves after a gesture, not during. */
const REST_MS = 160
/** A pointer this close to the rail (or inside it) is heading for the button, which then holds still, px. */
const APPROACH = 120
/** Period of the critically damped spring that carries the button, s. Calm, since it moves in peripheral vision. */
const RESPONSE = 0.42

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max)

/** The ancestors that clip the element vertically. Looked up once: styles are read outside the animation frames. */
function clippingAncestors(element: HTMLElement): HTMLElement[] {
  const clips: HTMLElement[] = []
  for (let parent = element.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
    if (getComputedStyle(parent).overflowY !== "visible") clips.push(parent)
  }
  return clips
}

/**
 * The vertical span of the element that is actually on screen, clipped by the
 * window and the clipping ancestors, and the element's own top.
 */
function visibleSpan(element: HTMLElement, clips: readonly HTMLElement[]): { top: number; bottom: number; elementTop: number } {
  const box = element.getBoundingClientRect()
  let top = Math.max(box.top, 0)
  let bottom = Math.min(box.bottom, window.innerHeight)
  for (const clip of clips) {
    const rect = clip.getBoundingClientRect()
    top = Math.max(top, rect.top)
    bottom = Math.min(bottom, rect.bottom)
  }
  return { top, bottom, elementTop: box.top }
}

/**
 * A narrow column beside a long form that carries a copy of its save action while there are
 * unsaved changes, so saving needs no scroll to the bottom. Once the form's own save area is on
 * screen the copy steps aside. Below `xl` there is no room beside the form and the column stays hidden.
 *
 * The copy follows the mouse lazily, like a visionOS window: it stays put while the pointer works
 * near it, relocates only after the pointer comes to rest elsewhere, holds still once the pointer
 * heads for the rail (a moving target is hard to hit), and travels on a critically damped spring.
 * It keeps its place on screen while the page scrolls, inside the visible part of the form.
 * Without a mouse it rests mid-way; with reduced motion it moves without travel.
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
  // Known before the form turns dirty, so the copy can appear where the user already is.
  const pointer = useRef<{ x: number; y: number } | null>(null)
  const [hasRoom, setHasRoom] = useState(false)
  const [anchorVisible, setAnchorVisible] = useState(false)

  // Observed afresh on every activation: a collapsible form mounts its save area only when opened.
  useEffect(() => {
    const anchor = anchorRef.current
    if (!active || !anchor) return
    const observer = new IntersectionObserver(([entry]) => setAnchorVisible(entry.isIntersecting))
    observer.observe(anchor)
    return () => observer.disconnect()
  }, [anchorRef, active])

  useEffect(() => {
    const track = (event: PointerEvent) => {
      if (event.pointerType === "mouse") pointer.current = { x: event.clientX, y: event.clientY }
    }
    window.addEventListener("pointermove", track, { passive: true })
    return () => window.removeEventListener("pointermove", track)
  }, [])

  useEffect(() => {
    const rail = railRef.current
    const item = itemRef.current
    if (!active || !rail || !item) return
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)")
    // The button's centre in viewport px: its presentation value, its velocity in px/s, and where it is heading.
    let y: number | null = null
    let velocity = 0
    let target: number | null = null
    let frame = 0
    let lastFrame = 0
    let rest = 0
    let room: boolean | null = null
    const clips = clippingAncestors(rail)

    const bounds = () => {
      const span = visibleSpan(rail, clips)
      const half = item.offsetHeight / 2
      return { top: span.top + EDGE + half, bottom: span.bottom - EDGE - half, half, railTop: span.elementTop }
    }
    // Scroll and layout apply at once, without the spring, so the button never lags behind the page.
    const render = () => {
      const b = bounds()
      const fits = b.bottom >= b.top
      if (fits !== room) {
        room = fits
        setHasRoom(fits)
      }
      if (!fits) return
      if (y == null) y = target = clamp(pointer.current?.y ?? (b.top + b.bottom) / 2, b.top, b.bottom)
      item.style.transform = `translateY(${clamp(y, b.top, b.bottom) - b.half - b.railTop}px)`
    }
    const step = (now: number) => {
      frame = 0
      if (y == null || target == null) return render()
      const dt = Math.min((now - lastFrame) / 1000, 1 / 30)
      lastFrame = now
      const next = stepSpring(y, velocity, target, dt, RESPONSE)
      y = next.position
      velocity = next.velocity
      if (Math.abs(y - target) < 0.3 && Math.abs(velocity) < 2) {
        y = target
        velocity = 0
      }
      render()
      if (y !== target) frame = requestAnimationFrame(step)
    }
    const relocate = () => {
      const at = pointer.current
      const b = bounds()
      if (!at || y == null || b.bottom < b.top) return
      // Start from where the button is seen, which scrolling may have pinned to an edge.
      y = clamp(y, b.top, b.bottom)
      const wanted = clamp(at.y, b.top, b.bottom)
      if (Math.abs(wanted - y) <= DEAD_ZONE) return
      target = wanted
      if (reducedMotion.matches) {
        y = target
        velocity = 0
        return render()
      }
      if (!frame) {
        lastFrame = performance.now()
        frame = requestAnimationFrame(step)
      }
    }
    const follow = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") return
      window.clearTimeout(rest)
      if (event.clientX >= rail.getBoundingClientRect().left - APPROACH) return
      rest = window.setTimeout(relocate, REST_MS)
    }
    const layout = () => {
      if (!frame) render()
    }

    const resize = new ResizeObserver(layout)
    resize.observe(rail)
    render()
    window.addEventListener("pointermove", follow, { passive: true })
    window.addEventListener("resize", layout)
    // Scroll does not bubble; capturing on the window hears every scrolling ancestor.
    window.addEventListener("scroll", layout, { capture: true, passive: true })
    return () => {
      cancelAnimationFrame(frame)
      window.clearTimeout(rest)
      resize.disconnect()
      window.removeEventListener("pointermove", follow)
      window.removeEventListener("resize", layout)
      window.removeEventListener("scroll", layout, { capture: true })
    }
  }, [active])

  const shown = active && !anchorVisible && hasRoom

  return (
    <div ref={railRef} data-floating-save={shown ? "shown" : "hidden"} className={cn("relative hidden w-36 shrink-0 xl:block", className)}>
      <div
        ref={itemRef}
        aria-hidden={!shown}
        className={cn(
          "absolute inset-x-0 top-0 flex pl-3.5 transition-[opacity,visibility] duration-200 ease-panel motion-reduce:transition-none",
          shown ? "visible opacity-100" : "invisible opacity-0",
        )}
      >
        {/* It emerges from the form's edge and slips back into it. */}
        <div className={cn("transition-transform duration-200 ease-panel motion-reduce:transition-none", shown ? "translate-x-0" : "-translate-x-1.5")}>
          {children}
        </div>
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
