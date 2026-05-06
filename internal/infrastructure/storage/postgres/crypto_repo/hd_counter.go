package crypto_repo

import (
	"context"
	"fmt"
	"strconv"

	"metapus/internal/infrastructure/storage/postgres"
)

// HDCounter provides atomic HD derivation path counters per network.
// Uses a PostgreSQL sequence for lock-free atomic increment.
type HDCounter struct{}

// NewHDCounter creates a new HD counter.
func NewHDCounter() *HDCounter {
	return &HDCounter{}
}

// NextIndex atomically returns the next derivation index for a network.
// Creates the sequence on first use (idempotent).
func (c *HDCounter) NextIndex(ctx context.Context, networkCode string) (int, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	// Ensure sequence exists (idempotent)
	seqName := fmt.Sprintf("seq_hd_derivation_%s", networkCode)
	createSQL := fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s START WITH 0 MINVALUE 0", seqName)

	if _, err := querier.Exec(ctx, createSQL); err != nil {
		return 0, fmt.Errorf("create HD sequence %s: %w", seqName, err)
	}

	// Get next value
	var nextVal int64
	nextSQL := fmt.Sprintf("SELECT nextval('%s')", seqName)
	if err := querier.QueryRow(ctx, nextSQL).Scan(&nextVal); err != nil {
		return 0, fmt.Errorf("nextval %s: %w", seqName, err)
	}

	return int(nextVal), nil
}

// BuildDerivationPath constructs a BIP-44 derivation path.
// Format: m/44'/{coin_type}'/0'/0/{index}
func BuildDerivationPath(coinType int, index int) string {
	return "m/44'/" + strconv.Itoa(coinType) + "'/0'/0/" + strconv.Itoa(index)
}

// TRON coin type per SLIP-44 = 195
// Ethereum coin type = 60
const (
	CoinTypeTRON     = 195
	CoinTypeEthereum = 60
)
