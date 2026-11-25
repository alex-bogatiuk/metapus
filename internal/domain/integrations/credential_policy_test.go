package integrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialProfileAllowsOmittingOptionalFields(t *testing.T) {
	profile := CredentialProfile{RequireFields: true, Fields: []CredentialField{
		{Name: "nile", Optional: true},
		{Name: "sepolia", Optional: true},
		{Name: "region"},
	}}
	keys := func(names ...string) Credentials {
		c := Credentials{}
		for _, name := range names {
			c.Keys = append(c.Keys, NamedKey{Name: name, Value: "token"})
		}
		return c
	}
	require.NoError(t, profile.Validate(keys("region", "nile")))
	require.NoError(t, profile.Validate(keys("region", "nile", "sepolia")))
	require.Error(t, profile.Validate(keys("nile", "sepolia")), "a required field stays required")
}
