package rpcconfig

import (
	"fmt"
	"slices"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
)

// An RPC endpoint reads one named key from whichever pool account is selected,
// and runtime selection skips an account without that name. A pool is
// consistent when every account with credentials has every key its endpoints
// read. Accounts without credentials are reported as unconfigured instead.

// RPCKeyUser is an endpoint of a pool and the account key it reads.
type RPCKeyUser struct {
	Endpoint string
	Key      string
}

// RequireRPCKeys rejects a pool in which an account with credentials lacks a
// key that one of its endpoints reads.
func RequireRPCKeys(users []RPCKeyUser, accounts []integrations.AccountKeys) error {
	for _, u := range users {
		var missing []string
		for _, a := range accounts {
			if a.HasCredentials && !slices.Contains(a.Keys, u.Key) {
				missing = append(missing, "«"+a.Name+"»")
			}
		}
		if len(missing) > 0 {
			return apperror.NewValidation(fmt.Sprintf("Подключение «%s» использует ключ «%s», которого нет в аккаунтах %s", u.Endpoint, u.Key, strings.Join(missing, ", "))).
				WithDetail("reasonCode", "rpc_key_missing").WithDetail("field", "credentialSlot")
		}
	}
	return nil
}

// CommonRPCKeyNames are the key names every account with credentials has, sorted.
func CommonRPCKeyNames(accounts []integrations.AccountKeys) []string {
	names := []string{}
	first := true
	for _, a := range accounts {
		if !a.HasCredentials {
			continue
		}
		if first {
			names, first = slices.Clone(a.Keys), false
			continue
		}
		names = slices.DeleteFunc(names, func(name string) bool { return !slices.Contains(a.Keys, name) })
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// RPCKeyOptions tells an endpoint form which account key it may read.
// Required is set when the provider profile fixes the name; otherwise Names
// lists the names every pool account with credentials has.
type RPCKeyOptions struct {
	Required string
	Names    []string
	Accounts []integrations.AccountKeys
}

type RPCKeyQuery struct {
	PoolID    id.ID
	NetworkID id.ID
	Role      string
	URL       string
}
