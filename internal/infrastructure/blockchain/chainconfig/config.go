// Package chainconfig loads non-tenant chain endpoint topology. API key
// values are read from referenced secret files and are never JSON-serializable.
package chainconfig

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/integrations"
)

const maxConfigBytes = 4 << 20
const maxSecretBytes = 1 << 20

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type Role string

const (
	RoleIndexer  Role = "indexer"
	RoleFullNode Role = "fullnode"
	RoleSolidity Role = "solidity"
	RoleRPC      Role = "rpc"
)

type File struct {
	Networks map[string]Network `json:"networks"`
}

type Network struct {
	Roles map[Role][]Endpoint `json:"roles"`
	TRON  *TRONSettings       `json:"tron,omitempty"`
	EVM   *EVMSettings        `json:"evm,omitempty"`
}

func (n Network) Endpoints(role Role) []Endpoint { return n.Roles[role] }

func (n Network) RequireRoles(roles ...Role) error {
	for _, role := range roles {
		if len(n.Roles[role]) == 0 {
			return fmt.Errorf("network has no %s endpoint", role)
		}
	}
	return nil
}

type Endpoint struct {
	EndpointID           id.ID                                     `json:"endpointId,omitempty"`
	AdmissionScope       string                                    `json:"-"`
	ConfigVersion        int                                       `json:"configVersion,omitempty"`
	WrapTransport        func(http.RoundTripper) http.RoundTripper `json:"-"`
	ProviderID           string                                    `json:"providerId"`
	TrustGroup           string                                    `json:"trustGroup,omitempty"`
	Priority             int                                       `json:"priority"`
	URL                  string                                    `json:"url"`
	DisableKeepAlives    bool                                      `json:"disableKeepAlives,omitempty"`
	MinRequestIntervalMS int                                       `json:"minRequestIntervalMs,omitempty"`
	APIKeySecretFile     string                                    `json:"apiKeySecretFile,omitempty"`
	APIKey               string                                    `json:"-"`
}

func Load(path string) (*File, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("CRYPTO_CHAIN_CONFIG_FILE is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat chain config: %w", err)
	}
	if info.IsDir() || info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("chain config must be a file no larger than %d bytes", maxConfigBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chain config: %w", err)
	}
	raw = bytes.TrimPrefix(raw, utf8BOM)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg File
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode chain config: %w", err)
	}
	if len(cfg.Networks) == 0 {
		return nil, errors.New("chain config must contain at least one network")
	}
	for code, network := range cfg.Networks {
		trustByURL := map[string]string{}
		if strings.TrimSpace(code) == "" {
			return nil, errors.New("chain config contains an empty network code")
		}
		if network.TRON != nil {
			if err := network.TRON.Validate(); err != nil {
				return nil, fmt.Errorf("network %s: %w", code, err)
			}
		}
		if network.EVM != nil {
			if err := network.EVM.Validate(); err != nil {
				return nil, fmt.Errorf("network %s: %w", code, err)
			}
		}
		if len(network.Roles) == 0 {
			return nil, fmt.Errorf("network %s has no RPC roles", code)
		}
		for role, endpoints := range network.Roles {
			if strings.TrimSpace(string(role)) == "" {
				return nil, fmt.Errorf("network %s contains an empty RPC role", code)
			}
			if err := loadEndpoints(code, role, endpoints); err != nil {
				return nil, err
			}
			sort.SliceStable(endpoints, func(i, j int) bool {
				return endpoints[i].Priority < endpoints[j].Priority
			})
			network.Roles[role] = endpoints
			for _, endpoint := range endpoints {
				if endpoint.TrustGroup == "" {
					continue
				}
				if existing := trustByURL[endpoint.URL]; existing != "" && existing != endpoint.TrustGroup {
					return nil, fmt.Errorf("network %s assigns conflicting trust groups to an endpoint", code)
				}
				trustByURL[endpoint.URL] = endpoint.TrustGroup
			}
		}
		cfg.Networks[code] = network
	}
	return &cfg, nil
}

func loadEndpoints(networkCode string, role Role, endpoints []Endpoint) error {
	if len(endpoints) == 0 {
		return fmt.Errorf("network %s has no %s endpoint", networkCode, role)
	}
	providers := make(map[string]struct{}, len(endpoints))
	for i := range endpoints {
		endpoint := &endpoints[i]
		endpoint.ProviderID = strings.TrimSpace(endpoint.ProviderID)
		endpoint.TrustGroup = strings.ToLower(strings.TrimSpace(endpoint.TrustGroup))
		for _, c := range endpoint.TrustGroup {
			valid := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
			if !valid {
				return fmt.Errorf("network %s provider %s has invalid trustGroup", networkCode, endpoint.ProviderID)
			}
		}
		if endpoint.ProviderID == "" || endpoint.Priority < 0 {
			return fmt.Errorf("network %s %s endpoint requires providerId and non-negative priority", networkCode, role)
		}
		if endpoint.MinRequestIntervalMS < 0 || endpoint.MinRequestIntervalMS > 30000 || (endpoint.MinRequestIntervalMS > 0 && (endpoint.MinRequestIntervalMS < 100 || role != RoleRPC)) {
			return fmt.Errorf("network %s provider %s has invalid EVM minimum request interval", networkCode, endpoint.ProviderID)
		}
		if _, duplicate := providers[endpoint.ProviderID]; duplicate {
			return fmt.Errorf("network %s %s has duplicate provider %s", networkCode, role, endpoint.ProviderID)
		}
		providers[endpoint.ProviderID] = struct{}{}
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("network %s %s provider %s has invalid URL", networkCode, role, endpoint.ProviderID)
		}
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		endpoint.URL = strings.TrimRight(parsed.String(), "/")
		if endpoint.APIKeySecretFile == "" {
			continue
		}
		secret, err := readSecret(endpoint.APIKeySecretFile)
		if err != nil {
			return fmt.Errorf("network %s %s provider %s API key: %w", networkCode, role, endpoint.ProviderID, err)
		}
		endpoint.APIKey = secret
	}
	return nil
}

func readSecret(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Size() > maxSecretBytes {
		return "", fmt.Errorf("secret must be a file no larger than %d bytes", maxSecretBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	raw = bytes.TrimPrefix(raw, utf8BOM)
	secret := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if strings.IndexByte(secret, 0) >= 0 {
		return "", errors.New("secret contains a NUL byte")
	}
	return secret, nil
}

// TrustIdentity prevents separate API keys of one service from voting independently.
func (e Endpoint) TrustIdentity() string {
	return integrations.RPCTrustIdentity(e.URL, e.TrustGroup, e.ProviderID)
}
func (n Network) IndependentProviders(role Role) int {
	groups := map[string]bool{}
	for _, e := range n.Endpoints(role) {
		groups[e.TrustIdentity()] = true
	}
	return len(groups)
}
