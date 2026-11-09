package backend

import (
	"bytes"
	"context"
	"fmt"
	"time"
	"vault-plugin-secrets-tron/internal/json"
	"vault-plugin-secrets-tron/internal/tron"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func pathSign(b *tronBackend) []*framework.Path {
	return []*framework.Path{{
		Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/tx/sign",
		Fields: map[string]*framework.FieldSchema{
			"tenant_id":         {Type: framework.TypeString},
			"authorization_jws": {Type: framework.TypeString, Required: true},
			"approval_bundle":   {Type: framework.TypeMap},
			"unsigned_envelope": {Type: framework.TypeMap, Required: true},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{Callback: b.pathSignTRONTransaction},
		},
		HelpSynopsis: "Signs one policy-authorized tenant-scoped TRON transaction",
	}}
}

func (b *tronBackend) pathSignTRONTransaction(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	authorizationJWS := data.Get("authorization_jws").(string)
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("tenant_id must be a canonical UUID"), nil
	}
	if authorizationJWS == "" {
		return logical.ErrorResponse("authorization_jws is required"), nil
	}
	unsignedEnvelope, err := decodeUnsignedEnvelope(data.Get("unsigned_envelope"))
	if err != nil {
		return logical.ErrorResponse("invalid unsigned_envelope: %v", err), nil
	}
	unsignedTx := unsignedEnvelope.Payload
	approvalBundle, err := decodeApprovalBundle(data.Get("approval_bundle"))
	if err != nil {
		return logical.ErrorResponse("invalid approval_bundle: %v", err), nil
	}

	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, tenantID)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if cfg.SchemaVersion != tenantSigningConfigSchemaV3 {
		return logical.ErrorResponse("authorization config is not set"), nil
	}
	kid, err := tron.AuthorizationJWSKeyID(authorizationJWS)
	if err != nil {
		return logical.ErrorResponse("invalid authorization: %v", err), nil
	}
	publicKey, err := cfg.authorizationPublicKey(tenantID, kid)
	if err != nil {
		return logical.ErrorResponse("invalid authorization: %v", err), nil
	}
	approvalKeys, err := cfg.approvalPublicKeys()
	if err != nil {
		return logical.ErrorResponse("invalid configured approval public keys: %v", err), nil
	}
	auth, err := tron.VerifyAuthorizationJWS(authorizationJWS, publicKey)
	if err != nil {
		return logical.ErrorResponse("invalid authorization: %v", err), nil
	}
	if auth.TenantID != tenantID {
		return logical.ErrorResponse("authorization tenant_id does not match path tenant_id"), nil
	}
	if auth.UnsignedFingerprint != unsignedEnvelope.UnsignedFingerprint {
		return logical.ErrorResponse("unsigned envelope fingerprint does not match authorization"), nil
	}
	if auth.Generation != unsignedEnvelope.Generation || auth.NetworkSequence != unsignedEnvelope.NetworkSequence {
		return logical.ErrorResponse("unsigned envelope generation or network sequence does not match authorization"), nil
	}
	if !tron.ValidateAddress(auth.SourceAddress) {
		return logical.ErrorResponse("invalid source_address format"), nil
	}
	if auth.Purpose != "withdrawal" && auth.Purpose != "treasury_withdrawal" && !approvalBundleEmpty(approvalBundle) {
		return logical.ErrorResponse("approval bundle is not allowed for %s", auth.Purpose), nil
	}
	if auth.Purpose == "sweep" {
		if err := b.validateSweepDestination(ctx, req.Storage, tenantID, tron.PluginNetwork(auth), auth.DestinationAddress); err != nil {
			return logical.ErrorResponse("sweep policy denied transaction: %v", err), nil
		}
	}
	if auth.Purpose == "native_funding" {
		if err := validateManagedFundingDestination(ctx, req.Storage, tenantID, tron.PluginNetwork(auth), auth.DestinationAddress); err != nil {
			return logical.ErrorResponse("native funding policy denied transaction: %v", err), nil
		}
	}
	intentDigest := ""
	if auth.Purpose == "withdrawal" || auth.Purpose == "treasury_withdrawal" {
		bundleDigest, err := tron.ApprovalBundleDigest(approvalBundle)
		if err != nil || bundleDigest != auth.ApprovalBundleDigest {
			return logical.ErrorResponse("approval bundle digest does not match authorization"), nil
		}
		intentDigest, err = tron.IntentJWSPayloadDigest(approvalBundle.IntentJWS)
		if err != nil {
			return logical.ErrorResponse("invalid approval bundle: %v", err), nil
		}
	}

	b.signMu.Lock()
	defer b.signMu.Unlock()

	if err := b.validateMerchantCustody(ctx, req.Storage, tenantID, tron.PluginNetwork(auth), auth.Purpose, auth.SourceAddress, auth.DestinationAddress, auth.MerchantID); err != nil {
		return logical.ErrorResponse("merchant custody denied transaction: %v", err), nil
	}

	consumptionKey := ""
	consumptionKind := ""
	switch auth.Purpose {
	case "withdrawal", "treasury_withdrawal":
		consumptionKey = intentConsumptionStorageKey(tenantID, intentDigest)
		consumptionKind = "approval intent"
		if auth.BasisType == "CryptoAMLWithdrawalLine" {
			consumptionKey = businessBasisStorageKey(tenantID, auth.Purpose, auth.BasisType, auth.BasisID)
			consumptionKind = "approval source line"
		}
	case "sweep", "activation", "native_funding":
		consumptionKey = businessBasisStorageKey(tenantID, auth.Purpose, auth.BasisType, auth.BasisID)
		consumptionKind = "business basis"
	}
	if consumptionKey != "" {
		stored, err := readConsumptionRecord(ctx, req.Storage, consumptionKey)
		if err != nil {
			return logical.ErrorResponse("internal error"), nil
		}
		if stored != nil {
			if stored.matches(auth, unsignedTx.TxID, intentDigest) {
				return idempotentResponse(auth, stored.SignedTx, consumptionKind), nil
			}
			return logical.ErrorResponse("%s already consumed by a different transaction or authorization", consumptionKind), nil
		}
	}

	existing, err := req.Storage.Get(ctx, signatureStorageKey(tenantID, auth.AuthorizationID))
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if existing != nil {
		var stored signatureRecord
		if err := json.Unmarshal(existing.Value, &stored); err != nil {
			return logical.ErrorResponse("internal error"), nil
		}
		if stored.TxID != unsignedTx.TxID {
			return logical.ErrorResponse("authorization already signed a different txID"), nil
		}
		if stored.ApprovalBundleDigest != auth.ApprovalBundleDigest {
			return logical.ErrorResponse("authorization already signed with a different approval bundle"), nil
		}
		if stored.UnsignedFingerprint != auth.UnsignedFingerprint {
			return logical.ErrorResponse("authorization already signed a different unsigned fingerprint"), nil
		}
		return idempotentResponse(auth, stored.SignedTx, "authorization"), nil
	}

	walletEntry, err := req.Storage.Get(ctx, walletStorageKey(tenantID, auth.SourceAddress))
	if err != nil {
		return logical.ErrorResponse("internal error reading wallet"), nil
	}
	if walletEntry == nil {
		return logical.ErrorResponse("wallet not found"), nil
	}
	var walletData map[string]string
	if err := json.Unmarshal(walletEntry.Value, &walletData); err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if walletData["tenant_id"] != tenantID || walletData["network"] != tron.PluginNetwork(auth) {
		return logical.ErrorResponse("wallet tenant or network scope mismatch"), nil
	}
	if walletData["private_key"] == "" || walletData["vault_key_id"] == "" {
		return logical.ErrorResponse("wallet corrupted"), nil
	}
	signedTx, verifiedAuth, err := tron.SignAuthorizedTransaction(
		walletData["private_key"], publicKey, authorizationJWS, approvalBundle, approvalKeys,
		unsignedTx, time.Now().UTC(), walletData["vault_key_id"], tenantID,
	)
	if err != nil {
		return logical.ErrorResponse("failed to sign transaction: %v", err), nil
	}

	if consumptionKey != "" {
		record := consumptionRecord{
			AuthorizationID: verifiedAuth.AuthorizationID, TxID: signedTx.TxID,
			SourceAddress: verifiedAuth.SourceAddress, DestinationAddress: verifiedAuth.DestinationAddress,
			ApprovalBundleDigest: verifiedAuth.ApprovalBundleDigest, IntentDigest: intentDigest,
			UnsignedFingerprint: verifiedAuth.UnsignedFingerprint, SignedTx: signedTx,
		}
		if err := putConsumptionRecord(ctx, req.Storage, consumptionKey, record); err != nil {
			return logical.ErrorResponse("failed to save one-time consumption record"), nil
		}
	}
	record := signatureRecord{TxID: signedTx.TxID, ApprovalBundleDigest: verifiedAuth.ApprovalBundleDigest, UnsignedFingerprint: verifiedAuth.UnsignedFingerprint, SignedTx: signedTx}
	payload, err := json.Marshal(record)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: signatureStorageKey(tenantID, verifiedAuth.AuthorizationID), Value: payload, SealWrap: true}); err != nil {
		return logical.ErrorResponse("failed to save signature record"), nil
	}

	b.Logger().Info("successfully signed authorized transaction", "authorization_id", verifiedAuth.AuthorizationID, "basis_id", verifiedAuth.BasisID, "tx_id", signedTx.TxID, "tenant_id", tenantID)
	return &logical.Response{Data: map[string]any{
		"signed_envelope": tron.NewSignedEnvelope(verifiedAuth, signedTx), "authorization_id": verifiedAuth.AuthorizationID, "tx_id": signedTx.TxID,
		"algorithm": "ECDSA secp256k1", "hash_type": "sha256(raw_data)", "message": "Transaction signed successfully",
	}}, nil
}

type signatureRecord struct {
	TxID                 string                 `json:"tx_id"`
	ApprovalBundleDigest string                 `json:"approval_bundle_digest,omitempty"`
	UnsignedFingerprint  string                 `json:"unsigned_fingerprint"`
	SignedTx             tron.SignedTransaction `json:"signed_tx"`
}

type consumptionRecord struct {
	AuthorizationID      string                 `json:"authorization_id"`
	TxID                 string                 `json:"tx_id"`
	SourceAddress        string                 `json:"source_address"`
	DestinationAddress   string                 `json:"destination_address"`
	ApprovalBundleDigest string                 `json:"approval_bundle_digest,omitempty"`
	IntentDigest         string                 `json:"intent_digest,omitempty"`
	UnsignedFingerprint  string                 `json:"unsigned_fingerprint"`
	SignedTx             tron.SignedTransaction `json:"signed_tx"`
}

func (r consumptionRecord) matches(auth tron.SigningAuthorization, txID, intentDigest string) bool {
	if r.TxID != txID || r.SourceAddress != auth.SourceAddress || r.DestinationAddress != auth.DestinationAddress || r.ApprovalBundleDigest != auth.ApprovalBundleDigest || r.IntentDigest != intentDigest || r.UnsignedFingerprint != auth.UnsignedFingerprint {
		return false
	}
	if intentDigest != "" && r.AuthorizationID != auth.AuthorizationID {
		return false
	}
	return true
}

func readConsumptionRecord(ctx context.Context, storage logical.Storage, key string) (*consumptionRecord, error) {
	entry, err := storage.Get(ctx, key)
	if err != nil || entry == nil {
		return nil, err
	}
	var record consumptionRecord
	if err := json.Unmarshal(entry.Value, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func putConsumptionRecord(ctx context.Context, storage logical.Storage, key string, record consumptionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return storage.Put(ctx, &logical.StorageEntry{Key: key, Value: payload, SealWrap: true})
}

func idempotentResponse(auth tron.SigningAuthorization, signed tron.SignedTransaction, scope string) *logical.Response {
	return &logical.Response{Data: map[string]any{
		"signed_envelope": tron.NewSignedEnvelope(auth, signed), "authorization_id": auth.AuthorizationID, "tx_id": signed.TxID,
		"message": fmt.Sprintf("Transaction signature returned from %s idempotency record", scope),
	}}
}

func approvalBundleEmpty(bundle tron.ApprovalBundle) bool {
	return bundle.PolicyBundleJWS == "" && bundle.IntentJWS == "" && bundle.RiskAssessmentJWS == "" && bundle.PolicyDecisionJWS == "" && len(bundle.Approvals) == 0
}

func validateManagedFundingDestination(ctx context.Context, storage logical.Storage, tenantID, network, destination string) error {
	entry, err := storage.Get(ctx, walletStorageKey(tenantID, destination))
	if err != nil {
		return fmt.Errorf("read destination wallet")
	}
	if entry == nil {
		return fmt.Errorf("destination is not a managed tenant wallet")
	}
	var wallet map[string]string
	if err := json.Unmarshal(entry.Value, &wallet); err != nil {
		return fmt.Errorf("destination wallet is corrupted")
	}
	if wallet["tenant_id"] != tenantID || wallet["network"] != network {
		return fmt.Errorf("destination wallet tenant or network scope mismatch")
	}
	return nil
}

func decodeUnsignedEnvelope(raw any) (tron.UnsignedEnvelope, error) {
	var envelope tron.UnsignedEnvelope
	payload, err := json.Marshal(raw)
	if err != nil {
		return envelope, err
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return envelope, err
	}
	if err := tron.ValidateUnsignedEnvelope(envelope); err != nil {
		return envelope, err
	}
	unsigned := envelope.Payload
	if unsigned.TxID == "" || unsigned.RawDataHex == "" {
		return envelope, fmt.Errorf("missing txID or raw_data_hex")
	}
	if !unsigned.Visible {
		return envelope, fmt.Errorf("visible=true is required")
	}
	rawData := bytes.TrimSpace(unsigned.RawData)
	if len(rawData) == 0 || bytes.Equal(rawData, []byte("null")) || rawData[0] != '{' {
		return envelope, fmt.Errorf("raw_data must be a JSON object")
	}
	return envelope, nil
}

func decodeApprovalBundle(raw any) (tron.ApprovalBundle, error) {
	var bundle tron.ApprovalBundle
	if raw == nil {
		return bundle, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return bundle, err
	}
	if len(payload) == 0 || string(payload) == "null" {
		return bundle, nil
	}
	err = json.Unmarshal(payload, &bundle)
	return bundle, err
}
