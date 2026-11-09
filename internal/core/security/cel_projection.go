package security

import (
	"fmt"
	"github.com/google/cel-go/cel"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// RequireAvailableFields rejects policies that require data outside a loaded
// projection (for example document lines). Missing data must not be replaced
// by zero values and then accidentally authorize a row.
func (e *PolicyEngine) RequireAvailableFields(rules []PolicyRule, available map[string]bool) error {
	for _, rule := range filterAndSort(rules, "read") {
		ast, issues := e.env.Compile(rule.GetExpression())
		if issues != nil && issues.Err() != nil {
			return issues.Err()
		}
		checked, err := cel.AstToCheckedExpr(ast)
		if err != nil {
			return err
		}
		if err := availableExpression(checked.GetExpr(), available); err != nil {
			return err
		}
	}
	return nil
}

func availableExpression(node *exprpb.Expr, fields map[string]bool) error {
	if node == nil {
		return nil
	}
	switch v := node.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		if v.IdentExpr.Name == "doc" {
			return fmt.Errorf("policy requires the complete document")
		}
	case *exprpb.Expr_SelectExpr:
		if v.SelectExpr.Operand.GetIdentExpr().GetName() == "doc" {
			if !fields[v.SelectExpr.Field] {
				return fmt.Errorf("policy field %s is unavailable", v.SelectExpr.Field)
			}
			return nil
		}
		return availableExpression(v.SelectExpr.Operand, fields)
	case *exprpb.Expr_CallExpr:
		call := v.CallExpr
		if call.Function == "_[_]" && len(call.Args) == 2 && call.Args[0].GetIdentExpr().GetName() == "doc" {
			name := call.Args[1].GetConstExpr().GetStringValue()
			if name == "" || !fields[name] {
				return fmt.Errorf("dynamic or unavailable policy field")
			}
			return nil
		}
		if err := availableExpression(call.Target, fields); err != nil {
			return err
		}
		for _, arg := range call.Args {
			if err := availableExpression(arg, fields); err != nil {
				return err
			}
		}
	case *exprpb.Expr_ListExpr:
		for _, item := range v.ListExpr.Elements {
			if err := availableExpression(item, fields); err != nil {
				return err
			}
		}
	case *exprpb.Expr_StructExpr:
		for _, entry := range v.StructExpr.Entries {
			if err := availableExpression(entry.GetMapKey(), fields); err != nil {
				return err
			}
			if err := availableExpression(entry.Value, fields); err != nil {
				return err
			}
		}
	case *exprpb.Expr_ComprehensionExpr:
		c := v.ComprehensionExpr
		for _, part := range []*exprpb.Expr{c.IterRange, c.AccuInit, c.LoopCondition, c.LoopStep, c.Result} {
			if err := availableExpression(part, fields); err != nil {
				return err
			}
		}
	}
	return nil
}
