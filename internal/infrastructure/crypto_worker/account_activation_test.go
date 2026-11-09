package crypto_worker

import (
	"testing"
	"time"

	"metapus/internal/domain/crypto"
)

func TestActivationReceiptRequiresSuccessfulOrRevertedFinalEvidence(t *testing.T) {
	tests := []struct {
		name    string
		receipt crypto.ChainReceipt
		want    bool
	}{
		{name: "pending solidified is not final", receipt: crypto.ChainReceipt{Status: crypto.ReceiptPending, Finality: crypto.FinalitySolidified}},
		{name: "provisional success is not final", receipt: crypto.ChainReceipt{Status: crypto.ReceiptSucceeded, Finality: crypto.FinalityProvisional}},
		{name: "solidified success is final", receipt: crypto.ChainReceipt{Status: crypto.ReceiptSucceeded, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical}, want: true},
		{name: "finalized revert is final", receipt: crypto.ChainReceipt{Status: crypto.ReceiptReverted, Finality: crypto.FinalityFinalized, Canonicality: crypto.CanonicalityCanonical}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.receipt.FinanciallyFinal(); got != tt.want {
				t.Fatalf("ChainReceipt.FinanciallyFinal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpiredUnbroadcastSigningAttempt(t *testing.T) {
	now := time.Now().UTC()
	expired := SigningAuthorizationRecord{
		Status:        "created",
		Authorization: crypto.SigningAuthorization{ExpiresAt: now.Add(-time.Second)},
	}
	if !expiredUnbroadcastSigningAttempt(expired, now) {
		t.Fatal("expired staged authorization must be retired")
	}

	withTransactionIdentity := expired
	withTransactionIdentity.TxID = "known-tx"
	if expiredUnbroadcastSigningAttempt(withTransactionIdentity, now) {
		t.Fatal("authorization with a transaction identity must remain held for reconciliation")
	}

	stillFresh := expired
	stillFresh.Authorization.ExpiresAt = now.Add(time.Minute)
	if expiredUnbroadcastSigningAttempt(stillFresh, now) {
		t.Fatal("fresh staged authorization must remain retryable")
	}
}
