package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/tariff"
)

// The receipt journal is the read side of unidentified custody receipts and
// recorded wallet fundings. Its rows are projections, not domain entities.

// Journal states of the unidentified receipts list.
const (
	ReceiptJournalOpen     = "open"     // accounted, remainder not yet attributed
	ReceiptJournalCredited = "credited" // fully attributed
	ReceiptJournalOrphaned = "orphaned" // transfer left the canonical chain
	ReceiptJournalPending  = "pending"  // observed, waiting for finality or proof
)

// UnidentifiedReceiptRow is one journal line: an accounted receipt or, for the
// pending state, an observed transfer that is not accounted yet.
type UnidentifiedReceiptRow struct {
	ReceiptID         *string    `db:"receipt_id"`
	InboxEventID      *string    `db:"inbox_event_id"`
	Number            string     `db:"number"`
	ReceivedAt        time.Time  `db:"received_at"`
	NetworkID         string     `db:"network_id"`
	NetworkName       string     `db:"network_name"`
	TokenID           string     `db:"token_id"`
	TokenSymbol       string     `db:"token_symbol"`
	DecimalPlaces     int        `db:"decimal_places"`
	WalletID          string     `db:"wallet_id"`
	ToAddress         string     `db:"to_address"`
	FromAddress       string     `db:"from_address"`
	TxHash            string     `db:"tx_hash"`
	Amount            string     `db:"amount"`
	Remainder         string     `db:"remainder"`
	Reason            string     `db:"reason"`
	State             string     `db:"state"`
	CustodyMerchantID *string    `db:"custody_merchant_id"`
	InvoiceID         *string    `db:"invoice_id"`
	PendingError      string     `db:"pending_error"`
	CreditID          *string    `db:"credit_id"`
	CreditNumber      *string    `db:"credit_number"`
	CreditStatus      *string    `db:"credit_status"`
	CreditVersion     *int       `db:"credit_version"`
	CreditMerchantID  *string    `db:"credit_merchant_id"`
	CreditMerchant    *string    `db:"credit_merchant"`
	CreditNet         *string    `db:"credit_net"`
	CreditFee         *string    `db:"credit_fee"`
	CreditApproval    *bool      `db:"credit_approval"`
	CreditPreparedBy  *string    `db:"credit_prepared_by"`
	CreditDecidedAt   *time.Time `db:"credit_decided_at"`
	CreditReason      *string    `db:"credit_reason"`
	PaymentID         *string    `db:"payment_id"`
	PaymentNumber     *string    `db:"payment_number"`
	PaymentStatus     *string    `db:"payment_status"`
	// The latest platform funding of the receipt.
	// The latest refund of the receipt to its sender.
	RefundID              *string    `db:"refund_id"`
	RefundNumber          *string    `db:"refund_number"`
	RefundStatus          *string    `db:"refund_status"`
	RefundAmount          *string    `db:"refund_amount"`
	RefundWithheld        *string    `db:"refund_withheld"`
	RefundDestination     *string    `db:"refund_destination"`
	RefundPreparedBy      *string    `db:"refund_prepared_by"`
	FundingID             *string    `db:"funding_id"`
	FundingNumber         *string    `db:"funding_number"`
	FundingStatus         *string    `db:"funding_status"`
	FundingVersion        *int       `db:"funding_version"`
	FundingReason         *string    `db:"funding_reason"`
	FundingMerchant       *string    `db:"funding_merchant"`
	FundingMerchantLink   *string    `db:"funding_merchant_link"`
	FundingPreparedBy     *string    `db:"funding_prepared_by"`
	FundingDecidedAt      *time.Time `db:"funding_decided_at"`
	FundingDecisionReason *string    `db:"funding_decision_reason"`
	// The merchant the wallet ties the receipt to now; operators are warned
	// before taking such a receipt as platform money.
	TieMerchantID   *string `db:"tie_merchant_id"`
	TieMerchantName *string `db:"tie_merchant_name"`
	TieLink         *string `db:"tie_link"`
}

type UnidentifiedReceiptList struct {
	Items []UnidentifiedReceiptRow
	Total int
}

// ReceiptProofSource is one independent provider that agreed on the transfer.
type ReceiptProofSource struct {
	ProviderID string `json:"providerId"`
	TrustGroup string `json:"trustGroup"`
	HeadHeight int64  `json:"headHeight"`
}

// ReceiptProof is the canonical proof the receipt audit currently rests on:
// the original proof, or the proof of the block a re-included transfer moved to.
type ReceiptProof struct {
	VerificationID string               `db:"verification_id"`
	Original       bool                 `db:"original"`
	TxHash         string               `db:"tx_hash"`
	EventKind      string               `db:"event_kind"`
	EventIndex     int64                `db:"event_index"`
	BlockNumber    int64                `db:"block_number"`
	BlockHash      string               `db:"block_hash"`
	Confirmations  int64                `db:"confirmations"`
	VerifiedAt     time.Time            `db:"verified_at"`
	Sources        []ReceiptProofSource `db:"sources"`
}

// ReceiptAuditState is the canonicality audit of the receipt.
type ReceiptAuditState struct {
	Status        string     `db:"status"`
	LastCheckedAt *time.Time `db:"last_checked_at"`
	NextCheckAt   time.Time  `db:"next_check_at"`
	Attempts      int        `db:"attempts"`
	LastError     string     `db:"last_error"`
	OrphanedAt    *time.Time `db:"orphaned_at"`
	IncidentID    *string    `db:"incident_id"`
}

// ReceiptAttribution is a document that attributes (or attributed) the
// receipt: an invoice payment or a manual merchant credit. Outstanding is the
// part of the receipt it still holds after compensations.
type ReceiptAttribution struct {
	DocumentType  string    `db:"document_type"`
	DocumentID    string    `db:"document_id"`
	Number        string    `db:"number"`
	Date          time.Time `db:"date"`
	Status        string    `db:"status"`
	Posted        bool      `db:"posted"`
	MerchantID    string    `db:"merchant_id"`
	MerchantName  string    `db:"merchant_name"`
	Amount        string    `db:"amount"`
	NetAmount     string    `db:"net_amount"`
	ProcessingFee string    `db:"processing_fee"`
	Outstanding   string    `db:"outstanding"`
	// The payment tariff snapshot; a credit stores its net and fee directly.
	FeeFixed     types.RawAmount `db:"fee_fixed"`
	FeePercentBP int             `db:"fee_percent_bp"`
	FeeMin       types.RawAmount `db:"fee_min"`
	FeeMax       types.RawAmount `db:"fee_max"`
}

// ReceiptDetail is the receipt card: the journal line, its proof, its audit
// and the documents that distributed it.
type ReceiptDetail struct {
	Row          UnidentifiedReceiptRow
	Proof        ReceiptProof
	Audit        ReceiptAuditState
	Attributions []ReceiptAttribution
}

// WalletFundingRow is one recorded external funding of a platform wallet:
// it added custody directly, without passing through unidentified receipts.
type WalletFundingRow struct {
	ID             string    `db:"id"`
	NetworkName    string    `db:"network_name"`
	WalletID       string    `db:"wallet_id"`
	WalletCode     string    `db:"wallet_code"`
	TokenID        string    `db:"token_id"`
	TokenSymbol    string    `db:"token_symbol"`
	DecimalPlaces  int       `db:"decimal_places"`
	TxHash         string    `db:"tx_hash"`
	FromAddress    string    `db:"from_address"`
	ToAddress      string    `db:"to_address"`
	Amount         string    `db:"amount"`
	BlockNumber    int64     `db:"block_number"`
	Confirmations  int       `db:"confirmations"`
	BlockTimestamp time.Time `db:"block_timestamp"`
	RecordedAt     time.Time `db:"recorded_at"`
}

type WalletFundingList struct {
	Items []WalletFundingRow
	Total int
}

// ReceiptJournalRepository reads the journal projections.
type ReceiptJournalRepository interface {
	List(ctx context.Context, state string, includeDust bool, limit, offset int) (UnidentifiedReceiptList, error)
	Detail(ctx context.Context, receiptID id.ID) (ReceiptDetail, error)
}

// WalletFundingJournalRepository reads recorded external fundings of platform wallets.
type WalletFundingJournalRepository interface {
	List(ctx context.Context, limit, offset int) (WalletFundingList, error)
}

// ReceiptJournal answers the journal queries of operators.
type ReceiptJournal struct {
	receipts ReceiptJournalRepository
	fundings WalletFundingJournalRepository
}

func NewReceiptJournal(receipts ReceiptJournalRepository, fundings WalletFundingJournalRepository) *ReceiptJournal {
	if receipts == nil || fundings == nil {
		panic("receipt journal requires the receipt and wallet funding projections")
	}
	return &ReceiptJournal{receipts: receipts, fundings: fundings}
}

const maxJournalPage = 100

func validJournalPage(limit, offset int) error {
	if limit < 1 || limit > maxJournalPage {
		return apperror.NewValidation("limit must be between 1 and 100")
	}
	if offset < 0 {
		return apperror.NewValidation("offset must be non-negative")
	}
	return nil
}

// List returns one page of receipts in a journal state. Dust is hidden unless
// asked for: it is accounted like any supported asset but is almost always spam.
func (j *ReceiptJournal) List(ctx context.Context, state string, includeDust bool, limit, offset int) (UnidentifiedReceiptList, error) {
	switch state {
	case ReceiptJournalOpen, ReceiptJournalCredited, ReceiptJournalOrphaned, ReceiptJournalPending:
	default:
		return UnidentifiedReceiptList{}, apperror.NewValidation("invalid receipt journal state")
	}
	if err := validJournalPage(limit, offset); err != nil {
		return UnidentifiedReceiptList{}, err
	}
	return j.receipts.List(ctx, state, includeDust, limit, offset)
}

// Detail is the receipt card. A payment keeps only its tariff snapshot, so its
// fee and net are derived here with the same rule the payment posts with.
func (j *ReceiptJournal) Detail(ctx context.Context, receiptID id.ID) (ReceiptDetail, error) {
	detail, err := j.receipts.Detail(ctx, receiptID)
	if err != nil {
		return detail, err
	}
	for i := range detail.Attributions {
		a := &detail.Attributions[i]
		if a.DocumentType != crypto_payment.Definition.Name {
			continue
		}
		gross, err := types.NewRawAmountFromString(a.Amount)
		if err != nil {
			return detail, fmt.Errorf("decode payment amount: %w", err)
		}
		fee, net := tariff.Fee{Fixed: a.FeeFixed, PercentBP: a.FeePercentBP, Min: a.FeeMin, Max: a.FeeMax}.Split(gross)
		a.ProcessingFee, a.NetAmount = fee.String(), net.String()
	}
	return detail, nil
}

// WalletFundings lists recorded external fundings of platform wallets, newest first.
func (j *ReceiptJournal) WalletFundings(ctx context.Context, limit, offset int) (WalletFundingList, error) {
	if err := validJournalPage(limit, offset); err != nil {
		return WalletFundingList{}, err
	}
	return j.fundings.List(ctx, limit, offset)
}
