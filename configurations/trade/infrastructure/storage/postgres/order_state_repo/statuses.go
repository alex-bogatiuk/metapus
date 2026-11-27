// Package order_state_repo reads customer order states from the
// configuration's catalog table.
package order_state_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// Statuses reads a state's status in the transaction of the context.
type Statuses struct{}

// New creates the reader.
func New() Statuses { return Statuses{} }

// StatusOf reads the status of state; a state that is missing or deleted is
// a validation error.
func (Statuses) StatusOf(ctx context.Context, state id.ID) (order_state.Status, error) {
	var status order_state.Status
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).
		QueryRow(ctx, `SELECT status FROM cat_customer_order_states WHERE id = $1 AND _deleted_at IS NULL`, state).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NewValidation("the order state is not found")
	}
	if err != nil {
		return "", fmt.Errorf("read order state: %w", err)
	}
	return status, nil
}

var _ order_state.Statuses = Statuses{}
