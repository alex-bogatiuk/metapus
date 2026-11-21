package evm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

// Only the Ethereum execution/blob receipt profile is admitted. Unknown fee
// fields (including L2 surcharges) require a dedicated supported fee profile.
func decodeFeeReceipt(raw json.RawMessage) (*gethtypes.Receipt, error) {
	if string(raw) == "null" || len(raw) == 0 {
		return nil, ethereum.NotFound
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for key := range fields {
		switch key {
		case "type", "root", "status", "cumulativeGasUsed", "logsBloom", "logs", "transactionHash", "contractAddress", "gasUsed", "effectiveGasPrice", "blobGasUsed", "blobGasPrice", "blockHash", "blockNumber", "transactionIndex", "from", "to":
		default:
			return nil, crypto.ErrVerificationMismatch
		}
	}
	if fields["status"] == nil || fields["effectiveGasPrice"] == nil {
		return nil, crypto.ErrVerificationMismatch
	}
	var status string
	if json.Unmarshal(fields["status"], &status) != nil || (status != "0x0" && status != "0x1") {
		return nil, crypto.ErrVerificationMismatch
	}
	var receipt gethtypes.Receipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (d *TransferVerifier) VerifyNetworkFee(ctx context.Context, hash string) (crypto.ChainReceipt, error) {
	empty := crypto.ChainReceipt{NetworkID: d.network.ID, TxHash: hash, Status: crypto.ReceiptPending, Finality: crypto.FinalityProvisional, Canonicality: crypto.CanonicalityUnknown, ObservedAt: time.Now().UTC()}
	if d.rpc == nil || !common.IsHexHash(hash) {
		return empty, crypto.ErrVerificationMismatch
	}
	// A standard-looking receipt is not evidence that a different EVM chain
	// has no additional fee. Extend this list only with a verified fee profile.
	if d.network.NetworkIdentifier != "1" && d.network.NetworkIdentifier != "11155111" {
		return empty, apperror.NewValidation("independent network fee profile is unavailable for this chain")
	}
	gen, release, err := d.rpc.runtime.Acquire(ctx)
	if err != nil {
		return empty, err
	}
	defer release()
	groups := map[string]bool{}
	providers := map[string]bool{}
	for e := range votingEndpoints(gen.endpoints) {
		if !providers[e.providerID] {
			providers[e.providerID] = true
			groups[e.trustGroup] = true
		}
	}
	if len(providers) < 2 || len(groups) < 2 {
		return empty, crypto.ErrChainQuorumUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, gen.settings.ReadBudget())
	defer cancel()
	chain, ok := new(big.Int).SetString(d.network.NetworkIdentifier, 10)
	if !ok || chain.Sign() <= 0 {
		return empty, crypto.ErrVerificationMismatch
	}
	var first *gethtypes.Header
	var result crypto.ChainReceipt
	var proof crypto.NetworkFeeProof
	failures := []error{}
	for endpoint := range votingEndpoints(gen.readOrder(opReceipt)) {
		if ctx.Err() != nil {
			break
		}
		if len(proof.Sources) > 0 && (endpoint.providerID == proof.Sources[0].ProviderID || endpoint.trustGroup == proof.Sources[0].TrustGroup) {
			continue
		}
		if !endpoint.admission.CanCompleteSequence(ctx, 7) {
			continue
		}
		var sourceDigest string
		observed, head, err := func() (crypto.ChainReceipt, *gethtypes.Header, error) {
			out := empty
			height := (*big.Int)(nil)
			if d.network.FinalityMode == crypto.FinalityByProtocolFinalized {
				height = finalizedBlockNumber()
			}
			current, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, c *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, c, height)
			})
			if err != nil {
				return out, nil, err
			}
			head := current
			if first != nil {
				if current.Number.Cmp(first.Number) < 0 {
					return out, nil, crypto.ErrVerificationPending
				}
				head, err = proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, c *ethclient.Client) (*gethtypes.Header, error) {
					return gen.readHeader(ctx, c, first.Number)
				})
				if err != nil {
					return out, nil, err
				}
				if head.Hash() != first.Hash() {
					return out, nil, crypto.ErrVerificationMismatch
				}
			}
			raw, err := proofEndpointRead(ctx, gen, endpoint, opReceipt, func(ctx context.Context, c *ethclient.Client) (json.RawMessage, error) {
				var raw json.RawMessage
				err := c.Client().CallContext(ctx, &raw, "eth_getTransactionReceipt", hash)
				return raw, err
			})
			if err != nil {
				return out, nil, err
			}
			r, err := decodeFeeReceipt(raw)
			if err != nil {
				return out, nil, err
			}
			if r.TxHash != common.HexToHash(hash) || r.BlockNumber == nil || !r.BlockNumber.IsInt64() || r.BlockNumber.Sign() < 0 || r.GasUsed == 0 || r.GasUsed > math.MaxInt64 || r.EffectiveGasPrice == nil || r.EffectiveGasPrice.Sign() < 0 || r.Status > 1 {
				return out, nil, crypto.ErrVerificationMismatch
			}
			block, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, c *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, c, r.BlockNumber)
			})
			if err != nil {
				return out, nil, err
			}
			if block.Hash() != r.BlockHash {
				return out, nil, crypto.ErrVerificationMismatch
			}
			confirmations := head.Number.Int64() - r.BlockNumber.Int64() + 1
			if confirmations < int64(max(1, d.network.RequiredConfirmations)) {
				return out, nil, crypto.ErrVerificationPending
			}
			txRaw, err := proofEndpointRead(ctx, gen, endpoint, opReceipt, func(ctx context.Context, c *ethclient.Client) (json.RawMessage, error) {
				var raw json.RawMessage
				err := c.Client().CallContext(ctx, &raw, "eth_getTransactionByHash", hash)
				return raw, err
			})
			if err != nil {
				return out, nil, err
			}
			var tx gethtypes.Transaction
			if err := json.Unmarshal(txRaw, &tx); err != nil {
				return out, nil, err
			}
			var mined struct {
				BlockHash   *common.Hash    `json:"blockHash"`
				BlockNumber *hexutil.Big    `json:"blockNumber"`
				Index       *hexutil.Uint64 `json:"transactionIndex"`
			}
			if err := json.Unmarshal(txRaw, &mined); err != nil {
				return out, nil, err
			}
			if mined.BlockHash == nil || *mined.BlockHash != r.BlockHash || mined.BlockNumber == nil || (*big.Int)(mined.BlockNumber).Cmp(r.BlockNumber) != 0 || mined.Index == nil || uint64(*mined.Index) != uint64(r.TransactionIndex) || tx.Hash() != r.TxHash || r.Type != tx.Type() || tx.Type() > gethtypes.SetCodeTxType || r.GasUsed > tx.Gas() || (tx.Protected() && tx.ChainId().Cmp(chain) != 0) || r.EffectiveGasPrice.Cmp(tx.GasFeeCap()) > 0 {
				return out, nil, crypto.ErrVerificationMismatch
			}
			if r.CumulativeGasUsed < r.GasUsed || r.CumulativeGasUsed > block.GasUsed || tx.Gas() > block.GasLimit {
				return out, nil, crypto.ErrVerificationMismatch
			}
			expectedPrice := tx.GasPrice()
			if tx.Type() >= gethtypes.DynamicFeeTxType {
				if block.BaseFee == nil || block.BaseFee.Sign() < 0 || tx.GasFeeCap().Cmp(block.BaseFee) < 0 {
					return out, nil, crypto.ErrVerificationMismatch
				}
				expectedPrice = new(big.Int).Add(block.BaseFee, tx.GasTipCap())
				if expectedPrice.Cmp(tx.GasFeeCap()) > 0 {
					expectedPrice = tx.GasFeeCap()
				}
			}
			if r.EffectiveGasPrice.Cmp(expectedPrice) != 0 {
				return out, nil, crypto.ErrVerificationMismatch
			}
			payer, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(chain), &tx)
			if err != nil {
				return out, nil, err
			}
			var parties map[string]json.RawMessage
			if err := json.Unmarshal(raw, &parties); err != nil {
				return out, nil, err
			}
			if value, ok := parties["from"]; ok {
				var sender string
				if json.Unmarshal(value, &sender) != nil || !common.IsHexAddress(sender) || common.HexToAddress(sender) != payer {
					return out, nil, crypto.ErrVerificationMismatch
				}
			}
			if value, ok := parties["to"]; ok {
				if tx.To() == nil {
					if string(value) != "null" {
						return out, nil, crypto.ErrVerificationMismatch
					}
				} else {
					var recipient string
					if json.Unmarshal(value, &recipient) != nil || !common.IsHexAddress(recipient) || common.HexToAddress(recipient) != *tx.To() {
						return out, nil, crypto.ErrVerificationMismatch
					}
				}
			}
			amount, err := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(r.GasUsed), r.EffectiveGasPrice))
			if err != nil {
				return out, nil, err
			}
			out.Status = crypto.ReceiptSucceeded
			if r.Status == 0 {
				out.Status = crypto.ReceiptReverted
			}
			out.TxHash, out.BlockHash, out.BlockHeight, out.Confirmations = r.TxHash.Hex(), r.BlockHash.Hex(), r.BlockNumber.Int64(), int(confirmations)
			out.Finality, out.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityCanonical
			if d.network.FinalityMode == crypto.FinalityByProtocolFinalized {
				out.Finality = crypto.FinalityFinalized
			}
			out.RawReceipt = raw
			out.Fee = crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: d.network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: d.network.NativeDecimals}, TotalFee: amount, Components: []crypto.FeeComponent{{Name: "execution_gas", Amount: amount, Quantity: int64(r.GasUsed)}}}
			if tx.Type() == gethtypes.BlobTxType {
				if r.BlobGasUsed == 0 || r.BlobGasUsed != tx.BlobGas() || r.BlobGasUsed > math.MaxInt64 || r.BlobGasPrice == nil || r.BlobGasPrice.Sign() < 0 || r.BlobGasPrice.Cmp(tx.BlobGasFeeCap()) > 0 {
					return out, nil, crypto.ErrVerificationMismatch
				}
				blob, err := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(r.BlobGasUsed), r.BlobGasPrice))
				if err != nil {
					return out, nil, err
				}
				out.Fee.TotalFee = out.Fee.TotalFee.Add(blob)
				out.Fee.Components = append(out.Fee.Components, crypto.FeeComponent{Name: "blob_gas", Amount: blob, Quantity: int64(r.BlobGasUsed)})
			} else if r.BlobGasUsed != 0 || r.BlobGasPrice != nil {
				return out, nil, crypto.ErrVerificationMismatch
			}
			after, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, c *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, c, r.BlockNumber)
			})
			if err != nil {
				return out, nil, err
			}
			if after.Hash() != block.Hash() {
				return out, nil, crypto.ErrVerificationMismatch
			}
			digest := sha256.Sum256(raw)
			sourceDigest = hex.EncodeToString(digest[:])
			out.FeeProof = &crypto.NetworkFeeProof{SchemaVersion: crypto.NetworkFeeProofSchemaV1, Family: crypto.ChainFamilyEVM, NetworkID: d.network.ID, TxHash: out.TxHash, PayerAddress: strings.ToLower(payer.Hex()), NativeTokenID: d.network.NativeTokenID,
				BlockHash: out.BlockHash, BlockNumber: out.BlockHeight, Status: out.Status, TotalFee: out.Fee.TotalFee, Components: out.Fee.Components, Confirmations: out.Confirmations, Finality: out.Finality, Canonicality: out.Canonicality, VerifiedAt: out.ObservedAt}
			return out, head, nil
		}()
		if err != nil {
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				return empty, err
			}
			failures = append(failures, err)
			continue
		}
		if first == nil {
			first = head
			result = observed
			proof = *observed.FeeProof
		} else if proof.Digest() != observed.FeeProof.Digest() {
			return empty, crypto.ErrVerificationMismatch
		}
		proof.Sources = append(proof.Sources, crypto.VerificationSource{ProviderID: endpoint.providerID, LatestProviderID: endpoint.providerID, TrustGroup: endpoint.trustGroup, Digest: proof.Digest(), SourceDigest: sourceDigest, HeadHash: head.Hash().Hex(), HeadHeight: head.Number.Int64()})
		if len(proof.Sources) == 2 {
			result.FeeProof = &proof
			return result, proof.ValidateReceipt(result)
		}
	}
	return empty, errors.Join(crypto.ErrChainQuorumUnavailable, errors.Join(failures...))
}
