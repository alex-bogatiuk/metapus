package crypto_repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

type ExpectedTransferRepo struct{}

func NewExpectedTransferRepo() *ExpectedTransferRepo { return &ExpectedTransferRepo{} }

type expectedTransferCandidate struct {
	expected, owner id.ID
	ownerType       string
	valid           bool
}

// RouteExpectedTransfer records handoff intent without taking wallet, balance,
// effect or business-document locks. Financial ownership stays with settlement.
func (*ExpectedTransferRepo) RouteExpectedTransfer(ctx context.Context, event crypto.BlockchainEvent) (bool, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return false, apperror.NewValidation("expected transfer routing requires a transaction")
	}
	if id.IsNil(event.NetworkID) || event.TxHash == "" || event.FromAddress == "" || event.ToAddress == "" || !event.Amount.IsPositive() {
		return false, nil
	}
	event.EventKind = event.EffectiveEventKind()
	q := tm.GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT expected_id,owner_type,owner_id,intent_valid FROM fn_crypto_expected_transfer_candidates($1,$2,$3,$4,$5,$6,$7) ORDER BY expected_id`, event.NetworkID, event.TxHash, event.EventKind, event.FromAddress, event.ToAddress, event.TokenContract, event.Amount)
	if err != nil {
		return false, fmt.Errorf("resolve expected transfer owner: %w", err)
	}
	var candidates []expectedTransferCandidate
	for rows.Next() {
		var c expectedTransferCandidate
		if err := rows.Scan(&c.expected, &c.ownerType, &c.owner, &c.valid); err != nil {
			rows.Close()
			return false, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		return false, nil
	}
	var proof crypto.TransferVerification
	var requiredConfirmations int
	if event.VerificationID != nil {
		var raw []byte
		if err := q.QueryRow(ctx, `SELECT evidence FROM reg_crypto_transfer_verifications WHERE id=$1`, *event.VerificationID).Scan(&raw); err != nil {
			return false, err
		}
		if err := json.Unmarshal(raw, &proof); err != nil {
			return false, err
		}
		if err := proof.Validate(); err != nil {
			return false, err
		}
		var family crypto.ChainFamily
		if err := q.QueryRow(ctx, `SELECT chain_family,confirmations_needed FROM cat_blockchain_networks WHERE id=$1`, event.NetworkID).Scan(&family, &requiredConfirmations); err != nil {
			return false, err
		}
		if !sameExpectedObservation(event, proof.Event, family) {
			return false, crypto.ErrVerificationMismatch
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	for _, c := range candidates {
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_expected_transfer_requests(expected_effect_id,owner_type,owner_id,first_event) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(expected_effect_id) DO NOTHING`, c.expected, c.ownerType, c.owner, string(raw))
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			if err := postgres.NewOutboxPublisher().Publish(ctx, domain.DomainEvent{AggregateType: "CryptoExpectedTransfer", AggregateID: c.expected, EventType: "settlement_requested", Payload: map[string]any{"entityType": "CryptoExpectedTransfer", "entityId": c.expected, "action": "settlement_requested", "expectedEffectId": c.expected, "ownerType": c.ownerType, "ownerId": c.owner}}); err != nil {
				return false, err
			}
		}
	}
	// Ambiguous or historical coverage remains pending, with no guessed ordinal.
	if len(candidates) != 1 || !candidates[0].valid || event.VerificationID == nil || proof.AccountingIdentity == nil || proof.MatchingEffects != 1 || proof.Event.Canonicality != crypto.CanonicalityCanonical || proof.Event.Confirmations < requiredConfirmations {
		return true, nil
	}
	c := candidates[0]
	var effect id.ID
	if err := q.QueryRow(ctx, `SELECT fn_crypto_validate_expected_route($1,$2)`, c.expected, *event.VerificationID).Scan(&effect); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_expected_transfer_routes(effect_id,expected_effect_id,first_verification_id,owner_type,owner_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, effect, c.expected, *event.VerificationID, c.ownerType, c.owner); err != nil {
		return false, err
	}
	var matches bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_routes WHERE effect_id=$1 AND expected_effect_id=$2 AND owner_type=$3 AND owner_id=$4)`, effect, c.expected, c.ownerType, c.owner).Scan(&matches); err != nil {
		return false, err
	}
	if !matches {
		return false, apperror.NewConflict("expected transfer already has another logical route")
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_expected_transfer_evidence(verification_id,effect_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, *event.VerificationID, effect); err != nil {
		return false, err
	}
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_evidence WHERE verification_id=$1 AND effect_id=$2)`, *event.VerificationID, effect).Scan(&matches); err != nil {
		return false, err
	}
	if !matches {
		return false, apperror.NewConflict("expected transfer proof cannot change its route")
	}
	return true, nil
}

func sameExpectedObservation(a, b crypto.BlockchainEvent, family crypto.ChainFamily) bool {
	hash := func(s string) string { return strings.TrimPrefix(strings.ToLower(s), "0x") }
	address := func(s string) string {
		if family == crypto.ChainFamilyEVM {
			return strings.ToLower(s)
		}
		return s
	}
	return a.NetworkID == b.NetworkID && hash(a.TxHash) == hash(b.TxHash) && a.EffectiveEventKind() == b.EffectiveEventKind() && a.EventIndex == b.EventIndex && a.BlockNumber == b.BlockNumber && hash(a.BlockHash) == hash(b.BlockHash) && a.Amount.Equal(b.Amount) && address(a.FromAddress) == address(b.FromAddress) && address(a.ToAddress) == address(b.ToAddress) && address(a.TokenContract) == address(b.TokenContract) && a.Canonicality == b.Canonicality
}

var _ crypto.ExpectedTransferRouter = (*ExpectedTransferRepo)(nil)
