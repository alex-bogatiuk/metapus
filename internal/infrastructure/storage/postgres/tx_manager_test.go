package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestValidateNestedOptionsRejectsWeakerOuterIsolation(t *testing.T) {
	outer := DefaultTxOptions()
	requested := SerializableTxOptions()

	err := validateNestedOptions(outer, requested)
	require.ErrorContains(t, err, "requires isolation serializable")
}

func TestValidateNestedOptionsAcceptsStrongerOuterIsolation(t *testing.T) {
	outer := SerializableTxOptions()
	requested := DefaultTxOptions()
	requested.IsolationLevel = pgx.RepeatableRead

	require.NoError(t, validateNestedOptions(outer, requested))
}

func TestValidateNestedOptionsRejectsReadOnlyContractMismatch(t *testing.T) {
	outer := DefaultTxOptions()
	requested := DefaultTxOptions()
	requested.AccessMode = pgx.ReadOnly

	err := validateNestedOptions(outer, requested)
	require.ErrorContains(t, err, "requires access mode read only")
}

func TestValidateNestedOptionsRejectsExplicitReadWriteInsideReadOnly(t *testing.T) {
	outer := DefaultTxOptions()
	outer.AccessMode = pgx.ReadOnly

	err := validateNestedOptions(outer, DefaultTxOptions())
	require.ErrorContains(t, err, "requires access mode read write")
}

func TestValidateNestedOptionsRejectsLongerCustomTimeout(t *testing.T) {
	outer := DefaultTxOptions()
	requested := DefaultTxOptions()
	requested.StatementTimeout = time.Minute

	err := validateNestedOptions(outer, requested)
	require.ErrorContains(t, err, "outer transaction enforces stricter timeout")
}

func TestNestedShorterTimeoutRequiresScope(t *testing.T) {
	outer := DefaultTxOptions()
	requested := DefaultTxOptions()
	requested.StatementTimeout = 5 * time.Second

	require.NoError(t, validateNestedOptions(outer, requested))
	require.True(t, nestedTimeoutRequiresScope(outer, requested))
}
