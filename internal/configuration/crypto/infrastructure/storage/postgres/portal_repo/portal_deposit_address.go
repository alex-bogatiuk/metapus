package portal_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/application/portalapp"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
)

// DepositAddressFilter contains filter parameters for persistent customer deposit addresses.
type DepositAddressFilter struct {
	Search    string
	NetworkID string
}

// ListDepositAddresses returns persistent customer deposit addresses and per-network quotas for a merchant.
func (r *DashboardRepo) ListDepositAddresses(
	ctx context.Context,
	merchantID id.ID,
	filter DepositAddressFilter,
	limit, offset int,
) ([]portalapp.PortalDepositAddressItem, int, []portalapp.PortalDepositAddressQuotaItem, error) {
	scope := merchant.MustGetPortalScope(ctx)
	if !slices.Contains(scope.MerchantIDs, merchantID) {
		return nil, 0, nil, apperror.NewForbidden("merchant_id not in scope")
	}

	quotas, tokensByNet, err := r.listDepositAddressQuotas(ctx, merchantID)
	if err != nil {
		return nil, 0, nil, err
	}

	q := querier(ctx)
	where := `w.merchant_id = $1 AND w.allocation_mode = 'persistent' AND w.deposit_retired_at IS NULL AND w.is_active = TRUE AND w.deletion_mark = FALSE AND w._deleted_at IS NULL`
	args := make([]any, 0, 5)
	args = append(args, merchantID)
	argIdx := 2

	if s := strings.TrimSpace(filter.Search); s != "" {
		pattern := "%" + s + "%"
		where += fmt.Sprintf(` AND (w.customer_id ILIKE $%d OR w.address ILIKE $%d)`, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}
	if rawNet := strings.TrimSpace(filter.NetworkID); rawNet != "" {
		netID, parseErr := id.Parse(rawNet)
		if parseErr != nil {
			return nil, 0, nil, apperror.NewValidation("invalid network_id")
		}
		where += fmt.Sprintf(` AND w.network_id = $%d`, argIdx)
		args = append(args, netID)
		argIdx++
	}

	var total int
	countSQL := `SELECT COUNT(*) FROM cat_wallets w WHERE ` + where
	if err := q.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, nil, fmt.Errorf("portal deposit addresses count: %w", err)
	}
	if total == 0 {
		return []portalapp.PortalDepositAddressItem{}, 0, quotas, nil
	}

	dataSQL := fmt.Sprintf(`
		SELECT
			w.id::text,
			w.customer_id,
			w.address,
			w.network_id::text,
			COALESCE(n.code, '') AS network_code,
			COALESCE(n.name, '') AS network_name,
			COALESCE(stats.top_up_count, 0) AS top_up_count,
			stats.last_top_up_at,
			COALESCE(w.deposit_assigned_at,w.updated_at),
			(SELECT MAX(r.created_at)+INTERVAL '24 hours' FROM reg_crypto_deposit_address_rotations r WHERE r.merchant_id=w.merchant_id AND r.customer_id=w.customer_id),
			CASE WHEN COALESCE(stats.top_up_count,0)=0 THEN 'unused_address'
			 WHEN w.status='sweeping' THEN 'sweep_in_progress'
			 WHEN EXISTS(SELECT 1 FROM doc_crypto_payments p WHERE p.wallet_id=w.id AND p.status IN ('detected','confirming') AND p._deleted_at IS NULL)
			 OR EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.wallet_id=w.id AND a.status IN ('claimed','resource_ready','signing','broadcast','held')) THEN 'operation_in_progress'
			 WHEN EXISTS(SELECT 1 FROM reg_crypto_deposit_address_rotations r WHERE r.merchant_id=w.merchant_id AND r.customer_id=w.customer_id AND r.created_at>NOW()-INTERVAL '24 hours') THEN 'rotation_cooldown'
			 WHEN (SELECT COUNT(*) FROM reg_crypto_deposit_address_rotations r WHERE r.merchant_id=w.merchant_id AND r.network_id=w.network_id AND r.created_at>NOW()-INTERVAL '24 hours')
			   >=COALESCE(limits_override.max_persistent_addresses,pool_policy.max_persistent_addresses_per_merchant,0) THEN 'merchant_rotation_limit'
			 ELSE '' END
		FROM cat_wallets w
		LEFT JOIN cat_blockchain_networks n ON n.id = w.network_id
		LEFT JOIN sys_crypto_wallet_pool_policies pool_policy ON pool_policy.network_id=w.network_id AND pool_policy._deleted_at IS NULL
		LEFT JOIN reg_merchant_wallet_allocation_overrides limits_override ON limits_override.merchant_id=w.merchant_id AND limits_override.network_id=w.network_id AND limits_override._deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)::INT AS top_up_count,
				MAX(COALESCE(p.confirmed_at,p.created_at)) AS last_top_up_at
			FROM doc_crypto_payments p
			WHERE p.wallet_id = w.id
			  AND p.merchant_id = w.merchant_id
			  AND p.status IN ('confirmed','settled') AND p.canonicality='canonical'
			  AND p._deleted_at IS NULL
		) stats ON TRUE
		WHERE %s
		ORDER BY w.updated_at DESC, w.id DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := q.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("portal deposit addresses list: %w", err)
	}
	defer rows.Close()

	items := make([]portalapp.PortalDepositAddressItem, 0, limit)
	walletIDs := make([]id.ID, 0, limit)
	for rows.Next() {
		var item portalapp.PortalDepositAddressItem
		var lastTopUpAt *time.Time
		var boundAt time.Time
		var walletID id.ID
		if err := rows.Scan(
			&walletID,
			&item.CustomerID,
			&item.Address,
			&item.NetworkID,
			&item.NetworkCode,
			&item.NetworkName,
			&item.TopUpCount,
			&lastTopUpAt,
			&boundAt,
			&item.NextRotationAt,
			&item.RotationBlockedReason,
		); err != nil {
			return nil, 0, nil, fmt.Errorf("portal deposit addresses scan: %w", err)
		}
		item.ID = walletID.String()
		walletIDs = append(walletIDs, walletID)
		item.CanRotate = item.RotationBlockedReason == ""
		item.RiskClass = string(aml.Unknown)
		item.RiskAssets = []aml.WalletAssetDisplay{}
		if lastTopUpAt != nil {
			ts := lastTopUpAt.UTC().Format(time.RFC3339)
			item.LastTopUpAt = &ts
		}
		item.CreatedAt = boundAt.UTC().Format(time.RFC3339)
		if toks, ok := tokensByNet[item.NetworkID]; ok {
			copied := make([]string, len(toks))
			copy(copied, toks)
			item.Tokens = copied
		} else {
			item.Tokens = []string{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, fmt.Errorf("portal deposit addresses rows: %w", err)
	}
	rows.Close()
	risks, err := crypto_repo.NewAMLRepository().ReadWalletRiskDisplays(ctx, walletIDs)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("read deposit address AML: %w", err)
	}
	for index, walletID := range walletIDs {
		if risk, ok := risks[walletID]; ok {
			items[index].RiskClass = string(risk.Class)
			items[index].RiskEnabled = risk.Enabled
			items[index].RiskExpiresAt = risk.ExpiresAt
			if risk.Assets != nil {
				items[index].RiskAssets = risk.Assets
			}
		}
	}

	return items, total, quotas, nil
}

func (r *DashboardRepo) listDepositAddressQuotas(
	ctx context.Context,
	merchantID id.ID,
) ([]portalapp.PortalDepositAddressQuotaItem, map[string][]string, error) {
	q := querier(ctx)
	const sqlQuery = `
		SELECT
			n.id::text AS network_id,
			n.code AS network_code,
			n.name AS network_name,
			COALESCE((
				SELECT COUNT(*)::INT
				FROM cat_wallets w
				WHERE w.merchant_id = $1
				  AND w.network_id = n.id
				  AND w.allocation_mode = 'persistent'
				  AND w.deposit_retired_at IS NULL
				  AND w.is_active = TRUE
				  AND w.deletion_mark = FALSE
				  AND w._deleted_at IS NULL
			), 0) AS used_count,
			COALESCE(o.max_persistent_addresses, p.max_persistent_addresses_per_merchant, 0) AS max_count,
			COALESCE(
				ARRAY_AGG(DISTINCT t.symbol ORDER BY t.symbol) FILTER (WHERE t.symbol IS NOT NULL),
				ARRAY[]::TEXT[]
			) AS tokens
		FROM cat_blockchain_networks n
		JOIN cat_tokens t
		  ON t.network_id = n.id
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		 AND t._deleted_at IS NULL
		LEFT JOIN sys_crypto_wallet_pool_policies p
		  ON p.network_id = n.id
		 AND p._deleted_at IS NULL
		LEFT JOIN reg_merchant_wallet_allocation_overrides o
		  ON o.network_id = n.id
		 AND o.merchant_id = $1
		 AND o._deleted_at IS NULL
		WHERE n.is_active = TRUE
		  AND n.deletion_mark = FALSE
		  AND n._deleted_at IS NULL
		GROUP BY n.id, n.code, n.name, o.max_persistent_addresses, p.max_persistent_addresses_per_merchant
		ORDER BY n.name, n.code
	`

	rows, err := q.Query(ctx, sqlQuery, merchantID)
	if err != nil {
		return nil, nil, fmt.Errorf("portal deposit address quotas: %w", err)
	}
	defer rows.Close()

	quotas := make([]portalapp.PortalDepositAddressQuotaItem, 0, 4)
	tokensByNet := make(map[string][]string, 4)
	for rows.Next() {
		var item portalapp.PortalDepositAddressQuotaItem
		if err := rows.Scan(
			&item.NetworkID,
			&item.NetworkCode,
			&item.NetworkName,
			&item.Used,
			&item.Max,
			&item.Tokens,
		); err != nil {
			return nil, nil, fmt.Errorf("portal deposit address quotas scan: %w", err)
		}
		if item.Tokens == nil {
			item.Tokens = []string{}
		}
		tokensByNet[item.NetworkID] = item.Tokens
		quotas = append(quotas, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("portal deposit address quotas rows: %w", err)
	}
	return quotas, tokensByNet, nil
}

// GetDepositNetworkMeta validates that a network is active and returns its code, name, and active token symbols.
func (r *DashboardRepo) GetDepositNetworkMeta(
	ctx context.Context,
	merchantID, networkID id.ID,
) (string, string, []string, error) {
	scope := merchant.MustGetPortalScope(ctx)
	if !slices.Contains(scope.MerchantIDs, merchantID) {
		return "", "", nil, apperror.NewForbidden("merchant_id not in scope")
	}

	q := querier(ctx)
	var networkCode, networkName string
	var tokens []string
	err := q.QueryRow(ctx, `
		SELECT
			n.code,
			n.name,
			COALESCE(
				ARRAY_AGG(DISTINCT t.symbol ORDER BY t.symbol) FILTER (WHERE t.symbol IS NOT NULL),
				ARRAY[]::TEXT[]
			) AS tokens
		FROM cat_blockchain_networks n
		JOIN cat_tokens t
		  ON t.network_id = n.id
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		 AND t._deleted_at IS NULL
		WHERE n.id = $1
		  AND n.is_active = TRUE
		  AND n.deletion_mark = FALSE
		  AND n._deleted_at IS NULL
		GROUP BY n.id, n.code, n.name
	`, networkID).Scan(&networkCode, &networkName, &tokens)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, apperror.NewValidation("unknown or inactive network").WithDetail("field", "networkId")
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("portal get deposit network meta: %w", err)
	}
	if tokens == nil {
		tokens = []string{}
	}
	return networkCode, networkName, tokens, nil
}
