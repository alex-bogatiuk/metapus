package integrations

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChainstackBaseProfilesRejectCredentialsAndOtherAPIs(t *testing.T) {
	for _, tt := range []struct {
		give string
		want bool
	}{
		{"https://tron-nile.core.chainstack.com", true},
		{"https://ethereum-sepolia.core.chainstack.com/", true},
		{"https://ethereum-sepolia.core.chainstack.com:443", true},
		{"http://ethereum-sepolia.core.chainstack.com", false},
		{"https://ethereum-sepolia.core.chainstack.com:444", false},
		{"https://ethereum-sepolia.core.chainstack.com/token", false},
		{"https://ethereum-sepolia.core.chainstack.com/beacon", false},
		{"https://ethereum-sepolia.core.chainstack.com/jsonrpc", false},
		{"https://ethereum-sepolia.core.chainstack.com?", false},
		{"https://ethereum-sepolia.core.chainstack.com/?token=x", false},
		{"https://user:pass@ethereum-sepolia.core.chainstack.com", false},
		{"https://ethereum-sepolia.core.chainstack.com.evil.test", false},
		{"https://ethereum-mainnet.core.chainstack.com", false},
		{"https://ethereum-sepolia.core.chainstack.com/%2f", false},
	} {
		t.Run(tt.give, func(t *testing.T) { _, ok := Chainstack.RPCProfileForURL(tt.give); require.Equal(t, tt.want, ok) })
	}
}
