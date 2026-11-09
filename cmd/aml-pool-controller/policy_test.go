package main

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPolicyManifestRejectsUnknownSettingsAndRequiresFiniteCap(t *testing.T) {
	raw := `{"id":"11111111-1111-4111-8111-111111111111","merchantId":"22222222-2222-4222-8222-222222222222","networkId":"33333333-3333-4333-8333-333333333333","role":"merchant_pool","enabled":true,"lowWatermark":0,"targetReady":2,"maxTotal":5,"maxProvisionBatch":2,"actor":"operator"}`
	request, err := decodePolicyManifest([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, 5, request.Policy.MaxTotal)
	_, err = decodePolicyManifest([]byte(`{"maxTotalWallets":100}`))
	require.Error(t, err)
	_, err = decodePolicyManifest([]byte(`{"enabled":true,"targetReady":100}`))
	require.Error(t, err)
}

func TestBatchPolicyManifestIsStrictAndRequiresMinorUnitsAndLowMedium(t *testing.T) {
	raw := `{"merchantId":"22222222-2222-4222-8222-222222222222","networkId":"33333333-3333-4333-8333-333333333333","tokenId":"44444444-4444-4444-8444-444444444444","riskClass":"low","maxGross":"100000000","maxAgeSeconds":86400,"actor":"operator"}`
	request, err := decodeBatchPolicyManifest([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "100000000", request.MaxGross.String())
	for _, invalid := range []string{strings.Replace(raw, `"low"`, `"high"`, 1), strings.Replace(raw, `"100000000"`, `"1.5"`, 1), strings.Replace(raw, `"actor"`, `"unknown"`, 1)} {
		_, err = decodeBatchPolicyManifest([]byte(invalid))
		require.Error(t, err)
	}
}
