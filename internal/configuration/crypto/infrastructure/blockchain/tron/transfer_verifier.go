package tron

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

func (d *Driver) VerificationRequired() bool {
	if d.rpc == nil {
		return false
	}
	mode := d.rpc.settings.IngestionMode
	return mode == "solidified" || mode == "hybrid" || mode == "paused"
}

func (d *Driver) VerifyTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	if expected.EventIndex < 0 {
		return crypto.TransferVerification{}, crypto.ErrVerificationMismatch
	}
	return d.verifyTransfer(ctx, expected)
}

func (d *Driver) VerifyExpectedTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	expected.EventIndex = -1
	return d.verifyTransfer(ctx, expected)
}

func (d *Driver) verifyTransfer(ctx context.Context, expected crypto.BlockchainEvent) (crypto.TransferVerification, error) {
	if expected.NetworkID != d.network.ID || !validHash(expected.TxHash) || !expected.Amount.IsPositive() || !expected.EffectiveEventKind().Valid() {
		return crypto.TransferVerification{}, crypto.ErrVerificationMismatch
	}
	expected.TxHash = strings.ToLower(expected.TxHash)
	expected.FromAddress, expected.ToAddress = NormalizeTronAddress(expected.FromAddress), NormalizeTronAddress(expected.ToAddress)
	if expected.TokenContract != "" {
		expected.TokenContract = NormalizeTronAddress(expected.TokenContract)
	}
	height := expected.BlockNumber
	if height <= 0 {
		info, err := d.rpc.GetTransactionInfo(ctx, expected.TxHash)
		if err != nil || info == nil || info.BlockNumber <= 0 {
			return crypto.TransferVerification{}, errors.Join(crypto.ErrVerificationPending, err)
		}
		height = info.BlockNumber
	}
	data, sources, err := d.rpc.ReadAgreedBlock(ctx, height)
	if err != nil {
		return crypto.TransferVerification{}, err
	}
	if expected.EffectiveEventKind() == crypto.EventKindInternalValue && expected.EffectiveEvidenceSchema() != crypto.TRONTransferEvidenceSchema {
		return crypto.TransferVerification{}, crypto.ErrDecoderVersionConflict
	}
	proof, found, err := d.matchVerifiedTransfer(expected, data, sources)
	if err != nil || found {
		return proof, err
	}
	// A locator may suggest a new height after a provisional reorg. It never
	// authorizes money; that block and transfer are read from both sources.
	info, locatorErr := d.rpc.GetTransactionInfo(ctx, expected.TxHash)
	if locatorErr == nil && info != nil && info.BlockNumber > 0 && info.BlockNumber != height {
		moved, movedSources, readErr := d.rpc.ReadAgreedBlock(ctx, info.BlockNumber)
		if readErr != nil {
			return crypto.TransferVerification{}, readErr
		}
		proof, found, err = d.matchVerifiedTransfer(expected, moved, movedSources)
		if err != nil || found {
			return proof, err
		}
	}
	if expected.EventIndex < 0 {
		return crypto.TransferVerification{}, crypto.ErrVerificationPending
	}
	expected.EventKind = expected.EffectiveEventKind()
	expected.DecoderVersion = crypto.TRONTransferEvidenceSchema
	expected.BlockNumber, expected.BlockHash = height, data.Block.BlockID
	expected.Finality, expected.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityOrphaned
	expected.EventType = crypto.EventTypeReorg
	expected.Confirmations = int(sources[0].HeadHeight-height) + 1
	expected.Timestamp = time.UnixMilli(data.Block.BlockHeader.RawData.Timestamp).UTC()
	proof = crypto.TransferVerification{Event: expected, Sources: sources, DecoderVersion: crypto.TRONTransferEvidenceSchema, VerifiedAt: time.Now().UTC(), Outcome: "absent", ExecutionStatus: crypto.ReceiptPending}
	return proof, proof.Validate()
}

func (d *Driver) matchVerifiedTransfer(expected crypto.BlockchainEvent, data completeBlock, sources []crypto.VerificationSource) (crypto.TransferVerification, bool, error) {
	confs := int(sources[0].HeadHeight-data.Block.BlockHeader.RawData.Number) + 1
	events, err := decodeTransfers(d.network, data, crypto.FinalitySolidified, confs)
	if err != nil {
		return crypto.TransferVerification{}, false, err
	}
	var matched *crypto.TransferVerification
	for _, event := range events {
		if event.TxHash != expected.TxHash || event.EventKind != expected.EffectiveEventKind() || (expected.EventIndex >= 0 && event.EventIndex != expected.EventIndex) {
			continue
		}
		basis := expected
		if expected.EventIndex < 0 {
			basis.EventIndex = event.EventIndex
		}
		if event.EconomicsHash() != basis.EconomicsHash() {
			if expected.EventIndex < 0 {
				continue
			}
			return crypto.TransferVerification{}, false, crypto.ErrVerificationMismatch
		}
		if !internalTransferIdentityEqual(event, expected) {
			return crypto.TransferVerification{}, false, crypto.ErrVerificationMismatch
		}
		proof := crypto.TransferVerification{MatchingEffects: crypto.NewAMLCoverageIndex(events, sources, crypto.TRONTransferEvidenceSchema).CoverageFor(event).MatchingEffects(), Event: event, Sources: sources, DecoderVersion: crypto.TRONTransferEvidenceSchema, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
		if err := proof.BindTRONAccountingIdentity(); err != nil {
			return crypto.TransferVerification{}, false, err
		}
		if err := proof.Validate(); err != nil {
			return proof, false, err
		}
		if matched != nil {
			return crypto.TransferVerification{}, false, crypto.ErrVerificationMismatch
		}
		matched = &proof
	}
	if matched != nil {
		return *matched, true, nil
	}
	return crypto.TransferVerification{}, false, nil
}

func (d *Driver) quorumReceipt(ctx context.Context, txHash string) (crypto.ChainReceipt, error) {
	receipt := crypto.ChainReceipt{NetworkID: d.network.ID, TxHash: txHash, Status: crypto.ReceiptPending, Finality: crypto.FinalityProvisional, ObservedAt: time.Now().UTC(),
		Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: d.network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: d.network.NativeDecimals}}}
	info, err := d.rpc.GetTransactionInfo(ctx, txHash)
	if err != nil {
		return receipt, err
	}
	if info == nil || info.BlockNumber <= 0 {
		return receipt, nil
	}
	data, sources, err := d.rpc.ReadAgreedBlock(ctx, info.BlockNumber)
	if err != nil {
		return receipt, err
	}
	for i, tx := range data.Block.Transactions {
		if !strings.EqualFold(tx.TxID, txHash) {
			continue
		}
		info := data.Receipts[i]
		status, err := executionResult(tx, info)
		if err != nil {
			return receipt, err
		}
		receipt.Status, receipt.Finality, receipt.Canonicality = status, crypto.FinalitySolidified, crypto.CanonicalityCanonical
		receipt.BlockHeight, receipt.BlockHash = info.BlockNumber, data.Block.BlockID
		receipt.Confirmations = int(sources[0].HeadHeight-info.BlockNumber) + 1
		receipt.RawReceipt, err = json.Marshal(struct {
			Receipt TransactionInfo             `json:"receipt"`
			Sources []crypto.VerificationSource `json:"sources"`
		}{info, sources})
		if err != nil {
			return receipt, err
		}
		if info.Fee < 0 || info.Receipt.EnergyFee < 0 || info.Receipt.NetFee < 0 || info.Receipt.EnergyUsageTotal < 0 || info.Receipt.NetUsage < 0 || len(tx.RawData.Contract) != 1 {
			return receipt, crypto.ErrVerificationMismatch
		}
		payer := NormalizeTronAddress(tx.RawData.Contract[0].Parameter.Value.OwnerAddress)
		if ValidateTronAddress(payer) != nil {
			return receipt, crypto.ErrVerificationMismatch
		}
		receipt.Fee.TotalFee = types.NewRawAmountFromInt64(info.Fee)
		receipt.Fee.Components = []crypto.FeeComponent{{Name: "energy_fee", Amount: types.NewRawAmountFromInt64(info.Receipt.EnergyFee), Quantity: info.Receipt.EnergyUsageTotal}, {Name: "bandwidth_fee", Amount: types.NewRawAmountFromInt64(info.Receipt.NetFee), Quantity: info.Receipt.NetUsage}}
		known := receipt.Fee.Components[0].Amount.Add(receipt.Fee.Components[1].Amount)
		if known.Cmp(receipt.Fee.TotalFee) > 0 {
			return receipt, crypto.ErrVerificationMismatch
		}
		if known.Cmp(receipt.Fee.TotalFee) < 0 {
			receipt.Fee.Components = append(receipt.Fee.Components, crypto.FeeComponent{Name: "other_chain_fee", Amount: receipt.Fee.TotalFee.Sub(known)})
		}
		if receipt.Confirmations < max(1, d.network.RequiredConfirmations) {
			receipt.Finality = crypto.FinalityProvisional
			return receipt, nil
		}
		proof := crypto.NetworkFeeProof{SchemaVersion: crypto.NetworkFeeProofSchemaV1, Family: crypto.ChainFamilyTRON, NetworkID: receipt.NetworkID, TxHash: receipt.TxHash, PayerAddress: payer, NativeTokenID: d.network.NativeTokenID,
			BlockHash: receipt.BlockHash, BlockNumber: receipt.BlockHeight, Status: receipt.Status, TotalFee: receipt.Fee.TotalFee, Components: receipt.Fee.Components, Confirmations: receipt.Confirmations, Finality: receipt.Finality, Canonicality: receipt.Canonicality, Sources: slices.Clone(sources), VerifiedAt: receipt.ObservedAt}
		for i := range proof.Sources {
			proof.Sources[i].SourceDigest = proof.Sources[i].Digest
			proof.Sources[i].Digest = proof.Digest()
		}
		if err := proof.ValidateReceipt(receipt); err != nil {
			return receipt, err
		}
		receipt.FeeProof = &proof
		return receipt, nil
	}
	return receipt, crypto.ErrVerificationPending
}

func (p *rpcGeneration) AgreedBlockHash(ctx context.Context, height int64) (string, error) {
	return withIndependentPair(ctx, p, chainconfig.RoleSolidity, nil, func(ctx context.Context, pair []*rpcEndpoint) (string, error) {
		blocks, err := pairRead(ctx, pair, func(readCtx context.Context, endpoint *rpcEndpoint) (*BlockInfo, error) {
			head, err := p.endpointHead(readCtx, endpoint)
			if err != nil {
				return nil, err
			}
			if head.BlockHeader.RawData.Number < height {
				return nil, crypto.ErrVerificationPending
			}
			return p.endpointBlock(readCtx, endpoint, height)
		})
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(blocks[0].BlockID, blocks[1].BlockID) {
			return "", crypto.ErrVerificationMismatch
		}
		return strings.ToLower(blocks[0].BlockID), nil
	})
}

var _ crypto.TransferVerifier = (*Driver)(nil)

// Operator funding shares the same exact-transfer verifier as incoming payments.
// The existing funding command accepts one transfer per transaction; ambiguous
// multi-event transactions require explicit matching through the payment inbox.
func (p *rpcGeneration) VerifyIncomingTransfer(ctx context.Context, network id.ID, hash, to, contract string) (crypto.TransferVerification, error) {
	info, err := p.GetTransactionInfo(ctx, hash)
	if err != nil || info == nil || info.BlockNumber <= 0 {
		return crypto.TransferVerification{}, errors.Join(crypto.ErrVerificationPending, err)
	}
	data, sources, err := p.ReadAgreedBlock(ctx, info.BlockNumber)
	if err != nil {
		return crypto.TransferVerification{}, err
	}
	events, err := decodeTransfers(crypto.NetworkRef{ID: network}, data, crypto.FinalitySolidified, int(sources[0].HeadHeight-info.BlockNumber)+1)
	if err != nil {
		return crypto.TransferVerification{}, err
	}
	var matched []crypto.BlockchainEvent
	for _, event := range events {
		if strings.EqualFold(event.TxHash, hash) && event.ToAddress == NormalizeTronAddress(to) && event.TokenContract == NormalizeTronAddress(contract) {
			matched = append(matched, event)
		}
	}
	if len(matched) != 1 {
		return crypto.TransferVerification{}, crypto.ErrVerificationMismatch
	}
	proof := crypto.TransferVerification{MatchingEffects: crypto.NewAMLCoverageIndex(events, sources, crypto.TRONTransferEvidenceSchema).CoverageFor(matched[0]).MatchingEffects(), Event: matched[0], Sources: sources, DecoderVersion: crypto.TRONTransferEvidenceSchema, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
	if err := proof.BindTRONAccountingIdentity(); err != nil {
		return crypto.TransferVerification{}, err
	}
	return proof, proof.Validate()
}
