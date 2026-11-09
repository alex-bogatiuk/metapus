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
 *  - Decimal numbers: 3.14 * 2 or 3,14 * 2
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
  if (!/^[\d+\-.,(]/.test(trimmed)) return false

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
  if (!expression || expression.length > 2048) return null

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
 * Format a number with space-separated thousands and 12 significant digits.
 * Trailing zeros after decimal point are removed.
 * Examples: 1234567 → "1 234 567", 3.14 → "3.14", 0.001 → "0.001"
 */
function formatNumber(n: number): string {
  // Round to avoid floating point artifacts (e.g. 0.1 + 0.2 = 0.30000...04)
  const rounded = Number(n.toPrecision(12))

  const text = rounded.toString()
  if (text.includes("e")) return text
  const [intPart, decPart] = text.split(".")
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
    if (/\s/.test(ch)) {
      i++
      continue
    }

    // Commas and dots are decimal separators; grouped integers use three-digit groups.
    if (/^[\d.,]$/.test(ch)) {
      const match = input.slice(i).match(/^(?:\d+(?:[ \u00a0\u2009\u202f]\d{3})*(?:[.,]\d+)?|[.,]\d+)/)
      if (!match) throw new Error("Invalid number")
      const value = Number(match[0].replace(/[ \u00a0\u2009\u202f]/g, "").replace(",", "."))
      if (!Number.isFinite(value)) throw new Error("Invalid number")
      tokens.push({ type: TokenType.Number, value })
      i += match[0].length
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
//   term     → unary (('*' | '/') unary)*
//   unary    → ('+' | '-') unary | power
//   power    → percent (('**' | '^') unary)?
//   percent  → primary ('%')?
//   primary  → NUMBER | '(' expr ')'

class Parser {
  tokens: Token[]
  pos: number
  private depth = 0

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
    let left = this.unary()
    while (this.peek()?.type === TokenType.Star || this.peek()?.type === TokenType.Slash) {
      const op = this.consume()
      const right = this.unary()
      if (op.type === TokenType.Star) left *= right
      else {
        if (right === 0) throw new Error("Division by zero")
        left /= right
      }
    }
    return left
  }

  private power(): number {
    const base = this.percent()
    if (this.peek()?.type === TokenType.Power) {
      this.consume()
      return Math.pow(base, this.unary())
    }
    return base
  }

  private unary(): number {
    if (++this.depth > 64) throw new Error("Expression nesting limit exceeded")
    try {
      if (this.peek()?.type === TokenType.Minus) {
        this.consume()
        return -this.unary()
      }
      if (this.peek()?.type === TokenType.Plus) {
        this.consume()
        return this.unary()
      }
      return this.power()
    } finally {
      this.depth--
    }
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
