package money_movement_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// A table name is selected only from known business document types.
func signingBasisTable(basisType string) (string, error) {
	switch basisType {
	case withdrawal_request.DocumentType:
		return "doc_withdrawal_requests", nil
	case treasury_withdrawal.DocumentType:
		return "doc_treasury_withdrawals", nil
	default:
		return "", fmt.Errorf("unsupported signing job basis type %q", basisType)
	}
}

func (r *Repo) SigningBasisStatus(ctx context.Context, basisType string, basisID id.ID, forUpdate bool) (string, error) {
	basisTable, err := signingBasisTable(basisType)
	if err != nil {
		return "", err
	}
	var status string
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if forUpdate {
		err = q.QueryRow(ctx, `SELECT status FROM `+basisTable+` WHERE id=$1 FOR UPDATE`, basisID).Scan(&status)
	} else {
		err = q.QueryRow(ctx, `SELECT status FROM `+basisTable+` WHERE id=$1`, basisID).Scan(&status)
	}
	return status, err
}

// PublishSigningBasis is called inside the same transaction as SaveArtifacts
// and job completion. The status check fences concurrent approval publication.
func (r *Repo) PublishSigningBasis(ctx context.Context, basisType string, basisID id.ID, nextStatus string) error {
	basisTable, err := signingBasisTable(basisType)
	if err != nil {
		return err
	}
	if nextStatus != "approved" && nextStatus != "pending_approval" {
		return fmt.Errorf("invalid signing publication status %q", nextStatus)
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
            UPDATE `+basisTable+`
            SET status=$2,approved_at=CASE WHEN $2='approved' THEN NOW() ELSE NULL END,
                updated_at=NOW()
            WHERE id=$1 AND status='pending_signature'`, basisID, nextStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("withdrawal left pending signature state")
	}
	return nil
}
