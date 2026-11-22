package crypto_repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"
)

type OperationalHoldRepo struct{}

func NewOperationalHoldRepo() *OperationalHoldRepo { return &OperationalHoldRepo{} }

// Never wait for the owner gate while a caller may already hold a wallet or
// business row. A busy command rolls back and retries with a fresh transaction.
func lockOperationalHoldOwner(ctx context.Context, ownerType, ownerID string) error {
	if strings.TrimSpace(ownerType) == "" || strings.TrimSpace(ownerID) == "" {
		return apperror.NewValidation("operational hold owner is required")
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return apperror.NewValidation("operational hold commands require read committed isolation")
	}
	var acquired bool
	if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_operational_hold_owner($1,$2)`, ownerType, ownerID).Scan(&acquired); err != nil {
		return err
	}
	if !acquired {
		return apperror.NewConflict("operational hold owner is busy; retry the command")
	}
	return nil
}

func operationalHoldTable(scope cryptoapp.OperationalHoldScope) (string, string, error) {
	switch scope {
	case cryptoapp.OperationalHoldScopeNetwork:
		return "reg_crypto_network_holds", "network_id", nil
	case cryptoapp.OperationalHoldScopeWallet:
		return "reg_crypto_wallet_allocation_holds", "wallet_id", nil
	default:
		return "", "", apperror.NewValidation("unsupported operational hold scope")
	}
}

func (r *OperationalHoldRepo) AcquireOperationalHold(ctx context.Context, command cryptoapp.AcquireOperationalHoldCommand) error {
	if id.IsNil(command.ScopeID) || strings.TrimSpace(command.Reason) == "" || strings.TrimSpace(command.HeldBy) == "" {
		return apperror.NewValidation("hold target, reason and actor are required")
	}
	table, column, err := operationalHoldTable(command.Scope)
	if err != nil {
		return err
	}
	if command.Details == nil {
		command.Details = map[string]any{}
	}
	details, err := json.Marshal(command.Details)
	if err != nil {
		return fmt.Errorf("marshal operational hold details: %w", err)
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockOperationalHoldOwner(ctx, command.OwnerType, command.OwnerID); err != nil {
			return err
		}
		q := tm.GetQuerier(ctx)
		check := func() (int, bool, error) {
			var count int
			var exact bool
			err := q.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(reason=$4 AND held_by=$5 AND details=$6::jsonb),TRUE) FROM `+table+`
 WHERE `+column+`=$1 AND owner_type=$2 AND owner_id=$3`, command.ScopeID, command.OwnerType, command.OwnerID, command.Reason, command.HeldBy, string(details)).Scan(&count, &exact)
			return count, exact, err
		}
		count, exact, err := check()
		if err != nil {
			return err
		}
		if count > 0 {
			if !exact {
				return apperror.NewConflict("operational hold replay differs from its original acquisition")
			}
			return nil
		}
		if command.Scope == cryptoapp.OperationalHoldScopeWallet {
			var eligible bool
			err := q.QueryRow(ctx, `SELECT tier='pool' AND wallet_role='merchant_pool' FROM cat_wallets WHERE id=$1`, command.ScopeID).Scan(&eligible)
			if err == pgx.ErrNoRows {
				return apperror.NewNotFound("wallet", command.ScopeID)
			}
			if err != nil {
				return err
			}
			if !eligible {
				return nil
			}
		}
		var resolved bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE owner_type=$1 AND owner_id=$2 AND status='resolved')
 OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type=$1 AND owner_id=$2 AND status='resolved')`, command.OwnerType, command.OwnerID).Scan(&resolved); err != nil {
			return err
		}
		if resolved {
			return apperror.NewConflict("resolved operational hold owner cannot acquire another target")
		}
		values := "VALUES($1,$2,$3,$4,$5::jsonb,$6)"
		if command.Scope == cryptoapp.OperationalHoldScopeWallet {
			values = "SELECT id,$2,$3,$4,$5::jsonb,$6 FROM cat_wallets WHERE id=$1 AND tier='pool' AND wallet_role='merchant_pool'"
		}
		_, err = q.Exec(ctx, `INSERT INTO `+table+`(`+column+`,reason,owner_type,owner_id,details,held_by) `+values+`
 ON CONFLICT (`+column+`,owner_type,owner_id) WHERE status='open' DO NOTHING`, command.ScopeID, command.Reason, command.OwnerType, command.OwnerID, string(details), command.HeldBy)
		if err != nil {
			return fmt.Errorf("acquire %s operational hold: %w", command.Scope, err)
		}
		count, exact, err = check()
		if err != nil {
			return err
		}
		if count == 0 || !exact {
			return apperror.NewConflict("operational hold replay differs from its original acquisition")
		}
		return nil
	})
}

func (r *OperationalHoldRepo) ResolveOperationalHolds(ctx context.Context, ownerType, ownerID, actor, reason string) (int64, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return 0, apperror.NewValidation("hold resolver and reason are required")
	}
	tm := postgres.MustGetTxManager(ctx)
	var resolved int64
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockOperationalHoldOwner(ctx, ownerType, ownerID); err != nil {
			return err
		}
		q := tm.GetQuerier(ctx)
		// Validate both tables before writing either. Existing mixed cohorts may
		// complete only with the same original resolution actor and reason.
		var total, open int64
		for _, table := range []string{"reg_crypto_network_holds", "reg_crypto_wallet_allocation_holds"} {
			if _, err := q.Exec(ctx, `SELECT id FROM `+table+` WHERE owner_type=$1 AND owner_id=$2 ORDER BY id FOR UPDATE`, ownerType, ownerID); err != nil {
				return err
			}
			var count, pending int64
			var exact bool
			if err := q.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='open'),
 COALESCE(bool_and(status='open' OR (resolved_by=$3 AND resolution=$4)),TRUE) FROM `+table+` WHERE owner_type=$1 AND owner_id=$2`, ownerType, ownerID, actor, reason).Scan(&count, &pending, &exact); err != nil {
				return err
			}
			if !exact {
				return apperror.NewConflict("operational hold resolution replay differs from its original actor or reason")
			}
			total += count
			open += pending
		}
		if total == 0 {
			return apperror.NewNotFound("operational hold owner", ownerType+":"+ownerID)
		}
		if open == 0 {
			return nil
		}
		for _, table := range []string{"reg_crypto_network_holds", "reg_crypto_wallet_allocation_holds"} {
			tag, err := q.Exec(ctx, `UPDATE `+table+` SET status='resolved',resolved_at=clock_timestamp(),resolved_by=$3,resolution=$4
 WHERE owner_type=$1 AND owner_id=$2 AND status='open'`, ownerType, ownerID, actor, reason)
			if err != nil {
				return fmt.Errorf("resolve owned operational holds: %w", err)
			}
			resolved += tag.RowsAffected()
		}
		if resolved != open {
			return apperror.NewConflict("operational hold targets changed; retry the command")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return resolved, nil
}

var _ cryptoapp.OperationalHoldRepository = (*OperationalHoldRepo)(nil)
