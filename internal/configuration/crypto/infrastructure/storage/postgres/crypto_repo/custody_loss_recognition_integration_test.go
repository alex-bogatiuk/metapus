//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type custodyLossFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	service *cryptoapp.ExternalOutflowService
	engine  *posting.Engine
	proof   crypto.TransferVerification
	doc     *crypto.CryptoCustodyAdjustment
	checker id.ID
}

func newCustodyLossFixture(t *testing.T, proofMode string) custodyLossFixture {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	proof := accountingProof(t, ctx, pool)
	proof.Event.Direction = crypto.TransferDirectionOutgoing
	asset, wallet, outflow, maker := id.New(), id.New(), id.New(), id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,token_standard,contract_address,symbol,decimal_places) VALUES($1,$1::uuid::text,'Loss asset',$2,'ERC-20',$3,'LOSS',6)`, asset, proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,wallet_role) VALUES($1,$1::uuid::text,'Loss source',$2,$3,'source-key','liquidity_hot')`, wallet, proof.Event.NetworkID, proof.Event.FromAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'Opening',1,now(),'receipt',$2,$3,100)`, id.New(), wallet, asset)
	require.NoError(t, err)
	e := proof.Event
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,contract_address,source_address_key,destination_address_key,contract_address_key,amount,block_number,block_hash,confirmations,required_confs,finality_status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,lower($8),lower($9),lower($10),$11,$12,$13,20,19,'finalized')`, outflow, e.NetworkID, asset, wallet, e.TxHash, e.EventKind, e.EventIndex, e.FromAddress, e.ToAddress, e.TokenContract, e.Amount, e.BlockNumber, e.BlockHash)
	require.NoError(t, err)
	if proofMode != "missing" {
		stored := proof
		if proofMode == "wrong_amount" {
			stored.Event.Amount = stored.Event.Amount.Add(stored.Event.Amount)
		}
		if proofMode == "legacy" {
			stored.AccountingIdentity = nil
		}
		_, err = NewBlockIngestionRepo().SaveVerification(ctx, stored)
		require.NoError(t, err)
	}
	tm := postgres.MustGetTxManager(ctx)
	engine := posting.NewLockingEngine(postgres.NewDocLocker())
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(crypto_register_repo.NewCryptoPlatformCostRepo())))
	service := cryptoapp.NewExternalOutflowService(tm, NewExternalOutflowWorkflowRepo(), cryptoapp.NewOperationalHoldService(tm, NewOperationalHoldRepo()), engine)
	classified, err := service.Classify(ctx, cryptoapp.ClassifyExternalOutflowCommand{OutflowID: outflow, Classification: crypto.OutflowUnauthorizedLoss, Reason: "proven unexplained outflow"}, maker)
	require.NoError(t, err)
	return custodyLossFixture{ctx: ctx, pool: pool, service: service, engine: engine, proof: proof, doc: classified.Adjustment, checker: id.New()}
}

func (f custodyLossFixture) approve() error {
	_, err := f.service.ApproveAdjustment(f.ctx, f.doc.ID, f.checker, "confirmed custody accounting loss")
	return err
}
func (f custodyLossFixture) counts(t *testing.T) []int {
	t.Helper()
	var physical, cost, bindings, events int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1),(SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1),
 (SELECT count(*) FROM reg_crypto_custody_loss_bindings WHERE adjustment_id=$1),(SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized')`, f.doc.ID).Scan(&physical, &cost, &bindings, &events))
	return []int{physical, cost, bindings, events}
}

func TestCustodyLossV2ConcurrentApprovalClaimsOneExpense(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- f.approve() })
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			var app *apperror.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, apperror.CodeConflict, app.Code)
		}
	}
	require.GreaterOrEqual(t, successes, 1, "matching concurrent commands may return the saved approval")
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.doc.WalletID, f.doc.TokenID).Scan(&balance))
	require.Equal(t, "77", balance)
}

func TestCustodyLossV2RequiresIndependentCurrentProof(t *testing.T) {
	for _, mode := range []string{"missing", "legacy", "wrong_amount", "orphan", "reinclusion", "insufficient_confirmations"} {
		t.Run(mode, func(t *testing.T) {
			f := newCustodyLossFixture(t, mode)
			if mode == "orphan" || mode == "reinclusion" {
				proof := f.proof
				proof.VerifiedAt = proof.VerifiedAt.Add(time.Second)
				if mode == "orphan" {
					proof.Outcome = "absent"
					proof.ExecutionStatus = crypto.ReceiptPending
					proof.Event.EventType = crypto.EventTypeReorg
					proof.Event.Canonicality = crypto.CanonicalityOrphaned
				} else {
					proof.Event.BlockHash = "0x" + strings.Repeat("c", 64)
					proof.Event.BlockNumber++
					for i := range proof.Sources {
						proof.Sources[i].HeadHeight++
					}
				}
				_, err := NewBlockIngestionRepo().SaveVerification(f.ctx, proof)
				require.NoError(t, err)
			}
			if mode == "insufficient_confirmations" {
				_, err := f.pool.Exec(f.ctx, `UPDATE cat_blockchain_networks SET confirmations_needed=30 WHERE id=$1`, f.doc.NetworkID)
				require.NoError(t, err)
			}
			require.Error(t, f.approve())
			require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
		})
	}
}

func TestCustodyLossV2IncompleteFinancialSetRollsBack(t *testing.T) {
	for _, component := range []string{"principal", "cost", "outbox", "audit", "reconciliation"} {
		t.Run(component, func(t *testing.T) {
			f := newCustodyLossFixture(t, "")
			f.engine.PrepareMovementsInTransaction(func(ctx context.Context, _ posting.Postable, set *posting.MovementSet) error {
				if component == "principal" {
					set.SetExtension("crypto_balance", []crypto_balance.CryptoBalanceMovement{})
					return nil
				}
				if component == "cost" {
					set.SetExtension("crypto_platform_cost", []crypto_platform_cost.CryptoPlatformCostMovement{})
					return nil
				}
				if component == "outbox" {
					_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `DELETE FROM sys_outbox WHERE aggregate_id=$1 AND event_type='custody_recognized'`, f.doc.ID)
					return err
				}
				return nil
			})
			if component == "audit" || component == "reconciliation" {
				tm := postgres.MustGetTxManager(f.ctx)
				err := tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
					repo := NewExternalOutflowWorkflowRepo()
					a, err := repo.PrepareCustodyAdjustmentForPosting(ctx, f.doc.ID, f.checker)
					if err != nil {
						return err
					}
					return f.engine.Post(ctx, a, func(ctx context.Context) error {
						if component == "audit" {
							q := tm.GetQuerier(ctx)
							if _, err := q.Exec(ctx, `ALTER TABLE doc_crypto_custody_adjustments DISABLE TRIGGER trg_crypto_custody_decision_audit`); err != nil {
								return err
							}
							if _, err := q.Exec(ctx, `UPDATE doc_crypto_custody_adjustments SET status='posted',posted_version=1,decision_by=$2,decision_reason='checker decision',decided_at=now(),decision_basis=jsonb_build_object('id',$3::uuid) WHERE id=$1`, a.ID, f.checker, id.New()); err != nil {
								return err
							}
							_, err := q.Exec(ctx, `INSERT INTO reg_crypto_reconciliation_requests(network_id,wallet_id,token_id,source_type,source_id) VALUES($1,$2,$3,'custody_adjustment',$4)`, a.NetworkID, a.WalletID, a.TokenID, a.ID)
							return err
						}
						if err := repo.MarkCustodyAdjustmentPosted(ctx, a, f.checker, "checker decision"); err != nil {
							return err
						}
						query := `DELETE FROM reg_crypto_reconciliation_requests WHERE source_type='custody_adjustment' AND source_id=$1`
						_, err := tm.GetQuerier(ctx).Exec(ctx, query, f.doc.ID)
						return err
					})
				})
				require.Error(t, err)
			} else {
				require.Error(t, f.approve())
			}
			require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
		})
	}
}

func TestCustodyLossV2RejectsManagedDestinationAndSelfApproval(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	_, err := f.service.ApproveAdjustment(f.ctx, f.doc.ID, f.doc.MakerID, "self approval")
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,is_active,deletion_mark) VALUES($1,$1::uuid::text,'Retired managed destination',$2,$3,'destination-key',false,true)`, id.New(), f.doc.NetworkID, f.proof.Event.ToAddress)
	require.NoError(t, err)
	require.Error(t, f.approve())
	require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
}

func TestCustodyLossV2ImmutableRecognitionAndLateLegacyWriter(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	require.NoError(t, f.approve())
	for _, query := range []string{
		`UPDATE doc_crypto_custody_adjustments SET status='pending_approval',posted_version=0 WHERE id=$1`,
		`UPDATE reg_crypto_custody_loss_bindings SET period=now() WHERE adjustment_id=$1`,
		`INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id) SELECT recorder_id,'UnboundCompensation',2,now(),'receipt',wallet_id,token_id,amount,line_id FROM reg_crypto_balance_movements WHERE recorder_id=$1`,
		`INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount,reversal_of_line_id)
		 SELECT gen_random_uuid_v7(),gen_random_uuid_v7(),'UnboundCompensation',1,now(),'receipt',network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount,line_id FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1`,
	} {
		_, err := f.pool.Exec(f.ctx, query, f.doc.ID)
		require.Error(t, err, query)
	}
	legacy, merchant := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Legacy merchant')`, merchant)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_withdrawals(id,merchant_id,token_id,source_wallet_id,dest_address,amount,tx_hash,status) VALUES($1,$2,$3,$4,$5,23,$6,'broadcast')`, legacy, merchant, f.doc.TokenID, f.doc.WalletID, f.proof.Event.ToAddress, f.proof.Event.TxHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'CryptoWithdrawal',1,now(),'expense',$2,$3,23)`, legacy, f.doc.WalletID, f.doc.TokenID)
	require.ErrorContains(t, err, "recognized custody loss already owns this outgoing principal")
}

func TestCustodyLossV2KnownSignedEffectRequiresExecutionOwner(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	authorization := id.New()
	e := f.proof.Event
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,
  source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,basis_type,basis_id,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,network_sequence)
  SELECT $1,$1::uuid::text,'loss-test','sweep',1,n.id,n.code,n.chain_family,n.network_identifier,$3,$3,'source-key',$4,'ERC-20',$5,23,'CryptoSweepAttempt',$6,now()+interval '1 hour','evm-v1','historical-jws',$1::uuid::text,'{}','0'
  FROM cat_blockchain_networks n WHERE n.id=$2`, authorization, f.doc.NetworkID, e.FromAddress, e.ToAddress, e.TokenContract, id.New())
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope='{"synthetic":true}',tx_hash=$2 WHERE id=$1`, authorization, e.TxHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
  VALUES($1,0,$2,'contract_log',lower($3),lower($4),$5,lower($6),23,$7)`, authorization, e.NetworkID, e.FromAddress, e.ToAddress, f.doc.TokenID, e.TokenContract, e.TxHash)
	require.NoError(t, err)
	err = f.approve()
	require.ErrorContains(t, err, "existing accounting or signed execution owner")
	require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
}

func TestCustodyLossV2RecognizesProvenLossWhileIntakeIsPaused(t *testing.T) {
	f := newCustodyLossFixture(t, "")
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'fixture',0,'paused',2)`, f.doc.NetworkID)
	require.NoError(t, err)
	require.NoError(t, f.approve())
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
}
