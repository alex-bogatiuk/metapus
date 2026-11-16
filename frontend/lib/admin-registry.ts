// frontend/lib/admin-registry.ts
/**
 * Sections of the administration page that a configuration adds after the
 * platform's own (users, security, integrations, system, tenants).
 */
import type { ComponentType, ElementType } from "react"

export interface AdminSectionDef {
  id: string
  title: string
  description: string
  icon: ElementType
  component: ComponentType
}

const adminSections: AdminSectionDef[] = []

/** Adds a configuration's administration section. Call at start-up. */
export function registerAdminSection(section: AdminSectionDef): void {
  if (adminSections.some((s) => s.id === section.id)) throw new Error(`Admin section ${section.id} is already registered`)
  adminSections.push(section)
}

export function getAdminSections(): AdminSectionDef[] {
  return [...adminSections]
}
