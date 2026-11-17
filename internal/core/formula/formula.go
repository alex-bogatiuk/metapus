// Package formula evaluates the arithmetic of calculated fields
// (meta:"calc:quantity*price"): numbers, names of sibling fields, + - * /,
// parentheses and unary minus, in exact decimal arithmetic. A document
// header can also total a column of a table part, sum(items.amount), as a
// 1C document's total is the sum of its lines.
//
// The frontend mirrors these rules in lib/formula.ts so a form can show a
// result as the user types; the server's result is the one stored.
//
// Division rounds to DivisionPlaces half away from zero; division by zero
// gives zero, so an empty quantity does not block saving a document.
package formula

import (
	"fmt"
	"slices"
	"unicode"

	"github.com/shopspring/decimal"
)

// DivisionPlaces is the scale a quotient is rounded to (lib/formula.ts uses the same).
const DivisionPlaces = 16

// Expr is a parsed formula.
type Expr struct {
	source string
	root   node
	vars   []string
	sums   []SumRef
}

// SumRef is a column a formula totals: sum(Part.Field).
type SumRef struct {
	Part  string // json name of the table part
	Field string // json name of the line field
}

// Env gives a formula its values: the struct's own fields and, for a
// header, the totals of its table parts' columns.
type Env struct {
	Value func(name string) decimal.Decimal
	Sum   func(ref SumRef) decimal.Decimal
}

type node interface {
	eval(env Env) decimal.Decimal
}

type number struct{ value decimal.Decimal }
type ref struct{ name string }
type total struct{ ref SumRef }
type negate struct{ operand node }
type binary struct {
	op          byte
	left, right node
}

func (n number) eval(Env) decimal.Decimal    { return n.value }
func (r ref) eval(env Env) decimal.Decimal   { return env.Value(r.name) }
func (t total) eval(env Env) decimal.Decimal { return env.Sum(t.ref) }
func (n negate) eval(env Env) decimal.Decimal {
	return n.operand.eval(env).Neg()
}
func (b binary) eval(env Env) decimal.Decimal {
	left, right := b.left.eval(env), b.right.eval(env)
	switch b.op {
	case '+':
		return left.Add(right)
	case '-':
		return left.Sub(right)
	case '*':
		return left.Mul(right)
	default:
		if right.IsZero() {
			return decimal.Zero
		}
		return left.DivRound(right, DivisionPlaces)
	}
}

// Parse reads a formula; names are sibling fields' json names.
func Parse(source string) (*Expr, error) {
	p := &parser{src: source}
	p.skipSpace()
	if p.pos == len(p.src) {
		return nil, fmt.Errorf("formula is empty")
	}
	root, err := p.sum()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("formula %q: unexpected %q at %d", source, p.src[p.pos], p.pos+1)
	}
	return &Expr{source: source, root: root, vars: p.vars, sums: p.sums}, nil
}

// Source is the formula as written.
func (e *Expr) Source() string { return e.source }

// Vars are the names the formula reads, in order of first use.
func (e *Expr) Vars() []string { return append([]string(nil), e.vars...) }

// Sums are the table-part columns the formula totals, in order of first use.
func (e *Expr) Sums() []SumRef { return append([]SumRef(nil), e.sums...) }

// Eval computes the formula with the given values.
func (e *Expr) Eval(env Env) decimal.Decimal { return e.root.eval(env) }

type parser struct {
	src  string
	pos  int
	vars []string
	sums []SumRef
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.skipSpace()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

// sum := product (('+'|'-') product)*
func (p *parser) sum() (node, error) {
	left, err := p.product()
	if err != nil {
		return nil, err
	}
	for op := p.peek(); op == '+' || op == '-'; op = p.peek() {
		p.pos++
		right, err := p.product()
		if err != nil {
			return nil, err
		}
		left = binary{op: op, left: left, right: right}
	}
	return left, nil
}

// product := unary (('*'|'/') unary)*
func (p *parser) product() (node, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for op := p.peek(); op == '*' || op == '/'; op = p.peek() {
		p.pos++
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		left = binary{op: op, left: left, right: right}
	}
	return left, nil
}

// unary := '-' unary | primary
func (p *parser) unary() (node, error) {
	if p.peek() == '-' {
		p.pos++
		operand, err := p.unary()
		if err != nil {
			return nil, err
		}
		return negate{operand}, nil
	}
	return p.primary()
}

func isNameStart(c byte) bool { return c == '_' || unicode.IsLetter(rune(c)) }
func isNamePart(c byte) bool  { return isNameStart(c) || unicode.IsDigit(rune(c)) }

// name reads an identifier at the current position ("" when there is none).
func (p *parser) name() string {
	p.skipSpace()
	start := p.pos
	if p.pos < len(p.src) && isNameStart(p.src[p.pos]) {
		for p.pos < len(p.src) && isNamePart(p.src[p.pos]) {
			p.pos++
		}
	}
	return p.src[start:p.pos]
}

// primary := number | name | 'sum' '(' name '.' name ')' | '(' sum ')'
func (p *parser) primary() (node, error) {
	c := p.peek()
	switch {
	case c == '(':
		p.pos++
		inner, err := p.sum()
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, fmt.Errorf("formula %q: missing )", p.src)
		}
		p.pos++
		return inner, nil
	case c >= '0' && c <= '9':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		value, err := decimal.NewFromString(p.src[start:p.pos])
		if err != nil {
			return nil, fmt.Errorf("formula %q: bad number %q", p.src, p.src[start:p.pos])
		}
		return number{value}, nil
	case isNameStart(c):
		name := p.name()
		if p.peek() == '(' {
			return p.call(name)
		}
		if !slices.Contains(p.vars, name) {
			p.vars = append(p.vars, name)
		}
		return ref{name}, nil
	case c == 0:
		return nil, fmt.Errorf("formula %q: ends too early", p.src)
	default:
		return nil, fmt.Errorf("formula %q: unexpected %q at %d", p.src, c, p.pos+1)
	}
}

// call reads sum(part.field), the only function: the total of a column.
func (p *parser) call(function string) (node, error) {
	if function != "sum" {
		return nil, fmt.Errorf("formula %q: unknown function %q (only sum(part.field))", p.src, function)
	}
	p.pos++ // (
	part := p.name()
	if part == "" || p.peek() != '.' {
		return nil, fmt.Errorf("formula %q: sum takes a table part column, sum(part.field)", p.src)
	}
	p.pos++ // .
	field := p.name()
	if field == "" || p.peek() != ')' {
		return nil, fmt.Errorf("formula %q: sum takes a table part column, sum(part.field)", p.src)
	}
	p.pos++ // )
	ref := SumRef{Part: part, Field: field}
	if !slices.Contains(p.sums, ref) {
		p.sums = append(p.sums, ref)
	}
	return total{ref}, nil
}
