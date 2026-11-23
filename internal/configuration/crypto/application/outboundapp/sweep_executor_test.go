package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type fakeSweepAccountLifecycle struct {
	accountExists      bool
	accountExistsErr   error
	accountExistsCalls int
}

func TestGroupSweepTasksKeepsExistingExecutionDocumentsSeparate(t *testing.T) {
	tokenID, networkID, hotWalletID := id.New(), id.New(), id.New()
	firstSweepID, secondSweepID := id.New(), id.New()
	tasks := []SweepTask{
		{AttemptID: id.New(), SweepID: &firstSweepID, TokenID: tokenID, NetworkID: networkID, HotWalletID: hotWalletID},
		{AttemptID: id.New(), SweepID: &secondSweepID, TokenID: tokenID, NetworkID: networkID, HotWalletID: hotWalletID},
		{AttemptID: id.New(), TokenID: tokenID, NetworkID: networkID, HotWalletID: hotWalletID},
		{AttemptID: id.New(), TokenID: tokenID, NetworkID: networkID, HotWalletID: hotWalletID},
	}

	groups := GroupSweepTasks(tasks)
	if len(groups) != 4 {
		t.Fatalf("group count = %d, want 4", len(groups))
	}
	if len(groups[2]) != 1 {
		t.Fatalf("new draft group size = %d, want 1", len(groups[2]))
	}
}

func (c *fakeSweepAccountLifecycle) IsActive(context.Context, string) (bool, error) {
	c.accountExistsCalls++
	return c.accountExists, c.accountExistsErr
}

func (c *fakeSweepAccountLifecycle) BuildActivation(context.Context, crypto.TransferIntent, crypto.FeeQuote) (crypto.UnsignedTxEnvelope, error) {
	return crypto.UnsignedTxEnvelope{}, errors.New("not implemented")
}

type fakeSweepNetworkDriver struct{ accounts crypto.AccountLifecycle }

func (d fakeSweepNetworkDriver) Network() crypto.NetworkRef {
	return crypto.NetworkRef{Code: "TRON-NILE", Family: crypto.ChainFamilyTRON}
}
func (d fakeSweepNetworkDriver) Capabilities() crypto.ChainFamilyCapabilities {
	return crypto.ChainFamilyCapabilities{
		Family:               crypto.ChainFamilyTRON,
		WalletProvisioning:   crypto.WalletProvisioningDerivedWithActivation,
		RequiredRPCRoles:     []string{"indexer", "fullnode", "solidity"},
		ExternalResourceMode: crypto.ExternalResourceOptional,
		SequenceModel:        crypto.SequenceNone,
		CheckpointTimeUnit:   crypto.CheckpointUnixMillis,
		TokenAccountModel:    crypto.TokenAccountSameAddress,
		MinWatcherLagSeconds: 180,
	}
}
func (d fakeSweepNetworkDriver) Watchers() crypto.WatcherFactory             { return nil }
func (d fakeSweepNetworkDriver) Transactions() crypto.TransactionBuilder     { return nil }
func (d fakeSweepNetworkDriver) Broadcaster() crypto.Broadcaster             { return nil }
func (d fakeSweepNetworkDriver) Finality() crypto.FinalityReader             { return nil }
func (d fakeSweepNetworkDriver) Balances() crypto.BalanceReader              { return nil }
func (d fakeSweepNetworkDriver) Wallets() crypto.WalletDeriver               { return nil }
func (d fakeSweepNetworkDriver) Accounts() crypto.AccountLifecycle           { return d.accounts }
func (d fakeSweepNetworkDriver) TokenAccounts() crypto.TokenAccountLifecycle { return nil }
func (d fakeSweepNetworkDriver) Addresses() crypto.AddressCodec              { return nil }

func TestEnsureSweepSourceAccountActive(t *testing.T) {
	tests := []struct {
		name      string
		task      SweepTask
		exists    bool
		existsErr error
		wantErr   string
		wantCalls int
	}{
		{
			name: "rejects non-active wallet without rpc",
			task: SweepTask{
				ChainActivationStatus: wallet.ChainActivationUnknown,
				Address:               "TPool",
			},
			wantErr:   "not chain-active",
			wantCalls: 0,
		},
		{
			name: "rejects missing on-chain account",
			task: SweepTask{
				ChainActivationStatus: wallet.ChainActivationActive,
				Address:               "TPool",
			},
			exists:    false,
			wantErr:   "not active on TRON",
			wantCalls: 1,
		},
		{
			name: "propagates rpc error",
			task: SweepTask{
				ChainActivationStatus: wallet.ChainActivationActive,
				Address:               "TPool",
			},
			existsErr: errors.New("rpc unavailable"),
			wantErr:   "check sweep source account activation",
			wantCalls: 1,
		},
		{
			name: "accepts active visible account",
			task: SweepTask{
				ChainActivationStatus: wallet.ChainActivationActive,
				Address:               "TPool",
			},
			exists:    true,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &fakeSweepAccountLifecycle{
				accountExists:    tt.exists,
				accountExistsErr: tt.existsErr,
			}
			executor := &SweepExecutor{}

			err := executor.EnsureSourceAccountActive(context.Background(), fakeSweepNetworkDriver{accounts: accounts}, tt.task)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("EnsureSourceAccountActive() error = %v, want nil", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EnsureSourceAccountActive() error = %v, want containing %q", err, tt.wantErr)
			}
			if accounts.accountExistsCalls != tt.wantCalls {
				t.Fatalf("IsActive calls = %d, want %d", accounts.accountExistsCalls, tt.wantCalls)
			}
		})
	}
}

func TestIsUnrecoverableSigningError(t *testing.T) {
	tests := []struct {
		name string
		give error
		want bool
	}{
		{
			name: "nil error",
			give: nil,
			want: false,
		},
		{
			name: "transient network error",
			give: errors.New("vault HTTP 503: service unavailable"),
			want: false,
		},
		{
			name: "transaction expired error",
			give: errors.New("vault sign TRON envelope: vault HTTP 400: {\"errors\":[\"failed to sign transaction: transaction expired\"]}"),
			want: true,
		},
		{
			name: "transaction expiration exceeds authorization",
			give: errors.New("family signer rejected transaction: transaction expiration exceeds authorization"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnrecoverableSigningError(tt.give)
			if got != tt.want {
				t.Fatalf("IsUnrecoverableSigningError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUnsignedEnvelopeExpired(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		give crypto.UnsignedTxEnvelope
		want bool
	}{
		{
			name: "non-tron family",
			give: crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM},
			want: false,
		},
		{
			name: "tron with future expiration",
			give: crypto.UnsignedTxEnvelope{
				Family:  crypto.ChainFamilyTRON,
				Payload: []byte(fmt.Sprintf(`{"raw_data":{"expiration":%d}}`, now.Add(time.Minute).UnixMilli())),
			},
			want: false,
		},
		{
			name: "tron with past expiration",
			give: crypto.UnsignedTxEnvelope{
				Family:  crypto.ChainFamilyTRON,
				Payload: []byte(fmt.Sprintf(`{"raw_data":{"expiration":%d}}`, now.Add(-time.Minute).UnixMilli())),
			},
			want: true,
		},
		{
			name: "tron invalid payload",
			give: crypto.UnsignedTxEnvelope{
				Family:  crypto.ChainFamilyTRON,
				Payload: []byte(`not json`),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnsignedEnvelopeExpired(tt.give, now)
			if got != tt.want {
				t.Fatalf("IsUnsignedEnvelopeExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}
