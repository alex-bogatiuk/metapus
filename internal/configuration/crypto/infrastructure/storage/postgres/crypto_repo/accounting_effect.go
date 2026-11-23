package crypto_repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// indexAccountingEvidence shares SaveVerification's transaction and indexes
// only the persisted proof. A replay of historical evidence without an
// accounting identity remains historical; its bytes are never upgraded here.
// No financial recognition, canonical generation or money movement is created.
func (r *BlockIngestionRepo) indexAccountingEvidence(ctx context.Context, verificationID id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("accounting evidence requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	var raw []byte
	if err := q.QueryRow(ctx, "SELECT evidence FROM reg_crypto_transfer_verifications WHERE id=$1", verificationID).Scan(&raw); err != nil {
		return fmt.Errorf("load accounting verification: %w", err)
	}
	var proof crypto.TransferVerification
	if err := json.Unmarshal(raw, &proof); err != nil {
		return fmt.Errorf("decode accounting verification: %w", err)
	}
	if proof.AccountingIdentity == nil && proof.Event.Canonicality != crypto.CanonicalityOrphaned {
		return nil
	}
	if err := proof.Validate(); err != nil {
		return fmt.Errorf("validate accounting verification: %w", err)
	}
	// ObserveVerification has already locked any AML scope. Serialize supported
	// managed recipients before the logical effect, including catalog aliases.
	keys, err := custodyRecipientKeys(ctx, proof.Event)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := q.Exec(ctx, "SELECT fn_crypto_lock_receipt_balance($1,$2)", key.wallet, key.token); err != nil {
			return err
		}
	}
	if proof.Event.Canonicality == crypto.CanonicalityOrphaned {
		// Absence does not create an occurrence. Lock its known observation
		// aliases so a concurrently posting writer cannot miss committed absence.
		_, err := q.Exec(ctx, `SELECT e.id FROM reg_crypto_accounting_effects e
			WHERE e.network_id=$1 AND e.tx_hash=$2 AND e.event_kind=$3 AND EXISTS(
				SELECT 1 FROM reg_crypto_accounting_effect_occurrences o
				JOIN reg_crypto_accounting_effect_evidence x ON x.occurrence_id=o.id
				JOIN reg_crypto_transfer_verifications v ON v.id=x.verification_id
				WHERE o.effect_id=e.id AND v.event_index=$4 AND v.economics_hash=$5)
			ORDER BY e.id FOR UPDATE OF e`, proof.Event.NetworkID, strings.TrimPrefix(strings.ToLower(proof.Event.TxHash), "0x"),
			proof.Event.EventKind, proof.Event.EventIndex, proof.Event.EconomicsHash())
		return err
	}
	identity := *proof.AccountingIdentity
	key, err := identity.Key()
	if err != nil {
		return err
	}
	occurrence, err := identity.Occurrence(proof.Event)
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_accounting_effects(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash) DO NOTHING
	`, identity.Family, identity.NetworkID, identity.TxHash, identity.EventKind, identity.Ordinal, identity.TraceHash); err != nil {
		return fmt.Errorf("insert accounting identity: %w", err)
	}
	var effectID id.ID
	if err := q.QueryRow(ctx, "SELECT id FROM reg_crypto_accounting_effects WHERE logical_key=$1 FOR UPDATE", key).Scan(&effectID); err != nil {
		return fmt.Errorf("verify accounting identity key: %w", err)
	}
	event := proof.Event
	from, to, contract := event.FromAddress, event.ToAddress, event.TokenContract
	if identity.Family == crypto.ChainFamilyEVM {
		from, to, contract = strings.ToLower(from), strings.ToLower(to), strings.ToLower(contract)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_accounting_effect_occurrences(
			effect_id,block_number,block_hash,economics_hash,source_address,destination_address,contract_address,amount)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(effect_id,block_hash) DO NOTHING
	`, effectID, occurrence.BlockNumber, occurrence.BlockHash, occurrence.EconomicsHash, from, to, contract, event.Amount); err != nil {
		return fmt.Errorf("insert accounting occurrence: %w", err)
	}
	var occurrenceID id.ID
	var matches bool
	if err := q.QueryRow(ctx, `
		SELECT id,block_number=$3 AND economics_hash=$4 AND source_address=$5
			AND destination_address=$6 AND contract_address=$7 AND amount=$8
		FROM reg_crypto_accounting_effect_occurrences WHERE effect_id=$1 AND block_hash=$2
	`, effectID, occurrence.BlockHash, occurrence.BlockNumber, occurrence.EconomicsHash, from, to, contract, event.Amount).Scan(&occurrenceID, &matches); err != nil {
		return fmt.Errorf("verify accounting occurrence: %w", err)
	}
	if !matches {
		return apperror.NewConflict("accounting occurrence evidence conflicts")
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_accounting_effect_evidence(verification_id,occurrence_id)
		VALUES($1,$2) ON CONFLICT(verification_id) DO NOTHING
	`, verificationID, occurrenceID); err != nil {
		return fmt.Errorf("insert accounting evidence link: %w", err)
	}
	var storedOccurrence id.ID
	if err := q.QueryRow(ctx, "SELECT occurrence_id FROM reg_crypto_accounting_effect_evidence WHERE verification_id=$1", verificationID).Scan(&storedOccurrence); err != nil {
		return fmt.Errorf("verify accounting evidence link: %w", err)
	}
	if storedOccurrence != occurrenceID {
		return apperror.NewConflict("accounting evidence cannot be rebound")
	}
	return nil
}
