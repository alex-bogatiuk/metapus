package cryptodto

import (
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"time"
)

type CreateTreasuryAddressRequest struct {
	NetworkID string `json:"networkId" binding:"required"`
	Address   string `json:"address" binding:"required"`
	Label     string `json:"label,omitempty"`
}

type TreasuryAddressResponse struct {
	ID        string `json:"id"`
	NetworkID string `json:"networkId"`
	Address   string `json:"address"`
	Label     string `json:"label"`
	IsActive  bool   `json:"isActive"`
}

type ApproveRejectRequest struct {
	Reason string `json:"reason,omitempty"`
}

type TreasuryAvailabilityResponse struct {
	TokenID   string          `json:"tokenId"`
	Amount    types.RawAmount `json:"amount"`
	Reserved  types.RawAmount `json:"reserved"`
	Available types.RawAmount `json:"available"`
}

type TreasurySourceWalletResponse struct {
	ID                   string          `json:"id"`
	NetworkID            string          `json:"networkId"`
	Name                 string          `json:"name"`
	Address              string          `json:"address"`
	LedgerAmount         types.RawAmount `json:"ledgerAmount"`
	ReservedAmount       types.RawAmount `json:"reservedAmount"`
	AvailableAmount      types.RawAmount `json:"availableAmount"`
	LastChainAmount      *string         `json:"lastChainAmount,omitempty"`
	LastReconciliationAt string          `json:"lastReconciliationAt,omitempty"`
}

type ExternalOutflowResponse struct {
	ID              string          `json:"id"`
	NetworkID       string          `json:"networkId"`
	TokenID         string          `json:"tokenId,omitempty"`
	SourceWalletID  string          `json:"sourceWalletId,omitempty"`
	TxHash          string          `json:"txHash"`
	EventKind       string          `json:"eventKind"`
	EventIndex      int64           `json:"eventIndex"`
	FromAddress     string          `json:"fromAddress"`
	ToAddress       string          `json:"toAddress"`
	ContractAddress string          `json:"contractAddress"`
	Amount          types.RawAmount `json:"amount"`
	BlockNumber     int64           `json:"blockNumber"`
	BlockHash       string          `json:"blockHash"`
	Confirmations   int             `json:"confirmations"`
	RequiredConfs   int             `json:"requiredConfirmations"`
	Status          string          `json:"status"`
	FinalityStatus  string          `json:"finalityStatus"`
	Classification  string          `json:"classification,omitempty"`
	DetectedAt      string          `json:"detectedAt"`
}

type ExternalOutflowActionResponse struct {
	ActorType string         `json:"actorType"`
	ActorID   string         `json:"actorId"`
	Action    string         `json:"action"`
	OldState  string         `json:"oldState"`
	NewState  string         `json:"newState"`
	Reason    string         `json:"reason"`
	Evidence  map[string]any `json:"evidence,omitempty"`
	CreatedAt string         `json:"createdAt"`
}

type OperationalHoldResponse struct {
	Scope     string `json:"scope"`
	Reason    string `json:"reason"`
	OwnerType string `json:"ownerType"`
	OwnerID   string `json:"ownerId"`
	HeldAt    string `json:"heldAt"`
}

type CustodyAdjustmentResponse struct {
	ID                string  `json:"id"`
	ExternalOutflowID string  `json:"externalOutflowId"`
	NetworkID         string  `json:"networkId"`
	WalletID          string  `json:"walletId"`
	TokenID           string  `json:"tokenId"`
	Amount            string  `json:"amount"`
	Status            string  `json:"status"`
	MakerID           string  `json:"makerId"`
	DecisionBy        *string `json:"decisionBy,omitempty"`
	DecisionReason    string  `json:"decisionReason,omitempty"`
	PostedVersion     int     `json:"postedVersion"`
	CreatedAt         string  `json:"createdAt"`
	DecidedAt         *string `json:"decidedAt,omitempty"`
}

type ClassifyExternalOutflowRequest struct {
	Classification   crypto.ExternalOutflowClassification `json:"classification" binding:"required"`
	MatchedBasisType string                               `json:"matchedBasisType,omitempty"`
	MatchedBasisID   string                               `json:"matchedBasisId,omitempty"`
	Reason           string                               `json:"reason" binding:"required"`
	Evidence         map[string]any                       `json:"evidence,omitempty"`
}
type TreasuryAddressListResponse struct {
	Items []TreasuryAddressResponse `json:"items"`
}
type TreasurySourceWalletListResponse struct {
	Items []TreasurySourceWalletResponse `json:"items"`
}
type ExternalOutflowListResponse struct {
	Items []ExternalOutflowResponse `json:"items"`
}
type ExternalOutflowDetailResponse struct {
	Outflow                ExternalOutflowResponse         `json:"outflow"`
	ClassificationReason   string                          `json:"classificationReason"`
	ClassificationEvidence map[string]any                  `json:"classificationEvidence"`
	MatchedBasisType       string                          `json:"matchedBasisType"`
	MatchedBasisID         string                          `json:"matchedBasisId"`
	Adjustment             *CustodyAdjustmentResponse      `json:"adjustment"`
	OpenHolds              []OperationalHoldResponse       `json:"openHolds"`
	Actions                []ExternalOutflowActionResponse `json:"actions"`
	Resolution             string                          `json:"resolution"`
	CanResolve             bool                            `json:"canResolve"`
	ResolutionBlockers     []string                        `json:"resolutionBlockers"`
}

// FromCustodyAdjustment keeps action and detail responses on the same wire DTO.
func FromCustodyAdjustment(value *crypto.CryptoCustodyAdjustment) *CustodyAdjustmentResponse {
	if value == nil {
		return nil
	}
	response := &CustodyAdjustmentResponse{
		ID: value.ID.String(), ExternalOutflowID: value.ExternalOutflowID.String(),
		NetworkID: value.NetworkID.String(), WalletID: value.WalletID.String(), TokenID: value.TokenID.String(),
		Amount: value.Amount.String(), Status: value.Status, MakerID: value.MakerID.String(),
		DecisionReason: value.DecisionReason, PostedVersion: value.PostedVersion, CreatedAt: value.CreatedAt.Format(time.RFC3339),
	}
	if value.DecisionBy != nil {
		formatted := value.DecisionBy.String()
		response.DecisionBy = &formatted
	}
	if value.DecidedAt != nil {
		formatted := value.DecidedAt.Format(time.RFC3339)
		response.DecidedAt = &formatted
	}
	return response
}
