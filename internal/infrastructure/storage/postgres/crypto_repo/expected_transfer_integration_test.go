//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type expectedRouteFixture struct {
	ctx                                               context.Context
	pool                                              *pgxpool.Pool
	proof                                             crypto.TransferVerification
	event                                             crypto.BlockchainEvent
	source, destination, asset, auth, expected, basis id.ID
}

func newExpectedRouteFixture(t *testing.T, mode string) expectedRouteFixture {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	p := accountingProof(t, ctx, pool)
	p.MatchingEffects = 1
	if mode == "ambiguous_proof" {
		p.MatchingEffects = 2
	}
	if mode == "legacy_proof" {
		p.AccountingIdentity = nil
	}
	if mode == "low_confirmations" {
		p.Event.Confirmations = 1
		for i := range p.Sources {
			p.Sources[i].HeadHeight = p.Event.BlockNumber
		}
	}
	p.Event.EventType = crypto.EventTypeTransfer
	p.Event.Direction = crypto.TransferDirectionIncoming
	f := expectedRouteFixture{ctx: ctx, pool: pool, proof: p, source: id.New(), destination: id.New(), asset: id.New(), auth: id.New(), expected: id.New(), basis: id.New()}
	e := p.Event
	_, err := pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,contract_address,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Expected asset',$2,'EXP',$3,6,'ERC-20')`, f.asset, e.NetworkID, e.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role) VALUES($1,$1::uuid::text,'Source',$3,$4,'expected-key','pool','merchant_pool'),($2,$2::uuid::text,'Destination',$3,$5,'destination-key','hot','liquidity_hot')`, f.source, f.destination, e.NetworkID, e.FromAddress, e.ToAddress)
	require.NoError(t, err)
	f.seedAuthorization(t, f.auth, f.expected, f.basis, mode)
	proofID, err := NewBlockIngestionRepo().SaveVerification(ctx, p)
	require.NoError(t, err)
	f.event = e
	f.event.VerificationID = &proofID
	if mode == "low_confirmations" {
		_, err = pool.Exec(ctx, `UPDATE cat_blockchain_networks SET confirmations_needed=19 WHERE id=$1`, e.NetworkID)
		require.NoError(t, err)
	}
	return f
}
func (f expectedRouteFixture) seedAuthorization(t *testing.T, auth, expected, basis id.ID, mode string) {
	t.Helper()
	e := f.proof.Event
	fp := "expected-" + auth.String()
	amount := e.Amount.String()
	if mode == "wrong_intent" {
		amount = "24"
	}
	unsigned, err := json.Marshal(crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: fp, Payload: json.RawMessage(`{"synthetic":true}`)})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,network_sequence)
 SELECT $1,$1::uuid::text,'expected-test','sweep',2,n.id,n.code,n.chain_family,CASE WHEN $9 THEN n.network_identifier||'-wrong' ELSE n.network_identifier END,$3,$3,'expected-key',$4,'ERC-20',$5,$6,'CryptoSweepAttempt',$7,now()-interval '1 hour','evm-v1','synthetic',$8,$10::jsonb,'0' FROM cat_blockchain_networks n WHERE n.id=$2`, auth, e.NetworkID, e.FromAddress, e.ToAddress, e.TokenContract, amount, basis, fp, mode == "wrong_network", string(unsigned))
	require.NoError(t, err)
	raw, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: fp, TxHash: e.TxHash, Payload: json.RawMessage(`{"synthetic":true}`)})
	require.NoError(t, err)
	if mode == "wrong_envelope_version" {
		raw, err = json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 2, Generation: 1, NetworkSequence: "0", UnsignedFingerprint: fp, TxHash: e.TxHash, Payload: json.RawMessage(`{"synthetic":true}`)})
		require.NoError(t, err)
	}
	if mode == "legacy_envelope" {
		raw = []byte(`{"synthetic":true}`)
	}
	if mode != "unsigned" {
		_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope=$2::jsonb,tx_hash=$3 WHERE id=$1`, auth, string(raw), e.TxHash)
		require.NoError(t, err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(id,signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash) VALUES($1,$2,0,$3,$4,lower($5),lower($6),$7,lower($8),$9,$10)`, expected, auth, e.NetworkID, e.EventKind, e.FromAddress, e.ToAddress, f.asset, e.TokenContract, e.Amount, e.TxHash)
	require.NoError(t, err)
}
func (f expectedRouteFixture) route(e crypto.BlockchainEvent) (bool, error) {
	var handled bool
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		var err error
		handled, err = NewExpectedTransferRepo().RouteExpectedTransfer(ctx, e)
		return err
	})
	return handled, err
}
func (f expectedRouteFixture) counts(t *testing.T) []int {
	t.Helper()
	var requests, routes, evidence, events, money int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_expected_transfer_requests),(SELECT count(*) FROM reg_crypto_expected_transfer_routes),(SELECT count(*) FROM reg_crypto_expected_transfer_evidence),(SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoExpectedTransfer'),(SELECT count(*) FROM reg_crypto_balance_movements)`).Scan(&requests, &routes, &evidence, &events, &money))
	return []int{requests, routes, evidence, events, money}
}
func TestExpectedTransferConcurrentRoutingAndStableReinclusion(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			handled, err := f.route(f.event)
			if err == nil && !handled {
				err = sql.ErrNoRows
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, []int{1, 1, 1, 1, 0}, f.counts(t))
	alias := f.event
	alias.TxHash = strings.ToUpper(strings.TrimPrefix(alias.TxHash, "0x"))
	alias.FromAddress = strings.ToLower(alias.FromAddress)
	handled, err := f.route(alias)
	require.NoError(t, err)
	require.True(t, handled)
	re := f.proof
	re.Event.EventIndex = 91
	re.Event.BlockNumber = 101
	re.Event.BlockHash = "0x" + strings.Repeat("c", 64)
	re.Event.Confirmations = 19
	proof, err := NewBlockIngestionRepo().SaveVerification(f.ctx, re)
	require.NoError(t, err)
	event := re.Event
	event.VerificationID = &proof
	handled, err = f.route(event)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, []int{1, 1, 2, 1, 0}, f.counts(t))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_expected_transfer_routes SET owner_id=$1`, id.New())
	require.Error(t, err)
	// A second logical transfer cannot consume the same single authorized effect.
	re = f.proof
	re.Event.EventIndex = 8
	identity, err := crypto.NewAccountingEffectIdentity(re.Event, crypto.ChainFamilyEVM, 3)
	require.NoError(t, err)
	re.AccountingIdentity = &identity
	proof, err = NewBlockIngestionRepo().SaveVerification(f.ctx, re)
	require.NoError(t, err)
	event = re.Event
	event.VerificationID = &proof
	_, err = f.route(event)
	require.Error(t, err)
	require.Equal(t, []int{1, 1, 2, 1, 0}, f.counts(t))
}
func TestExpectedTransferPendingNeverFallsBackAfterExpiry(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	event := f.event
	event.VerificationID = nil
	event.Finality = crypto.FinalityProvisional
	handled, err := f.route(event)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, []int{1, 0, 0, 1, 0}, f.counts(t))
	var firstEvent string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT first_event::text FROM reg_crypto_expected_transfer_requests WHERE expected_effect_id=$1`, f.expected).Scan(&firstEvent))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed' WHERE id=$1`, f.auth)
	require.NoError(t, err)
	delivered, err := NewExpectedTransferRepo().NudgePending(f.ctx, 50)
	require.NoError(t, err)
	require.Zero(t, delivered)
	handled, err = f.route(f.event)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, []int{1, 1, 1, 1, 0}, f.counts(t))
	var afterEvent string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT first_event::text FROM reg_crypto_expected_transfer_requests WHERE expected_effect_id=$1`, f.expected).Scan(&afterEvent))
	require.Equal(t, firstEvent, afterEvent, "verified replay preserves the original provisional observation")
	// Replay cannot seize the route by swapping in a different verified event.
	event = f.event
	event.EventIndex++
	_, err = f.route(event)
	require.ErrorIs(t, err, crypto.ErrVerificationMismatch)
}
func TestExpectedTransferEconomicMatchingAndAmbiguity(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	for _, field := range []string{"amount", "source", "destination", "contract", "kind", "hash", "network"} {
		event := f.event
		event.VerificationID = nil
		switch field {
		case "amount":
			event.Amount = event.Amount.Add(event.Amount)
		case "source":
			event.FromAddress = "other"
		case "destination":
			event.ToAddress = "other"
		case "contract":
			event.TokenContract = "other"
		case "kind":
			event.EventKind = crypto.EventKindInternalValue
		case "hash":
			event.TxHash = strings.Repeat("d", 64)
		case "network":
			event.NetworkID = id.New()
		}
		handled, err := f.route(event)
		require.NoError(t, err, field)
		require.False(t, handled, field)
	}
	require.Equal(t, []int{0, 0, 0, 0, 0}, f.counts(t))
	f.seedAuthorization(t, id.New(), id.New(), id.New(), "")
	handled, err := f.route(f.event)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, []int{2, 0, 0, 2, 0}, f.counts(t))
	delivered, err := NewExpectedTransferRepo().NudgePending(f.ctx, 50)
	require.NoError(t, err)
	require.Zero(t, delivered)
}
func TestExpectedTransferIncompleteIntentAndCoverageRemainPending(t *testing.T) {
	for _, mode := range []string{"legacy_envelope", "wrong_intent", "wrong_network", "wrong_envelope_version", "low_confirmations", "ambiguous_proof", "legacy_proof"} {
		t.Run(mode, func(t *testing.T) {
			f := newExpectedRouteFixture(t, mode)
			event := f.event
			if mode == "ambiguous_proof" || mode == "legacy_proof" {
				p := f.proof
				if mode == "ambiguous_proof" {
					p.MatchingEffects = 2
				} else {
					p.AccountingIdentity = nil
				}
				proof, err := NewBlockIngestionRepo().SaveVerification(f.ctx, p)
				require.NoError(t, err)
				event.VerificationID = &proof
			}
			handled, err := f.route(event)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, []int{1, 0, 0, 1, 0}, f.counts(t))
		})
	}
}
func TestExpectedTransferDirectSQLCannotStealOwnerOrOmitOutbox(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	raw, err := json.Marshal(f.event)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_transfer_requests(expected_effect_id,owner_type,owner_id,first_event) VALUES($1,'CryptoSweepAttempt',$2,$3::jsonb)`, f.expected, f.basis, string(raw))
	require.ErrorContains(t, err, "original-owner outbox")
	require.Equal(t, []int{0, 0, 0, 0, 0}, f.counts(t))
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_custody_effect_postings(id,effect_id,occurrence_id,verification_id,recorder_id,recorder_type,recorder_version)
 SELECT $1,o.effect_id,o.id,$2,$3,'CryptoCustodyReceipt',1 FROM reg_crypto_accounting_effect_evidence e JOIN reg_crypto_accounting_effect_occurrences o ON o.id=e.occurrence_id WHERE e.verification_id=$2`, id.New(), *f.event.VerificationID, id.New())
	require.ErrorContains(t, err, "original settlement owner")
	// The payment INSERT guard also covers unindexed legacy incoming admission.
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,token_id,wallet_id,network_id,tx_hash,event_kind,event_index,from_address,amount,block_number)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,23,100)`, id.New(), id.New(), id.New(), f.asset, f.destination, f.event.NetworkID, f.event.TxHash, f.event.EventKind, f.event.EventIndex, f.event.FromAddress)
	require.ErrorContains(t, err, "cannot create an external payment")
	handled, err := f.route(f.event)
	require.NoError(t, err)
	require.True(t, handled)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_transfer_routes(effect_id,expected_effect_id,first_verification_id,owner_type,owner_id) SELECT effect_id,expected_effect_id,first_verification_id,'OtherOwner',$1 FROM reg_crypto_expected_transfer_routes`, id.New())
	require.ErrorContains(t, err, "route owner is immutable")
	require.Equal(t, []int{1, 1, 1, 1, 0}, f.counts(t))
}
func TestExpectedTransferUpgradePreservesLegacyMatches(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 86)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_chain_effect_matches(expected_effect_id,network_id,tx_hash,event_kind,event_index) VALUES($1,$2,$3,$4,$5)`, f.expected, f.event.NetworkID, f.event.TxHash, f.event.EventKind, f.event.EventIndex)
	require.NoError(t, err)
	var before, after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY id)::text FROM reg_crypto_chain_effect_matches m`).Scan(&before))
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY id)::text FROM reg_crypto_chain_effect_matches m`).Scan(&after))
	require.Equal(t, before, after)
	require.Equal(t, []int{0, 0, 0, 0, 0}, f.counts(t))
	_, err = provider.DownTo(f.ctx, 86)
	require.NoError(t, err)
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	_, err = f.route(f.event)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 86)
	require.ErrorContains(t, err, "expected transfer routing requires forward migration")
}

func TestExpectedTransferDoesNotSuppressOtherEconomicsInSameTransaction(t *testing.T) {
	f := newExpectedRouteFixture(t, "")
	merchant, invoice, payment := id.New(), id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Other external payer')`, merchant)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,expires_at) VALUES($1,'ci_'||replace($1::uuid::text,'-',''),$1::uuid::text,now(),$2,$3,$4,23,now()+interval '1 hour')`, invoice, merchant, f.asset, f.destination)
	require.NoError(t, err)
	other := f.event
	other.FromAddress = "0x0000000000000000000000000000000000000011"
	other.VerificationID = nil
	handled, err := f.route(other)
	require.NoError(t, err)
	require.False(t, handled)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,token_id,wallet_id,network_id,tx_hash,event_kind,event_index,from_address,amount,block_number) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,23,100)`, payment, invoice, merchant, f.asset, f.destination, other.NetworkID, other.TxHash, other.EventKind, other.EventIndex, other.FromAddress)
	require.NoError(t, err, "an unrelated unposted incoming payment still uses normal admission")
	require.Equal(t, []int{0, 0, 0, 0, 0}, f.counts(t))
}

func TestExpectedTransferUnsignedIntentCannotSuppressUnknownOutflow(t *testing.T) {
	f := newExpectedRouteFixture(t, "unsigned")
	var handled bool
	err := postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		var err error
		handled, err = NewExpectedTransferRepo().MatchExpectedChainEffect(ctx, f.event)
		return err
	})
	require.NoError(t, err)
	require.False(t, handled)
	require.Equal(t, []int{0, 0, 0, 0, 0}, f.counts(t))
}
