//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type amlRoutingFixture struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	input EnqueueAMLInput
	repo  *AMLRepository
}

func newAMLRoutingFixture(t *testing.T, baseline bool, family ...crypto.ChainFamily) amlRoutingFixture {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	input, unused := seedAMLJob(t, ctx, pool)
	if len(family) > 0 && family[0] == crypto.ChainFamilyEVM {
		// Identity is selected before enabling any routing scope.
		_, err := pool.Exec(ctx, `UPDATE cat_blockchain_networks SET chain_family='evm',network_identifier='987654321' WHERE id=$1`, input.Request.NetworkID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE cat_tokens SET token_standard='erc20' WHERE id=$1`, input.Request.TokenID)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `UPDATE reg_crypto_aml_jobs SET status='needs_attention' WHERE id=$1`, unused)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, input.Request.NetworkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_risk_policies(version,low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals)
 VALUES('routing-v1',3000,7000,3600,600,ARRAY['wallet','sanctions'],ARRAY['sanctions'])`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 VALUES($1,$2,$3,TRUE,'routing-v1',$4,'test-mapping-v1')`, input.Request.MerchantID, input.Request.NetworkID, input.Request.TokenID, input.ProviderAccountID)
	require.NoError(t, err)
	f := amlRoutingFixture{ctx, pool, input, NewAMLRepository()}
	if baseline {
		f.baseline(t, input.WalletID)
	}
	return f
}
func (f amlRoutingFixture) baseline(t *testing.T, walletID id.ID) {
	t.Helper()
	runID, checkID := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_runs(id,network_id,status,completed_at) VALUES($1,$2,'completed',NOW())`, runID, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_reconciliation_checks(id,run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount) VALUES($1,$2,$3,$4,$5,0,0,0)`, checkID, runID, f.input.Request.NetworkID, walletID, f.input.Request.TokenID)
	require.NoError(t, err)
	require.NoError(t, f.repo.RegisterAMLWalletBaseline(f.ctx, walletID, f.input.Request.TokenID, checkID, 1, "empty-checkpoint", "synthetic independent empty checkpoint", "integration-test"))
}
func (f amlRoutingFixture) proof(to, tx string, matching int) crypto.TransferVerification {
	event := crypto.BlockchainEvent{NetworkID: f.input.Request.NetworkID, TxHash: tx, EventKind: crypto.EventKindContractLog, EventIndex: 0, FromAddress: "payer", ToAddress: to, TokenContract: "test-contract", Amount: types.MustCryptoAmount("100"), BlockNumber: 10, BlockHash: strings.Repeat("a", 64), Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, Confirmations: 20, RequiredConfs: 1, EventType: crypto.EventTypeTransfer, Direction: crypto.TransferDirectionIncoming, Timestamp: time.Now().UTC()}
	sources := []crypto.VerificationSource{{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "same-digest", HeadHeight: 29, HeadHash: "same-head"}, {ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "same-digest", HeadHeight: 29, HeadHash: "same-head"}}
	return crypto.TransferVerification{Event: event, Sources: sources, DecoderVersion: crypto.TransferEvidenceSchemaV1, MatchingEffects: matching, VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
}
func (f amlRoutingFixture) save(t *testing.T, proof crypto.TransferVerification) (id.ID, id.ID) {
	t.Helper()
	proofID, err := NewBlockIngestionRepo().SaveVerification(f.ctx, proof)
	require.NoError(t, err)
	var effectID id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT id FROM reg_crypto_aml_effects WHERE tx_hash=$1 AND wallet_id=(SELECT id FROM cat_wallets WHERE network_id=$2 AND address=$3)`, proof.Event.TxHash, proof.Event.NetworkID, proof.Event.ToAddress).Scan(&effectID))
	return proofID, effectID
}
func (f amlRoutingFixture) postPayment(t *testing.T, proof crypto.TransferVerification, proofID, walletID id.ID) {
	t.Helper()
	invoiceID, paymentID := id.New(), id.New()
	event := proof.Event
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,expires_at) VALUES($1,$2,$3,NOW(),$4,$5,$6,$7,NOW()+interval '1 hour')`, invoiceID, "ci_"+id.Compact(invoiceID), invoiceID.String(), f.input.Request.MerchantID, f.input.Request.TokenID, walletID, event.Amount)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,event_index,amount,block_hash,block_number,finality_status,canonicality,status,posted,posted_version,verification_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$12,$10,10,'solidified','canonical','settled',TRUE,1,$11)`, paymentID, invoiceID, f.input.Request.MerchantID, event.NetworkID, f.input.Request.TokenID, walletID, event.TxHash, event.EventKind, event.EventIndex, event.BlockHash, proofID, event.Amount)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'CryptoPayment',1,NOW(),'receipt',$2,$3,$4)`, paymentID, walletID, f.input.Request.TokenID, event.Amount)
	require.NoError(t, err)
}
func (f amlRoutingFixture) complete(t *testing.T, effectID id.ID, score aml.ScoreBP, coverage bool) {
	t.Helper()
	job, ok, err := f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, aml.JobSubmitting, job.Status)
	var expectedSubject id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT subject_id FROM reg_crypto_aml_effect_checks WHERE effect_id=$1`, effectID).Scan(&expectedSubject))
	require.Equal(t, expectedSubject, job.SubjectID)
	ticket := aml.Ticket{Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: id.New().String(), RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, SubmitRequestID: job.SubmitRequestID, Request: job.Request, SubmittedAt: time.Now()}
	require.NoError(t, f.repo.SaveAMLTicket(f.ctx, job, ticket, time.Now().Add(-time.Second)))
	job, ok, err = f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, aml.JobPolling, job.Status)
	report := aml.Report{ID: id.New(), Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: ticket.CheckHash, RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, ContractVersion: "synthetic-v1", AdapterVersion: "synthetic-adapter", Score: &score, CoverageAccepted: coverage, Signals: []aml.Signal{{Name: "wallet", Value: "1"}, {Name: "sanctions", Value: "0"}}, CompletedAt: time.Now().Add(-time.Second), ReceivedAt: time.Now(), RawResponse: []byte(`{"synthetic":true}`)}
	require.NoError(t, f.repo.SaveAMLReport(f.ctx, job, report))
	applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.True(t, applied)
}
func (f amlRoutingFixture) rebuild(t *testing.T) {
	t.Helper()
	for range 100 {
		done, err := f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
		require.NoError(t, err)
		if !done {
			break
		}
	}
	for range 100 {
		done, err := f.repo.RebuildNextAMLRiskProjection(f.ctx, f.databaseNow(t))
		require.NoError(t, err)
		if !done {
			return
		}
	}
	t.Fatal("projection queue did not drain")
}
func (f amlRoutingFixture) class(t *testing.T, walletID id.ID) aml.Class {
	t.Helper()
	var class aml.Class
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT risk_class FROM reg_crypto_wallet_risk_states WHERE wallet_id=$1 AND token_id=$2`, walletID, f.input.Request.TokenID).Scan(&class))
	return class
}

func TestAMLRoutingVerifiedReceiptIsCapturedOnceAndLowNeedsCoverage(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "incoming-one", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, proof)
	require.NoError(t, err)
	var effects, jobs int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_effects`).Scan(&effects))
	require.Equal(t, 1, effects)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_jobs WHERE status<>'needs_attention'`).Scan(&jobs))
	require.Equal(t, 1, jobs)
	var before int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements`).Scan(&before))
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "late intelligence update", "integration-test")
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.complete(t, effectID, 9000, true)
	f.rebuild(t)
	require.Equal(t, aml.High, f.class(t, f.input.WalletID))
	var after int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements`).Scan(&after))
	require.Equal(t, before, after, "risk changes cannot create or reverse money")
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "stale operator version", "integration-test")
	require.ErrorContains(t, err, "generation changed")
	for range 100 {
		done, err := f.repo.PublishNextAMLRiskEvent(f.ctx)
		require.NoError(t, err)
		if !done {
			break
		}
	}
	var outbox, events int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_outbox WHERE event_type='wallet.risk_changed'`).Scan(&outbox))
	require.Positive(t, outbox)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_merchant_events WHERE event_type='wallet.risk_changed'`).Scan(&events))
	require.Equal(t, outbox, events)
	done, err := f.repo.PublishNextAMLRiskEvent(f.ctx)
	require.NoError(t, err)
	require.False(t, done)
	var raw []byte
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT payload FROM sys_merchant_events ORDER BY created_at DESC LIMIT 1`).Scan(&raw))
	require.True(t, json.Valid(raw))
	require.NotContains(t, string(raw), "payment.confirmed")
}

func TestAMLRoutingDoesNotCoverLegacyOrUnmatchedMoneyWithEqualScoreAmount(t *testing.T) {
	t.Run("missing baseline", func(t *testing.T) {
		f := newAMLRoutingFixture(t, false)
		proof := f.proof(f.input.Request.Address, "without-baseline", 1)
		proofID, effectID := f.save(t, proof)
		f.postPayment(t, proof, proofID, f.input.WalletID)
		f.complete(t, effectID, 1000, true)
		f.rebuild(t)
		require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	})
	t.Run("unmatched income despite equal checked amount", func(t *testing.T) {
		f := newAMLRoutingFixture(t, true)
		proof := f.proof(f.input.Request.Address, "known-posted", 1)
		proofID, effectID := f.save(t, proof)
		f.postPayment(t, proof, proofID, f.input.WalletID)
		f.complete(t, effectID, 1000, true)
		f.rebuild(t)
		require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
		_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'UnmatchedLegacyReceipt',1,NOW(),'receipt',$2,$3,100)`, id.New(), f.input.WalletID, f.input.Request.TokenID)
		require.NoError(t, err)
		require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "new money invalidates the projection in the money transaction")
		second := f.proof(f.input.Request.Address, "checked-but-not-posted", 1)
		_, secondID := f.save(t, second)
		f.complete(t, secondID, 1000, true)
		f.rebuild(t)
		require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "200 checked units cannot stand in for a different 200-unit financial inventory")
	})
}

func TestAMLRoutingAmbiguousCoverageAndExpiredDecisionRemainUnknown(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "multi-effect", 2)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 0, false)
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	_, err := f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "independent scope reviewed", "test-operator")
	require.NoError(t, err)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "provider coverage cannot override ambiguous chain coverage")
	fresh := newAMLRoutingFixture(t, true)
	single := fresh.proof(fresh.input.Request.Address, "expiring", 1)
	singleProof, singleEffect := fresh.save(t, single)
	fresh.postPayment(t, single, singleProof, fresh.input.WalletID)
	fresh.complete(t, singleEffect, 1000, true)
	fresh.rebuild(t)
	require.Equal(t, aml.Low, fresh.class(t, fresh.input.WalletID))
	done, err := fresh.repo.RebuildNextAMLRiskProjection(fresh.ctx, time.Now().Add(2*time.Hour))
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, aml.Unknown, fresh.class(t, fresh.input.WalletID))
	require.ErrorIs(t, RequireAMLTransferPlan(f.ctx, crypto.TransferIntent{SourceWalletID: f.input.WalletID, Network: crypto.NetworkRef{ID: f.input.Request.NetworkID}, Asset: crypto.AssetRef{TokenID: f.input.Request.TokenID, ContractAddress: "test-contract"}}), ErrAMLRiskContractRequired)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_withdrawal_requests(id,merchant_id,token_id,amount,dest_address) VALUES($1,$2,$3,1,'external-wallet')`, id.New(), f.input.Request.MerchantID, f.input.Request.TokenID)
	require.ErrorContains(t, err, "explicit risk class")
}

func (f amlRoutingFixture) collector(t *testing.T, merchant id.ID, address string) id.ID {
	t.Helper()
	walletID := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,custody_merchant_id,address,vault_key_id,derivation_path,wallet_role,tier,chain_activation_status)
 VALUES($1,$2,'Risk collector',$3,$4,$5,$2,$2,'liquidity_hot','hot','active')`, walletID, walletID.String(), f.input.Request.NetworkID, merchant, address)
	require.NoError(t, err)
	return walletID
}

func TestAMLRoutingLateHighFollowsPhysicalSourceWithoutCrossingMerchants(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	deposit := f.proof(f.input.Request.Address, "original-deposit", 1)
	proofID, effectID := f.save(t, deposit)
	f.postPayment(t, deposit, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	collector := f.collector(t, f.input.Request.MerchantID, "own-collector")
	f.baseline(t, collector)
	source := f.input.Request.Address
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.input.Request.MerchantID,
		Purpose: crypto.SigningPurposeSweep, BasisType: "CryptoSweepAttempt", BasisID: id.New(), Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour), NetworkID: f.input.Request.NetworkID, NetworkCode: "RISK-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: source, FeePayerAddress: source, VaultKeyID: "test-key", DestinationAddress: "own-collector", AssetStandard: "trc20", ContractAddress: "test-contract", AmountMinorUnits: types.MustCryptoAmount("100"), MaxNetworkFeeMinor: types.MustCryptoAmount("1"), UnsignedFingerprint: "historical-fingerprint", PolicyVersion: "tron-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, Payload: []byte(`{"historical":true}`)}
	signingID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	txHash := "controlled-sweep"
	signed, _ := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, TxHash: txHash, Payload: []byte(`{"historical":true}`)})
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',tx_hash=$2,signed_envelope=$3::jsonb WHERE id=$1`, signingID, txHash, string(signed))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash) VALUES($1,0,$2,'contract_log',$3,'own-collector',$4,'test-contract',100,$5)`, signingID, f.input.Request.NetworkID, source, f.input.Request.TokenID, txHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,claimed_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,'own-collector',100,'broadcast',NOW(),NOW()+interval '1 hour')`, auth.BasisID, f.input.WalletID, f.input.Request.TokenID, f.input.Request.NetworkID, collector, source)
	require.NoError(t, err)
	internal := f.proof("own-collector", txHash, 1)
	internal.Event.FromAddress = source
	internal.Event.Direction = crypto.TransferDirectionInternal
	_, internalEffect := f.save(t, internal)
	var linked *id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT source_wallet_id FROM reg_crypto_aml_effects WHERE id=$1`, internalEffect).Scan(&linked))
	require.NotNil(t, linked)
	require.Equal(t, f.input.WalletID, *linked)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_effect_checks WHERE effect_id=$1`, internalEffect).Scan(&count))
	require.Zero(t, count, "a controlled internal transfer inherits source evidence without another paid submit")
	sweepID := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweeps(accounting_model_version,id,token_id,hot_wallet_id,total_amount,posted,posted_version,status) VALUES(1,$1,$2,$3,100,TRUE,1,'broadcast')`, sweepID, f.input.Request.TokenID, collector)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_sweep_lines(line_id,document_id,wallet_id,amount,tx_hash) VALUES($1,$2,$3,100,$4)`, auth.BasisID, sweepID, f.input.WalletID, txHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'CryptoSweep',1,NOW(),'expense',$2,$4,100),($1,'CryptoSweep',1,NOW(),'receipt',$3,$4,100)`, sweepID, f.input.WalletID, collector, f.input.Request.TokenID)
	require.NoError(t, err)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, collector))
	// A second merchant has an independent low inventory in the same network.
	merchantB := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Unrelated merchant')`, merchantB, merchantB.String())
	require.NoError(t, err)
	bWallet := f.collector(t, merchantB, "merchant-b")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version) VALUES($1,$2,$3,TRUE,'routing-v1',$4,'test-mapping-v1')`, merchantB, f.input.Request.NetworkID, f.input.Request.TokenID, f.input.ProviderAccountID)
	require.NoError(t, err)
	b := f
	b.input = f.input
	b.input.WalletID = bWallet
	b.input.Request.MerchantID = merchantB
	b.input.Request.Address = "merchant-b"
	b.baseline(t, bWallet)
	bProof := b.proof("merchant-b", "b-deposit", 1)
	bProofID, bEffect := b.save(t, bProof)
	b.postPayment(t, bProof, bProofID, bWallet)
	b.complete(t, bEffect, 1000, true)
	b.rebuild(t)
	require.Equal(t, aml.Low, b.class(t, bWallet))
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "late high risk attribution", "test-operator")
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, collector))
	f.complete(t, effectID, 9500, true)
	f.rebuild(t)
	require.Equal(t, aml.High, f.class(t, collector))
	require.Equal(t, aml.Low, b.class(t, bWallet))
}

func newHistoricalAMLAuthorization(ctx context.Context, auth crypto.SigningAuthorization, unsigned crypto.UnsignedTxEnvelope) (id.ID, error) {
	raw, err := json.Marshal(unsigned)
	if err != nil {
		return id.ID{}, err
	}
	var signingID id.ID
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `INSERT INTO reg_crypto_signing_authorizations(authorization_id,tenant_id,merchant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,max_network_fee_minor,unsigned_fingerprint,basis_type,basis_id,generation,expires_at,policy_version,unsigned_envelope,authorization_jws)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,1,$20,$21,$22::jsonb,'historical-jws') RETURNING id`, auth.AuthorizationID, auth.TenantID, auth.MerchantID, auth.Purpose, auth.SchemaVersion, auth.NetworkID, auth.NetworkCode, auth.ChainFamily, auth.NetworkIdentifier, auth.SourceAddress, auth.VaultKeyID, auth.DestinationAddress, auth.AssetStandard, auth.ContractAddress, auth.AmountMinorUnits, auth.MaxNetworkFeeMinor, auth.UnsignedFingerprint, auth.BasisType, auth.BasisID, auth.ExpiresAt, auth.PolicyVersion, string(raw)).Scan(&signingID)
	return signingID, err
}

func TestAMLRoutingStaleLowReportCannotReleaseNewGeneration(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "generation-race", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	_, err := f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "second review requested", "operator")
	require.NoError(t, err)
	job, ok, err := f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 2, job.Generation)
	ticket := aml.Ticket{Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: "generation-2-ticket", RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, SubmitRequestID: job.SubmitRequestID, Request: job.Request, SubmittedAt: time.Now()}
	require.NoError(t, f.repo.SaveAMLTicket(f.ctx, job, ticket, time.Now().Add(-time.Second)))
	old, ok, err := f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 2, old.Generation)
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 2, "new intelligence while old poll runs", "operator")
	require.NoError(t, err)
	score := aml.ScoreBP(1000)
	require.NoError(t, f.repo.SaveAMLReport(f.ctx, old, aml.Report{ID: id.New(), Provider: old.Provider, ProviderAccountID: old.ProviderAccountID, CheckHash: ticket.CheckHash, RequestDigest: old.RequestDigest, MappingVersion: old.MappingVersion, ContractVersion: "synthetic-v1", AdapterVersion: "synthetic-adapter", Score: &score, CoverageAccepted: true, Signals: []aml.Signal{{Name: "wallet", Value: "1"}}, CompletedAt: time.Now().Add(-time.Second), ReceivedAt: time.Now(), RawResponse: []byte(`{"late_generation":2}`)}))
	applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.True(t, applied)
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "late gen-2 low cannot replace pending gen-3")
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
}

func TestAMLRoutingLateHighReportBlocksBeforeAssessmentAndSurvivesLaterLow(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "late-high-report", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	_, err := f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "second review requested", "operator")
	require.NoError(t, err)
	job, ok, err := f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	ticket := aml.Ticket{Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: "delayed-high-ticket", RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, SubmitRequestID: job.SubmitRequestID, Request: job.Request, SubmittedAt: time.Now()}
	require.NoError(t, f.repo.SaveAMLTicket(f.ctx, job, ticket, time.Now().Add(-time.Second)))
	old, ok, err := f.repo.ClaimAMLJob(f.ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 2, old.Generation)
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 2, "third review while old poll runs", "operator")
	require.NoError(t, err)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	score := aml.ScoreBP(9000)
	require.NoError(t, f.repo.SaveAMLReport(f.ctx, old, aml.Report{ID: id.New(), Provider: old.Provider, ProviderAccountID: old.ProviderAccountID, CheckHash: ticket.CheckHash, RequestDigest: old.RequestDigest, MappingVersion: old.MappingVersion, ContractVersion: "synthetic-v1", AdapterVersion: "synthetic-adapter", Score: &score, CoverageAccepted: true, Signals: []aml.Signal{{Name: "wallet", Value: "1"}}, CompletedAt: time.Now().Add(-time.Second), ReceivedAt: time.Now(), RawResponse: []byte(`{"late_high_generation":2}`)}))
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "current low cannot bypass an unassessed report from an older generation")
	applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.True(t, applied)
	f.rebuild(t)
	require.Equal(t, aml.High, f.class(t, f.input.WalletID))
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 3, "later low needs reviewed resolution", "operator")
	require.NoError(t, err)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.High, f.class(t, f.input.WalletID), "a new low report alone cannot erase historical high exposure")
}

func TestAMLRoutingPolicyChangeReassessesReportsWithoutNewPaidJob(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "policy-change", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_risk_policies(version,low_max_bp,medium_max_bp,max_report_age_seconds,decision_ttl_seconds,accepted_signals,critical_signals)
 VALUES('routing-v2',500,1500,3600,600,ARRAY['wallet','sanctions'],ARRAY['sanctions'])`)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET policy_version='routing-v2',version=version+1`)
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "scope change invalidates the old low in the same SQL transaction")
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	applied, err := f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.True(t, applied)
	f.rebuild(t)
	require.Equal(t, aml.Medium, f.class(t, f.input.WalletID))
	applied, err = f.repo.ApplyNextAMLDecision(f.ctx, time.Now())
	require.NoError(t, err)
	require.False(t, applied)
	var checks int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_provider_checks`).Scan(&checks))
	require.Equal(t, 1, checks, "local policy reassessment must not create another paid request")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses,failure_statuses)
 SELECT network_id,token_id,'test-mapping-v2',contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses,failure_statuses FROM sys_crypto_aml_provider_mappings WHERE version='test-mapping-v1'`)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET mapping_version='test-mapping-v2',version=version+1`)
	require.NoError(t, err)
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "accepted replacement mapping cannot adopt a report pinned to the old mapping")
	_, err = f.repo.RequestAMLRescreen(f.ctx, effectID, 1, "mapping migration requires explicit workflow", "operator")
	require.ErrorContains(t, err, "mapping")
}

func TestAMLRoutingProofRollbackAndOrphanCannotDuplicateMoney(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "atomic-proof", 1)
	rollback := errors.New("synthetic failure after proof capture")
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := NewBlockIngestionRepo().SaveVerification(ctx, proof)
		require.NoError(t, err)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var effects, proofs, jobs int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_effects`).Scan(&effects))
	require.Zero(t, effects)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_transfer_verifications`).Scan(&proofs))
	require.Zero(t, proofs)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_jobs`).Scan(&jobs))
	require.Equal(t, 1, jobs, "only the disabled setup job remains")
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	orphan := proof
	orphan.Event.Canonicality = crypto.CanonicalityOrphaned
	orphan.Event.EventType = crypto.EventTypeReorg
	orphan.Outcome = "absent"
	orphan.ExecutionStatus = crypto.ReceiptPending
	_, err = NewBlockIngestionRepo().SaveVerification(f.ctx, orphan)
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	var money int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements`).Scan(&money))
	require.Equal(t, 1, money)
	var class string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT canonicality FROM reg_crypto_aml_effect_chain_states WHERE effect_id=$1`, effectID).Scan(&class))
	require.Equal(t, "orphaned", class)
}

func TestAMLRoutingScopeUpdateInvalidatesOldPolicyAndProofCoverageRegression(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "coverage-regression", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	// A later agreed block demonstrates another matching effect. The previous
	// report cannot stay low while a separate worker processes that other effect.
	changed := proof
	changed.MatchingEffects = 2
	for index := range changed.Sources {
		changed.Sources[index].Digest = "updated-complete-block"
	}
	_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, changed)
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET enabled=FALSE,version=version+1`)
	require.ErrorContains(t, err, "cannot be downgraded")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=10000 WHERE network_id=$1`, f.input.Request.NetworkID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET version=version+1`)
	require.ErrorContains(t, err, "pause network")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_effects SET amount=200 WHERE id=$1`, effectID)
	require.ErrorContains(t, err, "append-only")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_reconciliation_checks SET chain_amount=1 WHERE id=(SELECT reconciliation_check_id FROM reg_crypto_aml_wallet_baselines LIMIT 1)`)
	require.ErrorContains(t, err, "immutable")
}

func TestAMLRoutingDirectBalanceRepairAndTokenChangeCannotKeepLow(t *testing.T) {
	f := newAMLRoutingFixture(t, true)
	proof := f.proof(f.input.Request.Address, "balance-repair", 1)
	proofID, effectID := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.complete(t, effectID, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_balances SET amount=200 WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	f.rebuild(t)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_balances SET amount=100 WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	// No covered projection remains after the first repair, so request a rebuild
	// explicitly before exercising a direct repair that moves the balance key.
	_, err = f.pool.Exec(f.ctx, `SELECT fn_crypto_aml_invalidate_wallet($1,$2,'reviewed_balance_repair')`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, f.input.WalletID))
	other := f.collector(t, f.input.Request.MerchantID, "balance-repair-destination")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_balances SET wallet_id=$3 WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID, other)
	require.NoError(t, err)
	require.Equal(t, aml.Unknown, f.class(t, f.input.WalletID), "repair must invalidate the old balance key as well as the new key")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_tokens SET contract_address='different-contract' WHERE id=$1`, f.input.Request.TokenID)
	require.ErrorContains(t, err, "token identity is immutable")
}

// Projection queues are scheduled by PostgreSQL. Read its clock after enqueue
// rather than racing a separate host clock at the queue's exact due boundary.
func (f amlRoutingFixture) databaseNow(t *testing.T) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now))
	return now
}
