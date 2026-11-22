package evm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/types"
)

func (d *Driver) VerificationRequired() bool { return d.rpc != nil }

// TransferVerifier is the read-only evidence path shared by processing and
// wallet-funding tools. It requires no signer, wallet deriver or nonce manager.
type TransferVerifier struct {
	network crypto.NetworkRef
	rpc     *RPCPool
}

func (*TransferVerifier) VerificationRequired() bool { return true }

func NewTransferVerifier(network crypto.NetworkRef, rpc *RPCPool) (*TransferVerifier, error) {
	if err := network.Validate(); err != nil {
		return nil, err
	}
	if network.Family != crypto.ChainFamilyEVM || rpc == nil {
		return nil, errors.New("EVM evidence verifier requires an EVM network and RPC pool")
	}
	return &TransferVerifier{network: network, rpc: rpc}, nil
}

func (d *Driver) VerifyTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	return (&TransferVerifier{network: d.network, rpc: d.rpc}).VerifyTransfer(ctx, expected)
}

// VerifyTransfer binds successful receipt economics to two independent providers
// on one common canonical head. A direct native transfer proves complete native
// coverage only when its recipient has no executable code at that block.
func (d *TransferVerifier) VerifyTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	return d.verifyTransfer(ctx, expected, false)
}

func (d *Driver) VerifyExpectedTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	return (&TransferVerifier{network: d.network, rpc: d.rpc}).verifyTransfer(ctx, expected, true)
}

func (d *TransferVerifier) VerifyExpectedTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	return d.verifyTransfer(ctx, expected, true)
}

func (d *TransferVerifier) verifyTransfer(ctx context.Context, expected crypto.BlockchainEvent, locate bool) (crypto.TransferVerification, error) {
	var proof crypto.TransferVerification
	if d.rpc == nil || expected.NetworkID != d.network.ID || expected.BlockNumber < 0 || !expected.Amount.IsPositive() || (expected.EffectiveEventKind() != crypto.EventKindNativeValue && expected.EffectiveEventKind() != crypto.EventKindContractLog) || len(expected.TxHash) != 66 || !common.IsHexAddress(expected.FromAddress) || !common.IsHexAddress(expected.ToAddress) {
		return proof, crypto.ErrVerificationMismatch
	}
	hashBytes, err := hex.DecodeString(strings.TrimPrefix(expected.TxHash, "0x"))
	if err != nil || len(hashBytes) != 32 {
		return proof, crypto.ErrVerificationMismatch
	}
	gen, release, err := d.rpc.runtime.Acquire(ctx)
	if err != nil {
		return proof, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, gen.settings.ReadBudget())
	defer cancel()
	type observation struct {
		event      crypto.BlockchainEvent
		head       *gethtypes.Header
		digest     string
		matching   int
		accounting crypto.AccountingEffectIdentity
	}
	var first observation
	failures := make([]error, 0, len(gen.endpoints))
	requests := 5 // head, receipt, receipt block, transaction, block recheck
	if expected.EffectiveEventKind() == crypto.EventKindNativeValue {
		requests++ // historical recipient code
	}
	for endpoint := range votingEndpoints(gen.readOrder(opReceipt)) {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		if len(proof.Sources) > 0 && (endpoint.providerID == proof.Sources[0].ProviderID || endpoint.trustGroup == proof.Sources[0].TrustGroup) {
			continue
		}
		if !endpoint.admission.CanCompleteSequence(ctx, requests) {
			failures = append(failures, &RPCReadFailure{Provider: endpoint.providerID, Operation: "verification", Category: "budget_unavailable"})
			continue
		}

		step := "head"
		obs, err := func() (observation, error) {
			var out observation
			headNumber := (*big.Int)(nil)
			if first.head != nil {
				headNumber = first.head.Number
			}
			head, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, client *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, client, headNumber)
			})
			if err != nil {
				return out, err
			}
			step = "receipt"
			receipt, err := proofEndpointRead(ctx, gen, endpoint, opReceipt, func(ctx context.Context, client *ethclient.Client) (*gethtypes.Receipt, error) {
				return client.TransactionReceipt(ctx, common.HexToHash(expected.TxHash))
			})
			if err != nil {
				return out, err
			}
			if receipt.BlockNumber == nil || !receipt.BlockNumber.IsInt64() || receipt.BlockNumber.Sign() < 0 || receipt.TxHash != common.HexToHash(expected.TxHash) || receipt.Status != gethtypes.ReceiptStatusSuccessful {
				return out, crypto.ErrVerificationMismatch
			}
			step = "receipt_block"
			header, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, client *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, client, receipt.BlockNumber)
			})
			if err != nil {
				return out, err
			}
			if header.Hash() != receipt.BlockHash || (expected.BlockNumber > 0 && (header.Number.Int64() != expected.BlockNumber || !strings.EqualFold(header.Hash().Hex(), expected.BlockHash))) {
				return out, crypto.ErrVerificationMismatch
			}
			confirmations := head.Number.Int64() - header.Number.Int64() + 1
			if confirmations < int64(max(1, d.network.RequiredConfirmations)) {
				return out, crypto.ErrVerificationPending
			}
			step = "transaction"
			type locatedTx struct {
				tx      *gethtypes.Transaction
				pending bool
			}
			located, err := proofEndpointRead(ctx, gen, endpoint, opReceipt, func(ctx context.Context, client *ethclient.Client) (locatedTx, error) {
				tx, pending, err := client.TransactionByHash(ctx, receipt.TxHash)
				return locatedTx{tx, pending}, err
			})
			tx, pending := located.tx, located.pending
			if err != nil {
				return out, err
			}
			chain, ok := new(big.Int).SetString(d.network.NetworkIdentifier, 10)
			if pending || tx == nil || tx.Hash() != receipt.TxHash || !ok || (tx.Protected() && tx.ChainId().Cmp(chain) != 0) {
				return out, crypto.ErrVerificationMismatch
			}
			sender, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(chain), tx)
			if err != nil {
				return out, err
			}
			event := expected
			event.EventKind = expected.EffectiveEventKind()
			event.TxHash = tx.Hash().Hex()
			event.FromAddress = common.HexToAddress(expected.FromAddress).Hex()
			event.ToAddress = common.HexToAddress(expected.ToAddress).Hex()
			event.SourceAddress = sender.Hex()
			event.SourceNonce = evmNonce(tx.Nonce())
			event.BlockNumber = header.Number.Int64()
			event.BlockHash = header.Hash().Hex()
			event.Confirmations = int(confirmations)
			event.RequiredConfs = d.network.RequiredConfirmations
			event.Finality = crypto.FinalitySolidified
			event.Canonicality = crypto.CanonicalityCanonical
			event.DecoderVersion = crypto.TransferEvidenceSchemaV1
			event.EventType = crypto.EventTypeTransfer
			event.Timestamp = time.Unix(int64(header.Time), 0).UTC()
			matching := 0
			switch event.EventKind {
			case crypto.EventKindNativeValue:
				if locate {
					event.EventIndex = 0
				}
				if tx.To() == nil || *tx.To() != common.HexToAddress(event.ToAddress) || sender != common.HexToAddress(event.FromAddress) || tx.Value().String() != event.Amount.String() || event.EventIndex != 0 || event.TokenContract != "" {
					return out, crypto.ErrVerificationMismatch
				}
				step = "recipient_code"
				code, err := proofEndpointRead(ctx, gen, endpoint, opState, func(ctx context.Context, client *ethclient.Client) ([]byte, error) {
					return client.CodeAt(ctx, *tx.To(), header.Number)
				})
				if err != nil {
					return out, err
				}
				if len(code) == 0 {
					matching = 1
				}
			case crypto.EventKindContractLog:
				if !common.IsHexAddress(event.TokenContract) {
					return out, crypto.ErrVerificationMismatch
				}
				event.TokenContract = common.HexToAddress(event.TokenContract).Hex()
				matches := 0
				for _, log := range receipt.Logs {
					if log == nil || log.Address != common.HexToAddress(event.TokenContract) || len(log.Topics) != 3 || log.Topics[0] != erc20ABI.Events["Transfer"].ID {
						continue
					}
					if log.Removed || log.TxHash != tx.Hash() || log.BlockHash != header.Hash() || log.BlockNumber != header.Number.Uint64() || len(log.Data) != 32 {
						return out, crypto.ErrVerificationMismatch
					}
					from, to := common.BytesToAddress(log.Topics[1].Bytes()), common.BytesToAddress(log.Topics[2].Bytes())
					if to == common.HexToAddress(event.ToAddress) {
						matching++
					}
					amount, err := types.NewRawAmountFromBigInt(new(big.Int).SetBytes(log.Data))
					if err != nil {
						return out, err
					}
					if (locate || int64(log.Index) == expected.EventIndex) && from == common.HexToAddress(event.FromAddress) && to == common.HexToAddress(event.ToAddress) && amount.Cmp(event.Amount) == 0 {
						matches++
						event.EventIndex = int64(log.Index)
					}
				}
				if matches != 1 {
					return out, crypto.ErrVerificationMismatch
				}
			}
			step = "block_recheck"
			after, err := proofEndpointRead(ctx, gen, endpoint, opHeader, func(ctx context.Context, client *ethclient.Client) (*gethtypes.Header, error) {
				return gen.readHeader(ctx, client, header.Number)
			})
			if err != nil {
				return out, err
			}
			if after.Hash() != header.Hash() {
				return out, crypto.ErrVerificationMismatch
			}
			raw, err := json.Marshal(receipt)
			if err != nil {
				return out, err
			}
			identity, err := accountingIdentity(event, receipt)
			if err != nil {
				return out, err
			}
			digest := sha256.Sum256(raw)
			return observation{event: event, head: head, digest: hex.EncodeToString(digest[:]), matching: matching, accounting: identity}, nil
		}()
		if err != nil {
			category := rpcErrorCategory(err)
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				category = "transfer_mismatch"
			}
			if errors.Is(err, crypto.ErrVerificationPending) {
				category = "pending"
			}
			failure := fmt.Errorf("verify transfer: %w", &RPCReadFailure{Provider: endpoint.providerID, Operation: step, Category: category})
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				return crypto.TransferVerification{}, errors.Join(crypto.ErrVerificationMismatch, failure)
			}
			failures = append(failures, failure)
			continue
		}
		if first.head == nil {
			first = obs
		} else if first.head.Hash() != obs.head.Hash() || first.digest != obs.digest || first.event.EconomicsHash() != obs.event.EconomicsHash() || first.matching != obs.matching || first.accounting != obs.accounting {
			return crypto.TransferVerification{}, crypto.ErrVerificationMismatch
		}
		proof.Sources = append(proof.Sources, crypto.VerificationSource{ProviderID: endpoint.providerID, LatestProviderID: endpoint.providerID, TrustGroup: endpoint.trustGroup, Digest: obs.digest, HeadHeight: obs.head.Number.Int64(), HeadHash: obs.head.Hash().Hex(), DecoderVersion: crypto.TransferEvidenceSchemaV1})
		if len(proof.Sources) == 2 {
			proof.Event = first.event
			proof.MatchingEffects = first.matching
			proof.DecoderVersion = crypto.TransferEvidenceSchemaV1
			proof.VerifiedAt = time.Now().UTC()
			proof.Outcome = "verified"
			proof.ExecutionStatus = crypto.ReceiptSucceeded
			identity := first.accounting
			proof.AccountingIdentity = &identity
			return proof, proof.Validate()
		}
	}
	return crypto.TransferVerification{}, errors.Join(crypto.ErrChainQuorumUnavailable, errors.Join(failures...))
}

var _ crypto.TransferVerifier = (*Driver)(nil)
var _ crypto.TransferVerifier = (*TransferVerifier)(nil)

// Every wire request takes its own shared quota admission and circuit budget.
func proofEndpointRead[T any](ctx context.Context, gen *rpcGeneration, endpoint *rpcEndpoint, op rpcOperation, call func(context.Context, *ethclient.Client) (T, error)) (T, error) {
	if !gen.beginRead(endpoint, op, time.Now()) {
		var zero T
		return zero, ErrAllRPCProvidersUnavailable
	}
	return endpointRead(ctx, gen, endpoint, op, call)
}
