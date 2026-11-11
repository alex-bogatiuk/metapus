package crypto

import (
	"strings"
	"testing"

	"metapus/internal/core/types"

	"github.com/stretchr/testify/require"
)

func TestTransferEvidenceKeepsOrdinaryEconomicsAndBindsInternalTrace(t *testing.T) {
	event := BlockchainEvent{TxHash: strings.Repeat("a", 64), EventKind: EventKindNativeValue, FromAddress: "from", ToAddress: "to", Amount: types.NewCryptoAmountFromInt64(23)}
	original := event.EconomicsHash()
	event.DecoderVersion = 2
	require.Equal(t, original, event.EconomicsHash())
	event.EventKind = EventKindContractLog
	event.DecoderVersion = 0
	original = event.EconomicsHash()
	event.DecoderVersion = 2
	require.Equal(t, original, event.EconomicsHash())
	event.EventKind = EventKindInternalValue
	event.TraceHash = strings.Repeat("a", 64)
	index := int64(0)
	event.CallValueIndex = &index
	original = event.EconomicsHash()
	rawPosition := int64(99)
	event.TraceIndex = &rawPosition
	require.Equal(t, original, event.EconomicsHash(), "source position is audit-only")
	event.TraceHash = strings.Repeat("b", 64)
	require.NotEqual(t, original, event.EconomicsHash(), "same amount and scalar index cannot alias another trace")
}

func TestTransferEvidenceSchemaForFixesSchemaByFamily(t *testing.T) {
	schema, err := TransferEvidenceSchemaFor(ChainFamilyTRON)
	require.NoError(t, err)
	require.Equal(t, TRONTransferEvidenceSchema, schema)
	schema, err = TransferEvidenceSchemaFor(ChainFamilyEVM)
	require.NoError(t, err)
	require.Equal(t, TransferEvidenceSchemaV1, schema)
	for _, family := range []ChainFamily{ChainFamilyTON, ChainFamilySolana, "", "Tron"} {
		_, err = TransferEvidenceSchemaFor(family)
		require.ErrorIs(t, err, ErrUnsupportedEvidenceFamily, family)
	}
}
