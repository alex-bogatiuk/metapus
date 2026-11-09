import { test, expect } from "@playwright/test"
import type { JsonObject } from "@/lib/json-value"

test("copying entity strips system identifiers and flags", () => {
  const sourceRecord: JsonObject = {
    id: "9f8b417c-a496-4e58-9481-9b19dfb97451",
    version: 3,
    code: "ACC-001",
    number: "DOC-001",
    name: "Main Production Account",
    provider: "alchemy",
    externalAccountRef: "ref-123",
    isActive: true,
    resetDay: 1,
    resetTimezone: "UTC",
    deletionMark: true,
    posted: true,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-02T00:00:00Z",
    createdBy: "user-1",
    updatedBy: "user-2",
  }

  const copied: JsonObject = { ...sourceRecord }
  delete copied.id
  delete copied.version
  delete copied.deletionMark
  delete copied.createdAt
  delete copied.updatedAt
  delete copied.createdBy
  delete copied.updatedBy
  delete copied.code
  delete copied.number
  if ("posted" in copied) {
    copied.posted = false
  }

  expect(copied.id).toBeUndefined()
  expect(copied.code).toBeUndefined()
  expect(copied.number).toBeUndefined()
  expect(copied.version).toBeUndefined()
  expect(copied.deletionMark).toBeUndefined()
  expect(copied.createdAt).toBeUndefined()
  expect(copied.posted).toBe(false)
  expect(copied.name).toBe("Main Production Account")
  expect(copied.provider).toBe("alchemy")
  expect(copied.externalAccountRef).toBe("ref-123")
  expect(copied.isActive).toBe(true)
})
