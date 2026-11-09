package aml

import (
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

const TestnetProvider = "testnet_aml"
const TestnetContract = "aml-testnet.v1"
const TestnetAdapter = "testnet-aml.v1"
const TestnetPolicyVersion = "aml-testnet.v2"
const TestnetDecisionTTLSeconds = 86400
const TestnetMaxReportAgeSeconds = 86400

type EmulatorConfig struct {
	Seed            string
	MaxDelaySeconds int
	UnknownRateBP   int
}

func EmulationNetwork(family, identifier string) bool {
	return (family == "tron" && identifier == "nile") || (family == "evm" && identifier == "11155111")
}

func (c EmulatorConfig) Validate(family, identifier string) error {
	if !EmulationNetwork(family, identifier) {
		return apperror.NewValidation("AML emulation is restricted to TRON Nile and Ethereum Sepolia")
	}
	if len(c.Seed) == 0 || len(c.Seed) > 128 || c.MaxDelaySeconds < 0 || c.MaxDelaySeconds > 60 || c.UnknownRateBP < 0 || c.UnknownRateBP > 10000 {
		return apperror.NewValidation("invalid AML emulator seed, delay or unknown rate")
	}
	return nil
}

type ConfigureTestnetRequest struct {
	AccountID  id.ID
	MerchantID id.ID
	NetworkID  id.ID
	Config     EmulatorConfig
	Actor      string
}
