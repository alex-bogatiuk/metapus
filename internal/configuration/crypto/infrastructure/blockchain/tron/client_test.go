package tron

import (
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"metapus/pkg/sdk/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTransferTransaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _createTxPath, r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "tron-key", r.Header.Get("TRON-PRO-API-KEY"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "TFrom", body["owner_address"])
		assert.Equal(t, "TTo", body["to_address"])
		assert.Equal(t, float64(1000), body["amount"])
		assert.Equal(t, true, body["visible"])

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"txID":         "tx-1",
			"raw_data_hex": "raw-hex",
			"raw_data":     map[string]any{"contract": []any{}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "tron-key"})
	tx, err := client.CreateTransferTransaction(context.Background(), "TFrom", "TTo", 1000)
	require.NoError(t, err)
	assert.Equal(t, "tx-1", tx.TxID)
	assert.Equal(t, "raw-hex", tx.RawDataHex)
	assert.True(t, tx.Visible)
}

func TestClientDisablesKeepAlivesForAffectedProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, _blockPath, r.URL.Path)
		require.True(t, r.Close)
		_, _ = w.Write([]byte(`{"block_header":{"raw_data":{"number":42}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, DisableKeepAlives: true})
	for range 5 {
		block, err := client.GetCurrentBlock(context.Background())
		require.NoError(t, err)
		require.EqualValues(t, 42, block)
	}
}

func TestCreateAccountTransaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _createAccountPath, r.URL.Path)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "THot", body["owner_address"])
		assert.Equal(t, "TPool", body["account_address"])
		assert.Equal(t, true, body["visible"])

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"txID":         "activation-tx",
			"raw_data_hex": "activation-raw",
			"raw_data":     map[string]any{"contract": []any{}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	tx, err := client.CreateAccountTransaction(context.Background(), "THot", "TPool")
	require.NoError(t, err)
	assert.Equal(t, "activation-tx", tx.TxID)
	assert.Equal(t, "activation-raw", tx.RawDataHex)
	assert.True(t, tx.Visible)
}

func TestTriggerSmartContract_EncodesTRC20Transfer(t *testing.T) {
	toHex := "1111111111111111111111111111111111111111"
	toBase58 := ConvertTronAddress(toHex)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _triggerPath, r.URL.Path)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "TOwner", body["owner_address"])
		assert.Equal(t, "TContract", body["contract_address"])
		assert.Equal(t, "transfer(address,uint256)", body["function_selector"])
		assert.Equal(t, float64(_defaultFeeLimitSun), body["fee_limit"])
		assert.Equal(t, true, body["visible"])

		expectedParameter := strings.Repeat("0", 24) + toHex + strings.Repeat("0", 62) + "7b"
		assert.Equal(t, expectedParameter, body["parameter"])

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"transaction": map[string]any{
				"txID":         "trc20-tx",
				"raw_data_hex": "trc20-raw",
				"raw_data":     map[string]any{"contract": []any{}},
				"visible":      true,
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	tx, err := client.TriggerSmartContract(context.Background(), TriggerContractParams{
		OwnerAddress:    "TOwner",
		ContractAddress: "TContract",
		ToAddress:       toBase58,
		Amount:          big.NewInt(123),
	})
	require.NoError(t, err)
	assert.Equal(t, "trc20-tx", tx.TxID)
	assert.Equal(t, "trc20-raw", tx.RawDataHex)
	assert.True(t, tx.Visible)
}

func TestEstimateEnergy_EncodesTRC20TransferAndFailsClosed(t *testing.T) {
	toHex := "1111111111111111111111111111111111111111"
	toBase58 := ConvertTronAddress(toHex)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, _estimateEnergyPath, r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "TOwner", body["owner_address"])
		assert.Equal(t, "TContract", body["contract_address"])
		assert.Equal(t, true, body["visible"])
		assert.Equal(t, strings.Repeat("0", 24)+toHex+strings.Repeat("0", 62)+"7b", body["parameter"])
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"result": true}, "energy_required": 14650})
	}))
	defer server.Close()
	client := NewClient(ClientConfig{BaseURL: server.URL})
	estimate, err := client.EstimateEnergy(context.Background(), TriggerContractParams{OwnerAddress: "TOwner", ContractAddress: "TContract", ToAddress: toBase58, Amount: big.NewInt(123)})
	require.NoError(t, err)
	assert.Equal(t, int64(14650), estimate.EnergyRequired)
}

func TestBroadcastTransaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _broadcastPath, r.URL.Path)

		var body SignedTransaction
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "tx-1", body.TxID)
		assert.Equal(t, "raw", body.RawDataHex)
		assert.Equal(t, []string{"sig"}, body.Signature)
		assert.True(t, body.Visible)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": true,
			"txid":   "tx-1",
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	result, err := client.BroadcastTransaction(context.Background(), SignedTransaction{
		TxID:       "tx-1",
		RawDataHex: "raw",
		RawData:    json.RawMessage(`{"contract":[]}`),
		Signature:  []string{"sig"},
		Visible:    true,
	})
	require.NoError(t, err)
	assert.True(t, result.Result)
	assert.Equal(t, "tx-1", result.TxID)
}

func TestBroadcastTransaction_RejectsIncompleteEnvelope(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "http://127.0.0.1"})

	_, err := client.BroadcastTransaction(context.Background(), SignedTransaction{
		TxID:       "tx-1",
		RawDataHex: "raw",
		RawData:    json.RawMessage(`{"contract":[]}`),
		Signature:  []string{"sig"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "visible=true")

	_, err = client.BroadcastTransaction(context.Background(), SignedTransaction{
		TxID:       "tx-1",
		RawDataHex: "raw",
		Signature:  []string{"sig"},
		Visible:    true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raw_data")
}

func TestGetTRC20Balance(t *testing.T) {
	ownerHex := "1111111111111111111111111111111111111111"
	ownerBase58 := ConvertTronAddress(ownerHex)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _triggerConstantPath, r.URL.Path)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, ownerBase58, body["owner_address"])
		assert.Equal(t, "TContract", body["contract_address"])
		assert.Equal(t, "balanceOf(address)", body["function_selector"])
		assert.Equal(t, true, body["visible"])
		assert.Equal(t, strings.Repeat("0", 24)+ownerHex, body["parameter"])

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"result": true},
			"constant_result": []string{
				strings.Repeat("0", 60) + "03e8",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	balance, err := client.GetTRC20Balance(context.Background(), ownerBase58, "TContract")
	require.NoError(t, err)
	assert.Equal(t, "1000", balance.String())
}

func TestGetTRC20BalanceForUsesSeparateCaller(t *testing.T) {
	accountHex := "1111111111111111111111111111111111111111"
	accountBase58 := ConvertTronAddress(accountHex)
	callerBase58 := ConvertTronAddress("2222222222222222222222222222222222222222")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, callerBase58, body["owner_address"])
		assert.Equal(t, strings.Repeat("0", 24)+accountHex, body["parameter"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":          map[string]any{"result": true},
			"constant_result": []string{strings.Repeat("0", 63) + "a"},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	balance, err := client.GetTRC20BalanceFor(context.Background(), callerBase58, accountBase58, "TContract")
	require.NoError(t, err)
	assert.Equal(t, "10", balance.String())
}

func TestTransactionInfoDecodesReceiptResult(t *testing.T) {
	var info TransactionInfo
	require.NoError(t, json.Unmarshal([]byte(`{
		"blockNumber": 123,
		"blockTimeStamp": 456,
		"fee": 1100000,
		"receipt": {"result": "FAILED"}
	}`), &info))
	assert.Equal(t, int64(1100000), info.Fee)
	assert.Equal(t, "FAILED", info.Receipt.Result)
	assert.Equal(t, int64(0), info.Receipt.EnergyUsageTotal)
}

func TestTransactionInfoDecodesResourceReceipt(t *testing.T) {
	var info TransactionInfo
	require.NoError(t, json.Unmarshal([]byte(`{"fee":345000,"receipt":{"result":"SUCCESS","energy_usage_total":14650,"energy_fee":0,"net_fee":345000,"net_usage":345}}`), &info))
	assert.Equal(t, int64(345000), info.Fee)
	assert.Equal(t, int64(14650), info.Receipt.EnergyUsageTotal)
	assert.Equal(t, int64(345000), info.Receipt.NetFee)
	assert.Equal(t, int64(345), info.Receipt.NetUsage)
}

func TestNormalizeTronAddress(t *testing.T) {
	hex20 := strings.Repeat("1", 40)
	base58 := ConvertTronAddress(hex20)

	assert.Equal(t, base58, NormalizeTronAddress(hex20))
	assert.Equal(t, base58, NormalizeTronAddress("41"+hex20))
	assert.Equal(t, base58, NormalizeTronAddress("0x41"+hex20))
	assert.Equal(t, base58, NormalizeTronAddress(base58))
}

func TestGetTRXBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, _accountPath, r.URL.Path)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "TNativeOwner", body["address"])
		assert.Equal(t, true, body["visible"])

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance": 42_000_000,
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	balance, err := client.GetTRXBalance(context.Background(), "TNativeOwner")
	require.NoError(t, err)
	assert.Equal(t, "42000000", balance.String())
}

func TestAccountExists(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want bool
	}{
		{name: "active account", resp: map[string]any{"address": "TNativeOwner", "balance": 0}, want: true},
		{name: "inactive account", resp: map[string]any{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, _accountPath, r.URL.Path)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(tt.resp)
			}))
			defer server.Close()

			client := NewClient(ClientConfig{BaseURL: server.URL})
			got, err := client.AccountExists(context.Background(), "TNativeOwner")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDoPost_RetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("temporary"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	var result BroadcastResult
	err := client.doPost(context.Background(), _broadcastPath, map[string]string{"x": "y"}, &result)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.True(t, result.Result)
}
