package outbound_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

func (r *OutboundRepo) FindNativeTokenID(ctx context.Context, networkID id.ID) (id.ID, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var tokenID id.ID
	if err := querier.QueryRow(ctx, `
		SELECT id
		FROM cat_tokens
		WHERE network_id = $1
		  AND token_standard = $2
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
		ORDER BY id
		LIMIT 1
	`, networkID, token.TokenStandardNative).Scan(&tokenID); err != nil {
		return id.Nil(), fmt.Errorf("find native token for network %s: %w", networkID, err)
	}
	return tokenID, nil
}
