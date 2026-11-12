package integration_repo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/integrations/profiles"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// ImportBinding contains no secrets. Endpoint keys are NETWORK/role/providerId;
// exchange-rate keys are rateSource:CODE. ExternalAccountRef is explicit because
// separate API keys need not represent separate external quota accounts.
type ImportBinding struct {
	ExternalAccountRef   string     `json:"externalAccountRef"`
	CredentialsExpiresAt *time.Time `json:"credentialsExpiresAt"`
	Name                 string     `json:"name"`
	PoolCode             string     `json:"poolCode"`
	Slot                 string     `json:"slot"`
}
type ImportReport struct {
	Accounts          int `json:"accounts"`
	Pools             int `json:"pools"`
	Endpoints         int `json:"endpoints"`
	RateSources       int `json:"rateSources"`
	ActivatedNetworks int `json:"activatedNetworks"`
}
type importedEndpoint struct {
	model    *model.RPCEndpoint
	poolCode string
}
type importedAccount struct {
	provider model.Provider
	binding  ImportBinding
	keys     map[string]string
}

func importedCode(kind, key string) string {
	return fmt.Sprintf("IMP-%s-%x", kind, sha256.Sum256([]byte(key)))[:len(kind)+5+24]
}
func ImportConfiguration(ctx context.Context, cfg *chainconfig.File, bindings map[string]ImportBinding, activate bool) (ImportReport, error) {
	var report ImportReport
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	accounts := map[string]*importedAccount{}
	poolMembers := map[string][]string{}
	poolProvider := map[string]model.Provider{}
	// profileSlot is the key name fixed by the provider profile, if any.
	add := func(target string, provider model.Provider, secret, profileSlot string) (string, string, error) {
		b, ok := bindings[target]
		if !ok || strings.TrimSpace(b.ExternalAccountRef) == "" || strings.TrimSpace(b.PoolCode) == "" {
			return "", "", fmt.Errorf("explicit external account and pool mapping required for %s", target)
		}
		if b.Slot == "" {
			b.Slot = profileSlot
		}
		if b.Slot == "" {
			b.Slot = "default"
		}
		if profileSlot != "" && b.Slot != profileSlot {
			return "", "", fmt.Errorf("key name for %s must be %s", target, profileSlot)
		}
		if b.Name == "" {
			b.Name = b.ExternalAccountRef
		}
		if b.CredentialsExpiresAt != nil {
			end := b.CredentialsExpiresAt.UTC().Truncate(time.Microsecond)
			b.CredentialsExpiresAt = &end
		}
		if previous := poolProvider[b.PoolCode]; previous != "" && previous != provider {
			return "", "", fmt.Errorf("pool %s mixes providers", b.PoolCode)
		}
		poolProvider[b.PoolCode] = provider
		code := importedCode("ACC", string(provider)+"/"+b.ExternalAccountRef)
		a := accounts[code]
		if a == nil {
			a = &importedAccount{provider: provider, binding: b, keys: map[string]string{}}
			accounts[code] = a
		}
		if previous, exists := a.keys[b.Slot]; exists && previous != secret {
			return "", "", fmt.Errorf("conflicting keys for account %s slot %s", b.Name, b.Slot)
		}
		definition, _ := a.provider.Definition()
		if definition.QuotaPolicy == model.QuotaManual {
			if b.CredentialsExpiresAt == nil || a.binding.CredentialsExpiresAt == nil || !b.CredentialsExpiresAt.Equal(*a.binding.CredentialsExpiresAt) {
				return "", "", fmt.Errorf("explicit consistent credential expiry required for manual account %s", b.Name)
			}
		}
		a.keys[b.Slot] = secret
		found := false
		for _, member := range poolMembers[b.PoolCode] {
			if member == code {
				found = true
			}
		}
		if !found {
			poolMembers[b.PoolCode] = append(poolMembers[b.PoolCode], code)
		}
		return b.PoolCode, b.Slot, nil
	}
	var endpoints []importedEndpoint
	networks := map[id.ID]chainconfig.Network{}
	codes := make([]string, 0)
	if cfg != nil {
		for code := range cfg.Networks {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		n := cfg.Networks[code]
		var networkID id.ID
		var family, networkIdentifier string
		if err := q.QueryRow(ctx, `SELECT id,chain_family,network_identifier FROM cat_blockchain_networks WHERE code=$1 AND NOT deletion_mark AND _deleted_at IS NULL`, code).Scan(&networkID, &family, &networkIdentifier); err != nil {
			return report, fmt.Errorf("network %s unavailable", code)
		}
		networks[networkID] = n
		roles := make([]string, 0, len(n.Roles))
		for role := range n.Roles {
			roles = append(roles, string(role))
		}
		sort.Strings(roles)
		for _, role := range roles {
			for _, entry := range n.Endpoints(chainconfig.Role(role)) {
				target := code + "/" + role + "/" + entry.ProviderID
				provider, baseURL, secret, err := (*profiles.Registry)(nil).ImportRPC(entry.URL, entry.APIKey, role, bindings[target].Slot)
				if err != nil {
					return report, fmt.Errorf("invalid RPC credential mapping for %s: %w", target, err)
				}
				e := model.NewRPCEndpoint(importedCode("RPC", target), target)
				e.NetworkID = networkID
				e.Role = role
				e.ProviderID = entry.ProviderID
				e.TrustGroup = entry.TrustIdentity()
				e.URL = baseURL
				e.Priority = entry.Priority
				e.MinRequestIntervalMS = entry.MinRequestIntervalMS
				e.DisableKeepAlives = entry.DisableKeepAlives
				poolCode := ""
				if secret != "" {
					profileSlot := provider.RPCKeyName(model.RPCBinding{Family: family, NetworkIdentifier: networkIdentifier, Role: role, URL: baseURL})
					poolCode, e.Slot, err = add(target, provider, secret, profileSlot)
					if err != nil {
						return report, err
					}
				}
				if err = e.Validate(ctx); err != nil {
					return report, fmt.Errorf("invalid RPC configuration for %s", target)
				}
				if family == string(crypto.ChainFamilyEVM) && role != "rpc" || family == string(crypto.ChainFamilyTRON) && role == "rpc" {
					return report, fmt.Errorf("invalid RPC role for %s", target)
				}
				endpoints = append(endpoints, importedEndpoint{model: e, poolCode: poolCode})
			}
		}
	}
	type source struct {
		uid                 id.ID
		code, key, poolCode string
	}
	var sources []source
	rows, err := q.Query(ctx, `SELECT id,code,api_key FROM cat_rate_sources WHERE source_type='coingecko' AND api_key IS NOT NULL AND api_key<>'' AND NOT deletion_mark AND _deleted_at IS NULL ORDER BY code`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var s source
		if err = rows.Scan(&s.uid, &s.code, &s.key); err != nil {
			rows.Close()
			return report, err
		}
		sources = append(sources, s)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return report, err
	}
	for i := range sources {
		sources[i].poolCode, _, err = add("rateSource:"+sources[i].code, model.CoinGeckoDemo, sources[i].key, "")
		if err != nil {
			return report, err
		}
	}
	ar, pr, er := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo(), catalog_repo.NewCryptoRPCEndpointRepo()
	accountIDs := map[string]id.ID{}
	accountCodes := make([]string, 0, len(accounts))
	for code := range accounts {
		accountCodes = append(accountCodes, code)
	}
	sort.Strings(accountCodes)
	for _, code := range accountCodes {
		item := accounts[code]
		a, err := ar.GetByCode(ctx, code)
		if err != nil && !isMissing(err) {
			return report, err
		}
		if isMissing(err) {
			a = nil
		}
		if a == nil {
			a = model.NewAccount(code, item.binding.Name)
			a.Provider = item.provider
			a.CredentialsExpiresAt = item.binding.CredentialsExpiresAt
			a.ExternalAccountRef = item.binding.ExternalAccountRef
			credentials := &model.Credentials{Keys: []model.NamedKey{}}
			slots := make([]string, 0, len(item.keys))
			for slot := range item.keys {
				slots = append(slots, slot)
			}
			sort.Strings(slots)
			for _, slot := range slots {
				credentials.Keys = append(credentials.Keys, model.NamedKey{Name: slot, Value: item.keys[slot]})
			}
			a.PendingCredentials = credentials
			if err = a.Validate(ctx); err != nil {
				return report, err
			}
			if err = ar.Prepare(ctx, a); err != nil {
				return report, err
			}
			if err = ar.Create(ctx, a); err != nil {
				return report, err
			}
			report.Accounts++
		} else if a.Provider != item.provider || a.ExternalAccountRef != item.binding.ExternalAccountRef {
			return report, fmt.Errorf("imported account identity conflict")
		}
		if a.ManualPeriod() && (item.binding.CredentialsExpiresAt == nil || a.CredentialsExpiresAt == nil || !item.binding.CredentialsExpiresAt.Equal(*a.CredentialsExpiresAt)) {
			return report, fmt.Errorf("existing account period differs from import; renew explicitly")
		}
		// Re-import never overwrites a manually replaced key or clears the only copy.
		key, err := catalog_repo.IntegrationEncryptionKey()
		if err != nil {
			return report, err
		}
		raw, err := corecrypto.Decrypt(a.CredentialsEnc, key)
		if err != nil {
			return report, fmt.Errorf("existing account credentials unreadable")
		}
		var stored model.Credentials
		if json.Unmarshal(raw, &stored) != nil {
			return report, fmt.Errorf("existing account credentials invalid")
		}
		for slot, expected := range item.keys {
			actual, ok := stored.Key(slot)
			if !ok || actual != expected {
				return report, fmt.Errorf("existing account %s differs from import; reconcile explicitly", item.binding.Name)
			}
		}
		accountIDs[code] = a.ID
	}
	poolIDs := map[string]id.ID{}
	poolCodes := make([]string, 0, len(poolMembers))
	for code := range poolMembers {
		poolCodes = append(poolCodes, code)
	}
	sort.Strings(poolCodes)
	for _, code := range poolCodes {
		p, err := pr.GetByCode(ctx, code)
		if err != nil && !isMissing(err) {
			return report, err
		}
		if p == nil {
			p = model.NewPool(code, code)
			p.Provider = poolProvider[code]
			for _, a := range poolMembers[code] {
				p.AccountIDs = append(p.AccountIDs, accountIDs[a])
			}
			if err = p.Validate(ctx); err != nil {
				return report, err
			}
			if err = pr.ValidateMembers(ctx, p); err != nil {
				return report, err
			}
			if err = pr.Create(ctx, p); err != nil {
				return report, err
			}
			report.Pools++
		} else {
			if p.Provider != poolProvider[code] {
				return report, fmt.Errorf("imported pool provider conflict")
			}
			for _, a := range poolMembers[code] {
				found := false
				for _, uid := range p.AccountIDs {
					if uid == accountIDs[a] {
						found = true
					}
				}
				if !found {
					return report, fmt.Errorf("existing pool %s has different membership; edit explicitly", code)
				}
			}
		}
		poolIDs[code] = p.ID
	}
	for _, item := range endpoints {
		e := item.model
		if item.poolCode != "" {
			uid := poolIDs[item.poolCode]
			e.PoolID = &uid
		}
		existing, err := er.GetByCode(ctx, e.Code)
		if err != nil && !isMissing(err) {
			return report, err
		}
		if err == nil && existing != nil {
			continue
		}
		if err = er.Create(ctx, e); err != nil {
			return report, err
		}
		report.Endpoints++
	}
	for uid, n := range networks {
		evm, _ := json.Marshal(n.EVM)
		tron, _ := json.Marshal(n.TRON)
		if _, err = q.Exec(ctx, `INSERT INTO sys_crypto_rpc_network_settings(network_id,evm_settings,tron_settings) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, uid, evm, tron); err != nil {
			return report, err
		}
		if activate {
			tag, err := q.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='database',version=version+1 WHERE id=$1 AND rpc_config_source<>'database'`, uid)
			if err != nil {
				return report, err
			}
			report.ActivatedNetworks += int(tag.RowsAffected())
		}
	}
	for _, s := range sources {
		if _, err = q.Exec(ctx, `UPDATE cat_rate_sources SET integration_pool_id=$2,api_key=NULL,version=version+1 WHERE id=$1`, s.uid, poolIDs[s.poolCode]); err != nil {
			return report, err
		}
		report.RateSources++
	}
	return report, nil
}
func isMissing(err error) bool { return errors.Is(err, pgx.ErrNoRows) || apperror.IsNotFound(err) }
