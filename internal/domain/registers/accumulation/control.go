package accumulation

import (
	"context"
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// CodeInsufficientBalance is the code of the error that refuses a posting
// which would take a NonNegative register below zero.
const CodeInsufficientBalance = "INSUFFICIENT_BALANCE"

// Shortage is one resource of one combination of dimensions that a posting
// would take below zero, as the user reads it. It is the "shortages" detail
// of a CodeInsufficientBalance error.
type Shortage struct {
	// Register is the register's name.
	Register string `json:"register"`
	// Dimensions are the combination's dimensions: label and presentation.
	Dimensions []DimensionValue `json:"dimensions"`
	// Resource is the resource's name.
	Resource string `json:"resource"`
	// Available is the balance without the document.
	Available string `json:"available"`
	// Required is what the document takes; empty when the shortage comes
	// from receipts the document no longer makes.
	Required string `json:"required,omitempty"`
	// Result is the balance the posting would leave.
	Result string `json:"result"`
}

// DimensionValue is a dimension of a Shortage.
type DimensionValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// combination is one combination of a register's dimensions that a posting
// moves: what the document's earlier posting and this one add to each
// resource, receipts less expenses.
type combination struct {
	key        string
	dimensions []any
	old, new   []int64
}

// controlPlan is what a register knows of a posting before it changes any
// movement: the combinations it moves and how to read their balances after.
type controlPlan struct {
	schema       *Schema
	store        Store
	names        domain.RefResolver
	combinations []*combination
	balances     func(ctx context.Context) (map[string][]int64, error)
}

// planControl notes the combinations the document's current movements and
// the posting's new ones move. A Turnovers register, or a register the
// document does not move, has no plan.
func (r *Register[M]) planControl(ctx context.Context, doc posting.Postable, unposting bool) (*controlPlan, error) {
	if r.schema.Kind != Balances {
		return nil, nil
	}
	store, err := r.storage()
	if err != nil {
		return nil, err
	}
	byKey := map[string]*combination{}
	touch := func(v reflect.Value) *combination {
		dims := r.schema.dimensionValues(v)
		key := combinationKey(dims)
		c, ok := byKey[key]
		if !ok {
			c = &combination{key: key, dimensions: dims, old: make([]int64, len(r.schema.Resources)), new: make([]int64, len(r.schema.Resources))}
			byKey[key] = c
		}
		return c
	}

	var totals []M
	if err := store.SelectRecorderTotals(ctx, r.schema, doc.GetID(), &totals); err != nil {
		return nil, fmt.Errorf("select %s movements of the document: %w", r.schema.Key, err)
	}
	for i := range totals {
		v := reflect.ValueOf(&totals[i]).Elem()
		c := touch(v)
		for j, value := range r.schema.resourceValues(v) {
			c.old[j] += value
		}
	}
	if source, ok := doc.(Source); ok && !unposting {
		out := NewMovements(doc.GetID(), doc.GetDocumentType(), doc.GetPostedVersion()+1, time.Time{})
		if err := source.RegisterMovements(ctx, out); err != nil {
			return nil, fmt.Errorf("generate register movements: %w", err)
		}
		if err := out.Err(); err != nil {
			return nil, fmt.Errorf("generate register movements: %w", err)
		}
		movements, _ := out.byKey[r.schema.Key].([]M)
		for i := range movements {
			v := reflect.ValueOf(&movements[i]).Elem()
			sign := int64(1)
			if any(&movements[i]).(Movement).Header().RecordType == entity.RecordTypeExpense {
				sign = -1
			}
			c := touch(v)
			for j, value := range r.schema.resourceValues(v) {
				c.new[j] += sign * value
			}
		}
	}
	if len(byKey) == 0 {
		return nil, nil
	}

	plan := &controlPlan{schema: r.schema, store: store, names: r.resolver()}
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		plan.combinations = append(plan.combinations, byKey[key])
	}
	plan.balances = func(ctx context.Context) (map[string][]int64, error) {
		dims := make([][]any, len(plan.combinations))
		for i, c := range plan.combinations {
			dims[i] = c.dimensions
		}
		var rows []M
		if err := store.SelectBalancesOf(ctx, r.schema, dims, &rows); err != nil {
			return nil, fmt.Errorf("select %s balances: %w", r.schema.Key, err)
		}
		result := make(map[string][]int64, len(rows))
		for i := range rows {
			v := reflect.ValueOf(&rows[i]).Elem()
			result[combinationKey(r.schema.dimensionValues(v))] = r.schema.resourceValues(v)
		}
		return result, nil
	}
	return plan, nil
}

// combinationKey is the text of a combination's dimension values.
func combinationKey(dims []any) string {
	parts := make([]string, len(dims))
	for i, d := range dims {
		switch value := d.(type) {
		case id.ID:
			parts[i] = value.String()
		case string:
			parts[i] = strconv.Quote(value)
		default:
			parts[i] = fmt.Sprintf("%v", value)
		}
	}
	return strings.Join(parts, "|")
}

// lockKey is the transaction lock of a register's combination.
func lockKey(register, combination string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(register))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(combination))
	return int64(h.Sum64()) //nolint:gosec // a lock key, any bit pattern will do
}

// controller locks and checks the combinations a posting moves in all
// declarative Balances registers. Install adds one to the engine.
type controller struct{ registers []Declared }

func (controller) Name() string { return "accumulation control" }

func (controller) RecordFromSet(context.Context, *posting.MovementSet) error { return nil }

func (controller) ReverseMovements(context.Context, id.ID, int) error { return nil }

// BeginControl locks every combination the document moves, in the order of
// their keys across all registers, so two postings never wait for each other
// in a circle; then the balance triggers write rows no other posting holds.
// The check it returns refuses the posting when a NonNegative register's
// resource ends below zero in a combination the posting lowered.
func (c controller) BeginControl(ctx context.Context, doc posting.Postable, unposting bool) (posting.PostingCheck, error) {
	var plans []*controlPlan
	var keys []int64
	for _, r := range c.registers {
		plan, err := r.planControl(ctx, doc, unposting)
		if err != nil {
			return nil, err
		}
		if plan == nil {
			continue
		}
		plans = append(plans, plan)
		for _, comb := range plan.combinations {
			keys = append(keys, lockKey(plan.schema.Key, comb.key))
		}
	}
	if len(plans) == 0 {
		return nil, nil
	}
	slices.Sort(keys)
	if err := plans[0].store.LockCombinations(ctx, slices.Compact(keys)); err != nil {
		return nil, fmt.Errorf("lock register balances: %w", err)
	}
	return func(ctx context.Context) error { return check(ctx, plans) }, nil
}

type shortage struct {
	plan        *controlPlan
	combination *combination
	resource    int
	after       int64
}

func check(ctx context.Context, plans []*controlPlan) error {
	var found []shortage
	for _, plan := range plans {
		if !plan.schema.NonNegative {
			continue
		}
		after, err := plan.balances(ctx)
		if err != nil {
			return err
		}
		for _, comb := range plan.combinations {
			values := after[comb.key]
			for i := range plan.schema.Resources {
				var balance int64
				if values != nil {
					balance = values[i]
				}
				if balance < 0 && comb.new[i] < comb.old[i] {
					found = append(found, shortage{plan: plan, combination: comb, resource: i, after: balance})
				}
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	return shortageError(ctx, found)
}

// maxShortagesInMessage keeps the message readable; the details list all.
const maxShortagesInMessage = 5

func shortageError(ctx context.Context, found []shortage) error {
	names := presentations(ctx, found)
	shortages := make([]Shortage, len(found))
	lines := make([]string, 0, min(len(found), maxShortagesInMessage))
	for i, s := range found {
		schema := s.plan.schema
		resource := schema.Resources[s.resource]
		format := func(v int64) string { return formatResource(resource, v, s.combination, schema, names) }
		required := -s.combination.new[s.resource]
		sh := Shortage{
			Register:  schema.Label,
			Resource:  resource.Label,
			Available: format(s.after - s.combination.new[s.resource]),
			Result:    format(s.after),
		}
		if required > 0 {
			sh.Required = format(required)
		}
		dims := make([]string, len(schema.Dimensions))
		for j, f := range schema.Dimensions {
			value := presentDimension(f, s.combination.dimensions[j], names)
			sh.Dimensions = append(sh.Dimensions, DimensionValue{Label: f.Label, Value: value})
			dims[j] = fmt.Sprintf("%s «%s»", f.Label, value)
		}
		shortages[i] = sh
		if i >= maxShortagesInMessage {
			continue
		}
		line := fmt.Sprintf("«%s», %s: %s — ", sh.Register, strings.Join(dims, ", "), sh.Resource)
		if sh.Required != "" {
			line += fmt.Sprintf("в наличии %s, требуется %s, не хватает %s", sh.Available, sh.Required, format(-s.after))
		} else {
			line += fmt.Sprintf("остаток станет %s", sh.Result)
		}
		lines = append(lines, line)
	}
	message := "Недостаточно остатка: " + strings.Join(lines, "; ")
	if len(found) > maxShortagesInMessage {
		message += fmt.Sprintf("; и ещё %d", len(found)-maxShortagesInMessage)
	}
	return apperror.NewBusinessRule(CodeInsufficientBalance, message).WithDetail("shortages", shortages)
}

// presentations names the references of the shortages, and the currencies of
// their amounts, through each register's names; a reference the user may not
// read, or a register without names, keeps its ID.
func presentations(ctx context.Context, found []shortage) map[domain.RefResolveRequest]domain.RefResolveResult {
	requests := map[domain.RefResolver][]domain.RefResolveRequest{}
	seen := map[domain.RefResolveRequest]bool{}
	for _, s := range found {
		if s.plan.names == nil {
			continue
		}
		for j, f := range s.plan.schema.Dimensions {
			ref, ok := s.combination.dimensions[j].(id.ID)
			if f.Type != FieldRef || !ok || id.IsNil(ref) {
				continue
			}
			req := domain.RefResolveRequest{RefType: f.Ref, RefID: ref}
			if !seen[req] {
				seen[req] = true
				requests[s.plan.names] = append(requests[s.plan.names], req)
			}
		}
	}
	result := map[domain.RefResolveRequest]domain.RefResolveResult{}
	for names, refs := range requests {
		resolved, err := names.ResolveRefs(ctx, refs)
		if err != nil {
			logger.Warn(ctx, "shortage references are not named", "error", err)
			continue
		}
		for _, r := range resolved {
			result[domain.RefResolveRequest{RefType: r.RefType, RefID: r.RefID}] = r
		}
	}
	return result
}

func presentDimension(f Field, value any, names map[domain.RefResolveRequest]domain.RefResolveResult) string {
	switch v := value.(type) {
	case id.ID:
		if id.IsNil(v) {
			return "—"
		}
		if r, ok := names[domain.RefResolveRequest{RefType: f.Ref, RefID: v}]; ok && r.Presentation != "" {
			return r.Presentation
		}
		return v.String()
	case string:
		if v == "" {
			return "—"
		}
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatResource shows a quantity with its decimals and an amount in its
// currency's units when the currency is named, in minor units otherwise.
func formatResource(f Field, value int64, comb *combination, schema *Schema, names map[domain.RefResolveRequest]domain.RefResolveResult) string {
	if f.Type != FieldAmount {
		return types.Quantity(value).String()
	}
	for j, d := range schema.Dimensions {
		if d.Column != f.Currency {
			continue
		}
		currency, _ := comb.dimensions[j].(id.ID)
		if r, ok := names[domain.RefResolveRequest{RefType: d.Ref, RefID: currency}]; ok && r.DecimalPlaces != nil {
			amount := types.MinorUnits(value).ToDecimal(*r.DecimalPlaces).StringFixed(int32(*r.DecimalPlaces)) //nolint:gosec // decimal places of a currency
			if r.CurrencyCode != "" {
				amount += " " + r.CurrencyCode
			}
			return amount
		}
	}
	return strconv.FormatInt(value, 10) + " (в минимальных единицах)"
}

var _ posting.PostingController = controller{}
