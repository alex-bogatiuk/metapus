import { test, expect } from "@playwright/test"
import type { JsonObject } from "../../lib/json-value"
import type { MetadataField } from "../../lib/schemas/metadata"
import {
    addToCart, cartTotal, documentQuantities, parseQuantity, pickTarget, setCartQuantity, splitQuantityPrefix, transferPicks,
} from "../../lib/pick-lines"
import { ROW_KEY } from "../../lib/table-part-rows"

const columns = [
    { name: "lineNo", label: "№", type: "integer" },
    { name: "warehouseId", label: "Склад", type: "reference", referenceType: "warehouse" },
    { name: "productId", label: "Товар", type: "reference", referenceType: "product", required: true },
    { name: "quantity", label: "Количество", type: "decimal" },
    { name: "price", label: "Цена", type: "money" },
] as MetadataField[]
const writable = ["lineId", "warehouseId", "productId", "quantity", "price"]

test("a part picks into its required reference column and its quantity column", () => {
    const target = pickTarget(columns, writable)
    expect(target?.item.name).toBe("productId")
    expect(target?.quantity?.name).toBe("quantity")
    // Without a required reference the first editable one is picked; no quantity is fine.
    expect(pickTarget(columns.filter((c) => c.name !== "productId" && c.name !== "quantity"), writable)).toMatchObject({ item: { name: "warehouseId" }, quantity: null })
    // A part with nothing to pick, or one the form cannot write, has no "Подбор".
    expect(pickTarget(columns.filter((c) => c.type !== "reference"), writable)).toBeNull()
    expect(pickTarget(columns, null)).toBeNull()
})

test("quantities are typed with a comma or a dot and must be positive (and whole for integers)", () => {
    expect(parseQuantity("2,5", false)).toBe("2.5")
    expect(parseQuantity(" 3 ", true)).toBe("3")
    expect(parseQuantity("2.5", true)).toBeNull()
    expect(parseQuantity("0", false)).toBeNull()
    expect(parseQuantity("abc", false)).toBeNull()
    expect(splitQuantityPrefix("3*гвозди", true)).toEqual({ quantity: "3", text: "гвозди" })
    expect(splitQuantityPrefix("1,5 * кабель", false)).toEqual({ quantity: "1.5", text: "кабель" })
    expect(splitQuantityPrefix("P-100", true)).toEqual({ quantity: null, text: "P-100" })
})

test("the cart adds up an item picked again and drops one set to zero", () => {
    let cart = addToCart([], { id: "p1", name: "Гвозди", code: "P-1" }, "3")
    cart = addToCart(cart, { id: "p2", name: "Шурупы" }, "1")
    cart = addToCart(cart, { id: "p1", name: "Гвозди", code: "P-1" }, "2")
    expect(cart.map((line) => `${line.id}:${line.quantity}`)).toEqual(["p1:5", "p2:1"])
    expect(cartTotal(cart)).toBe("6")
    expect(setCartQuantity(cart, "p2", "0").map((line) => line.id)).toEqual(["p1"])
    expect(setCartQuantity(cart, "p2", "4")[1].quantity).toBe("4")
})

test("transfer merges into lines the document has and adds the rest at the end", () => {
    const target = pickTarget(columns, writable)!
    const rows: JsonObject[] = [
        { lineId: "l1", productId: "p1", product: { id: "p1", name: "Гвозди" }, quantity: "2.5" },
        { lineId: "l2", productId: "p3", quantity: 1 },
    ]
    expect([...documentQuantities(rows, target)]).toEqual([["p1", "2.5"], ["p3", "1"]])
    const cart = [{ id: "p1", name: "Гвозди", quantity: "3" }, { id: "p2", name: "Шурупы", code: "P-2", quantity: "4" }]

    const merged = transferPicks(rows, cart, target, true)
    expect(merged).toMatchObject({ added: 1, merged: 1 })
    // A merged line keeps its JSON type: a decimal string stays a string.
    expect(merged.rows[0].quantity).toBe("5.5")
    expect(merged.rows[2]).toMatchObject({ productId: "p2", product: { id: "p2", name: "Шурупы", code: "P-2" }, quantity: 4 })
    expect(typeof merged.rows[2][ROW_KEY]).toBe("string")
    expect(merged.touched).toEqual(["l1", merged.rows[2][ROW_KEY]])

    const separate = transferPicks(rows, cart, target, false)
    expect(separate).toMatchObject({ added: 2, merged: 0 })
    expect(separate.rows).toHaveLength(4)
    expect(separate.rows[0].quantity).toBe("2.5")
})
