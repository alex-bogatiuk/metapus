"use client"
import { createContext, useContext, type ReactNode } from "react"

const DragHandleContext = createContext<ReactNode>(null)

/**
 * The grid builds the handle where it calls useSortable and hands the finished element down,
 * so drag refs never leave the component that owns them. Given only while widgets can be moved.
 */
export const WidgetDragHandleProvider = DragHandleContext.Provider

/** The handle element for the widget header, or null when the widget cannot be moved now. */
export const useWidgetDragHandle = () => useContext(DragHandleContext)
