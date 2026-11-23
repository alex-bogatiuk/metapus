package blockchain_network

import (
	"context"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

func TestBlockchainNetworkValidateExplorerURL(t *testing.T) {
	tests := []struct {
		name        string
		explorerURL string
		wantErr     bool
	}{
		{name: "empty disables explorer link", explorerURL: ""},
		{name: "valid transaction template", explorerURL: "https://nile.tronscan.org/#/transaction/{txHash}"},
		{name: "missing placeholder", explorerURL: "https://nile.tronscan.org", wantErr: true},
		{name: "duplicate placeholder", explorerURL: "https://example.com/{txHash}?tx={txHash}", wantErr: true},
		{name: "unknown placeholder", explorerURL: "https://example.com/{address}/{txHash}", wantErr: true},
		{name: "non HTTPS URL", explorerURL: "http://example.com/tx/{txHash}", wantErr: true},
		{name: "relative URL", explorerURL: "/tx/{txHash}", wantErr: true},
		{name: "credentials in URL", explorerURL: "https://user:pass@example.com/tx/{txHash}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := NewBlockchainNetwork("TRON-NILE", "TRON Nile Testnet", crypto.ChainFamilyTRON, "nile", "TRX", 6)
			network.ExplorerURL = tt.explorerURL

			err := network.Validate(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
