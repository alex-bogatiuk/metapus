package main

import (
	"fmt"
	"io"
	"os"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
)

// This is a local control-plane manifest, not a merchant-facing API payload.
type policyManifest struct {
	ID                id.ID  `json:"id"`
	MerchantID        id.ID  `json:"merchantId"`
	NetworkID         id.ID  `json:"networkId"`
	Role              string `json:"role"`
	TokenID           *id.ID `json:"tokenId"`
	RiskClass         string `json:"riskClass"`
	Enabled           bool   `json:"enabled"`
	AutomaticBaseline bool   `json:"automaticBaseline"`
	ExpectedVersion   int64  `json:"expectedVersion"`
	LowWatermark      int    `json:"lowWatermark"`
	TargetReady       int    `json:"targetReady"`
	MaxTotal          int    `json:"maxTotal"`
	MaxProvisionBatch int    `json:"maxProvisionBatch"`
	Actor             string `json:"actor"`
}

func readPolicyManifest(path string) (crypto.MerchantPoolPolicyRequest, error) {
	var request crypto.MerchantPoolPolicyRequest
	file, err := os.Open(path)
	if err != nil {
		return request, fmt.Errorf("cannot open merchant pool policy manifest")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return request, fmt.Errorf("merchant pool manifest exceeds read limit")
	}
	return decodePolicyManifest(raw)
}

func decodePolicyManifest(raw []byte) (crypto.MerchantPoolPolicyRequest, error) {
	var manifest policyManifest
	if err := json.UnmarshalStrict(raw, &manifest); err != nil {
		return crypto.MerchantPoolPolicyRequest{}, fmt.Errorf("invalid merchant pool manifest: %w", err)
	}
	request := crypto.MerchantPoolPolicyRequest{Policy: crypto.MerchantPoolPolicy{ID: manifest.ID, MerchantID: manifest.MerchantID, NetworkID: manifest.NetworkID, Role: manifest.Role, TokenID: manifest.TokenID, RiskClass: manifest.RiskClass, Enabled: manifest.Enabled,
		LowWatermark: manifest.LowWatermark, TargetReady: manifest.TargetReady, MaxTotal: manifest.MaxTotal, MaxProvisionBatch: manifest.MaxProvisionBatch, AutomaticBaseline: manifest.AutomaticBaseline}, ExpectedVersion: manifest.ExpectedVersion, Actor: manifest.Actor}
	return request, request.Validate()
}

// Batch policy limits are exact minor units; no floating-point conversion.
type batchPolicyManifest struct {
	MerchantID      id.ID     `json:"merchantId"`
	NetworkID       id.ID     `json:"networkId"`
	TokenID         id.ID     `json:"tokenId"`
	RiskClass       aml.Class `json:"riskClass"`
	ExpectedVersion int64     `json:"expectedVersion"`
	MaxGross        string    `json:"maxGross"`
	MaxAgeSeconds   int       `json:"maxAgeSeconds"`
	Actor           string    `json:"actor"`
}

func readBatchPolicyManifest(path string) (aml.BatchPolicyRequest, error) {
	var request aml.BatchPolicyRequest
	file, err := os.Open(path)
	if err != nil {
		return request, fmt.Errorf("cannot open batch policy manifest")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return request, fmt.Errorf("batch policy manifest exceeds read limit")
	}
	return decodeBatchPolicyManifest(raw)
}
func decodeBatchPolicyManifest(raw []byte) (aml.BatchPolicyRequest, error) {
	var m batchPolicyManifest
	var request aml.BatchPolicyRequest
	if err := json.UnmarshalStrict(raw, &m); err != nil {
		return request, fmt.Errorf("invalid batch policy manifest: %w", err)
	}
	amount, err := types.NewRawAmountFromString(m.MaxGross)
	if err != nil {
		return request, err
	}
	request = aml.BatchPolicyRequest{MerchantID: m.MerchantID, NetworkID: m.NetworkID, TokenID: m.TokenID, Class: m.RiskClass, ExpectedVersion: m.ExpectedVersion, MaxGross: amount, MaxAgeSeconds: m.MaxAgeSeconds, Actor: m.Actor}
	return request, request.Validate()
}
