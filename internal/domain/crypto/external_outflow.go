package crypto

import (
	"context"
	"time"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

type ExternalOutflowStatus string
type ExternalOutflowClassification string

var ErrExternalOutflowBusy = apperror.NewConflict("external outflow resources are busy; retry the command")

const (
	ExternalOutflowPendingReview ExternalOutflowStatus = "pending_review"
	ExternalOutflowClassified    ExternalOutflowStatus = "classified"
	ExternalOutflowResolved      ExternalOutflowStatus = "resolved"

	OutflowMatchedBasis         ExternalOutflowClassification = "matched_basis"
	OutflowUnauthorizedLoss     ExternalOutflowClassification = "unauthorized_loss"
	OutflowFalsePositiveOrReorg ExternalOutflowClassification = "false_positive_or_reorg"
)

func (c ExternalOutflowClassification) Valid() bool {
	return c == OutflowMatchedBasis || c == OutflowUnauthorizedLoss || c == OutflowFalsePositiveOrReorg
}

type ExternalOutflow struct {
	ID                     id.ID
	NetworkID              id.ID
	TokenID                *id.ID
	SourceWalletID         *id.ID
	TxHash                 string
	EventKind              EventKind
	EventIndex             int64
	FromAddress            string
	ToAddress              string
	ContractAddress        string
	Amount                 types.CryptoAmount
	BlockNumber            int64
	BlockHash              string
	Confirmations          int
	RequiredConfirmations  int
	FinalityStatus         string
	Status                 ExternalOutflowStatus
	Classification         ExternalOutflowClassification
	MatchedBasisType       string
	MatchedBasisID         *id.ID
	ClassificationReason   string
	ClassificationEvidence map[string]any
	ClassifiedBy           *id.ID
	ClassifiedAt           *time.Time
	ResolvedBy             *id.ID
	ResolvedAt             *time.Time
	Resolution             string
	DetectedAt             time.Time
	LastObservedAt         time.Time
	Actions                []ExternalOutflowAction
	Adjustment             *CryptoCustodyAdjustment
	OpenHolds              []OperationalHold
}

type ExternalOutflowAction struct {
	ActorType string
	ActorID   string
	Action    string
	OldState  string
	NewState  string
	Reason    string
	Evidence  map[string]any
	CreatedAt time.Time
}

type OperationalHold struct {
	Scope     string
	Reason    string
	OwnerType string
	OwnerID   string
	HeldAt    time.Time
}

type CryptoCustodyAdjustment struct {
	AccountingModelVersion int
	// RecognitionPeriod is assigned by financial admission for model 2 and
	// shared by both immutable movement generators and the custody binding.
	RecognitionPeriod time.Time
	ID                id.ID
	ExternalOutflowID id.ID
	NetworkID         id.ID
	WalletID          id.ID
	TokenID           id.ID
	Amount            types.CryptoAmount
	Status            string
	MakerID           id.ID
	DecisionBy        *id.ID
	DecisionReason    string
	PostedVersion     int
	CreatedAt         time.Time
	DecidedAt         *time.Time
}

const CryptoCustodyAdjustmentDocumentType = "CryptoCustodyAdjustment"

var _ posting.Postable = (*CryptoCustodyAdjustment)(nil)
var _ posting.UnpostGuard = (*CryptoCustodyAdjustment)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoCustodyAdjustment)(nil)
var _ posting.CryptoPlatformCostMovementSource = (*CryptoCustodyAdjustment)(nil)

func (a *CryptoCustodyAdjustment) GetID() id.ID { return a.ID }
func (a *CryptoCustodyAdjustment) GetDocumentType() string {
	return CryptoCustodyAdjustmentDocumentType
}
func (a *CryptoCustodyAdjustment) GetPostedVersion() int { return a.PostedVersion }
func (a *CryptoCustodyAdjustment) IsPosted() bool        { return a.Status == "posted" }

func (a *CryptoCustodyAdjustment) CanPost(context.Context) error {
	if id.IsNil(a.ID) || id.IsNil(a.ExternalOutflowID) || id.IsNil(a.NetworkID) ||
		id.IsNil(a.WalletID) || id.IsNil(a.TokenID) || id.IsNil(a.MakerID) || !a.Amount.IsPositive() {
		return apperror.NewValidation("custody adjustment identity and positive amount are required")
	}
	if a.Status != "pending_approval" || a.PostedVersion != 0 {
		return apperror.NewConflict("custody adjustment is not pending approval")
	}
	if a.AccountingModelVersion == 2 && a.RecognitionPeriod.IsZero() {
		return apperror.NewValidation("custody loss requires financial admission")
	}
	return nil
}

func (a *CryptoCustodyAdjustment) MarkPosted() {
	a.Status = "posted"
	a.PostedVersion++
}

// Custody losses are corrected by a new compensating document, never by
// deleting history or unposting the original loss.
func (a *CryptoCustodyAdjustment) CanUnpost(context.Context) error {
	return apperror.NewConflict("custody adjustment cannot be unposted; create a compensating adjustment")
}

func (a *CryptoCustodyAdjustment) MarkUnposted() {}

func (a *CryptoCustodyAdjustment) GenerateCryptoBalanceMovements(context.Context) ([]entity.CryptoBalanceMovement, error) {
	movement := entity.NewCryptoBalanceMovement(
		a.ID, a.GetDocumentType(), a.PostedVersion+1, time.Now().UTC(), entity.RecordTypeExpense,
		a.WalletID, a.TokenID, a.Amount,
	)
	if a.AccountingModelVersion == 2 {
		movement.LineID = a.PrincipalLineID()
		movement.Period = a.RecognitionPeriod
	}
	return []entity.CryptoBalanceMovement{movement}, nil
}

func (a *CryptoCustodyAdjustment) GenerateCryptoPlatformCostMovements(context.Context) ([]entity.CryptoPlatformCostMovement, error) {
	movement := entity.NewCryptoPlatformCostMovement(
		a.ID, a.GetDocumentType(), a.PostedVersion+1, time.Now().UTC(), entity.RecordTypeExpense,
		a.NetworkID, a.TokenID, a.TokenID, entity.PlatformCostTypeCustodyLoss,
		entity.PlatformCostPurposeCustodyLoss, "custody", a.ExternalOutflowID.String(), a.Amount,
	)
	if a.AccountingModelVersion == 2 {
		movement.LineID = uuid.NewSHA1(a.ID, []byte("custody-loss-cost"))
		movement.Period = a.RecognitionPeriod
	}
	return []entity.CryptoPlatformCostMovement{movement}, nil
}

func (a *CryptoCustodyAdjustment) PrincipalLineID() id.ID {
	return uuid.NewSHA1(a.ID, []byte("custody-loss-principal"))
}
