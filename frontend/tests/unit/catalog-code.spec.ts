import { test, expect } from "@playwright/test"
import { codePlaceholder } from "../../lib/catalog-code"

test("an empty code field says how the server fills it", () => {
  expect(codePlaceholder({ source: "series", prefix: "ORG" })).toBe("Присвоится при записи")
  expect(codePlaceholder({ source: "derived" })).toBe("Заполнится из реквизитов")
  expect(codePlaceholder({ source: "manual" }), "a code the user enters is a required field, not a hint").toBeUndefined()
  expect(codePlaceholder(undefined)).toBeUndefined()
})
