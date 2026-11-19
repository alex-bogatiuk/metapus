package outboundapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

type executionGuardKey struct{}
type leaseSigningGuard struct{ valid bool }

func (g *leaseSigningGuard) Run(ctx context.Context, work func(context.Context) error) error {
	if !g.valid {
		return apperror.NewConflict("stale execution lease")
	}
	return work(context.WithValue(ctx, executionGuardKey{}, true))
}
func (g *leaseSigningGuard) BeginDispatch(ctx context.Context, _ crypto.SigningAuthorization, _ string, work func(context.Context) error) error {
	return g.Run(ctx, work)
}

type delayedActivationSigner struct {
	familySignerStub
	t     *testing.T
	after func()
}

func (s *delayedActivationSigner) Sign(ctx context.Context, request crypto.PolicySigningRequest) (crypto.SignedTxEnvelope, error) {
	require.Nil(s.t, ctx.Value(executionGuardKey{}), "external signer must run outside the claim transaction")
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	signed, err := s.familySignerStub.Sign(ctx, request)
	if s.after != nil {
		s.after()
	}
	return signed, err
}

type delayedControlSigner struct {
	controlSignerStub
	t     *testing.T
	after func()
}

func (s *delayedControlSigner) SignAuthorization(ctx context.Context, tenant string, auth crypto.SigningAuthorization) (string, error) {
	require.Nil(s.t, ctx.Value(executionGuardKey{}))
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	result, err := s.controlSignerStub.SignAuthorization(ctx, tenant, auth)
	if s.after != nil {
		s.after()
	}
	return result, err
}

func TestActivationSigningFenceKeepsLateSignerEvidence(t *testing.T) {
	for _, loss := range []string{"before dispatch", "during authorize", "during sign"} {
		t.Run(loss, func(t *testing.T) {
			now := time.Now().UTC()
			auth := crypto.SigningAuthorization{SchemaVersion: crypto.SigningAuthorizationSchemaVersion, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeActivation, BasisType: "CryptoAccountActivationTask", BasisID: id.New(), Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: now.Add(time.Hour), NetworkID: id.New(), NetworkCode: "TEST", ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: "1", SourceAddress: "source", FeePayerAddress: "source", VaultKeyID: "key", DestinationAddress: "target", AssetStandard: "native", AmountMinorUnits: types.ZeroRawAmount(), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "fingerprint", PolicyVersion: crypto.SigningAuthorizationPolicyVersion(crypto.ChainFamilyEVM)}
			unsigned := crypto.UnsignedTxEnvelope{Family: auth.ChainFamily, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint}
			signed := crypto.SignedTxEnvelope{Family: auth.ChainFamily, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: "signed-hash", Payload: []byte("signed bytes")}
			record := SigningAuthorizationRecord{Authorization: auth, UnsignedTx: unsigned}
			store, custody := &signingStoreStub{}, &signingCustodyStub{}
			guard := &leaseSigningGuard{valid: loss != "before dispatch"}
			control := &delayedControlSigner{t: t}
			family := &delayedActivationSigner{t: t, familySignerStub: familySignerStub{store: store, result: signed}}
			if loss == "during authorize" {
				control.after = func() { guard.valid = false }
			}
			if loss == "during sign" {
				family.after = func() { guard.valid = false }
			}
			service, err := NewSigningAuthorizationService(store, control, custody, auth.TenantID, func() time.Time { return now })
			require.NoError(t, err)
			result, err := service.SignOrReuseGuarded(t.Context(), family, record, guard)
			if loss == "during sign" {
				require.NoError(t, err)
				require.Equal(t, signed, result)
				require.NotNil(t, store.signed)
				require.Equal(t, 1, family.calls)
				record.SignedTx = store.signed
				recovered, err := service.SignOrReuse(t.Context(), nil, record)
				require.NoError(t, err)
				require.Equal(t, signed, recovered)
			} else {
				require.Error(t, err)
				require.Nil(t, store.signed)
				require.Zero(t, family.calls)
			}
			if loss == "during authorize" {
				require.Equal(t, "immutable-jws", store.jws)
			}
			dispatched := false
			require.Error(t, runSigningDispatch(t.Context(), guard, auth, "broadcast", func(context.Context) error { dispatched = true; return nil }))
			require.False(t, dispatched)
		})
	}
}

func TestActivationNewSigningRequiresExplicitFence(t *testing.T) {
	store, custody, control := &signingStoreStub{}, &signingCustodyStub{}, &controlSignerStub{}
	service, err := NewSigningAuthorizationService(store, control, custody, id.New().String(), time.Now)
	require.NoError(t, err)
	_, err = service.SignOrReuse(t.Context(), nil, SigningAuthorizationRecord{Authorization: crypto.SigningAuthorization{Purpose: crypto.SigningPurposeActivation}})
	require.ErrorContains(t, err, "execution claim")
	require.Empty(t, custody.stages)
	require.Empty(t, control.requests)
	_, err = service.Create(t.Context(), SigningAttemptInput{Intent: crypto.TransferIntent{Purpose: crypto.SigningPurposeActivation}})
	require.ErrorContains(t, err, "execution claim")
}

type activationActorStore struct {
	ActivationRepository
	t                               *testing.T
	valid                           bool
	updates, broadcasts, recoveries int
	evidence                        *ActivationBroadcastEvidence
}

func (s *activationActorStore) GetActivationBroadcastEvidence(context.Context, AccountActivationTask, string) (ActivationBroadcastEvidence, bool, error) {
	if s.evidence != nil {
		return *s.evidence, true, nil
	}
	return ActivationBroadcastEvidence{}, false, nil
}

func TestActivationKnownTransactionRecoveryRebuildsFeeMetadataWithoutActorLease(t *testing.T) {
	evidence := ActivationBroadcastEvidence{AuthorizationID: "original-auth", ReservationID: id.New(), Fingerprint: "original-fingerprint", TxHash: "known-transaction"}
	store := &activationActorStore{t: t, valid: false, evidence: &evidence}
	fees := &activationEvidenceFees{t: t, reservation: crypto.FeeReservation{ID: evidence.ReservationID, Status: crypto.FeeReservationHeld}}
	operations := &activationRecoveryOperations{t: t}
	driver := &workflowDriver{t: t, receipt: crypto.ChainReceipt{Status: crypto.ReceiptPending}}
	executor := NewAccountActivationExecutor(ActivationConfig{Repository: store, Fees: fees, Operations: operations, Transactions: &workflowTx{}})
	require.NoError(t, executor.confirmBroadcast(t.Context(), driver, AccountActivationTask{ActivationID: id.New(), ClaimToken: id.New()}, evidence.TxHash))
	require.Equal(t, 1, fees.broadcasts)
	require.Equal(t, 1, store.broadcasts)
	require.Equal(t, 1, store.recoveries)
	require.Equal(t, 1, operations.recoveries)
	require.Zero(t, store.updates)
}

func (s *activationActorStore) PrepareAccountActivationState(context.Context, AccountActivationTask) error {
	if !s.valid {
		return apperror.NewConflict("stale actor")
	}
	return nil
}
func (s *activationActorStore) BeginAccountActivationDispatch(ctx context.Context, _ AccountActivationTask, _ crypto.SigningAuthorization, _ string) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	return nil
}
func (s *activationActorStore) ApplyAccountActivationClaim(context.Context, AccountActivationTask, ActivationClaimUpdate) error {
	s.updates++
	return nil
}
func (s *activationActorStore) PrepareActivationBroadcastRecovery(ctx context.Context, _ AccountActivationTask, evidence ActivationBroadcastEvidence) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	require.NotEmpty(s.t, evidence.AuthorizationID)
	require.NotEmpty(s.t, evidence.Fingerprint)
	s.recoveries++
	return nil
}
func (s *activationActorStore) MarkSigningAuthorizationBroadcast(ctx context.Context, _ string, _ string) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	s.broadcasts++
	return nil
}

type activationDispatchStore struct {
	ActivationRepository
	t      *testing.T
	valid  bool
	events []string
}

func (s *activationDispatchStore) PrepareAccountActivationState(ctx context.Context, _ AccountActivationTask) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	if !s.valid {
		return apperror.NewConflict("stale actor")
	}
	s.events = append(s.events, "fence")
	return nil
}
func (s *activationDispatchStore) BeginAccountActivationDispatch(ctx context.Context, _ AccountActivationTask, _ crypto.SigningAuthorization, phase string) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	s.events = append(s.events, "marker:"+phase)
	return nil
}

// The custody permit refuses a caller transaction (BeginAMLDispatch), so the
// guard must run it outside the claim transaction and commit the marker last.
func TestActivationDispatchRunsCustodyPermitOutsideClaimTransaction(t *testing.T) {
	store := &activationDispatchStore{t: t, valid: true}
	transactions := &workflowTx{}
	guard := activationExecutionGuard{repo: store, transactions: transactions, task: AccountActivationTask{ActivationID: id.New()}}
	err := guard.BeginDispatch(t.Context(), crypto.SigningAuthorization{}, "authorize", func(ctx context.Context) error {
		require.Nil(t, ctx.Value(workflowTxKey{}), "custody permit must not run inside the claim transaction")
		store.events = append(store.events, "permit")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"fence", "permit", "fence", "marker:authorize"}, store.events)
	require.Equal(t, 2, transactions.commits)
}

func TestActivationDispatchStopsWithoutMarkerOnStaleClaimOrPermitFailure(t *testing.T) {
	stale := &activationDispatchStore{t: t, valid: false}
	permitted := false
	err := activationExecutionGuard{repo: stale, transactions: &workflowTx{}}.BeginDispatch(t.Context(), crypto.SigningAuthorization{}, "sign", func(context.Context) error {
		permitted = true
		return nil
	})
	require.Error(t, err)
	require.False(t, permitted)
	require.Empty(t, stale.events)

	store := &activationDispatchStore{t: t, valid: true}
	err = activationExecutionGuard{repo: store, transactions: &workflowTx{}}.BeginDispatch(t.Context(), crypto.SigningAuthorization{}, "sign", func(context.Context) error {
		return apperror.NewConflict("AML dispatch scope was disabled")
	})
	require.ErrorContains(t, err, "AML dispatch scope was disabled")
	require.Equal(t, []string{"fence"}, store.events, "a refused permit must not record signing exposure")
}

type activationEvidenceFees struct {
	ActivationFees
	t           *testing.T
	reservation crypto.FeeReservation
	broadcasts  int
}

func (s *activationEvidenceFees) Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error) {
	return s.reservation, true, nil
}
func (s *activationEvidenceFees) MarkBroadcast(ctx context.Context, _ id.ID, _, _ string) error {
	require.NotNil(s.t, ctx.Value(workflowTxKey{}))
	s.broadcasts++
	return nil
}

type activationGuardedAuthorizations struct {
	Authorizations
	service *SigningAuthorizationService
}

func (s activationGuardedAuthorizations) CreateGuarded(ctx context.Context, input SigningAttemptInput, guard SigningExecutionGuard) (SigningAuthorizationRecord, error) {
	return s.service.CreateGuarded(ctx, input, guard)
}
func (s activationGuardedAuthorizations) SignOrReuseGuarded(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord, guard SigningExecutionGuard) (crypto.SignedTxEnvelope, error) {
	return s.service.SignOrReuseGuarded(ctx, signer, record, guard)
}
func (s activationGuardedAuthorizations) CheckCustodyDispatchGuarded(ctx context.Context, auth crypto.SigningAuthorization, _ *crypto.FamilySignerRegistry, guard SigningExecutionGuard) error {
	return guard.BeginDispatch(ctx, auth, "broadcast", func(context.Context) error { return nil })
}

type activationRecoveryOperations struct {
	ActivationOperations
	t          *testing.T
	recoveries int
}

func (s *activationRecoveryOperations) RecoverActivationBroadcast(ctx context.Context, _ AccountActivationTask, _ string) (id.ID, error) {
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	s.recoveries++
	return id.New(), nil
}

type activationBroadcastDriver struct {
	*workflowDriver
	after func()
	calls int
	hash  string
}

func (s *activationBroadcastDriver) Broadcaster() crypto.Broadcaster { return s }
func (s *activationBroadcastDriver) Broadcast(ctx context.Context, _ crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	require.Nil(s.t, ctx.Value(workflowTxKey{}))
	s.calls++
	s.after()
	return crypto.BroadcastOutcome{Status: crypto.BroadcastAccepted, TxHash: s.hash}, nil
}

func TestActivationActorKeepsEvidenceAfterLeaseLossDuringExternalIO(t *testing.T) {
	for _, phase := range []string{"sign", "broadcast"} {
		t.Run(phase, func(t *testing.T) {
			_, record, now := signingReplayFixture(t)
			record.Authorization.Purpose = crypto.SigningPurposeActivation
			record.Authorization.BasisType = "CryptoAccountActivationTask"
			record.Authorization.AmountMinorUnits = types.ZeroRawAmount()
			record.Authorization.ApprovalBundleDigest = ""
			record.JWS = "saved-jws"
			signed := crypto.SignedTxEnvelope{Family: record.UnsignedTx.Family, SchemaVersion: record.UnsignedTx.SchemaVersion, Generation: record.UnsignedTx.Generation, NetworkSequence: record.UnsignedTx.NetworkSequence, UnsignedFingerprint: record.UnsignedTx.UnsignedFingerprint, TxHash: "activation-result", Payload: []byte(`{"signed":true}`)}
			store := &activationActorStore{t: t, valid: true}
			signing := &signingStoreStub{jws: "saved-jws"}
			service, err := NewSigningAuthorizationService(signing, &controlSignerStub{}, &signingCustodyStub{}, record.Authorization.TenantID, func() time.Time { return now })
			require.NoError(t, err)
			family := &delayedActivationSigner{t: t, familySignerStub: familySignerStub{store: signing, result: signed}}
			driver := &activationBroadcastDriver{workflowDriver: &workflowDriver{t: t, network: crypto.NetworkRef{ID: record.Authorization.NetworkID, Family: crypto.ChainFamilyEVM}, receipt: crypto.ChainReceipt{Status: crypto.ReceiptPending}}, hash: signed.TxHash, after: func() { store.valid = false }}
			if phase == "sign" {
				family.after = func() { store.valid = false }
			} else {
				record.SignedTx = &signed
				signing.signed = &signed
			}
			fees := &activationEvidenceFees{t: t, reservation: crypto.FeeReservation{ID: id.New(), Status: crypto.FeeReservationReserved}}
			operations := &activationRecoveryOperations{t: t}
			executor := NewAccountActivationExecutor(ActivationConfig{Repository: store, Authorizations: activationGuardedAuthorizations{service: service}, Fees: fees, Operations: operations, Transactions: &workflowTx{}})
			task := AccountActivationTask{ActivationID: record.Authorization.BasisID, NetworkID: record.Authorization.NetworkID, ClaimToken: id.New()}
			err = executor.signAndBroadcastActivation(t.Context(), driver, task, record, fees.reservation, family)
			require.NotNil(t, signing.signed)
			require.Zero(t, store.updates)
			if phase == "sign" {
				require.Error(t, err)
				require.Zero(t, driver.calls)
				require.Zero(t, fees.broadcasts)
				require.Zero(t, store.recoveries)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, driver.calls)
				require.Equal(t, 1, fees.broadcasts)
				require.Equal(t, 1, store.broadcasts)
				require.Equal(t, 1, store.recoveries)
				require.Equal(t, 1, operations.recoveries)
			}
		})
	}
}
