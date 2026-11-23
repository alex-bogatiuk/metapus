package readpolicy

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/operators"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type SQLScalarKind string

const (
	SQLString  SQLScalarKind = "string"
	SQLInteger SQLScalarKind = "integer"
	SQLBoolean SQLScalarKind = "boolean"
	sqlNull    SQLScalarKind = "null"
)

// SQLPolicyField is a trusted storage projection of precisely the value exposed
// by entityToCELMap. SQL must come from registered metadata, never an HTTP field.
// UUID/decimal/quantity codecs must be represented as their CEL strings by the
// adapter. Unknown scalar or NULL semantics must not be registered here.
type SQLPolicyField struct {
	SQL      string
	Kind     SQLScalarKind
	Nullable bool
}

type policySQL struct {
	sql     string
	args    []any
	kind    SQLScalarKind
	field   bool
	derived bool
}
type sqlPolicyCompiler struct {
	fields map[string]SQLPolicyField
	user   map[string]any
}

// CompileReadSQL preserves the PolicyEngine's first matching effect, priority,
// default allow and fail-closed evaluation errors. SQL NULL represents a CEL
// evaluation error in boolean expressions; CASE distinguishes it from false.
// SQL retains '?' placeholders until the complete query is assembled.
func CompileReadSQL(ctx context.Context, e *security.PolicyEngine, rules []security.PolicyRule, fields map[string]SQLPolicyField) (squirrel.Sqlizer, error) {
	compiler := sqlPolicyCompiler{fields: fields, user: security.UserPolicyContext(ctx)}
	compiled := make([]policySQL, 0, len(rules))
	applicable := security.FilterPolicyRules(rules, "read")
	for _, rule := range applicable {
		ast, compileErr := e.CompilePolicyExpression(rule.GetExpression())
		if compileErr != nil {
			return nil, unsupportedReportPolicy(compileErr)
		}
		checked, err := cel.AstToCheckedExpr(ast)
		if err != nil {
			return nil, unsupportedReportPolicy(err)
		}
		expression, err := compiler.expression(checked.GetExpr())
		if err != nil {
			return nil, unsupportedReportPolicy(err)
		}
		if expression.kind != SQLBoolean {
			return nil, unsupportedReportPolicy(fmt.Errorf("policy is not boolean"))
		}
		if rule.GetEffect() != "allow" && rule.GetEffect() != "deny" {
			return nil, unsupportedReportPolicy(fmt.Errorf("unknown policy effect"))
		}
		compiled = append(compiled, expression)
	}
	result := policySQL{sql: "TRUE", kind: SQLBoolean}
	for i := len(compiled) - 1; i >= 0; i-- {
		effect := "FALSE"
		if applicable[i].GetEffect() == "allow" {
			effect = "TRUE"
		}
		args := append([]any(nil), compiled[i].args...)
		args = append(args, result.args...)
		result = policySQL{sql: "(CASE (" + compiled[i].sql + ") WHEN TRUE THEN " + effect + " WHEN FALSE THEN " + result.sql + " ELSE FALSE END)", args: args, kind: SQLBoolean}
	}
	return squirrel.Expr(result.sql, result.args...), nil
}

func unsupportedReportPolicy(cause error) error {
	err := apperror.NewForbidden("report policy cannot be represented exactly in SQL").WithCause(cause)
	err.Code = "REPORT_POLICY_UNSUPPORTED"
	return err
}

func sqlConstant(value any) (policySQL, error) {
	kind := sqlNull
	switch value.(type) {
	case nil:
		return policySQL{sql: "NULL", kind: sqlNull}, nil
	case string:
		kind = SQLString
	case int64:
		kind = SQLInteger
	case bool:
		kind = SQLBoolean
	default:
		return policySQL{}, fmt.Errorf("unsupported constant type %T", value)
	}
	sql := "?"
	if kind == SQLInteger {
		sql = "?::bigint"
	}
	if kind == SQLBoolean {
		sql = "?::boolean"
	}
	if kind == SQLString {
		sql = "(?::text COLLATE \"C\")"
	}
	return policySQL{sql: sql, args: []any{value}, kind: kind}, nil
}

func (c sqlPolicyCompiler) field(root, name string) (policySQL, error) {
	switch root {
	case "doc":
		field, ok := c.fields[name]
		if !ok || field.SQL == "" {
			return policySQL{}, fmt.Errorf("unsupported policy field %s", name)
		}
		if field.Kind != SQLString && field.Kind != SQLInteger && field.Kind != SQLBoolean {
			return policySQL{}, fmt.Errorf("unsupported field type")
		}
		sql := "(" + field.SQL + ")"
		if field.Kind == SQLString {
			sql = "(" + sql + " COLLATE \"C\")"
		}
		return policySQL{sql: sql, kind: field.Kind, field: true}, nil
	case "user":
		value, ok := c.user[name]
		if !ok {
			return policySQL{}, fmt.Errorf("unsupported user field")
		}
		return sqlConstant(value)
	default:
		return policySQL{}, fmt.Errorf("unsupported variable %s", root)
	}
}

func (c sqlPolicyCompiler) expression(node *exprpb.Expr) (policySQL, error) {
	if node == nil {
		return policySQL{}, fmt.Errorf("missing expression")
	}
	switch expression := node.ExprKind.(type) {
	case *exprpb.Expr_ConstExpr:
		switch value := expression.ConstExpr.ConstantKind.(type) {
		case *exprpb.Constant_StringValue:
			return sqlConstant(value.StringValue)
		case *exprpb.Constant_Int64Value:
			return sqlConstant(value.Int64Value)
		case *exprpb.Constant_BoolValue:
			return sqlConstant(value.BoolValue)
		case *exprpb.Constant_NullValue:
			return sqlConstant(nil)
		default:
			return policySQL{}, fmt.Errorf("unsupported literal")
		}
	case *exprpb.Expr_IdentExpr:
		if expression.IdentExpr.Name == "action" {
			return sqlConstant("read")
		}
		return policySQL{}, fmt.Errorf("unsupported bare variable")
	case *exprpb.Expr_SelectExpr:
		if expression.SelectExpr.TestOnly {
			return policySQL{}, fmt.Errorf("has() is unsupported")
		}
		return c.field(expression.SelectExpr.Operand.GetIdentExpr().GetName(), expression.SelectExpr.Field)
	case *exprpb.Expr_CallExpr:
		call := expression.CallExpr
		if call.Target != nil {
			return policySQL{}, fmt.Errorf("member functions are unsupported")
		}
		if call.Function == operators.Index && len(call.Args) == 2 {
			literal, ok := call.Args[1].GetConstExpr().GetConstantKind().(*exprpb.Constant_StringValue)
			if !ok {
				return policySQL{}, fmt.Errorf("dynamic field index")
			}
			return c.field(call.Args[0].GetIdentExpr().GetName(), literal.StringValue)
		}
		if call.Function == operators.LogicalNot && len(call.Args) == 1 {
			arg, err := c.expression(call.Args[0])
			if err != nil {
				return policySQL{}, err
			}
			if arg.kind != SQLBoolean {
				return policySQL{}, fmt.Errorf("negation of non-boolean")
			}
			return policySQL{sql: "(NOT " + arg.sql + ")", args: arg.args, kind: SQLBoolean, derived: true}, nil
		}
		if len(call.Args) != 2 {
			return policySQL{}, fmt.Errorf("unsupported function %s", call.Function)
		}
		if call.Function == operators.In {
			return c.membership(call.Args[0], call.Args[1])
		}
		left, err := c.expression(call.Args[0])
		if err != nil {
			return policySQL{}, err
		}
		right, err := c.expression(call.Args[1])
		if err != nil {
			return policySQL{}, err
		}
		return sqlBinary(call.Function, left, right)
	default:
		return policySQL{}, fmt.Errorf("dynamic structures and comprehensions are unsupported")
	}
}

func sqlBinary(operator string, left, right policySQL) (policySQL, error) {
	op := ""
	switch operator {
	case operators.LogicalAnd, operators.LogicalOr:
		if left.kind != SQLBoolean || right.kind != SQLBoolean {
			return policySQL{}, fmt.Errorf("boolean operands required")
		}
		op = "AND"
		if operator == operators.LogicalOr {
			op = "OR"
		}
	case operators.Equals, operators.NotEquals, operators.Less, operators.LessEquals, operators.Greater, operators.GreaterEquals:
		if left.derived || right.derived {
			return policySQL{}, fmt.Errorf("comparison of computed values is unsupported")
		}
		if left.field && right.field {
			return policySQL{}, fmt.Errorf("field-to-field comparison is unsupported")
		}
		if left.kind != right.kind && left.kind != sqlNull && right.kind != sqlNull {
			return policySQL{}, fmt.Errorf("mixed scalar types")
		}
		switch operator {
		case operators.Equals:
			op = "IS NOT DISTINCT FROM"
		case operators.NotEquals:
			op = "IS DISTINCT FROM"
		default:
			if left.kind == sqlNull || right.kind == sqlNull {
				return policySQL{sql: "NULL::boolean", kind: SQLBoolean, derived: true}, nil
			}
			if left.kind != SQLString && left.kind != SQLInteger {
				return policySQL{}, fmt.Errorf("unsupported ordered type")
			}
			op = map[string]string{operators.Less: "<", operators.LessEquals: "<=", operators.Greater: ">", operators.GreaterEquals: ">="}[operator]
		}
	default:
		return policySQL{}, fmt.Errorf("unsupported operator %s", operator)
	}
	args := append([]any(nil), left.args...)
	args = append(args, right.args...)
	return policySQL{sql: "(" + left.sql + " " + op + " " + right.sql + ")", args: args, kind: SQLBoolean, derived: true}, nil
}

func (c sqlPolicyCompiler) membership(value, list *exprpb.Expr) (policySQL, error) {
	left, err := c.expression(value)
	if err != nil {
		return policySQL{}, err
	}
	var elements []policySQL
	if literal := list.GetListExpr(); literal != nil {
		if len(literal.OptionalIndices) > 0 {
			return policySQL{}, fmt.Errorf("optional list elements")
		}
		for _, item := range literal.Elements {
			if item.GetConstExpr() == nil {
				return policySQL{}, fmt.Errorf("membership list must be constant")
			}
			element, err := c.expression(item)
			if err != nil {
				return policySQL{}, err
			}
			elements = append(elements, element)
		}
	} else if selected := list.GetSelectExpr(); selected != nil && !selected.TestOnly && selected.Operand.GetIdentExpr().GetName() == "user" && selected.Field == "roles" {
		roles, ok := c.user["roles"].([]any)
		if !ok {
			return policySQL{}, fmt.Errorf("unsupported roles")
		}
		for _, role := range roles {
			element, err := sqlConstant(role)
			if err != nil || element.kind != SQLString {
				return policySQL{}, fmt.Errorf("unsupported role value")
			}
			elements = append(elements, element)
		}
	} else {
		return policySQL{}, fmt.Errorf("dynamic membership list")
	}
	if len(elements) == 0 {
		return policySQL{sql: "FALSE", kind: SQLBoolean, derived: true}, nil
	}
	comparisons := make([]string, 0, len(elements))
	var args []any
	for _, element := range elements {
		equal, err := sqlBinary(operators.Equals, left, element)
		if err != nil {
			return policySQL{}, err
		}
		comparisons = append(comparisons, equal.sql)
		args = append(args, equal.args...)
	}
	return policySQL{sql: "(" + strings.Join(comparisons, " OR ") + ")", args: args, kind: SQLBoolean, derived: true}, nil
}
