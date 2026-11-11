package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type NetworkPolicyRepo struct{}

func NewNetworkPolicyRepo() *NetworkPolicyRepo { return &NetworkPolicyRepo{} }

const networkPolicyStateSelect = `
	SELECT n.id,n.code,n.chain_family,n.network_identifier,n.native_token_symbol,native.id,native.decimal_places,n.confirmations_needed,n.finality_mode,
	       p.enabled,p.per_transaction_max_fee_minor,p.daily_network_fee_budget_minor,p.fee_funding_mode,
	       p.daily_jit_funding_budget_minor,p.min_fee_wallet_balance_minor,p.min_source_wallet_residual_minor,
	       p.jit_funding_max_amount_minor,p.retry_delay_seconds,p.rpc_failure_threshold,p.rpc_open_seconds,
	       p.version,p.updated_by,p.created_at,p.updated_at,
	       tr.resource_strategy,COALESCE(tr.provider_account_ids,ARRAY[]::uuid[]),tr.provider_failure_threshold,tr.provider_open_seconds,
	       COALESCE(rs.rpc_endpoints,'[]'::jsonb),COALESCE(rs.signer_ready,FALSE),COALESCE(rs.signer_error,''),
	       COALESCE(rs.resource_provider_ready,FALSE),COALESCE(rs.resource_provider_state,'unconfigured'),
	       COALESCE(rs.resource_providers,'[]'::jsonb),
	       rs.observed_at,COALESCE(rs.worker_id,''),
	       EXISTS (SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=n.id AND h.status='open'),
	       COALESCE((SELECT h.reason FROM reg_crypto_network_holds h WHERE h.network_id=n.id AND h.status='open' ORDER BY h.held_at DESC LIMIT 1),''),
	       n.is_active
	FROM cat_blockchain_networks n
	JOIN LATERAL (
		SELECT id,decimal_places FROM cat_tokens
		WHERE network_id=n.id AND token_standard='native' AND is_active=TRUE AND deletion_mark=FALSE
		ORDER BY created_at,id LIMIT 1
	) native ON TRUE
	JOIN sys_crypto_network_policies p ON p.network_id=n.id AND p._deleted_at IS NULL
	LEFT JOIN sys_crypto_external_resource_policies tr ON tr.network_id=n.id
	LEFT JOIN sys_crypto_network_runtime_status rs ON rs.network_id=n.id
	WHERE n.deletion_mark=FALSE`

func (r *NetworkPolicyRepo) ListNetworkPolicyStates(ctx context.Context) ([]crypto.NetworkPolicyState, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, networkPolicyStateSelect+` ORDER BY n.code,n.id`)
	if err != nil {
		return nil, fmt.Errorf("list crypto network policies: %w", err)
	}
	defer rows.Close()
	states := make([]crypto.NetworkPolicyState, 0)
	for rows.Next() {
		state, err := scanNetworkPolicyState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *NetworkPolicyRepo) GetNetworkPolicyState(ctx context.Context, networkID id.ID) (crypto.NetworkPolicyState, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	state, err := scanNetworkPolicyState(q.QueryRow(ctx, networkPolicyStateSelect+` AND n.id=$1`, networkID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return state, apperror.NewNotFound("crypto_network_policy", networkID.String())
		}
		return state, fmt.Errorf("get crypto network policy: %w", err)
	}
	return state, nil
}

type policyStateScanner interface {
	Scan(...any) error
}

func scanNetworkPolicyState(row policyStateScanner) (crypto.NetworkPolicyState, error) {
	var state crypto.NetworkPolicyState
	var family, finalityMode, fundingMode string
	var strategy *string
	var retrySeconds, rpcOpenSeconds, providerOpenSeconds int
	var providerFailureThreshold *int
	var providerOpenSecondsNullable *int
	var providerAccountIDs []id.ID
	var endpoints []byte
	var resourceProviders []byte
	var observedAt *time.Time
	err := row.Scan(
		&state.Network.ID, &state.Network.Code, &family, &state.Network.NetworkIdentifier, &state.Network.NativeTokenSymbol, &state.Network.NativeTokenID, &state.Network.NativeDecimals,
		&state.Network.RequiredConfirmations, &finalityMode,
		&state.Policy.Enabled, &state.Policy.PerTransactionMaxFee, &state.Policy.DailyNetworkFeeBudget, &fundingMode,
		&state.Policy.FeeFunding.DailyBudget, &state.Policy.FeeFunding.MinFeeWalletBalance, &state.Policy.FeeFunding.MinSourceWalletResidual,
		&state.Policy.FeeFunding.MaxFundingAmount, &retrySeconds, &state.Policy.RPCFailureThreshold, &rpcOpenSeconds,
		&state.Policy.Version,
		&state.Policy.UpdatedBy, &state.Policy.CreatedAt, &state.Policy.UpdatedAt,
		&strategy, &providerAccountIDs, &providerFailureThreshold, &providerOpenSecondsNullable,
		&endpoints, &state.Runtime.SignerReady,
		&state.Runtime.SignerError, &state.Runtime.ResourceProviderReady, &state.Runtime.ResourceProviderState,
		&resourceProviders, &observedAt, &state.Runtime.WorkerID,
		&state.Runtime.OperationalHold, &state.Runtime.OperationalHoldReason,
		&state.Watching,
	)
	if err != nil {
		return state, err
	}
	state.Network.Family = crypto.ChainFamily(family)
	state.Network.FinalityMode = crypto.FinalityMode(finalityMode)
	state.Policy.NetworkID, state.Runtime.NetworkID = state.Network.ID, state.Network.ID
	state.Policy.FeeFunding.Mode = crypto.FeeFundingMode(fundingMode)
	state.Policy.RetryDelay = time.Duration(retrySeconds) * time.Second
	state.Policy.RPCOpenDuration = time.Duration(rpcOpenSeconds) * time.Second
	if strategy != nil && providerFailureThreshold != nil && providerOpenSecondsNullable != nil {
		providerOpenSeconds = *providerOpenSecondsNullable
		state.Policy.ExternalResources = &crypto.ExternalResourcePolicy{
			Strategy:                 crypto.ResourceStrategyPolicy(*strategy),
			ProviderAccountIDs:       providerAccountIDs,
			ProviderFailureThreshold: *providerFailureThreshold,
			ProviderOpenDuration:     time.Duration(providerOpenSeconds) * time.Second,
		}
	}
	if observedAt != nil {
		state.Runtime.ObservedAt = *observedAt
	}
	if err := json.Unmarshal(endpoints, &state.Runtime.RPCEndpoints); err != nil {
		return state, fmt.Errorf("decode RPC runtime status: %w", err)
	}
	if len(resourceProviders) > 0 {
		if err := json.Unmarshal(resourceProviders, &state.Runtime.ResourceProviders); err != nil {
			return state, fmt.Errorf("decode resource provider runtime status: %w", err)
		}
	}
	return state, nil
}

// NetworkFundingSourcesReady checks the source registered to each active owner.
// The same owner match is repeated when an individual funding operation starts.
func (r *NetworkPolicyRepo) NetworkFundingSourcesReady(ctx context.Context, networkID id.ID, minimum types.CryptoAmount) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var ready bool
	err := q.QueryRow(ctx, `
		WITH owners AS (
			SELECT DISTINCT custody_merchant_id FROM cat_wallets
			WHERE network_id=$1 AND is_active AND NOT deletion_mark AND NOT is_folder
			  AND wallet_role IN ('merchant_pool','liquidity_hot','network_fee')
		)
		SELECT EXISTS(SELECT 1 FROM owners) AND NOT EXISTS (
			SELECT 1 FROM owners o WHERE NOT EXISTS (
				SELECT 1 FROM cat_wallets w
				JOIN cat_tokens native ON native.network_id=w.network_id AND native.token_standard='native'
				  AND native.is_active AND NOT native.deletion_mark
				LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=native.id
				WHERE w.network_id=$1 AND w.wallet_role='network_fee'
				  AND w.custody_merchant_id IS NOT DISTINCT FROM o.custody_merchant_id
				  AND w.is_active AND NOT w.deletion_mark AND COALESCE(b.amount,0) >= $2
			)
		)
	`, networkID, minimum).Scan(&ready)
	return ready, err
}

func (r *NetworkPolicyRepo) NetworkHasActiveContractAssets(ctx context.Context, networkID id.ID) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cat_tokens
			WHERE network_id=$1 AND token_standard <> 'native'
			  AND is_active=TRUE AND deletion_mark=FALSE
		)
	`, networkID).Scan(&exists)
	return exists, err
}

func (r *NetworkPolicyRepo) NetworkProviderAccountCodes(ctx context.Context, networkID id.ID, accountIDs []id.ID, observedAt time.Time) (map[id.ID]string, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT id, code FROM cat_crypto_provider_accounts
		WHERE network_id=$1 AND id=ANY($2) AND is_active=TRUE AND is_folder=FALSE AND deletion_mark=FALSE
		  AND length(credentials_enc)>0
		  AND COALESCE(credentials_updated_at, created_at) <= $3
	`, networkID, accountIDs, observedAt)
	if err != nil {
		return nil, fmt.Errorf("list selected crypto provider accounts: %w", err)
	}
	defer rows.Close()
	codes := make(map[id.ID]string, len(accountIDs))
	for rows.Next() {
		var accountID id.ID
		var code string
		if err := rows.Scan(&accountID, &code); err != nil {
			return nil, err
		}
		codes[accountID] = code
	}
	return codes, rows.Err()
}

func (r *NetworkPolicyRepo) UpdateNetworkPolicy(ctx context.Context, policy crypto.NetworkPolicy, expectedVersion int, actor string) (crypto.NetworkPolicyState, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE sys_crypto_network_policies SET
			enabled=$2,per_transaction_max_fee_minor=$3,daily_network_fee_budget_minor=$4,fee_funding_mode=$5,
			daily_jit_funding_budget_minor=$6,min_fee_wallet_balance_minor=$7,min_source_wallet_residual_minor=$8,
			jit_funding_max_amount_minor=$9,retry_delay_seconds=$10,rpc_failure_threshold=$11,rpc_open_seconds=$12,
			version=version+1,updated_by=$13
		WHERE network_id=$1 AND version=$14 AND _deleted_at IS NULL
	`, policy.NetworkID, policy.Enabled, policy.PerTransactionMaxFee, policy.DailyNetworkFeeBudget, policy.FeeFunding.Mode,
		policy.FeeFunding.DailyBudget, policy.FeeFunding.MinFeeWalletBalance, policy.FeeFunding.MinSourceWalletResidual, policy.FeeFunding.MaxFundingAmount,
		int(policy.RetryDelay.Seconds()), policy.RPCFailureThreshold, int(policy.RPCOpenDuration.Seconds()),
		actor, expectedVersion)
	if err != nil {
		return crypto.NetworkPolicyState{}, fmt.Errorf("update crypto network policy: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return crypto.NetworkPolicyState{}, apperror.NewConcurrentModification("crypto_network_policy", policy.NetworkID.String())
	}
	if policy.ExternalResources == nil {
		if _, err := q.Exec(ctx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id=$1`, policy.NetworkID); err != nil {
			return crypto.NetworkPolicyState{}, fmt.Errorf("delete TRON resource policy: %w", err)
		}
	} else {
		accountIDs := policy.ExternalResources.ProviderAccountIDs
		if accountIDs == nil {
			accountIDs = []id.ID{}
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO sys_crypto_external_resource_policies (network_id,resource_strategy,provider_account_ids,provider_failure_threshold,provider_open_seconds)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (network_id) DO UPDATE SET resource_strategy=EXCLUDED.resource_strategy,
				provider_account_ids=EXCLUDED.provider_account_ids,
				provider_failure_threshold=EXCLUDED.provider_failure_threshold,
				provider_open_seconds=EXCLUDED.provider_open_seconds,updated_at=NOW()
		`, policy.NetworkID, policy.ExternalResources.Strategy, accountIDs, policy.ExternalResources.ProviderFailureThreshold, int(policy.ExternalResources.ProviderOpenDuration.Seconds())); err != nil {
			return crypto.NetworkPolicyState{}, fmt.Errorf("upsert TRON resource policy: %w", err)
		}
	}
	return r.GetNetworkPolicyState(ctx, policy.NetworkID)
}

var _ crypto.NetworkPolicyAdminRepository = (*NetworkPolicyRepo)(nil)

// SetNetworkWatching flips the catalog activity that decides whether the
// worker reads the network. The SQL watch guards keep their reason text, which
// names the blocking condition without data, as the validation message.
func (r *NetworkPolicyRepo) SetNetworkWatching(ctx context.Context, networkID id.ID, watching bool) (crypto.NetworkPolicyState, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := q.Exec(ctx, `UPDATE cat_blockchain_networks SET is_active=$2,version=version+1
		WHERE id=$1 AND deletion_mark=FALSE AND is_active<>$2`, networkID, watching)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23514":
			return crypto.NetworkPolicyState{}, apperror.NewValidation(pgErr.Message)
		case "40P01", "55P03":
			return crypto.NetworkPolicyState{}, apperror.NewConflict("network settings are changing concurrently; retry the operation")
		}
	}
	if err != nil {
		return crypto.NetworkPolicyState{}, fmt.Errorf("set crypto network watching: %w", err)
	}
	return r.GetNetworkPolicyState(ctx, networkID)
}
