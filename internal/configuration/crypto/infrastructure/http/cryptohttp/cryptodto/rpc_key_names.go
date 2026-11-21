package cryptodto

import "metapus/internal/configuration/crypto/domain/crypto/rpcconfig"

// IntegrationRPCKeyAccount lists key names of one pool account, never values.
type IntegrationRPCKeyAccount struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	HasCredentials bool     `json:"hasCredentials"`
	Keys           []string `json:"keys"`
}

// IntegrationRPCKeyOptionsResponse tells an RPC endpoint form which key it reads.
// A non-empty required name is fixed by the provider profile.
type IntegrationRPCKeyOptionsResponse struct {
	Required string                     `json:"required"`
	Names    []string                   `json:"names"`
	Accounts []IntegrationRPCKeyAccount `json:"accounts"`
}

func FromIntegrationRPCKeyOptions(o rpcconfig.RPCKeyOptions) IntegrationRPCKeyOptionsResponse {
	out := IntegrationRPCKeyOptionsResponse{Required: o.Required, Names: o.Names, Accounts: []IntegrationRPCKeyAccount{}}
	if out.Names == nil {
		out.Names = []string{}
	}
	for _, a := range o.Accounts {
		keys := a.Keys
		if keys == nil {
			keys = []string{}
		}
		out.Accounts = append(out.Accounts, IntegrationRPCKeyAccount{ID: a.ID, Name: a.Name, HasCredentials: a.HasCredentials, Keys: keys})
	}
	return out
}
