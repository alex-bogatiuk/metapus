package outboundapp

import (
	"context"
	"fmt"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"strings"
	"time"
)

func observedSavedOutflow(ctx context.Context, driver crypto.NetworkDriver, record SigningAuthorizationRecord) bool {
	if record.SignedTx == nil || record.SignedTx.TxHash == "" || record.TxID != record.SignedTx.TxHash || driver.Finality() == nil {
		return false
	}
	receipt, err := driver.Finality().Receipt(ctx, record.SignedTx.TxHash)
	return err == nil && receipt.NetworkID == record.Authorization.NetworkID && receipt.TxHash == record.SignedTx.TxHash && receipt.FinanciallyFinal()
}

func reclaimUnsignedSequence(ctx context.Context, driver crypto.NetworkDriver, intent crypto.TransferIntent) {
	if reclaimer, ok := driver.(crypto.UnsignedSequenceReclaimer); ok {
		_ = reclaimer.ReclaimUnsignedSequence(ctx, intent)
	}
}

func trackableAmbiguousBroadcast(_ crypto.NetworkRef, outcome crypto.BroadcastOutcome, signedTxHash string) bool {
	return outcome.Status == crypto.BroadcastAmbiguous &&
		strings.TrimSpace(outcome.TxHash) != "" && strings.EqualFold(outcome.TxHash, signedTxHash)
}

func resolveResourcePreparer(resolver ResourcePreparerResolver, family crypto.ChainFamily) ResourcePreparer {
	if resolver == nil {
		return nil
	}
	return resolver.Resolve(family)
}

func releasePreparedResource(ctx context.Context, preparer ResourcePreparer, allocationID id.ID, reason string) error {
	if id.IsNil(allocationID) {
		return nil
	}
	if preparer == nil {
		return fmt.Errorf("resource allocation exists without a configured resource preparer")
	}
	return preparer.ReleaseResource(ctx, allocationID, reason)
}

func markPreparedResourceBroadcast(ctx context.Context, preparer ResourcePreparer, command ResourceBroadcastCommand) error {
	if preparer == nil {
		if id.IsNil(command.AllocationID) {
			return nil
		}
		return fmt.Errorf("resource allocation exists without a configured resource preparer")
	}
	return preparer.MarkResourceBroadcast(ctx, command)
}

func ExpiredUnbroadcastSigningAttempt(record SigningAuthorizationRecord, now time.Time) bool {
	if record.Status != "created" && record.Status != "held" {
		return false
	}
	return record.SignedTx == nil && strings.TrimSpace(record.TxID) == "" &&
		!record.Authorization.ExpiresAt.IsZero() && !now.Before(record.Authorization.ExpiresAt)
}

// FailedUnsignedSigningAttempt reports an attempt whose authorization failed
// before any transaction was signed. Its immutable identity cannot be signed
// again, so the attempt must be retired rather than retried.
func FailedUnsignedSigningAttempt(record SigningAuthorizationRecord) bool {
	return record.Status == "failed" && record.SignedTx == nil && strings.TrimSpace(record.TxID) == ""
}

func ValidateSignedEnvelope(auth crypto.SigningAuthorization, unsigned crypto.UnsignedTxEnvelope, signed crypto.SignedTxEnvelope) error {
	if signed.Family != auth.ChainFamily || signed.Family != unsigned.Family ||
		signed.SchemaVersion != unsigned.SchemaVersion ||
		signed.UnsignedFingerprint != auth.UnsignedFingerprint ||
		signed.UnsignedFingerprint != unsigned.UnsignedFingerprint ||
		signed.Generation != auth.Generation || signed.Generation != unsigned.Generation ||
		signed.NetworkSequence != auth.NetworkSequence || signed.NetworkSequence != unsigned.NetworkSequence ||
		strings.TrimSpace(signed.TxHash) == "" || len(signed.Payload) == 0 {
		return fmt.Errorf("signed envelope identity does not match authorization")
	}
	return nil
}
