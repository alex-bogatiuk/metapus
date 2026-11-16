package crypto_integration_repo

import (
	"math/big"
	"net/http"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/cryptointegrations"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

// RPCProbe builds the connection check of an RPC endpoint. Probe semantics follow the chain protocol and configured network, independent
// of the service hosting the endpoint.
func RPCProbe(target model.ConnectionTarget) (profiles.Probe, error) {
	probe := profiles.Probe{Method: http.MethodPost, URL: target.URL}
	if !cryptointegrations.Catalog().Supports(target.Provider, cryptointegrations.KindRPC) {
		return probe, apperror.NewValidation("Проверка этого типа выполняется его подсистемой")
	}
	if id.IsNil(target.EndpointID) {
		return probe, apperror.NewValidation("Для проверки настройте RPC-подключение этого пула")
	}
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
