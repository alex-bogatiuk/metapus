package tron

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestTRC20SweepReserveLiveAcceptance(t *testing.T) {
	rpcURL := os.Getenv("TRON_SWEEP_RESERVE_RPC_URL")
	callerAddress := os.Getenv("TRON_SWEEP_RESERVE_CALLER_ADDRESS")
	depositAddress := os.Getenv("TRON_SWEEP_RESERVE_DEPOSIT_ADDRESS")
	zeroBalanceAddress := os.Getenv("TRON_SWEEP_RESERVE_ZERO_ADDRESS")
	contractAddress := os.Getenv("TRON_SWEEP_RESERVE_CONTRACT_ADDRESS")
	if rpcURL == "" || callerAddress == "" || depositAddress == "" || zeroBalanceAddress == "" || contractAddress == "" {
		t.Skip("TRON sweep reserve live acceptance environment is not configured")
	}

	client := NewClient(ClientConfig{
		BaseURL: rpcURL,
		APIKey:  os.Getenv("TRON_SWEEP_RESERVE_API_KEY"),
		Timeout: 30 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	balance, err := client.GetTRC20BalanceFor(ctx, callerAddress, depositAddress, contractAddress)
	if err != nil {
		t.Fatalf("read deposit token balance: %v", err)
	}
	if balance.String() != "1" {
		t.Fatalf("deposit balance = %s raw, want exactly 1", balance.String())
	}

	nonZeroEstimate, err := client.EstimateEnergy(ctx, TriggerContractParams{
		OwnerAddress:    callerAddress,
		ContractAddress: contractAddress,
		ToAddress:       depositAddress,
		Amount:          big.NewInt(1),
	})
	if err != nil {
		t.Fatalf("estimate next non-zero to non-zero transfer: %v", err)
	}
	if nonZeroEstimate.EnergyRequired <= 0 {
		t.Fatalf("next transfer energy estimate = %d, want positive", nonZeroEstimate.EnergyRequired)
	}

	zeroBalance, err := client.GetTRC20BalanceFor(ctx, callerAddress, zeroBalanceAddress, contractAddress)
	if err != nil {
		t.Fatalf("read control token balance: %v", err)
	}
	if !zeroBalance.IsZero() {
		t.Fatalf("control balance = %s raw, want 0", zeroBalance.String())
	}
	zeroEstimate, err := client.EstimateEnergy(ctx, TriggerContractParams{
		OwnerAddress:    callerAddress,
		ContractAddress: contractAddress,
		ToAddress:       zeroBalanceAddress,
		Amount:          big.NewInt(1),
	})
	if err != nil {
		t.Fatalf("estimate zero to non-zero control transfer: %v", err)
	}
	if nonZeroEstimate.EnergyRequired >= zeroEstimate.EnergyRequired {
		t.Fatalf("non-zero recipient energy = %d, want less than zero recipient energy %d",
			nonZeroEstimate.EnergyRequired, zeroEstimate.EnergyRequired)
	}

	t.Logf("recipient balances: reserve=%s raw control=%s raw; estimated energy: non-zero=%d zero=%d",
		balance.String(), zeroBalance.String(), nonZeroEstimate.EnergyRequired, zeroEstimate.EnergyRequired)
}
