import { test, expect } from "@playwright/test"
import type { JsonObject } from "../../lib/json-value"
import type { MetadataField } from "../../lib/schemas/metadata"
import { MoneyDecimal } from "../../lib/money"
import { applyFormulas, formulaLabel, formulaScale, parseFormula } from "../../lib/formula"

const evaluate = (source: string, values: Record<string, string>) =>
    parseFormula(source)!.evaluate({
        value: (name) => new MoneyDecimal(values[name]),
        sum: ({ part, field }) => new MoneyDecimal(values[`${part}.${field}`]),
    }).toString()

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
    for (const bad of ["", "quantity*", "(price", "price)", "price $ 2", "1..2",
        "avg(items.amount)", "sum(items)", "sum(items.amount", "sum(items.amount*2)", "sum(.amount)"]) expect(parseFormula(bad)).toBeNull()
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

test("a header totals a table-part column, as the server does", () => {
    // Mirrors TestHeaderFormulasTotalTablePartColumns.
    expect(evaluate("sum(items.amount) + sum( services . amount ) - discount", { "items.amount": "5997", "services.amount": "1500", discount: "97" })).toBe("7400")
    const formula = parseFormula("sum(items.amount) + sum(items.amount) + sum(services.amount)")!
    expect(formula.sums).toEqual([{ part: "items", field: "amount" }, { part: "services", field: "amount" }])
    expect(formula.vars).toEqual([])
    expect(evaluate("sum * 2", { sum: "4" }), "a field named sum still reads as a field").toBe("8")

    const header = [
        { name: "discount", label: "Скидка", type: "money" },
        { name: "total", label: "Итого", type: "money", readOnly: true, calc: "sum(items.amount)-discount" },
    ] as MetadataField[]
    const document: JsonObject = { discount: "97", items: [{ amount: "5997" }, { amount: "1000" }, { amount: null }] }
    expect(applyFormulas(document, header)).toMatchObject({ total: "6900" })
    expect(applyFormulas({ discount: "0" }, header), "no lines yet: zero").toMatchObject({ total: "0" })
    const parts = [{ name: "items", label: "Товары", columns: lineFields }]
    expect(formulaLabel("sum(items.amount)-discount", header, parts)).toBe("Σ «Товары: Сумма» - Скидка")
})
