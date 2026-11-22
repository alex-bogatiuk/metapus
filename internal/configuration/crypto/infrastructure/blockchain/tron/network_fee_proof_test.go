package tron

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/id"
)

func TestNetworkFeeProofUsesAgreedTRONReceiptAndOwner(t *testing.T) {
	for _, mode := range []string{"success", "reverted", "zero", "residual", "mismatch", "negative", "paused"} {
		t.Run(mode, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, false)
			other, otherReceipts := blockFixture(t, 10, false)
			for _, list := range [][]TransactionInfo{receipts, otherReceipts} {
				list[0].Fee = 7
				list[0].Receipt.EnergyFee = 2
				list[0].Receipt.NetFee = 5
			}
			if mode == "zero" {
				for _, list := range [][]TransactionInfo{receipts, otherReceipts} {
					list[0].Fee = 0
					list[0].Receipt.EnergyFee = 0
					list[0].Receipt.NetFee = 0
				}
			}
			if mode == "residual" {
				receipts[0].Fee = 9
				otherReceipts[0].Fee = 9
			}
			if mode == "mismatch" {
				otherReceipts[0].Fee = 8
			}
			if mode == "negative" {
				receipts[0].Fee = -1
				otherReceipts[0].Fee = -1
			}
			if mode == "reverted" {
				block.Transactions[0].Ret[0].ContractRet = "REVERT"
				other.Transactions[0].Ret[0].ContractRet = "REVERT"
				receipts[0].Result = "FAILED"
				otherReceipts[0].Result = "FAILED"
				receipts[0].Receipt.Result = "REVERT"
				otherReceipts[0].Receipt.Result = "REVERT"
			}
			var calls atomic.Int32
			a := rpcBlockServer(t, block, receipts, "", &calls)
			b := rpcBlockServer(t, other, otherReceipts, "", &calls)
			defer a.Close()
			defer b.Close()
			pool := newTestRPCPool(t, []string{a.URL, b.URL}, []string{a.URL, b.URL})
			pool.settings.IngestionMode = "hybrid"
			pool.settings.RequestsPerSecond = 1000
			if mode == "paused" {
				pool.settings.IngestionMode = "paused"
			}
			for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
				for i, e := range pool.byRole[role] {
					e.trustGroup = fmt.Sprint(i)
				}
			}
			driver := Driver{network: crypto.NetworkRef{ID: id.New(), Family: crypto.ChainFamilyTRON, NativeTokenID: id.New(), NativeDecimals: 6, RequiredConfirmations: 19}, rpc: pool}
			receipt, err := driver.Receipt(t.Context(), block.Transactions[0].TxID)
			if mode == "mismatch" || mode == "negative" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, receipt.FeeProof)
			require.NoError(t, receipt.FeeProof.ValidateReceipt(receipt))
			require.Equal(t, NormalizeTronAddress(block.Transactions[0].RawData.Contract[0].Parameter.Value.OwnerAddress), receipt.FeeProof.PayerKey())
			total := "7"
			if mode == "zero" {
				total = "0"
			}
			if mode == "residual" {
				total = "9"
			}
			require.Equal(t, total, receipt.Fee.TotalFee.String())
			if mode == "reverted" {
				require.Equal(t, crypto.ReceiptReverted, receipt.Status)
			}
		})
	}
}
