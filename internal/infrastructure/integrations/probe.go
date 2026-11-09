package integrations

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

type ConnectionProber struct {
	Accounts          model.RuntimeRepository
	Profiles          *profiles.Registry
	Base              http.RoundTripper
	EndpointTransport func(id.ID, string) func(http.RoundTripper) http.RoundTripper
}

func (p ConnectionProber) Probe(ctx context.Context, target model.ConnectionTarget) (bool, error) {
	profile, ok := p.Profiles.Lookup(target.Provider)
	if !ok {
		return false, apperror.NewValidation("Проверка этого типа выполняется его подсистемой")
	}
	var probe profiles.Probe
	if target.Provider.Supports(model.KindRPC) {
		if id.IsNil(target.EndpointID) {
			return false, apperror.NewValidation("Для проверки настройте RPC-подключение этого пула")
		}
		var err error
		probe, err = rpcProbe(target)
		if err != nil {
			return false, err
		}
	} else if profile.Probe != nil {
		probe = *profile.Probe
	} else {
		return false, apperror.NewValidation("Проверка этого типа выполняется его подсистемой")
	}
	base := p.Base
	if base == nil {
		base = http.DefaultTransport
	}
	var transport http.RoundTripper
	if !id.IsNil(target.EndpointID) {
		if p.EndpointTransport == nil {
			return false, apperror.NewInternal(errors.New("RPC endpoint transport factory is not configured"))
		}
		transport = p.EndpointTransport(target.EndpointID, target.URL)(base)
	} else {
		transport = &Transport{Base: base, Repository: p.Accounts, PoolID: target.PoolID, Provider: target.Provider, Profiles: p.Profiles}
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, probe.Method, probe.URL, strings.NewReader(probe.Body))
	if err != nil {
		return false, apperror.NewValidation("Некорректные параметры проверки подключения")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	return err == nil && len(body) <= 1<<20 && resp.StatusCode == http.StatusOK && probe.Valid(body), nil
}

// Probe semantics follow the chain protocol and configured network, independent
// of the service hosting the endpoint.
func rpcProbe(target model.ConnectionTarget) (profiles.Probe, error) {
	probe := profiles.Probe{Method: http.MethodPost, URL: target.URL}
	switch target.Family {
	case "evm":
		chain, ok := new(big.Int).SetString(target.NetworkIdentifier, 10)
		if !ok || chain.Sign() <= 0 || target.Role != "rpc" {
			return probe, apperror.NewValidation("Некорректная сеть для проверки RPC")
		}
		probe.Body = `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`
		probe.Valid = func(body []byte) bool {
			var v struct {
				Result string          `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(body, &v) != nil || !strings.HasPrefix(v.Result, "0x") || len(v.Error) > 0 && string(v.Error) != "null" {
				return false
			}
			actual, ok := new(big.Int).SetString(strings.TrimPrefix(v.Result, "0x"), 16)
			return ok && actual.Cmp(chain) == 0
		}
	case "tron":
		path := "/wallet/getnowblock"
		if target.Role == "solidity" {
			path = "/walletsolidity/getnowblock"
		} else if target.Role != "fullnode" && target.Role != "indexer" {
			return probe, apperror.NewValidation("Некорректная роль для проверки RPC")
		}
		probe.URL, probe.Body = strings.TrimRight(target.URL, "/")+path, `{}`
		probe.Valid = func(body []byte) bool {
			var v struct {
				BlockID     string          `json:"blockID"`
				Error       json.RawMessage `json:"Error"`
				LowerError  json.RawMessage `json:"error"`
				BlockHeader struct {
					RawData struct {
						Number int64 `json:"number"`
					} `json:"raw_data"`
				} `json:"block_header"`
			}
			return json.Unmarshal(body, &v) == nil && len(v.BlockID) == 64 && v.BlockHeader.RawData.Number > 0 && (len(v.Error) == 0 || string(v.Error) == "null") && (len(v.LowerError) == 0 || string(v.LowerError) == "null")
		}
	default:
		return probe, apperror.NewValidation("Проверка этого протокола не поддерживается")
	}
	return probe, nil
}
