package tron

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

func TestExpectedTransferLocatorRequiresUniqueEconomics(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		block, receipts := blockFixture(t, 10, true)
		log := TransactionLog{Address: testTokenHex, Topics: []string{transferTopic, strings.Repeat("0", 24) + testFromHex[2:], strings.Repeat("0", 24) + testToHex[2:]}, Data: fmt.Sprintf("%064x", 100)}
		receipts[0].Log = []TransactionLog{{Topics: []string{"other"}}, log}
		if duplicate {
			receipts[0].Log = append(receipts[0].Log, log)
		}
		data, err := assembleBlock(block, receipts)
		require.NoError(t, err)
		d := Driver{network: crypto.NetworkRef{ID: id.New()}}
		events, err := decodeTransfers(d.network, data, crypto.FinalitySolidified, 21)
		require.NoError(t, err)
		expected := events[1]
		expected.EventIndex = -1
		sources := []crypto.VerificationSource{
			{ProviderID: "one", TrustGroup: "one", LatestProviderID: "one", Digest: data.Digest, SourceDigest: data.SourceDigest, DecoderVersion: 2, HeadHeight: 30, HeadHash: "head"},
			{ProviderID: "two", TrustGroup: "two", LatestProviderID: "two", Digest: data.Digest, SourceDigest: data.SourceDigest, DecoderVersion: 2, HeadHeight: 30, HeadHash: "head"},
		}
		proof, found, err := d.matchVerifiedTransfer(expected, data, sources)
		if duplicate {
			require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
			require.False(t, found)
			continue
		}
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(1), proof.Event.EventIndex)
		require.NotNil(t, proof.AccountingIdentity)
		require.NoError(t, proof.Validate())
	}
}
