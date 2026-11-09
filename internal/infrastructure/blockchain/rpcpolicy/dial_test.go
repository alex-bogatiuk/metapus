package rpcpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCResolverRejectsUnsafeConfiguration(t *testing.T) {
	for _, server := range []string{"localhost:853", "127.0.0.1:853", "169.254.169.254:853", "198.18.1.5:853", "[::1]:853", "1.1.1.1:53", "1.1.1.1"} {
		t.Run(server, func(t *testing.T) {
			_, err := rpcResolver(server)
			require.Error(t, err)
			_, err = PublicDialContext(server)(t.Context(), "tcp", "example.com:443")
			require.Error(t, err, "invalid DNS configuration must fail closed")
		})
	}
	_, err := rpcResolver("1.1.1.1:853")
	require.NoError(t, err)
}

func TestRPCDialRejectsReservedDestination(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "169.254.169.254:443", "198.18.1.5:443", "1.1.1.1:8080"} {
		t.Run(address, func(t *testing.T) {
			_, err := PublicDialContext("1.1.1.1:853")(t.Context(), "tcp", address)
			require.Error(t, err)
		})
	}
}
