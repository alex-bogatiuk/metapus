// Package formula evaluates the arithmetic of calculated fields
// (meta:"calc:quantity*price"): numbers, names of sibling fields, + - * /,
// parentheses and unary minus, in exact decimal arithmetic.
//
// The frontend mirrors these rules in lib/formula.ts so a table part can
// show a result as the user types; the server's result is the one stored.
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
}

type node interface {
	eval(value func(string) decimal.Decimal) decimal.Decimal
}

type number struct{ value decimal.Decimal }
type ref struct{ name string }
type negate struct{ operand node }
type binary struct {
	op          byte
	left, right node
}

func (n number) eval(func(string) decimal.Decimal) decimal.Decimal { return n.value }
func (r ref) eval(value func(string) decimal.Decimal) decimal.Decimal {
	return value(r.name)
}
func (n negate) eval(value func(string) decimal.Decimal) decimal.Decimal {
	return n.operand.eval(value).Neg()
}
func (b binary) eval(value func(string) decimal.Decimal) decimal.Decimal {
	left, right := b.left.eval(value), b.right.eval(value)
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
	return &Expr{source: source, root: root, vars: p.vars}, nil
}

// Source is the formula as written.
func (e *Expr) Source() string { return e.source }

// Vars are the names the formula reads, in order of first use.
func (e *Expr) Vars() []string { return append([]string(nil), e.vars...) }

// Eval computes the formula with the given values of its names.
func (e *Expr) Eval(value func(name string) decimal.Decimal) decimal.Decimal {
	return e.root.eval(value)
}

type parser struct {
	src  string
	pos  int
	vars []string
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

// primary := number | name | '(' sum ')'
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
	case c == '_' || unicode.IsLetter(rune(c)):
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] == '_' || unicode.IsLetter(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos]))) {
			p.pos++
		}
		name := p.src[start:p.pos]
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
