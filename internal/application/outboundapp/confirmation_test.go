package outboundapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

type workflowTxKey struct{}
type workflowEffects struct{ pending []func() }
type workflowTx struct{ runs, commits int }

func (s *workflowTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	s.runs++
	effects := &workflowEffects{}
	if err := fn(context.WithValue(ctx, workflowTxKey{}, effects)); err != nil {
		return err
	}
	for _, apply := range effects.pending {
		apply()
	}
	s.commits++
	return nil
}
func stageWorkflowEffect(t *testing.T, ctx context.Context, apply func()) {
	t.Helper()
	effects, ok := ctx.Value(workflowTxKey{}).(*workflowEffects)
	require.True(t, ok, "financial effect must receive the transaction context")
	effects.pending = append(effects.pending, apply)
}

type workflowDriver struct {
	crypto.NetworkDriver
	t                       *testing.T
	network                 crypto.NetworkRef
	receipt                 crypto.ChainReceipt
	assessment              crypto.ReplacementAssessment
	submitted               []crypto.SignedTxEnvelope
	broadcast               crypto.BroadcastOutcome
	broadcastErr            error
	observedHashes          []string
	builds, divergenceHolds int
	replacement             crypto.UnsignedTxEnvelope
}

func (s *workflowDriver) Network() crypto.NetworkRef { return s.network }
func (*workflowDriver) Capabilities() crypto.ChainFamilyCapabilities {
	return crypto.ChainFamilyCapabilities{
		Family: crypto.ChainFamilyEVM, WalletProvisioning: crypto.WalletProvisioningUnsupported,
		RequiredRPCRoles: []string{"rpc"}, ExternalResourceMode: crypto.ExternalResourceNone,
		SequenceModel: crypto.SequenceAccount, CheckpointTimeUnit: crypto.CheckpointUnixSeconds,
		TokenAccountModel: crypto.TokenAccountSameAddress, SupportsReplacement: true,
	}
}
func (*workflowDriver) TokenAccounts() crypto.TokenAccountLifecycle { return nil }
func (s *workflowDriver) Finality() crypto.FinalityReader           { return s }
func (s *workflowDriver) Receipt(ctx context.Context, _ string) (crypto.ChainReceipt, error) {
	require.Nil(s.t, ctx.Value(workflowTxKey{}), "RPC must precede the financial transaction")
	return s.receipt, nil
}
func (*workflowDriver) FinalizedBlock(context.Context) (int64, string, error) { return 1, "block", nil }
func (s *workflowDriver) Broadcaster() crypto.Broadcaster                     { return s }
func (s *workflowDriver) Broadcast(ctx context.Context, signed crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	s.submitted = append(s.submitted, signed)
	return s.broadcast, s.broadcastErr
}
func (s *workflowDriver) AssessReplacement(ctx context.Context, _, _ string, hashes []string) (crypto.ReplacementAssessment, error) {
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	s.observedHashes = append([]string(nil), hashes...)
	return s.assessment, nil
}
func (s *workflowDriver) Transactions() crypto.TransactionBuilder { return s }
func (*workflowDriver) QuoteFee(context.Context, crypto.TransferIntent) (crypto.FeeQuote, error) {
	panic("unexpected new transaction")
}
func (*workflowDriver) Build(context.Context, crypto.TransferIntent, crypto.FeeQuote) (crypto.UnsignedTxEnvelope, error) {
	panic("unexpected new transaction")
}
func (s *workflowDriver) BuildReplacement(context.Context, crypto.TransferIntent, crypto.UnsignedTxEnvelope, types.CryptoAmount) (crypto.UnsignedTxEnvelope, crypto.FeeQuote, error) {
	s.builds++
	return s.replacement, crypto.FeeQuote{MaxFee: types.NewCryptoAmountFromInt64(9)}, nil
}
func (s *workflowDriver) HoldNonceDivergence(context.Context, string, string) error {
	s.divergenceHolds++
	return nil
}
func newWorkflowDriver(t *testing.T) (*workflowDriver, *crypto.ChainDriverRegistry) {
	t.Helper()
	driver := &workflowDriver{t: t, network: crypto.NetworkRef{
		ID: id.New(), Code: "WORKFLOW", Family: crypto.ChainFamilyEVM, NetworkIdentifier: "31337",
		NativeTokenID: id.New(), NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: crypto.FinalityByConfirmations,
	}}
	registry := crypto.NewChainDriverRegistry()
	require.NoError(t, registry.Register(driver))
	return driver, registry
}

type settlementStoreStub struct {
	SettlementRepository
	t                        *testing.T
	recorded, held, released int
	admissionErr             error
}

func (s *settlementStoreStub) PrepareWithdrawalSettlement(ctx context.Context, _ SettlementTask, _ crypto.ChainReceipt) (bool, error) {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	return s.admissionErr == nil, s.admissionErr
}

type withdrawalEvidenceStub struct{}

func (withdrawalEvidenceStub) VerifyWithdrawal(context.Context, crypto.NetworkDriver, SettlementTask, crypto.ChainReceipt) (id.ID, error) {
	return id.New(), nil
}

func (s *settlementStoreStub) RecordOutboundSettlement(ctx context.Context, _ SettlementTask, _ crypto.ChainReceipt) error {
	stageWorkflowEffect(s.t, ctx, func() { s.recorded++ })
	return nil
}
func (s *settlementStoreStub) HoldOutboundSettlementClaim(ctx context.Context, _ SettlementTask, _ error) error {
	require.Nil(s.t, ctx.Value(workflowTxKey{}), "the failed claim hold must survive rollback")
	s.held++
	return nil
}
func (s *settlementStoreStub) ReleaseOutboundSettlementClaim(context.Context, SettlementTask) error {
	s.released++
	return nil
}

type settlementFeeStub struct {
	t                  *testing.T
	reservation        crypto.FeeReservation
	settled, incidents int
	calls              int
}

func (s *settlementFeeStub) Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error) {
	return s.reservation, true, nil
}
func (s *settlementFeeStub) Settle(ctx context.Context, _ id.ID, receipt crypto.ChainReceipt) error {
	s.calls++
	if receipt.Fee.TotalFee.Cmp(s.reservation.MaxFee) > 0 {
		require.Nil(s.t, ctx.Value(workflowTxKey{}), "cap incident must survive independently")
		s.incidents++
		return ErrFeeReservationHeld
	}
	stageWorkflowEffect(s.t, ctx, func() { s.settled++ })
	return nil
}

func TestSettlementAdmissionFailurePrecedesFeeAndFinancialWrites(t *testing.T) {
	driver, drivers := newWorkflowDriver(t)
	task := SettlementTask{Kind: SettlementWithdrawal, DocumentID: id.New(), RequestID: id.New(), NetworkID: driver.network.ID, TxHash: "saved-effect"}
	driver.receipt = crypto.ChainReceipt{NetworkID: task.NetworkID, TxHash: task.TxHash, Status: crypto.ReceiptReverted, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized}
	failed := errors.New("resource prefix unavailable")
	txm := &workflowTx{}
	store := &settlementStoreStub{t: t, admissionErr: failed}
	fees := &settlementFeeStub{t: t, reservation: crypto.FeeReservation{ID: id.New(), MaxFee: types.MustCryptoAmount("10")}}
	service := NewSettlementService(SettlementConfig{WithdrawalEvidence: withdrawalEvidenceStub{}, Drivers: drivers, Repository: store, Fees: fees, Transactions: txm})
	require.ErrorIs(t, service.Settle(t.Context(), task), failed)
	require.Zero(t, fees.calls)
	require.Zero(t, store.recorded)
	require.Zero(t, txm.commits)
}

type settlementResourceStub struct {
	ResourcePreparer
	t       *testing.T
	settled int
}

func (s *settlementResourceStub) SettleResource(ctx context.Context, _ string, _ crypto.ChainReceipt) error {
	stageWorkflowEffect(s.t, ctx, func() { s.settled++ })
	return nil
}

type workflowResources struct{ preparer ResourcePreparer }

func (s workflowResources) Resolve(crypto.ChainFamily) ResourcePreparer { return s.preparer }

type settlementWebhookStub struct {
	t      *testing.T
	err    error
	events []crypto.LifecycleWebhookEvent
}

func (s *settlementWebhookStub) PublishLifecycleWebhook(ctx context.Context, event crypto.LifecycleWebhookEvent) error {
	if s.err != nil {
		return s.err
	}
	stageWorkflowEffect(s.t, ctx, func() { s.events = append(s.events, event) })
	return nil
}

func TestSettlementKeepsFeesDocumentResourceAndWebhookAtomic(t *testing.T) {
	webhookFailure := errors.New("outbox unavailable")
	tests := []struct {
		name                                            string
		status                                          crypto.ReceiptStatus
		webhookErr                                      error
		fee                                             int64
		wantCommit, wantHold, wantRelease, wantIncident int
		wantEvent                                       crypto.WebhookEventType
		wantErr                                         error
	}{
		{name: "success", status: crypto.ReceiptSucceeded, fee: 2, wantCommit: 1, wantEvent: crypto.WebhookWithdrawalConfirmed},
		{name: "reverted still settles fee", status: crypto.ReceiptReverted, fee: 2, wantCommit: 1, wantEvent: crypto.WebhookWithdrawalFailed},
		{name: "webhook failure rolls back all effects", status: crypto.ReceiptSucceeded, fee: 2, webhookErr: webhookFailure, wantHold: 1, wantErr: webhookFailure},
		{name: "pending releases claim without effects", status: crypto.ReceiptPending, fee: 2, wantRelease: 1},
		{name: "fee cap incident survives independently", status: crypto.ReceiptSucceeded, fee: 20, wantIncident: 1, wantErr: ErrFeeReservationHeld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, drivers := newWorkflowDriver(t)
			task := SettlementTask{Kind: SettlementWithdrawal, DocumentID: id.New(), RequestID: id.New(), MerchantID: id.New(), NetworkID: driver.network.ID, TxHash: "saved-effect"}
			driver.receipt = crypto.ChainReceipt{NetworkID: task.NetworkID, TxHash: task.TxHash, Status: tt.status,
				Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized,
				Fee: crypto.FeeSettlementReport{TotalFee: types.NewCryptoAmountFromInt64(tt.fee)}}
			txm := &workflowTx{}
			store := &settlementStoreStub{t: t}
			fees := &settlementFeeStub{t: t, reservation: crypto.FeeReservation{ID: id.New(), MaxFee: types.NewCryptoAmountFromInt64(10)}}
			resources := &settlementResourceStub{t: t}
			webhooks := &settlementWebhookStub{t: t, err: tt.webhookErr}
			now := time.Unix(1000, 0).UTC()
			service := NewSettlementService(SettlementConfig{WithdrawalEvidence: withdrawalEvidenceStub{}, Drivers: drivers, Repository: store, Fees: fees, Resources: workflowResources{resources}, Transactions: txm, Webhooks: webhooks, Now: func() time.Time { return now }})
			err := service.Settle(t.Context(), task)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCommit, store.recorded)
			require.Equal(t, tt.wantCommit, fees.settled)
			require.Equal(t, tt.wantCommit, resources.settled)
			require.Len(t, webhooks.events, tt.wantCommit)
			require.Equal(t, tt.wantCommit, txm.commits)
			require.Equal(t, tt.wantHold, store.held)
			require.Equal(t, tt.wantRelease, store.released)
			require.Equal(t, tt.wantIncident, fees.incidents)
			if tt.wantCommit > 0 {
				require.Equal(t, tt.wantEvent, webhooks.events[0].Event)
				require.Equal(t, now, webhooks.events[0].OccurredAt)
			}
			if tt.wantRelease > 0 || tt.wantIncident > 0 {
				require.Zero(t, txm.runs)
			}
		})
	}
}
