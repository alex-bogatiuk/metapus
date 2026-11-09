import { test, expect } from "@playwright/test"
import { evaluateExpression, isMathExpression } from "../../lib/calc-engine"

test("calculator respects decimal commas, mathematical precedence and right-associative powers", () => {
  for (const [expression, expected] of [
    ["1,5 + 2", 3.5], ["1.5 + 2", 3.5], [",5 * 8", 4],
    ["2^3^2", 512], ["2 ** 3 ** 2", 512], ["-2^2", -4], ["(-2)^2", 4],
    ["2^-2", 0.25], ["--2^2", 4], ["2 * -3^2", -18], ["2^(1+2)", 8],
    ["200000 * 15%", 30000], ["1 234 567,5 + 0,5", 1234568],
    ["1\u00a0234,5 + 1", 1235.5], ["1\u2009234 + 1", 1235],
    ["2 + 3 * 4", 14], ["(2 + 3) * 4", 20], ["0.1 + 0.2", 0.3],
  ] as const) {
    expect(isMathExpression(expression)).toBe(true)
    expect(evaluateExpression(expression)?.value).toBeCloseTo(expected)
  }
})

test("calculator rejects malformed decimals and excessive nesting without partial results", () => {
  for (const input of ["1,+2", "1,", "1.2.3 + 4", "1,2,3 + 4", "1 2 + 3", "1/0", "2^", "()", "2(3)", "1;alert(1)", "(".repeat(70) + "1" + ")".repeat(70), "-".repeat(70) + "1", "1".repeat(2049)]) {
    expect(evaluateExpression(input), input).toBeNull()
  }
  expect(evaluateExpression("10^100")?.formatted).toBe("1e+100")
  expect(evaluateExpression("0.1+0.2")?.formatted).toBe("0.3")
})
