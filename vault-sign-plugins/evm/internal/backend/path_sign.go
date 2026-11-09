package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"vault-plugin-secrets-evm/internal/evm"
	"vault-plugin-secrets-evm/internal/json"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func pathSign(b *evmBackend) []*framework.Path {
	return []*framework.Path{{
		Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/tx/sign",
		Fields: map[string]*framework.FieldSchema{
			"tenant_id":         {Type: framework.TypeString},
			"authorization_jws": {Type: framework.TypeString, Required: true},
			"approval_bundle":   {Type: framework.TypeMap},
			"unsigned_envelope": {Type: framework.TypeMap, Required: true},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{Callback: b.pathSignEVMTransaction},
		},
		HelpSynopsis: "Signs one policy-authorized EIP-1559 transaction",
	}}
}

func (b *evmBackend) pathSignEVMTransaction(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := strings.TrimSpace(data.Get("tenant_id").(string))
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("tenant_id must be a canonical UUID"), nil
	}
	authorizationJWS := data.Get("authorization_jws").(string)
	envelope, err := decodeUnsignedEnvelope(data.Get("unsigned_envelope"))
	if err != nil {
		return logical.ErrorResponse("invalid unsigned_envelope: %v", err), nil
	}
	approval, err := decodeApprovalBundle(data.Get("approval_bundle"))
	if err != nil {
		return logical.ErrorResponse("invalid approval_bundle: %v", err), nil
	}
	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, tenantID)
	if err != nil || cfg.SchemaVersion != tenantSigningConfigSchemaV1 {
		return logical.ErrorResponse("tenant signing configuration is unavailable"), nil
	}
	kid, err := evm.AuthorizationJWSKeyID(authorizationJWS)
	if err != nil {
		return logical.ErrorResponse("invalid authorization: %v", err), nil
	}
	authorizationKey, err := cfg.authorizationPublicKey(tenantID, kid)
	if err != nil {
		return logical.ErrorResponse("invalid authorization: %v", err), nil
	}
	auth, err := evm.VerifyAuthorizationJWS(authorizationJWS, authorizationKey)
	if err != nil || auth.TenantID != tenantID || auth.UnsignedFingerprint != envelope.UnsignedFingerprint {
		return logical.ErrorResponse("authorization does not match tenant or envelope"), nil
	}
	if auth.AssetStandard == evm.AssetStandardERC20 && auth.ContractAddress != evm.SepoliaPYUSDContract {
		return logical.ErrorResponse("ERC-20 contract is not allowlisted"), nil
	}
	if auth.AssetStandard == evm.AssetStandardNative && auth.ContractAddress != "" {
		return logical.ErrorResponse("native authorization must not specify a contract"), nil
	}
	if auth.Purpose != "withdrawal" && auth.Purpose != "treasury_withdrawal" && !approvalBundleEmpty(approval) {
		return logical.ErrorResponse("approval bundle is not allowed for %s", auth.Purpose), nil
	}
	if auth.Purpose == "sweep" {
		if err := b.validateSweepDestination(ctx, req.Storage, tenantID, auth.NetworkIdentifier, auth.DestinationAddress); err != nil {
			return logical.ErrorResponse("sweep policy denied transaction: %v", err), nil
		}
	}
	if auth.Purpose == "native_funding" {
		if err := requireDerivedTenantWallet(ctx, req.Storage, tenantID, auth.NetworkIdentifier, auth.DestinationAddress); err != nil {
			return logical.ErrorResponse("native funding destination is not a managed tenant wallet"), nil
		}
	}
	approvalKeys, err := cfg.approvalPublicKeys()
	if err != nil {
		return logical.ErrorResponse("approval verification keys are unavailable"), nil
	}
	intentDigest := ""
	if auth.Purpose == "withdrawal" || auth.Purpose == "treasury_withdrawal" {
		intentDigest, err = evm.ValidateApprovalBundleAndGetIntentDigest(approval, approvalKeys, auth, time.Now().UTC())
		if err != nil {
			return logical.ErrorResponse("approval bundle denied transaction: %v", err), nil
		}
	}

	b.signMu.Lock()
	defer b.signMu.Unlock()
	if err := b.validateMerchantCustody(ctx, req.Storage, tenantID, auth.NetworkIdentifier, auth.Purpose, auth.SourceAddress, auth.DestinationAddress, auth.MerchantID); err != nil {
		return logical.ErrorResponse("merchant custody denied transaction: %v", err), nil
	}
	if existing, err := readSignature(ctx, req.Storage, tenantID, auth.AuthorizationID); err != nil {
		return logical.ErrorResponse("internal error"), nil
	} else if existing != nil {
		if existing.UnsignedFingerprint != auth.UnsignedFingerprint {
			return logical.ErrorResponse("authorization already signed a different envelope"), nil
		}
		return signedResponse(auth.AuthorizationID, existing.Signed, true), nil
	}

	walletEntry, err := req.Storage.Get(ctx, walletStorageKey(tenantID, auth.SourceAddress))
	if err != nil || walletEntry == nil {
		return logical.ErrorResponse("authorized source wallet is unavailable"), nil
	}
	var wallet map[string]string
	if json.Unmarshal(walletEntry.Value, &wallet) != nil || wallet["tenant_id"] != tenantID || wallet["chain_id"] != auth.NetworkIdentifier || wallet["vault_key_id"] != auth.VaultKeyID || wallet["private_key"] == "" {
		return logical.ErrorResponse("source wallet ownership mismatch"), nil
	}

	semanticDigest, err := transactionSemanticDigest(auth, envelope)
	if err != nil {
		return logical.ErrorResponse("invalid transaction semantics: %v", err), nil
	}
	intentKey := tenantStorageKey(tenantID, "business-intents", safeStoragePart(auth.Purpose), safeStoragePart(auth.BasisType), safeStoragePart(auth.BasisID), safeStoragePart(strconv.Itoa(auth.Attempt)))
	if auth.BasisType == "CryptoAMLWithdrawalLine" {
		intentKey = tenantStorageKey(tenantID, "approval-source-lines", safeStoragePart(auth.BasisID))
	}
	if intentDigest != "" && auth.BasisType != "CryptoAMLWithdrawalLine" {
		intentKey = tenantStorageKey(tenantID, "approval-intents", safeStoragePart(intentDigest))
	}
	if reused, err := checkReplacementIntent(ctx, req.Storage, intentKey, auth, semanticDigest, intentDigest); err != nil {
		return logical.ErrorResponse("replacement policy denied transaction: %v", err), nil
	} else if reused != nil {
		return signedResponse(auth.AuthorizationID, *reused, true), nil
	}
	allowed := make(map[string]bool, len(cfg.AllowedChainIDs))
	for _, chainID := range cfg.AllowedChainIDs {
		allowed[chainID] = true
	}
	signed, verified, err := evm.SignAuthorizedTransaction(wallet["private_key"], authorizationKey, authorizationJWS, approval, approvalKeys,
		envelope, time.Now().UTC(), wallet["vault_key_id"], tenantID, allowed)
	if err != nil {
		return logical.ErrorResponse("failed to sign transaction: %v", err), nil
	}
	if err := commitReplacementIntent(ctx, req.Storage, intentKey, verified, semanticDigest, intentDigest, signed); err != nil {
		return logical.ErrorResponse("failed to persist replacement intent record"), nil
	}
	record := signatureRecord{UnsignedFingerprint: verified.UnsignedFingerprint, Signed: signed}
	encoded, _ := json.Marshal(record)
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: signatureStorageKey(tenantID, verified.AuthorizationID), Value: encoded, SealWrap: true}); err != nil {
		return logical.ErrorResponse("failed to persist signature record"), nil
	}
	return signedResponse(verified.AuthorizationID, signed, false), nil
}

type signatureRecord struct {
	UnsignedFingerprint string             `json:"unsigned_fingerprint"`
	Signed              evm.SignedEnvelope `json:"signed"`
}

type replacementIntentRecord struct {
	SemanticDigest      string             `json:"semantic_digest"`
	IntentDigest        string             `json:"intent_digest,omitempty"`
	Nonce               string             `json:"nonce"`
	MaxGeneration       int                `json:"max_generation"`
	AuthorizationID     string             `json:"authorization_id"`
	UnsignedFingerprint string             `json:"unsigned_fingerprint"`
	Signed              evm.SignedEnvelope `json:"signed"`
}

func checkReplacementIntent(ctx context.Context, storage logical.Storage, key string, auth evm.SigningAuthorization, semanticDigest, intentDigest string) (*evm.SignedEnvelope, error) {
	entry, err := storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		if auth.Generation != 1 {
			return nil, fmt.Errorf("first signed generation must be 1")
		}
		return nil, nil
	}
	var record replacementIntentRecord
	if json.Unmarshal(entry.Value, &record) != nil || record.SemanticDigest != semanticDigest || record.IntentDigest != intentDigest || record.Nonce != auth.NetworkSequence {
		return nil, fmt.Errorf("business intent, approval, or nonce changed")
	}
	if auth.Generation == record.MaxGeneration && auth.AuthorizationID == record.AuthorizationID && auth.UnsignedFingerprint == record.UnsignedFingerprint && record.Signed.TxHash != "" {
		return &record.Signed, nil
	}
	if auth.Generation != record.MaxGeneration+1 {
		return nil, fmt.Errorf("generation must be consecutive")
	}
	return nil, nil
}

func commitReplacementIntent(ctx context.Context, storage logical.Storage, key string, auth evm.SigningAuthorization, semanticDigest, intentDigest string, signed evm.SignedEnvelope) error {
	if reused, err := checkReplacementIntent(ctx, storage, key, auth, semanticDigest, intentDigest); err != nil {
		return err
	} else if reused != nil {
		return nil
	}
	record := replacementIntentRecord{SemanticDigest: semanticDigest, IntentDigest: intentDigest, Nonce: auth.NetworkSequence,
		MaxGeneration: auth.Generation, AuthorizationID: auth.AuthorizationID, UnsignedFingerprint: auth.UnsignedFingerprint, Signed: signed}
	payload, _ := json.Marshal(record)
	return storage.Put(ctx, &logical.StorageEntry{Key: key, Value: payload, SealWrap: true})
}

func transactionSemanticDigest(auth evm.SigningAuthorization, envelope evm.UnsignedEnvelope) (string, error) {
	semantic := struct {
		NetworkID, NetworkCode, ChainID, Source, Destination, Standard, Contract, Amount, Nonce, To, Value, Data string
		GasLimit                                                                                                 uint64
	}{auth.NetworkID, auth.NetworkCode, auth.NetworkIdentifier, auth.SourceAddress, auth.DestinationAddress, auth.AssetStandard, auth.ContractAddress, auth.AmountMinorUnits.String(), auth.NetworkSequence,
		envelope.Payload.To, envelope.Payload.ValueWei, envelope.Payload.Data, envelope.Payload.GasLimit}
	payload, err := json.Marshal(semantic)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func readSignature(ctx context.Context, storage logical.Storage, tenantID, authorizationID string) (*signatureRecord, error) {
	entry, err := storage.Get(ctx, signatureStorageKey(tenantID, authorizationID))
	if err != nil || entry == nil {
		return nil, err
	}
	var record signatureRecord
	if err := json.Unmarshal(entry.Value, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func signedResponse(authorizationID string, signed evm.SignedEnvelope, reused bool) *logical.Response {
	return &logical.Response{Data: map[string]any{"signed_envelope": signed, "authorization_id": authorizationID, "tx_id": signed.TxHash, "reused": reused}}
}

func decodeUnsignedEnvelope(raw any) (evm.UnsignedEnvelope, error) {
	var envelope evm.UnsignedEnvelope
	payload, err := json.Marshal(raw)
	if err != nil {
		return envelope, err
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return envelope, err
	}
	return envelope, evm.ValidateUnsignedEnvelope(envelope)
}

func decodeApprovalBundle(raw any) (evm.ApprovalBundle, error) {
	var bundle evm.ApprovalBundle
	if raw == nil {
		return bundle, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil || string(payload) == "null" {
		return bundle, err
	}
	return bundle, json.Unmarshal(payload, &bundle)
}

func approvalBundleEmpty(bundle evm.ApprovalBundle) bool {
	return bundle.PolicyBundleJWS == "" && bundle.IntentJWS == "" && bundle.RiskAssessmentJWS == "" && bundle.PolicyDecisionJWS == "" && len(bundle.Approvals) == 0
}
