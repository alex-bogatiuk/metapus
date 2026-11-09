package dto

import "metapus/internal/domain/catalogs/merchant"

type SetMerchantNetworkPolicyRequest struct {
	Mode   merchant.NetworkPolicyMode `json:"mode" binding:"required"`
	Reason string                     `json:"reason" binding:"required"`
}
type AddMerchantAllowedNetworkRequest struct {
	CIDR   string `json:"cidr" binding:"required"`
	Label  string `json:"label"`
	Reason string `json:"reason" binding:"required"`
}
type MerchantNetworkPolicyResponse struct {
	Policy        *merchant.NetworkPolicy        `json:"policy"`
	DetectedIP    string                         `json:"detectedIp"`
	NetworkAccess merchant.NetworkAccessDecision `json:"networkAccess"`
}
type MerchantNetworkPolicy = merchant.NetworkPolicy
type RequiredReasonRequest struct {
	Reason string `json:"reason" binding:"required"`
}
