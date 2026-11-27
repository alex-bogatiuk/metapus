import { test, expect } from "@playwright/test"
import { z } from "zod"
import { formRequestErrorMessage } from "../../lib/form-request-error"
import type { EntityMetadata } from "../../lib/schemas/metadata"
import type { JsonObject } from "../../lib/json-value"

const meta: EntityMetadata = {
  name: "GoodsReceipt", key: "goods_receipt", type: "document", presentation: {},
  fields: [{ name: "supplierId", label: "Поставщик", type: "reference" }, { name: "date", label: "Дата", type: "datetime" }],
  tableParts: [{ name: "goods", columns: [{ name: "quantity", label: "Количество", type: "number" }] }],
} as never

const request = z.object({
  supplierId: z.string(), date: z.string(),
  goods: z.array(z.object({ quantity: z.string() })),
})

function failure(draft: JsonObject): Error {
  const result = request.safeParse(draft)
  if (result.success) throw new Error("the draft passed")
  return result.error
}

test("an empty required field is named", () => {
  const draft: JsonObject = { date: "2026-11-27T10:00:00Z", goods: [] }
  expect(formRequestErrorMessage(meta, draft, failure(draft))).toBe("Заполните поле «Поставщик»")
})

test("a wrong value and a line cell are named", () => {
  const wrong: JsonObject = { supplierId: 5, date: "2026-11-27T10:00:00Z", goods: [] }
  expect(formRequestErrorMessage(meta, wrong, failure(wrong))).toBe("Поле «Поставщик» заполнено неверно")
  const line: JsonObject = { supplierId: "s", date: "d", goods: [{ quantity: "1" }, {}] }
  expect(formRequestErrorMessage(meta, line, failure(line))).toBe("Строка 2: заполните «Количество»")
})

test("other errors are left to the caller", () => {
  expect(formRequestErrorMessage(meta, {}, new Error("network"))).toBeNull()
})
