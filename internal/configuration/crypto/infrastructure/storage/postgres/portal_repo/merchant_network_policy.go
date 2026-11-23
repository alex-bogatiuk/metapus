package portal_repo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/notifications"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

type MerchantNetworkPolicyRepo struct{}

func NewMerchantNetworkPolicyRepo() *MerchantNetworkPolicyRepo { return &MerchantNetworkPolicyRepo{} }

func (r *MerchantNetworkPolicyRepo) GetNetworkPolicy(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID) (*merchant.NetworkPolicy, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	policy := &merchant.NetworkPolicy{MerchantID: merchantID, Surface: surface, APIKeyID: apiKeyID, Mode: merchant.NetworkPolicyOff, Version: 1, Networks: []merchant.AllowedNetwork{}}
	err := q.QueryRow(ctx, `
		SELECT id,mode,version,updated_at
		FROM auth_merchant_network_policies
		WHERE merchant_id=$1 AND surface=$2 AND api_key_id IS NOT DISTINCT FROM $3
	`, merchantID, surface, apiKeyID).Scan(&policy.ID, &policy.Mode, &policy.Version, &policy.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merchant network policy: %w", err)
	}
	rows, err := q.Query(ctx, `
		SELECT id,network::text,label,is_active,created_at
		FROM auth_merchant_allowed_networks WHERE policy_id=$1 AND is_active=TRUE
		ORDER BY network,id
	`, policy.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var network merchant.AllowedNetwork
		if err := rows.Scan(&network.ID, &network.Network, &network.Label, &network.IsActive, &network.CreatedAt); err != nil {
			return nil, err
		}
		policy.Networks = append(policy.Networks, network)
	}
	return policy, rows.Err()
}

func (r *MerchantNetworkPolicyRepo) GetEffectiveNetworkPolicies(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID) ([]merchant.NetworkPolicy, error) {
	baseline, err := r.GetNetworkPolicy(ctx, merchantID, surface, nil)
	if err != nil {
		return nil, err
	}
	policies := []merchant.NetworkPolicy{*baseline}
	if surface == merchant.NetworkSurfaceAPI && apiKeyID != nil {
		keyPolicy, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
		if err != nil {
			return nil, err
		}
		if !id.IsNil(keyPolicy.ID) {
			policies = append(policies, *keyPolicy)
		}
	}
	return policies, nil
}

func (r *MerchantNetworkPolicyRepo) SetNetworkPolicyMode(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, mode merchant.NetworkPolicyMode, actorID id.ID, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	before, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	policyID, err := r.ensurePolicy(ctx, merchantID, surface, apiKeyID, actorID)
	if err != nil {
		return nil, err
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE auth_merchant_network_policies SET mode=$2,updated_by=$3 WHERE id=$1
	`, policyID, mode, actorID)
	if err != nil {
		return nil, fmt.Errorf("set merchant network policy mode: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, apperror.NewConflict("merchant network policy update raced")
	}
	after, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	if err := r.auditChange(ctx, merchantID, policyID, actorID, "set_mode", before, after, reason, recovery); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *MerchantNetworkPolicyRepo) AddAllowedNetwork(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, prefix netip.Prefix, label string, actorID id.ID, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	before, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	policyID, err := r.ensurePolicy(ctx, merchantID, surface, apiKeyID, actorID)
	if err != nil {
		return nil, err
	}
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO auth_merchant_allowed_networks(policy_id,network,label,created_by)
		VALUES ($1,$2::cidr,$3,$4)
		ON CONFLICT (policy_id,network) WHERE is_active DO UPDATE SET label=EXCLUDED.label
	`, policyID, prefix.String(), label, actorID)
	if err != nil {
		return nil, fmt.Errorf("add merchant allowed network: %w", err)
	}
	after, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	if err := r.auditChange(ctx, merchantID, policyID, actorID, "add_network", before, after, reason, recovery); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *MerchantNetworkPolicyRepo) RevokeAllowedNetwork(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, networkID, actorID id.ID, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	before, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	if id.IsNil(before.ID) {
		return nil, apperror.NewNotFound("merchant network policy", merchantID.String())
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE auth_merchant_allowed_networks
		SET is_active=FALSE,revoked_by=$3,revoked_at=NOW()
		WHERE id=$1 AND policy_id=$2 AND is_active=TRUE
	`, networkID, before.ID, actorID)
	if err != nil {
		return nil, fmt.Errorf("revoke merchant allowed network: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, apperror.NewNotFound("allowed network", networkID.String())
	}
	after, err := r.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	if err := r.auditChange(ctx, merchantID, before.ID, actorID, "revoke_network", before, after, reason, recovery); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *MerchantNetworkPolicyRepo) RecordNetworkDecision(ctx context.Context, merchantID id.ID, policyID *id.ID, actorType, actorID string, decision merchant.NetworkAccessDecision, clientIP netip.Addr) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO auth_merchant_network_policy_audit(
			merchant_id,policy_id,actor_type,actor_id,action,decision,client_ip,reason
		) VALUES ($1,$2,$3,$4,'request_evaluated',$5,$6::inet,'network access evaluation')
	`, merchantID, policyID, actorType, actorID, decision, clientIP.Unmap().String())
	return err
}

func (r *MerchantNetworkPolicyRepo) ensurePolicy(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, actorID id.ID) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var policyID id.ID
	err := q.QueryRow(ctx, `SELECT id FROM auth_merchant_network_policies
		WHERE merchant_id=$1 AND surface=$2 AND api_key_id IS NOT DISTINCT FROM $3 FOR UPDATE`, merchantID, surface, apiKeyID).Scan(&policyID)
	if err == nil {
		return policyID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), err
	}
	err = q.QueryRow(ctx, `
		INSERT INTO auth_merchant_network_policies(merchant_id,surface,api_key_id,created_by,updated_by)
		VALUES($1,$2,$3,$4,$4) RETURNING id
	`, merchantID, surface, apiKeyID, actorID).Scan(&policyID)
	return policyID, err
}

func (r *MerchantNetworkPolicyRepo) auditChange(ctx context.Context, merchantID, policyID, actorID id.ID, action string, before, after *merchant.NetworkPolicy, reason string, recovery bool) error {
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(after)
	if recovery {
		action = "recovery_" + action
	}
	actorType := "user"
	if recovery {
		actorType = "system_admin"
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `
		INSERT INTO auth_merchant_network_policy_audit(
			merchant_id,policy_id,actor_type,actor_id,action,old_policy,new_policy,reason
		)VALUES($1,$2,$8,$3::text,$4,$5::jsonb,$6::jsonb,$7)
	`, merchantID, policyID, actorID, action, string(oldJSON), string(newJSON), reason, actorType); err != nil {
		return err
	}
	rows, err := q.Query(ctx, `
		SELECT user_id FROM sys_merchant_users
		WHERE merchant_id=$1 AND role=1 AND user_id<>$2 ORDER BY user_id
	`, merchantID, actorID)
	if err != nil {
		return err
	}
	var items []*notifications.Notification
	link := "/portal/settings"
	for rows.Next() {
		var owner id.ID
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		items = append(items, &notifications.Notification{
			UserID: owner, Title: "Изменена политика сетевого доступа",
			Message:  "Политика Portal или Merchant API была изменена другим владельцем.",
			Severity: notifications.SeverityWarning, Link: &link,
			Attributes: map[string]any{"merchantId": merchantID.String(), "policyId": policyID.String(), "action": action},
		})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return postgres.NewNotificationRepo().CreateBatch(ctx, items)
}

var _ merchant.NetworkPolicyRepository = (*MerchantNetworkPolicyRepo)(nil)
