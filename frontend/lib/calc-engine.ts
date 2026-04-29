// frontend/lib/calc-engine.ts
/**
 * Inline calculator engine for Command Palette.
 *
 * Pure functions, no side effects, no dependencies.
 * Evaluates safe math expressions using a simple recursive descent parser.
 *
 * Supported:
 *  - Basic arithmetic: +, -, *, /
 *  - Parentheses: (2 + 3) * 4
 *  - Percentages: 200000 * 15%  →  30000
 *  - Exponentiation: 2 ** 10  →  1024 (or 2^10)
 *  - Unary minus: -5 + 3
 *  - Decimal numbers: 3.14 * 2
 *
 * Security: NO eval/Function. Pure recursive descent parser.
 */

// ── Public API ──────────────────────────────────────────────────────────

export interface CalcResult {
  /** The computed numeric value */
  value: number
  /** Human-readable formatted string (e.g. "1 234 567.89") */
  formatted: string
  /** The original expression that was evaluated */
  expression: string
}

/**
 * Check if a string looks like a math expression worth evaluating.
 * Quick pre-check to avoid parsing non-math input.
 */
export function isMathExpression(input: string): boolean {
  const trimmed = input.trim()
  if (trimmed.length < 2) return false

  // Must start with a digit, minus, or opening paren
  if (!/^[\d\-.(]/.test(trimmed)) return false

  // Must contain at least one operator
  if (!/[+\-*/^%]/.test(trimmed)) return false

  // Must contain at least one digit
  if (!/\d/.test(trimmed)) return false

  return true
}

/**
 * Evaluate a math expression string.
 * Returns CalcResult on success, null if expression is invalid.
 */
export function evaluateExpression(input: string): CalcResult | null {
  const expression = input.trim()
  if (!expression) return null

  try {
    const parser = new Parser(expression)
    const value = parser.parse()

    // Must have consumed all input
    if (parser.pos < parser.tokens.length) return null

    // Guard against non-finite results
    if (!Number.isFinite(value)) return null

    return {
      value,
      formatted: formatNumber(value),
      expression,
    }
  } catch {
    return null
  }
}

// ── Number Formatting ───────────────────────────────────────────────────

/**
 * Format a number with space-separated thousands and up to 10 decimal places.
 * Trailing zeros after decimal point are removed.
 * Examples: 1234567 → "1 234 567", 3.14 → "3.14", 0.001 → "0.001"
 */
function formatNumber(n: number): string {
  // Round to avoid floating point artifacts (e.g. 0.1 + 0.2 = 0.30000...04)
  const rounded = Number(n.toPrecision(12))

  const [intPart, decPart] = rounded.toString().split(".")
  const isNegative = intPart.startsWith("-")
  const absInt = isNegative ? intPart.slice(1) : intPart

  // Add thousand separators (thin space U+2009 for visual, regular space for copy)
  const withSeparators = absInt.replace(/\B(?=(\d{3})+(?!\d))/g, " ")

  let result = isNegative ? `-${withSeparators}` : withSeparators
  if (decPart) {
    result += `.${decPart}`
  }
  return result
}

// ── Token Types ─────────────────────────────────────────────────────────

const enum TokenType {
  Number,
  Plus,
  Minus,
  Star,
  Slash,
  Percent,
  Power,
  LParen,
  RParen,
}

interface Token {
  type: TokenType
  value: number // only meaningful for TokenType.Number
}

// ── Lexer ───────────────────────────────────────────────────────────────

function tokenize(input: string): Token[] {
  const tokens: Token[] = []
  let i = 0

  while (i < input.length) {
    const ch = input[i]

    // Skip whitespace
    if (ch === " " || ch === "\t") {
      i++
      continue
    }

    // Number (integer or decimal)
    if ((ch >= "0" && ch <= "9") || ch === ".") {
      let numStr = ""
      let hasDot = false
      while (i < input.length) {
        const c = input[i]
        if (c >= "0" && c <= "9") {
          numStr += c
          i++
        } else if (c === "." && !hasDot) {
          hasDot = true
          numStr += c
          i++
        } else if (c === " " || c === "\u2009") {
          // Allow spaces inside numbers (e.g. "1 234 567")
          // But only if next char is a digit
          if (i + 1 < input.length && input[i + 1] >= "0" && input[i + 1] <= "9") {
            i++
          } else {
            break
          }
        } else {
          break
        }
      }
      tokens.push({ type: TokenType.Number, value: parseFloat(numStr) })
      continue
    }

    switch (ch) {
      case "+": tokens.push({ type: TokenType.Plus, value: 0 }); break
      case "-": tokens.push({ type: TokenType.Minus, value: 0 }); break
      case "*":
        if (input[i + 1] === "*") {
          tokens.push({ type: TokenType.Power, value: 0 })
          i++ // skip second *
        } else {
          tokens.push({ type: TokenType.Star, value: 0 })
        }
        break
      case "/": tokens.push({ type: TokenType.Slash, value: 0 }); break
      case "%": tokens.push({ type: TokenType.Percent, value: 0 }); break
      case "^": tokens.push({ type: TokenType.Power, value: 0 }); break
      case "(": tokens.push({ type: TokenType.LParen, value: 0 }); break
      case ")": tokens.push({ type: TokenType.RParen, value: 0 }); break
      // Allow comma as decimal separator (user habit)
      case ",":
        // Look ahead: if followed by digits, treat previous number + comma + digits as decimal
        // But this is complex — just skip for now, commas are ignored
        break
      default:
        throw new Error(`Unexpected character: ${ch}`)
    }
    i++
  }

  return tokens
}

// ── Recursive Descent Parser ────────────────────────────────────────────
// Grammar:
//   expr     → term (('+' | '-') term)*
//   term     → power (('*' | '/') power)*
//   power    → unary (('**' | '^') unary)*
//   unary    → ('-')? percent
//   percent  → primary ('%')?
//   primary  → NUMBER | '(' expr ')'

class Parser {
  tokens: Token[]
  pos: number

  constructor(input: string) {
    this.tokens = tokenize(input)
    this.pos = 0
  }

  parse(): number {
    const result = this.expr()
    return result
  }

  private peek(): Token | undefined {
    return this.tokens[this.pos]
  }

  private consume(): Token {
    return this.tokens[this.pos++]
  }

  private expr(): number {
    let left = this.term()

    while (this.peek()?.type === TokenType.Plus || this.peek()?.type === TokenType.Minus) {
      const op = this.consume()
      const right = this.term()
      if (op.type === TokenType.Plus) {
        left += right
      } else {
        left -= right
      }
    }
    return left
  }

  private term(): number {
    let left = this.power()

    while (this.peek()?.type === TokenType.Star || this.peek()?.type === TokenType.Slash) {
      const op = this.consume()
      const right = this.power()
      if (op.type === TokenType.Star) {
        left *= right
      } else {
        if (right === 0) throw new Error("Division by zero")
        left /= right
      }
    }
    return left
  }

  private power(): number {
    let base = this.unary()

    while (this.peek()?.type === TokenType.Power) {
      this.consume()
      const exp = this.unary()
      base = Math.pow(base, exp)
    }
    return base
  }

  private unary(): number {
    if (this.peek()?.type === TokenType.Minus) {
      this.consume()
      return -this.percent()
    }
    if (this.peek()?.type === TokenType.Plus) {
      this.consume()
      return this.percent()
    }
    return this.percent()
  }

  private percent(): number {
    const val = this.primary()
    if (this.peek()?.type === TokenType.Percent) {
      this.consume()
      return val / 100
    }
    return val
  }

  private primary(): number {
    const token = this.peek()
    if (!token) throw new Error("Unexpected end of expression")

    if (token.type === TokenType.Number) {
      this.consume()
      return token.value
    }

    if (token.type === TokenType.LParen) {
      this.consume() // skip '('
      const result = this.expr()
      if (this.peek()?.type !== TokenType.RParen) {
        throw new Error("Missing closing parenthesis")
      }
      this.consume() // skip ')'
      return result
    }

    throw new Error(`Unexpected token`)
  }
}
