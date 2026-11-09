package crypto_worker

import (
	"strings"

	"metapus/internal/domain/crypto"
)

// trackableAmbiguousBroadcast identifies the one ambiguous outcome that can
// safely enter ordinary confirmation/replacement reconciliation: the driver
// returned the hash of the exact signed bytes already persisted for this
// business attempt. Families that do not echo a hash keep the hold policy.
func trackableAmbiguousBroadcast(_ crypto.NetworkRef, outcome crypto.BroadcastOutcome, signedTxHash string) bool {
	return outcome.Status == crypto.BroadcastAmbiguous &&
		strings.TrimSpace(outcome.TxHash) != "" && strings.EqualFold(outcome.TxHash, signedTxHash)
}
