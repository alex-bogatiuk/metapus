package crypto_worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

type custodyObservationDriver struct {
	crypto.NetworkDriver
	reader crypto.FinalityReader
}

func (d custodyObservationDriver) Finality() crypto.FinalityReader { return d.reader }

type custodyObservationReader struct {
	crypto.FinalityReader
	receipt crypto.ChainReceipt
	err     error
}

func (r custodyObservationReader) Receipt(context.Context, string) (crypto.ChainReceipt, error) {
	return r.receipt, r.err
}

func TestCustodyObservationRequiresMatchingFinancialFinality(t *testing.T) {
	network := id.New()
	record := SigningAuthorizationRecord{Authorization: crypto.SigningAuthorization{NetworkID: network}, TxID: "saved-hash", SignedTx: &crypto.SignedTxEnvelope{TxHash: "saved-hash"}}
	valid := crypto.ChainReceipt{NetworkID: network, TxHash: "saved-hash", Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized}
	for _, tc := range []struct {
		name     string
		receipt  crypto.ChainReceipt
		err      error
		observed bool
	}{
		{name: "final", receipt: valid, observed: true},
		{name: "RPC failure", receipt: valid, err: errors.New("unavailable")},
		{name: "pending", receipt: crypto.ChainReceipt{NetworkID: network, TxHash: record.TxID, Status: crypto.ReceiptPending}},
		{name: "wrong network", receipt: crypto.ChainReceipt{NetworkID: id.New(), TxHash: record.TxID, Status: valid.Status, Canonicality: valid.Canonicality, Finality: valid.Finality}},
		{name: "wrong hash", receipt: crypto.ChainReceipt{NetworkID: network, TxHash: "foreign-hash", Status: valid.Status, Canonicality: valid.Canonicality, Finality: valid.Finality}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver := custodyObservationDriver{reader: custodyObservationReader{receipt: tc.receipt, err: tc.err}}
			require.Equal(t, tc.observed, observedSavedOutflow(t.Context(), driver, record))
		})
	}
}
