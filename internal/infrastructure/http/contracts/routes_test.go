package contracts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The assembled application's route file is checked against the committed
// frontend copy in internal/content; these cases cover the generator itself.
func TestRouteContractsResolveConcreteDirections(t *testing.T) {
	_, err := GenerateRoutes(Registry(), Routes())
	require.NoError(t, err)
	for _, routes := range [][]RouteContract{
		{{Scope: "api", Method: "POST", Path: "/test", Request: "AuthMessageResponse"}},
		{{Scope: "", Method: "GET", Path: "/test", Response: "LoginResponse"}},
		{{Scope: "API", Method: "GET", Path: "/test", Response: "LoginResponse"}},
		{{Scope: "api", Method: "GET", Path: "/test", Response: "Missing"}},
		{Routes()[0], Routes()[0]},
		{{Scope: "api", Method: "POST", Path: "/test", ResponseVariants: []string{"LoginResponse"}}},
		{{Scope: "api", Method: "POST", Path: "/test", ResponseVariants: []string{"LoginResponse", "Missing"}}},
		{{Scope: "api", Method: "POST", Path: "/test", Response: "LoginResponse", ResponseVariants: []string{"LoginResponse", "MFAChallengeResponse"}}},
	} {
		_, err := GenerateRoutes(Registry(), routes)
		require.Error(t, err)
	}
}
