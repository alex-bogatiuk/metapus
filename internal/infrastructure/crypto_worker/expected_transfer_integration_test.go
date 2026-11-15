//go:build integration

package crypto_worker

import (
	"context"
	"testing"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"

	"github.com/stretchr/testify/require"
)

func TestExpectedNativeFundingWatcherAndSettlementHaveOneOwner(t *testing.T) {
	for _, order := range []string{"watcher_first", "settlement_first"} {
		t.Run(order, func(t *testing.T) {
			payer := "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
			runSweepCoordinationFixtureWithAddresses(t, true, payer, "", func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				funding, _, receipt := seedNativeFunding(t, ctx, tm, c, 1, false)
				proveTRONNetworkFee(t, &receipt, payer)
				p := nativeFundingProof(t, ctx, tm, funding, receipt)
				p.MatchingEffects = 1
				proof, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, p)
				require.NoError(t, err)
				auth, expected := id.New(), id.New()
				fp := "expected-funding-" + auth.String()
				var source, destination, key string
				q := tm.GetQuerier(ctx)
				require.NoError(t, q.QueryRow(ctx, `SELECT s.address,d.address,s.vault_key_id FROM cat_wallets s,cat_wallets d WHERE s.id=$1 AND d.id=$2`, c.hot, c.wallet).Scan(&source, &destination, &key))
				unsigned, err := json.Marshal(crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fp, Payload: json.RawMessage(`{"synthetic":true}`)})
				require.NoError(t, err)
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,amount_minor_units,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope)
    SELECT $1,$1::uuid::text,'expected-funding','native_funding',1,n.id,n.code,n.chain_family,n.network_identifier,$3,$3,$4,$5,'native',100,'CryptoNativeFunding',$6,now()-interval '1 hour','tron-v1','synthetic',$7,$8::jsonb FROM cat_blockchain_networks n WHERE n.id=$2`, auth, c.network, source, key, destination, funding, fp, string(unsigned))
				require.NoError(t, err)
				raw, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: fp, TxHash: receipt.TxHash, Payload: json.RawMessage(`{"synthetic":true}`)})
				require.NoError(t, err)
				_, err = q.Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope=$2::jsonb,tx_hash=$3 WHERE id=$1`, auth, string(raw), receipt.TxHash)
				require.NoError(t, err)
				_, err = q.Exec(ctx, `INSERT INTO reg_crypto_expected_chain_effects(id,signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,amount,tx_hash) VALUES($1,$2,0,$3,'native_value',$4,$5,$6,100,$7)`, expected, auth, c.network, source, destination, c.native, receipt.TxHash)
				require.NoError(t, err)
				event := p.Event
				event.VerificationID = &proof
				event.Direction = crypto.TransferDirectionInternal
				route := func() {
					require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
						handled, err := crypto_repo.NewExpectedTransferRepo().RouteExpectedTransfer(ctx, event)
						require.True(t, handled)
						return err
					}))
				}
				service := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager())
				if order == "watcher_first" {
					// An existing worker keeps its lease; handoff changes only the due time.
					until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
					_, err = q.Exec(ctx, `UPDATE doc_crypto_native_fundings SET next_attempt_at=now()+interval '1 hour',claimed_by='original-owner',lease_expires_at=$2 WHERE id=$1`, funding, until)
					require.NoError(t, err)
					route()
					n, err := crypto_repo.NewExpectedTransferRepo().NudgePending(ctx, 200)
					require.NoError(t, err)
					require.Positive(t, n)
					var lease time.Time
					var owner string
					var due bool
					require.NoError(t, q.QueryRow(ctx, `SELECT lease_expires_at,claimed_by,next_attempt_at<=now() FROM doc_crypto_native_fundings WHERE id=$1`, funding).Scan(&lease, &owner, &due))
					require.Equal(t, until, lease.UTC())
					require.Equal(t, "original-owner", owner)
					require.True(t, due)
				}
				settled, err := service.Settle(ctx, funding, receipt)
				require.NoError(t, err)
				require.True(t, settled)
				route()
				route()
				if order == "settlement_first" {
					_, err = crypto_repo.NewExpectedTransferRepo().NudgePending(ctx, 200)
					require.NoError(t, err)
				}
				var principal, requests, routes, external, events int
				var sourceBalance, destinationBalance string
				require.NoError(t, q.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNativeFunding'),
    (SELECT count(*) FROM reg_crypto_expected_transfer_requests WHERE expected_effect_id=$2),(SELECT count(*) FROM reg_crypto_expected_transfer_routes WHERE expected_effect_id=$2),
    (SELECT count(*) FROM doc_crypto_custody_receipts WHERE network_id=$3),(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$2 AND event_type='settlement_requested'),
    (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$4 AND token_id=$6),(SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$5 AND token_id=$6)`, funding, expected, c.network, c.hot, c.wallet, c.native).Scan(&principal, &requests, &routes, &external, &events, &sourceBalance, &destinationBalance))
				require.Equal(t, []int{2, 1, 1, 0, 1}, []int{principal, requests, routes, external, events})
				require.Equal(t, "898", sourceBalance)
				require.Equal(t, "200000100", destinationBalance)
			})
		})
	}
}

func TestExpectedOutboundHandoffPreservesWorkerLeasesAndMoney(t *testing.T) {
	for _, kind := range []string{"withdrawal", "treasury", "sweep"} {
		t.Run(kind, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
				var ownerType, table, keyColumn string
				var owner, target, document id.ID
				switch kind {
				case "withdrawal":
					f := newWithdrawalFinalFixture(t, ctx, tm, c)
					f.broadcast(t)
					ownerType = "WithdrawalRequest"
					owner = f.request.ID
					target = f.doc.ID
					document = f.doc.ID
					table = "doc_crypto_withdrawals"
					keyColumn = "id"
				case "treasury":
					f := newTreasuryFinalFixture(t, ctx, tm, c)
					f.broadcast(t)
					ownerType = "TreasuryWithdrawal"
					owner = f.doc.ID
					target = f.doc.ID
					document = f.doc.ID
					table = "doc_treasury_withdrawals"
					keyColumn = "id"
				case "sweep":
					f := newSweepFinalFixture(t, ctx, tm, c, 1)
					f.broadcast(t)
					ownerType = "CryptoSweepAttempt"
					owner = f.tasks[0].LineID
					target = owner
					document = f.doc.ID
					table = "doc_crypto_sweeps__lines"
					keyColumn = "line_id"
				}
				q := tm.GetQuerier(ctx)
				event := crypto.BlockchainEvent{NetworkID: c.network, EventType: crypto.EventTypeTransfer, Direction: crypto.TransferDirectionInternal}
				var expected id.ID
				require.NoError(t, q.QueryRow(ctx, `SELECT x.id,x.tx_hash,x.event_kind,x.source_address_key,x.destination_address_key,x.contract_address_key,x.amount
    FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id WHERE a.basis_type=$1 AND a.basis_id=$2`, ownerType, owner).Scan(&expected, &event.TxHash, &event.EventKind, &event.FromAddress, &event.ToAddress, &event.TokenContract, &event.Amount))
				until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
				_, err := q.Exec(ctx, `UPDATE `+table+` SET settlement_claimed_by='original-owner',settlement_lease_expires_at=$2 WHERE `+keyColumn+`=$1`, target, until)
				require.NoError(t, err)
				processor := crypto.NewEventProcessor(crypto.EventProcessorConfig{TxManager: contextTxManager{}, ExpectedTransfers: crypto_repo.NewExpectedTransferRepo()})
				require.NoError(t, processor.ProcessEvent(ctx, event))
				n, err := crypto_repo.NewExpectedTransferRepo().NudgePending(ctx, 200)
				require.NoError(t, err)
				require.Positive(t, n)
				var lease, checked time.Time
				var claimed string
				require.NoError(t, q.QueryRow(ctx, `SELECT settlement_lease_expires_at,settlement_claimed_by,settlement_checked_at FROM `+table+` WHERE `+keyColumn+`=$1`, target).Scan(&lease, &claimed, &checked))
				require.Equal(t, until, lease.UTC())
				require.Equal(t, "original-owner", claimed)
				require.Equal(t, int64(0), checked.Unix())
				var money, requests int
				require.NoError(t, q.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),(SELECT count(*) FROM reg_crypto_expected_transfer_requests WHERE expected_effect_id=$2)`, document, expected).Scan(&money, &requests))
				require.Zero(t, money)
				require.Equal(t, 1, requests)
			})
		})
	}
}
