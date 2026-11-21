package rpcconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
)

func TestRPCCoverageChangeAllowsSetupAndRepairButNotDegradation(t *testing.T) {
	view := func(count int) RPCConfiguration {
		return RPCConfiguration{Source: "database", Requirements: []RPCRoleRequirement{{Role: "rpc", RequiredProviders: 2, ConfiguredProviders: count}}}
	}
	require.NoError(t, ValidateRPCCoverageChange(view(0), view(1)))
	require.NoError(t, ValidateRPCCoverageChange(view(1), view(1)))
	require.Error(t, ValidateRPCCoverageChange(view(2), view(1)))
	require.Error(t, ValidateRPCCoverageChange(view(1), view(0)))
	require.Error(t, ValidateRPCCoverageChange(RPCConfiguration{Source: "file"}, view(1)))
	changedFamily := view(0)
	changedFamily.Requirements[0].Role = "solidity"
	require.Error(t, ValidateRPCCoverageChange(view(2), changedFamily))
}

func TestRPCConfigurationRequestRejectsDuplicatesAndInvalidLimits(t *testing.T) {
	change := RPCConfigurationChange{ID: id.New(), Version: 1, Priority: 10, IsActive: true}
	valid := RPCConfigurationUpdate{ExpectedRevision: strings.Repeat("a", 64), Endpoints: []RPCConfigurationChange{change}}
	require.NoError(t, valid.Validate())
	valid.Endpoints = append(valid.Endpoints, change)
	require.Error(t, valid.Validate())
	valid.Endpoints = []RPCConfigurationChange{change}
	valid.Endpoints[0].Priority = -1
	require.Error(t, valid.Validate())
}
