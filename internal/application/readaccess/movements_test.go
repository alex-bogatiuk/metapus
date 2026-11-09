package readaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	"metapus/internal/domain"
)

type movementRegistry struct{}

func (movementRegistry) Definition(key string) (Definition, bool) {
	return Definition{Key: key, Permission: "document:read"}, true
}

func TestMovementMoneyRequiresAuthorizedCurrencyUnits(t *testing.T) {
	visible, hidden, denied := id.New(), id.New(), id.New()
	columns := []entity.MovementColumnDef{{Key: "currency", Type: "ref"}, {Key: "amount", Type: "amount", CurrencyRef: "currency"}}
	rows := make([]entity.DocumentMovement, 0, 4)
	for _, uid := range []id.ID{visible, hidden, denied, visible} {
		rows = append(rows, entity.DocumentMovement{Columns: columns, Data: map[string]any{
			"currency": entity.MovementRefValue{ID: uid.String(), CurrencyCode: "UNTRUSTED"}, "amount": types.MinorUnits(9007199254740993),
		}})
	}
	scale := 9
	resolver := movementResolver{resolve: func(refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
		require.Len(t, refs, 3, "unit metadata uses the same deduplicated batch")
		return []domain.RefResolveResult{
			{RefType: "Currency", RefID: visible, Presentation: "TON", CurrencyCode: "TON", DecimalPlaces: &scale},
			{RefType: "Currency", RefID: hidden, Presentation: "Hidden units"},
		}, nil
	}}
	require.NoError(t, EnrichMovementRefs(context.Background(), rows, resolver))
	for _, index := range []int{0, 3} {
		require.Equal(t, types.MinorUnits(9007199254740993), rows[index].Data["amount"])
		ref := rows[index].Data["currency"].(entity.MovementRefValue)
		require.Equal(t, "TON", ref.CurrencyCode)
		require.Equal(t, 9, *ref.DecimalPlaces)
	}
	for _, index := range []int{1, 2} {
		require.NotContains(t, rows[index].Data, "amount")
	}
	require.NotContains(t, rows[2].Data, "currency")
}

type movementProvider struct {
	load func(context.Context) ([]entity.DocumentMovement, error)
}

func (movementProvider) RegisterName() string { return "test" }
func (p movementProvider) GetDocumentMovements(ctx context.Context, _ id.ID) ([]entity.DocumentMovement, error) {
	return p.load(ctx)
}

type movementResolver struct {
	resolve func([]domain.RefResolveRequest) ([]domain.RefResolveResult, error)
}

func (r movementResolver) ResolveRefs(_ context.Context, refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
	return r.resolve(refs)
}

func TestMovementSourceAuthorizationPrecedesEveryProvider(t *testing.T) {
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{Permissions: []string{"document:read"}})
	service := New(movementRegistry{}, nil, nil)
	snapshot := func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	provider := movementProvider{load: func(context.Context) ([]entity.DocumentMovement, error) {
		t.Fatal("provider ran before authorization")
		return nil, nil
	}}
	for _, failure := range []error{apperror.NewForbidden("RLS/CEL denied"), apperror.NewNotFound("doc", "id"), errors.New("DB unavailable")} {
		rows, err := service.ReadMovements(ctx, "crypto_invoice", id.New(), snapshot, func(context.Context, id.ID) error { return failure }, []entity.MovementProvider{provider}, nil)
		require.ErrorIs(t, err, failure)
		require.Nil(t, rows)
	}
}

func TestMovementProjectionChecksSourceAndRegisterWithoutMutatingProviderData(t *testing.T) {
	source := entity.DocumentMovement{PolicyEntity: "crypto_fee", Data: map[string]any{"amount": int64(100), "feeType": "fixed", "unlisted": "private"}, Columns: []entity.MovementColumnDef{
		{Key: "amount", PolicyField: "amount"}, {Key: "feeType", PolicyField: "fee_type"},
	}}
	for _, policies := range []map[string]*security.FieldPolicy{
		{"crypto_payment:read": {AllowedFields: []string{"*", "-amount"}}},
		{"crypto_fee:read": {AllowedFields: []string{"*", "-amount"}}},
	} {
		ctx := security.WithFieldPolicies(context.Background(), policies)
		projected, ok := projectMovement(ctx, "crypto_payment", source)
		require.True(t, ok)
		require.Len(t, projected.Columns, 1)
		require.Equal(t, map[string]any{"feeType": "fixed"}, projected.Data)
		require.Equal(t, int64(100), source.Data["amount"])
	}
	ctx := security.WithFieldPolicies(context.Background(), map[string]*security.FieldPolicy{"crypto_payment:read": {AllowedFields: []string{"*", "-posted"}}})
	_, ok := projectMovement(ctx, "crypto_payment", source)
	require.False(t, ok, "movement existence must not disclose a hidden posting state")
}

func TestMovementReferencesAreBatchedAndInaccessibleRepresentationsRemoved(t *testing.T) {
	visible, denied := id.New(), id.New()
	columns := []entity.MovementColumnDef{{Key: "wallet", Type: "ref"}, {Key: "unexpected", Type: "ref"}}
	rows := []entity.DocumentMovement{
		{Columns: columns, Data: map[string]any{"wallet": entity.MovementRefValue{ID: visible.String(), Name: "unsafe"}, "unexpected": entity.MovementRefValue{ID: visible.String(), Name: "private"}}},
		{Columns: columns, Data: map[string]any{"wallet": entity.MovementRefValue{ID: denied.String(), Name: "private"}}},
		{Columns: columns, Data: map[string]any{"wallet": entity.MovementRefValue{ID: visible.String()}}},
	}
	calls := 0
	resolver := movementResolver{resolve: func(refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
		calls++
		require.Len(t, refs, 2)
		return []domain.RefResolveResult{{RefType: "Wallet", RefID: visible, Presentation: "Allowed wallet"}}, nil
	}}
	require.NoError(t, EnrichMovementRefs(context.Background(), rows, resolver))
	require.Equal(t, 1, calls)
	require.Equal(t, "Allowed wallet", rows[0].Data["wallet"].(entity.MovementRefValue).Name)
	require.NotContains(t, rows[0].Data, "unexpected")
	require.NotContains(t, rows[1].Data, "wallet")
}

func TestMovementProjectionWithdrawalRequest(t *testing.T) {
	source := entity.DocumentMovement{
		PolicyEntity: "crypto_merchant_balance",
		Data: map[string]any{
			"merchant": entity.MovementRefValue{ID: "m1", Name: "Merchant 1"},
			"token":    entity.MovementRefValue{ID: "t1", Name: "USDT"},
			"amount":   "1000000",
		},
		Columns: []entity.MovementColumnDef{
			{Key: "merchant", PolicyField: "merchant_id"},
			{Key: "token", PolicyField: "token_id"},
			{Key: "amount", PolicyField: "amount"},
		},
	}
	ctx := context.Background()
	projected, ok := projectMovement(ctx, "withdrawal_request", source)
	require.True(t, ok)
	require.Len(t, projected.Columns, 3)
	require.Equal(t, "1000000", projected.Data["amount"])
}

