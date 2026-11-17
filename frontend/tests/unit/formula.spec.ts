import { test, expect } from "@playwright/test"
import type { JsonObject } from "../../lib/json-value"
import type { MetadataField } from "../../lib/schemas/metadata"
import { MoneyDecimal } from "../../lib/money"
import { applyFormulas, formulaLabel, formulaScale, parseFormula } from "../../lib/formula"

const evaluate = (source: string, values: Record<string, string>) =>
    parseFormula(source)!.evaluate((name) => new MoneyDecimal(values[name])).toString()

test("formulas compute exactly, with precedence, as the server does", () => {
    // The same cases as internal/core/formula/formula_test.go.
    const values = { quantity: "1.5", price: "1999", discount: "10", vatRate: "20" }
    expect(evaluate("quantity*price", values)).toBe("2998.5")
    expect(evaluate("quantity * price - discount", values)).toBe("2988.5")
    expect(evaluate("quantity*(price-discount)", values)).toBe("2983.5")
    expect(evaluate("-quantity + 2", values)).toBe("0.5")
    expect(evaluate("price*vatRate/100", values)).toBe("399.8")
    expect(evaluate("0.1+0.2", values)).toBe("0.3")
    expect(evaluate("price/0", values)).toBe("0")
    expect(evaluate("price/3", values)).toBe("666.3333333333333333")
    expect(evaluate("(quantity*price)*(1+vatRate/100)", values)).toBe("3598.2")
    expect(parseFormula("quantity*price + quantity")?.vars).toEqual(["quantity", "price"])
    for (const bad of ["", "quantity*", "(price", "price)", "price $ 2", "1..2"]) expect(parseFormula(bad)).toBeNull()
})

const lineFields = [
    { name: "quantity", label: "Количество", type: "number", valueScale: 10000 },
    { name: "price", label: "Цена", type: "money" },
    { name: "discount", label: "Скидка", type: "integer" },
    { name: "amount", label: "Сумма", type: "money", readOnly: true, calc: "gross-discount" },
    { name: "gross", label: "Без скидки", type: "money", readOnly: true, calc: "quantity*price" },
    { name: "share", label: "Доля", type: "decimal", scale: 4, readOnly: true, calc: "amount/3" },
] as MetadataField[]

test("a row's calculated fields follow its inputs, in dependency order, rounded like the server", () => {
    // Mirrors TestAutoCalculatedFieldsAreTheServersAndComputedInOrder.
    // Money travels as a string of minor units, as on the wire; a result keeps that type.
    const row: JsonObject = { quantity: 1.5, price: "1999", discount: 150 }
    const next = applyFormulas(row, lineFields)
    expect(next).toEqual({ quantity: 1.5, price: "1999", discount: 150, gross: "2999", amount: "2849", share: "949.6667" })
    expect(applyFormulas(next, lineFields)).toBe(next)
    expect(applyFormulas({ quantity: 2, price: "500" }, lineFields)).toMatchObject({ amount: "1000" })
    expect(applyFormulas({ quantity: 2, price: "500", amount: 7 }, lineFields), "a number stays a number").toMatchObject({ amount: 1000 })
    expect(formulaScale(lineFields[0])).toBe(4)
    expect(formulaScale(lineFields[1])).toBe(0)
    expect(formulaLabel("quantity*price", lineFields)).toBe("Количество × Цена")
    expect(formulaLabel("gross - discount", lineFields)).toBe("Без скидки - Скидка")
})
