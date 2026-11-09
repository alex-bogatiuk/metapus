package tron

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"vault-plugin-secrets-tron/internal/json"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/fbsobreira/gotron-sdk/pkg/address"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestSignAuthorizedTransaction_NativeTransfer(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	bindUnsignedFingerprint(t, &auth, unsigned)
	approvalBundle := attachTestApprovalBundle(t, &auth, keys, now)
	authJWS := signTestAuthorization(t, auth, keys.authPriv)

	signedTx, returnedAuth, err := SignAuthorizedTransaction(
		source.PrivateKey,
		keys.authPub,
		authJWS,
		approvalBundle,
		keys.approvalPublicKeys(),
		unsigned,
		now,
		auth.VaultKeyID,
		auth.TenantID,
	)
	require.NoError(t, err)

	assert.Equal(t, auth.AuthorizationID, returnedAuth.AuthorizationID)
	assert.Equal(t, unsigned.TxID, signedTx.TxID)
	assert.Equal(t, strings.ToLower(unsigned.RawDataHex), signedTx.RawDataHex)
	assert.True(t, signedTx.Visible)
	require.Len(t, signedTx.Signature, 1)
	assert.Len(t, signedTx.Signature[0], 130)

	sig, err := hex.DecodeString(signedTx.Signature[0])
	require.NoError(t, err)
	txHash, err := hex.DecodeString(unsigned.TxID)
	require.NoError(t, err)
	recoveredPub, err := gethcrypto.SigToPub(txHash, sig)
	require.NoError(t, err)
	assert.Equal(t, source.Address, address.PubkeyToAddress(*recoveredPub).String())
}

func TestSignAuthorizedTransaction_RejectsTxIDMismatch(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	unsigned.TxID = strings.Repeat("0", 64)
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "txID does not match raw_data_hex")
}

func TestSignAuthorizedTransaction_RejectsIncompleteEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	unsigned.Visible = false

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "visible=true")

	unsigned = makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	unsigned.RawData = nil
	_, _, err = signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing raw_data")
}

func TestSignAuthorizedTransaction_RejectsIntentTampering(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 2_000_000, now.Add(10*time.Minute))

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount does not match authorization")
}

func TestSignAuthorizedTransaction_RejectsRiskApprovalForDifferentAssessment(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	bindUnsignedFingerprint(t, &auth, unsigned)
	approvalBundle := attachTestApprovalBundle(t, &auth, keys, now)
	require.Len(t, approvalBundle.Approvals, 1)

	var approval MoneyMovementApproval
	_, err := verifyEdDSAJWS(approvalBundle.Approvals[0].JWS, keys.riskApprovalPub, &approval)
	require.NoError(t, err)
	approval.RiskAssessmentDigest = strings.Repeat("0", 64)
	approvalBundle.Approvals[0].JWS = signTestPayload(t, approval, keys.riskApprovalPriv)
	bundleDigest, err := digestJSON(approvalBundle)
	require.NoError(t, err)
	auth.ApprovalBundleDigest = bundleDigest

	_, _, err = SignAuthorizedTransaction(
		source.PrivateKey,
		keys.authPub,
		signTestAuthorization(t, auth, keys.authPriv),
		approvalBundle,
		keys.approvalPublicKeys(),
		unsigned,
		now,
		auth.VaultKeyID,
		auth.TenantID,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "risk_assessment_digest does not match")
}

func TestSignAuthorizedTransaction_RejectsApprovalSignedByWrongPrincipalKey(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	bindUnsignedFingerprint(t, &auth, unsigned)
	approvalBundle := attachTestApprovalBundle(t, &auth, keys, now)
	require.Len(t, approvalBundle.Approvals, 1)

	var approval MoneyMovementApproval
	_, err := verifyEdDSAJWS(approvalBundle.Approvals[0].JWS, keys.riskApprovalPub, &approval)
	require.NoError(t, err)
	approvalBundle.Approvals[0].JWS = signTestPayload(t, approval, keys.adminApprovalPriv)
	bundleDigest, err := digestJSON(approvalBundle)
	require.NoError(t, err)
	auth.ApprovalBundleDigest = bundleDigest

	_, _, err = SignAuthorizedTransaction(
		source.PrivateKey,
		keys.authPub,
		signTestAuthorization(t, auth, keys.authPriv),
		approvalBundle,
		keys.approvalPublicKeys(),
		unsigned,
		now,
		auth.VaultKeyID,
		auth.TenantID,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify risk approval")
}

func TestSignAuthorizedTransaction_RejectsWrongVaultKeyID(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, "tron_mainnet:other", auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault_key_id does not match wallet")
}

func TestSignAuthorizedTransaction_RejectsWrongTenant(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, "tenant-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id does not match path")
}

func TestSignAuthorizedTransaction_RejectsCrossNetworkReplay(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)
	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	auth.NetworkIdentifier = "nile"

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported network identity")
}

func TestSignAuthorizedTransaction_AllowsTreasuryWithoutMerchant(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 1_000_000, now.Add(10*time.Minute))
	auth.Purpose = "treasury_withdrawal"
	auth.MerchantID = nil
	auth.BasisType = "TreasuryWithdrawal"
	auth.BasisID = "treasury-1"

	signedTx, returnedAuth, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.NoError(t, err)
	assert.Equal(t, "treasury_withdrawal", returnedAuth.Purpose)
	assert.Equal(t, unsigned.TxID, signedTx.TxID)
}

func TestSignAuthorizedTransaction_AccountActivation(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeAccountCreateUnsignedTx(t, source.Address, destination.Address, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 0, now.Add(10*time.Minute))
	auth.Purpose = "activation"
	auth.MerchantID = nil
	auth.BasisType = "CryptoAccountActivationTask"
	auth.BasisID = "activation-1"
	bindUnsignedFingerprint(t, &auth, unsigned)
	authJWS := signTestAuthorization(t, auth, keys.authPriv)

	signedTx, returnedAuth, err := SignAuthorizedTransaction(
		source.PrivateKey,
		keys.authPub,
		authJWS,
		ApprovalBundle{},
		keys.approvalPublicKeys(),
		unsigned,
		now,
		auth.VaultKeyID,
		auth.TenantID,
	)
	require.NoError(t, err)

	assert.Equal(t, "activation", returnedAuth.Purpose)
	assert.Equal(t, unsigned.TxID, signedTx.TxID)
	require.Len(t, signedTx.Signature, 1)
}

func TestSignAuthorizedTransaction_RejectsActivationAsTransfer(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	source := mustGenerateWallet(t)
	destination := mustGenerateWallet(t)
	keys := newTestSigningKeys(t)

	unsigned := makeNativeUnsignedTx(t, source.Address, destination.Address, 1_000_000, now.Add(5*time.Minute))
	auth := makeTestAuthorization(source.Address, destination.Address, 0, now.Add(10*time.Minute))
	auth.Purpose = "activation"
	auth.MerchantID = nil

	_, _, err := signAuthorizedTest(t, source.PrivateKey, keys, auth, unsigned, now, auth.VaultKeyID, auth.TenantID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "activation must use AccountCreateContract")
}

func mustGenerateWallet(t *testing.T) *WalletData {
	t.Helper()
	wallet, err := GenerateWallet()
	require.NoError(t, err)
	return wallet
}

func makeTestAuthorization(source, destination string, amount int64, expiresAt time.Time) SigningAuthorization {
	merchantID := "merchant-1"
	return SigningAuthorization{
		SchemaVersion:      SigningAuthorizationSchemaVersion,
		AuthorizationID:    "auth-1",
		TenantID:           "tenant-1",
		MerchantID:         &merchantID,
		Purpose:            "withdrawal",
		NetworkID:          "00000000-0000-0000-0000-000000000001",
		NetworkCode:        "TRON-MAINNET",
		ChainFamily:        "tron",
		NetworkIdentifier:  "mainnet",
		SourceAddress:      source,
		FeePayerAddress:    source,
		VaultKeyID:         "tron_mainnet:" + source,
		DestinationAddress: destination,
		AssetStandard:      AssetStandardNative,
		AmountMinorUnits:   NewRawAmountFromInt64(amount),
		MaxNetworkFeeMinor: NewRawAmountFromInt64(100_000_000),
		BasisType:          "WithdrawalRequest",
		BasisID:            "request-1",
		Attempt:            1,
		Generation:         1,
		ExpiresAt:          expiresAt,
		PolicyVersion:      SigningAuthorizationPolicyVersion,
	}
}

func bindUnsignedFingerprint(t *testing.T, auth *SigningAuthorization, unsigned UnsignedTransaction) {
	t.Helper()
	fingerprint, err := UnsignedFingerprint(unsigned)
	require.NoError(t, err)
	auth.UnsignedFingerprint = fingerprint
}

func signTestAuthorization(t *testing.T, auth SigningAuthorization, privateKey ed25519.PrivateKey) string {
	t.Helper()
	header, err := json.Marshal(jwsHeader{Alg: "EdDSA", Typ: "JWT", Kid: "transit/metapus-signing-auth-00000000-0000-0000-0000-000000000001/v1"})
	require.NoError(t, err)
	payload, err := json.Marshal(auth)
	require.NoError(t, err)

	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func signAuthorizedTest(
	t *testing.T,
	privateKeyHex string,
	keys testSigningKeys,
	auth SigningAuthorization,
	unsigned UnsignedTransaction,
	now time.Time,
	walletVaultKeyID string,
	expectedTenantID string,
) (SignedTransaction, SigningAuthorization, error) {
	t.Helper()
	bindUnsignedFingerprint(t, &auth, unsigned)
	bundle := ApprovalBundle{}
	if auth.Purpose == "withdrawal" || auth.Purpose == "treasury_withdrawal" {
		bundle = attachTestApprovalBundle(t, &auth, keys, now)
	}
	return SignAuthorizedTransaction(
		privateKeyHex,
		keys.authPub,
		signTestAuthorization(t, auth, keys.authPriv),
		bundle,
		keys.approvalPublicKeys(),
		unsigned,
		now,
		walletVaultKeyID,
		expectedTenantID,
	)
}

type testSigningKeys struct {
	authPub           ed25519.PublicKey
	authPriv          ed25519.PrivateKey
	policyPub         ed25519.PublicKey
	policyPriv        ed25519.PrivateKey
	intentPub         ed25519.PublicKey
	intentPriv        ed25519.PrivateKey
	riskPub           ed25519.PublicKey
	riskPriv          ed25519.PrivateKey
	decisionPub       ed25519.PublicKey
	decisionPriv      ed25519.PrivateKey
	adminApprovalPub  ed25519.PublicKey
	adminApprovalPriv ed25519.PrivateKey
	riskApprovalPub   ed25519.PublicKey
	riskApprovalPriv  ed25519.PrivateKey
}

func newTestSigningKeys(t *testing.T) testSigningKeys {
	t.Helper()
	authPub, authPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	policyPub, policyPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	intentPub, intentPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskPub, riskPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	decisionPub, decisionPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	adminPub, adminPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	riskApprovalPub, riskApprovalPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return testSigningKeys{
		authPub:           authPub,
		authPriv:          authPriv,
		policyPub:         policyPub,
		policyPriv:        policyPriv,
		intentPub:         intentPub,
		intentPriv:        intentPriv,
		riskPub:           riskPub,
		riskPriv:          riskPriv,
		decisionPub:       decisionPub,
		decisionPriv:      decisionPriv,
		adminApprovalPub:  adminPub,
		adminApprovalPriv: adminPriv,
		riskApprovalPub:   riskApprovalPub,
		riskApprovalPriv:  riskApprovalPriv,
	}
}

func (k testSigningKeys) approvalPublicKeys() ApprovalPublicKeys {
	return ApprovalPublicKeys{
		PolicyPublicKey:        k.policyPub,
		IntentPublicKey:        k.intentPub,
		RiskPublicKey:          k.riskPub,
		DecisionPublicKey:      k.decisionPub,
		AdminApprovalPublicKey: k.adminApprovalPub,
		RiskApprovalPublicKey:  k.riskApprovalPub,
	}
}

func attachTestApprovalBundle(t *testing.T, auth *SigningAuthorization, keys testSigningKeys, now time.Time) ApprovalBundle {
	t.Helper()
	policy := ApprovalPolicyBundle{
		SchemaVersion:             "approval_policy.v1",
		PolicyVersion:             "money-movement-v1.0",
		TenantID:                  auth.TenantID,
		DefaultRequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk},
		EffectiveFrom:             now.Add(-time.Hour),
		ExpiresAt:                 now.Add(time.Hour),
	}
	policyJWS := signTestPayload(t, policy, keys.policyPriv)
	policyDigest := testJWSPayloadDigest(t, policyJWS)

	intent := MoneyMovementIntent{
		SchemaVersion:      "money_movement_intent.v1",
		IntentID:           auth.AuthorizationID + "-intent",
		TenantID:           auth.TenantID,
		Purpose:            auth.Purpose,
		BasisType:          auth.BasisType,
		BasisID:            auth.BasisID,
		MerchantID:         auth.MerchantID,
		NetworkCode:        auth.NetworkCode,
		TokenID:            "token-1",
		TokenStandard:      auth.AssetStandard,
		ContractAddress:    auth.ContractAddress,
		AmountMinorUnits:   auth.AmountMinorUnits,
		DestinationAddress: auth.DestinationAddress,
		AddressID:          "address-1",
		SourceAddress:      auth.SourceAddress,
		SourceVaultKeyID:   auth.VaultKeyID,
		PolicyVersion:      policy.PolicyVersion,
		PolicyDigest:       policyDigest,
		ExpiresAt:          auth.ExpiresAt,
	}
	if auth.Purpose == "withdrawal" {
		intent.SourceAddress = ""
		intent.SourceVaultKeyID = ""
	}
	intentJWS := signTestPayload(t, intent, keys.intentPriv)
	intentDigest := testJWSPayloadDigest(t, intentJWS)

	risk := RiskAssessment{
		SchemaVersion: "risk_assessment.v1",
		TenantID:      auth.TenantID,
		IntentDigest:  intentDigest,
		RiskLevel:     "low",
		RiskScore:     1,
		Signals:       []string{"test"},
		AssessedAt:    now,
		ExpiresAt:     now.Add(time.Hour),
	}
	riskJWS := signTestPayload(t, risk, keys.riskPriv)
	riskDigest := testJWSPayloadDigest(t, riskJWS)

	decision := ApprovalPolicyDecision{
		SchemaVersion:      "approval_policy_decision.v1",
		TenantID:           auth.TenantID,
		IntentDigest:       intentDigest,
		PolicyVersion:      policy.PolicyVersion,
		PolicyDigest:       policyDigest,
		RequiredPrincipals: []ApprovalPrincipal{ApprovalPrincipalRisk},
		DecidedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	}
	decisionJWS := signTestPayload(t, decision, keys.decisionPriv)
	decisionDigest := testJWSPayloadDigest(t, decisionJWS)

	approval := MoneyMovementApproval{
		SchemaVersion:        "money_movement_approval.v1",
		ApprovalID:           auth.AuthorizationID + "-risk-approval",
		TenantID:             auth.TenantID,
		IntentDigest:         intentDigest,
		PrincipalType:        ApprovalPrincipalRisk,
		PrincipalID:          "risk-engine",
		PolicyVersion:        policy.PolicyVersion,
		PolicyDecisionDigest: decisionDigest,
		RiskAssessmentDigest: riskDigest,
		ApprovedAt:           now,
		ExpiresAt:            now.Add(time.Hour),
	}
	approvalJWS := signTestPayload(t, approval, keys.riskApprovalPriv)

	bundle := ApprovalBundle{
		PolicyBundleJWS:   policyJWS,
		IntentJWS:         intentJWS,
		RiskAssessmentJWS: riskJWS,
		PolicyDecisionJWS: decisionJWS,
		Approvals: []ApprovalBundleApproval{{
			PrincipalType: ApprovalPrincipalRisk,
			JWS:           approvalJWS,
		}},
	}
	bundleDigest, err := digestJSON(bundle)
	require.NoError(t, err)
	auth.ApprovalBundleDigest = bundleDigest
	return bundle
}

func signTestPayload(t *testing.T, payload any, privateKey ed25519.PrivateKey) string {
	t.Helper()
	header, err := json.Marshal(jwsHeader{Alg: "EdDSA", Typ: "JWT"})
	require.NoError(t, err)
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := encodedHeader + "." + encodedPayload
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func testJWSPayloadDigest(t *testing.T, jws string) string {
	t.Helper()
	parts := strings.Split(jws, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func makeAccountCreateUnsignedTx(t *testing.T, source, destination string, expiresAt time.Time) UnsignedTransaction {
	t.Helper()
	sourceAddress, err := address.Base58ToAddress(source)
	require.NoError(t, err)
	destinationAddress, err := address.Base58ToAddress(destination)
	require.NoError(t, err)

	accountCreateContract, err := proto.Marshal(&core.AccountCreateContract{
		OwnerAddress:   sourceAddress.Bytes(),
		AccountAddress: destinationAddress.Bytes(),
	})
	require.NoError(t, err)

	raw := &core.TransactionRaw{
		RefBlockBytes: []byte{0x00, 0x01},
		RefBlockHash:  []byte{0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
		Expiration:    expiresAt.UnixMilli(),
		Timestamp:     expiresAt.Add(-5 * time.Minute).UnixMilli(),
		Contract: []*core.Transaction_Contract{
			{
				Type: core.Transaction_Contract_AccountCreateContract,
				Parameter: &anypb.Any{
					TypeUrl: "type.googleapis.com/protocol.AccountCreateContract",
					Value:   accountCreateContract,
				},
			},
		},
	}
	rawBytes, err := proto.Marshal(raw)
	require.NoError(t, err)
	rawJSON, err := protojson.Marshal(raw)
	require.NoError(t, err)
	txID := sha256.Sum256(rawBytes)

	return UnsignedTransaction{
		TxID:       hex.EncodeToString(txID[:]),
		RawDataHex: hex.EncodeToString(rawBytes),
		RawData:    rawJSON,
		Visible:    true,
	}
}

func makeNativeUnsignedTx(t *testing.T, source, destination string, amount int64, expiresAt time.Time) UnsignedTransaction {
	t.Helper()
	sourceAddress, err := address.Base58ToAddress(source)
	require.NoError(t, err)
	destinationAddress, err := address.Base58ToAddress(destination)
	require.NoError(t, err)

	transferContract, err := proto.Marshal(&core.TransferContract{
		OwnerAddress: sourceAddress.Bytes(),
		ToAddress:    destinationAddress.Bytes(),
		Amount:       amount,
	})
	require.NoError(t, err)

	raw := &core.TransactionRaw{
		RefBlockBytes: []byte{0x00, 0x01},
		RefBlockHash:  []byte{0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
		Expiration:    expiresAt.UnixMilli(),
		Timestamp:     expiresAt.Add(-5 * time.Minute).UnixMilli(),
		Contract: []*core.Transaction_Contract{
			{
				Type: core.Transaction_Contract_TransferContract,
				Parameter: &anypb.Any{
					TypeUrl: "type.googleapis.com/protocol.TransferContract",
					Value:   transferContract,
				},
			},
		},
	}
	rawBytes, err := proto.Marshal(raw)
	require.NoError(t, err)
	rawJSON, err := protojson.Marshal(raw)
	require.NoError(t, err)
	txID := sha256.Sum256(rawBytes)

	return UnsignedTransaction{
		TxID:       hex.EncodeToString(txID[:]),
		RawDataHex: hex.EncodeToString(rawBytes),
		RawData:    rawJSON,
		Visible:    true,
	}
}
