//go:build integration

package crypto_worker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

type networkFeeFixture struct {
	ctx         context.Context
	tm          *postgres.TxManager
	reservation crypto.FeeReservation
	receipt     crypto.ChainReceipt
}

func newNetworkFeeFixture(t *testing.T, purpose crypto.SigningPurpose) networkFeeFixture {
	t.Helper()
	ctx, tm, closePool := cryptoIntegrationTxManager(t)
	t.Cleanup(closePool)
	q := tm.GetQuerier(ctx)
	network, asset, wallet, basis := id.New(), id.New(), id.New(), id.New()
	_, err := q.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Fee proof network','evm',$1::uuid::text,'ETH',18)`, network)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address) VALUES($1,$1::uuid::text,'Native',$2,'ETH',18,'native','')`, asset, network)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,chain_activation_status) VALUES($1,$1::uuid::text,'Fee payer',$2,'0x00000000000000000000000000000000000000ab','source-key','hot','liquidity_hot','active')`, wallet, network)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100)`, wallet, asset)
	require.NoError(t, err)
	_, err = q.Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=true,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000,fee_funding_mode='source_only',min_fee_wallet_balance_minor=0,min_source_wallet_residual_minor=0 WHERE network_id=$1`, network)
	require.NoError(t, err)
	basisType := "NetworkFeeProofTest"
	if purpose == crypto.SigningPurposeActivation {
		basisType = crypto_network_operation.ActivationTaskBasisType
	}
	rsv, err := NewFeeBudgetManager().Reserve(ctx, FeeReservationRequest{NetworkID: network, PayerWalletID: wallet, NativeTokenID: asset, OperationTokenID: asset, BasisType: basisType, BasisID: basis, Purpose: purpose, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: types.NewRawAmountFromInt64(10)})
	require.NoError(t, err)
	hash := id.Compact(basis) + id.Compact(basis)
	require.NoError(t, NewFeeBudgetManager().BindUnsignedFingerprint(ctx, rsv.ID, "fee-"+basis.String()))
	require.NoError(t, NewFeeBudgetManager().MarkBroadcast(ctx, rsv.ID, "fee-"+basis.String(), hash))
	r := crypto.ChainReceipt{NetworkID: network, TxHash: hash, Status: crypto.ReceiptSucceeded, BlockHash: strings.Repeat("b", 64), BlockHeight: 100, Confirmations: 20, Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, ObservedAt: time.Now().UTC().Truncate(time.Microsecond), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: asset}, TotalFee: types.NewRawAmountFromInt64(2), Components: []crypto.FeeComponent{{Name: "execution_gas", Amount: types.NewRawAmountFromInt64(2), Quantity: 1}}}}
	f := networkFeeFixture{ctx, tm, rsv, r}
	f.prove(t)
	t.Cleanup(func() {
		_, _ = q.Exec(context.Background(), `UPDATE sys_crypto_network_policies SET enabled=false WHERE network_id=$1`, network)
	})
	return f
}
func (f *networkFeeFixture) prove(t *testing.T) {
	t.Helper()
	r := f.receipt
	p := crypto.NetworkFeeProof{SchemaVersion: 1, Family: crypto.ChainFamilyEVM, NetworkID: r.NetworkID, TxHash: r.TxHash, PayerAddress: "0x00000000000000000000000000000000000000ab", NativeTokenID: r.Fee.NativeAsset.TokenID, BlockHash: r.BlockHash, BlockNumber: r.BlockHeight, Status: r.Status, TotalFee: r.Fee.TotalFee, Components: r.Fee.Components, Confirmations: r.Confirmations, Finality: r.Finality, Canonicality: r.Canonicality, VerifiedAt: r.ObservedAt,
		Sources: []crypto.VerificationSource{{ProviderID: "one", TrustGroup: "one", SourceDigest: strings.Repeat("a", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: r.BlockHeight + int64(r.Confirmations) - 1}, {ProviderID: "two", TrustGroup: "two", SourceDigest: strings.Repeat("a", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: r.BlockHeight + int64(r.Confirmations) - 1}}}
	for i := range p.Sources {
		p.Sources[i].Digest = p.Digest()
	}
	require.NoError(t, p.ValidateReceipt(r))
	f.receipt.FeeProof = &p
}
func (f networkFeeFixture) counts(t *testing.T) []int {
	t.Helper()
	var money, cost, bindings, events int
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE line_id=$1),
 (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE line_id=$1),(SELECT count(*) FROM reg_crypto_network_fee_bindings WHERE reservation_id=$1),
 (SELECT count(*) FROM sys_outbox WHERE aggregate_id=$1 AND event_type='fee_recognized')`, f.reservation.ID).Scan(&money, &cost, &bindings, &events))
	return []int{money, cost, bindings, events}
}
func TestNetworkFeeV2ConcurrentSettlementIsOneEffect(t *testing.T) {
	f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
	var balance string
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.reservation.PayerWalletID, f.reservation.NativeTokenID).Scan(&balance))
	require.Equal(t, "98", balance)
	f.receipt.Status = crypto.ReceiptReverted
	f.prove(t)
	require.Error(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
}
func TestNetworkFeeV2ZeroAndRevertedFees(t *testing.T) {
	for _, status := range []crypto.ReceiptStatus{crypto.ReceiptSucceeded, crypto.ReceiptReverted} {
		for _, amount := range []int64{0, 2} {
			f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
			f.receipt.Status = status
			f.receipt.Fee.TotalFee = types.NewRawAmountFromInt64(amount)
			f.receipt.Fee.Components[0].Amount = f.receipt.Fee.TotalFee
			f.prove(t)
			require.NoError(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
			expected := 0
			if amount > 0 {
				expected = 1
			}
			require.Equal(t, []int{expected, expected, 1, 1}, f.counts(t))
		}
	}
}
func TestNetworkFeeV2MissingOrWrongProofRetainsReservation(t *testing.T) {
	for _, mode := range []string{"missing", "payer", "amount", "block", "quorum"} {
		t.Run(mode, func(t *testing.T) {
			f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
			switch mode {
			case "missing":
				f.receipt.FeeProof = nil
			case "payer":
				f.receipt.FeeProof.PayerAddress = "0x00000000000000000000000000000000000000cd"
				for i := range f.receipt.FeeProof.Sources {
					f.receipt.FeeProof.Sources[i].Digest = f.receipt.FeeProof.Digest()
				}
			case "amount":
				f.receipt.Fee.TotalFee = types.NewRawAmountFromInt64(3)
			case "block":
				f.receipt.BlockHash = strings.Repeat("d", 64)
			case "quorum":
				f.receipt.FeeProof.Sources[1].TrustGroup = "one"
			}
			require.Error(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
			require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
		})
	}
}
func TestNetworkFeeV2ActivationUsesDocumentOwner(t *testing.T) {
	for _, amount := range []int64{0, 2} {
		f := newNetworkFeeFixture(t, crypto.SigningPurposeActivation)
		f.receipt.Fee.TotalFee = types.NewRawAmountFromInt64(amount)
		f.receipt.Fee.Components[0].Amount = f.receipt.Fee.TotalFee
		f.prove(t)
		q := f.tm.GetQuerier(f.ctx)
		target := id.New()
		_, err := q.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,chain_activation_status) VALUES($1,$1::uuid::text,'Activation target',$2,'0x00000000000000000000000000000000000000cd','target-key','pool','merchant_pool','activating')`, target, f.reservation.NetworkID)
		require.NoError(t, err)
		_, err = q.Exec(f.ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status) VALUES($1,$2,$3,'signing')`, f.reservation.BasisID, target, f.reservation.NetworkID)
		require.NoError(t, err)
		engine := posting.NewLockingEngine(postgres.NewDocLocker())
		engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
		engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
		engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
		engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(crypto_register_repo.NewCryptoPlatformCostRepo())))
		repo := crypto_document_repo.NewCryptoNetworkOperationRepo()
		service := crypto_network_operation.NewService(repo, engine, fixedNetworkOperationNumerator{}, nil)
		coordinator := NewCryptoNetworkOperationCoordinator(repo, service, NewFeeBudgetManager())
		doc, err := coordinator.EnsureActivationBroadcast(f.ctx, AccountActivationTask{ActivationID: f.reservation.BasisID, WalletID: target, NetworkID: f.reservation.NetworkID, Address: "0x00000000000000000000000000000000000000cd", HotWallet: HotWallet{ID: f.reservation.PayerWalletID}, CostTokenID: f.reservation.NativeTokenID}, f.receipt.TxHash)
		require.NoError(t, err)
		finalized, err := coordinator.FinalizeActivation(f.ctx, f.reservation.BasisID, f.reservation.ID, f.receipt)
		require.NoError(t, err)
		require.True(t, finalized.Posted)
		require.Equal(t, &f.reservation.ID, finalized.NetworkFeeReservationID)
		var owners int
		require.NoError(t, q.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_network_fee_bindings WHERE reservation_id=$1 AND recorder_type='CryptoNetworkOperation' AND recorder_id=$2`, f.reservation.ID, doc).Scan(&owners))
		require.Equal(t, 1, owners)
		require.Error(t, engine.Unpost(f.ctx, finalized, func(context.Context) error { return nil }))
		_, err = coordinator.FinalizeActivation(f.ctx, f.reservation.BasisID, f.reservation.ID, f.receipt)
		require.NoError(t, err)
		f.receipt.BlockHash = strings.Repeat("d", 64)
		f.prove(t)
		_, err = coordinator.FinalizeActivation(f.ctx, f.reservation.BasisID, f.reservation.ID, f.receipt)
		require.Error(t, err, "posted activation replay must validate its recognized fee")
	}
}

func TestNetworkFeeV2FundingPrincipalAndFeeHaveDifferentOwners(t *testing.T) {
	for _, status := range []crypto.ReceiptStatus{crypto.ReceiptSucceeded, crypto.ReceiptReverted} {
		payer := "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
		runSweepCoordinationFixtureWithAddresses(t, true, payer, "", func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
			funding, _, receipt := seedNativeFunding(t, ctx, tm, c, 1, status == crypto.ReceiptSucceeded)
			receipt.Status = status
			receipt.Fee.Components = []crypto.FeeComponent{{Name: "energy_fee", Amount: types.ZeroRawAmount()}, {Name: "bandwidth_fee", Amount: receipt.Fee.TotalFee, Quantity: 2}}
			p := crypto.NetworkFeeProof{SchemaVersion: 1, Family: crypto.ChainFamilyTRON, NetworkID: c.network, TxHash: receipt.TxHash, PayerAddress: payer, NativeTokenID: c.native, BlockHash: receipt.BlockHash, BlockNumber: receipt.BlockHeight, Status: status, TotalFee: receipt.Fee.TotalFee, Components: receipt.Fee.Components, Confirmations: receipt.Confirmations, Finality: receipt.Finality, Canonicality: receipt.Canonicality, VerifiedAt: receipt.ObservedAt,
				Sources: []crypto.VerificationSource{{ProviderID: "one", TrustGroup: "one", SourceDigest: strings.Repeat("a", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: 119}, {ProviderID: "two", TrustGroup: "two", SourceDigest: strings.Repeat("b", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: 119}}}
			for i := range p.Sources {
				p.Sources[i].Digest = p.Digest()
			}
			receipt.FeeProof = &p
			require.NoError(t, p.ValidateReceipt(receipt))
			_, err := cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager()).Settle(ctx, funding, receipt)
			require.NoError(t, err)
			var balance string
			var principal, fees int
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT amount::text FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2),
    (SELECT count(*) FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=$3),(SELECT count(*) FROM reg_crypto_network_fee_bindings b JOIN reg_crypto_fee_reservations f ON f.id=b.reservation_id WHERE f.basis_id=$3)`, c.hot, c.native, funding).Scan(&balance, &principal, &fees))
			expected := "998"
			if status == crypto.ReceiptSucceeded {
				expected = "898"
				require.Equal(t, 1, principal)
			} else {
				require.Zero(t, principal)
			}
			require.Equal(t, expected, balance)
			require.Equal(t, 1, fees)
			receipt.Fee.TotalFee = types.NewRawAmountFromInt64(3)
			receipt.Fee.Components[1].Amount = receipt.Fee.TotalFee
			p.TotalFee = receipt.Fee.TotalFee
			p.Components = receipt.Fee.Components
			for i := range p.Sources {
				p.Sources[i].Digest = p.Digest()
			}
			receipt.FeeProof = &p
			_, err = cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, NewFeeBudgetManager()).Settle(ctx, funding, receipt)
			require.Error(t, err, "terminal funding replay must validate its recognized fee")
		})
	}
}

// Omission faults exercise deferred SQL checks independently of the production writer.
func TestNetworkFeeV2IncompleteFinancialSetsRollback(t *testing.T) {
	for _, missing := range []string{"none", "binding", "outbox", "expense", "cost", "components"} {
		t.Run(missing, func(t *testing.T) {
			f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
			err := f.tm.RunInTransaction(f.ctx, func(ctx context.Context) error {
				q := f.tm.GetQuerier(ctx)
				settlement := id.New()
				r := f.receipt
				v := f.reservation
				if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,observed_at,network_fee_reservation_id,raw_receipt) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'{}')`, settlement, r.NetworkID, v.BasisType, v.BasisID, r.TxHash, r.Status, r.BlockHash, r.BlockHeight, r.Finality, v.NativeTokenID, r.Fee.TotalFee, r.ObservedAt, v.ID); err != nil {
					return err
				}
				if missing != "binding" {
					if err := crypto_repo.NewNetworkFeeRepo().Prepare(ctx, v, r, settlement, "", id.Nil()); err != nil {
						return err
					}
				}
				if missing != "expense" {
					if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'ChainSettlement',1,$3,'expense',$4,$5,2)`, v.ID, v.BasisID, r.ObservedAt, v.PayerWalletID, v.NativeTokenID); err != nil {
						return err
					}
				}
				if missing != "cost" {
					if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount) VALUES($1,$2,'ChainSettlement',1,$3,'expense',$4,$5,$5,'chain_network_fee','sweep','chain',$6,2)`, v.ID, v.BasisID, r.ObservedAt, v.NetworkID, v.NativeTokenID, r.TxHash); err != nil {
						return err
					}
				}
				if missing != "components" {
					if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlement_components(settlement_id,component,amount_minor,quantity) VALUES($1,'execution_gas',2,1)`, settlement); err != nil {
						return err
					}
				}
				if missing == "outbox" {
					if _, err := q.Exec(ctx, `DELETE FROM sys_outbox WHERE aggregate_id=$1`, v.ID); err != nil {
						return err
					}
				}
				_, err := q.Exec(ctx, `UPDATE reg_crypto_fee_reservations SET status='settled',actual_fee_minor=2,settled_at=now() WHERE id=$1`, v.ID)
				return err
			})
			if missing == "none" {
				require.NoError(t, err, "complete SQL baseline must commit before testing omissions")
				require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
				return
			}
			require.Error(t, err)
			require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
			var balance, status string
			require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT b.amount::text,f.status FROM reg_crypto_balance_balances b JOIN reg_crypto_fee_reservations f ON f.payer_wallet_id=b.wallet_id AND f.native_token_id=b.token_id WHERE f.id=$1`, f.reservation.ID).Scan(&balance, &status))
			require.Equal(t, "100", balance)
			require.Equal(t, "broadcast", status)
		})
	}
}
func siblingNetworkFee(t *testing.T, f networkFeeFixture, asset id.ID) id.ID {
	t.Helper()
	reservation, basis := id.New(), id.New()
	_, err := f.tm.GetQuerier(f.ctx).Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status) VALUES($1,$2,$3,$4,$4,'NetworkFeeProofTest',$5,'sweep',1,'native_burn',10,$6,'broadcast')`, reservation, f.reservation.NetworkID, f.reservation.PayerWalletID, asset, basis, f.receipt.TxHash)
	require.NoError(t, err)
	return reservation
}
func TestNetworkFeeV2OneOwnerAcrossAliases(t *testing.T) {
	t.Run("alias", func(t *testing.T) {
		f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
		alias := id.New()
		_, err := f.tm.GetQuerier(f.ctx).Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address,is_active) VALUES($1,$1::uuid::text,'Retired native alias',$2,'ETH-ALIAS',18,'native','',false)`, alias, f.reservation.NetworkID)
		require.NoError(t, err)
		second := siblingNetworkFee(t, f, alias)
		require.NoError(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
		f.receipt.Fee.NativeAsset.TokenID = alias
		f.prove(t)
		require.Error(t, NewFeeBudgetManager().Settle(f.ctx, second, f.receipt))
		require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
	})
}
func TestNetworkFeeV2BasisAndGenericReversalAreFrozen(t *testing.T) {
	f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
	q := f.tm.GetQuerier(f.ctx)
	require.NoError(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
	for _, sql := range []string{
		`UPDATE reg_crypto_fee_reservations SET actual_fee_minor=3 WHERE id=$1`,
		`UPDATE reg_crypto_fee_reservations SET tx_hash=repeat('d',64) WHERE id=$1`,
		`DELETE FROM reg_crypto_network_fee_bindings WHERE reservation_id=$1`,
		`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount,reversal_of_line_id) SELECT $2,recorder_id,'NetworkFeeReversal',1,period,'receipt',wallet_id,token_id,amount,line_id FROM reg_crypto_balance_movements WHERE line_id=$1`,
		`INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount,reversal_of_line_id) SELECT $2,recorder_id,'NetworkFeeReversal',1,period,'receipt',network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount,line_id FROM reg_crypto_platform_cost_movements WHERE line_id=$1`,
	} {
		args := []any{f.reservation.ID}
		if strings.Contains(sql, "$2") {
			args = append(args, id.New())
		}
		_, err := q.Exec(f.ctx, sql, args...)
		require.Error(t, err, sql)
	}
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
}
func TestNetworkFeeV2ReplacementRequiresSignedAttemptAndAtomicAudit(t *testing.T) {
	f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
	q := f.tm.GetQuerier(f.ctx)
	fp := "replacement-" + f.reservation.ID.String()
	hash := strings.Repeat("d", 64)
	auth := id.New()
	_, err := q.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET tx_hash=$2,unsigned_fingerprint=$3 WHERE id=$1`, f.reservation.ID, hash, fp)
	require.ErrorContains(t, err, "signed authorization")
	_, err = q.Exec(f.ctx, `INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,amount_minor_units,basis_type,basis_id,generation,expires_at,policy_version,authorization_jws,unsigned_fingerprint,unsigned_envelope,network_sequence)
 SELECT $1,$1::uuid::text,'fee-test','sweep',1,n.id,n.code,n.chain_family,n.network_identifier,w.address,w.address,w.vault_key_id,'0x00000000000000000000000000000000000000cd','native',1,$4,$5,2,now()+interval '1 hour','evm-v1','synthetic',$3,'{}','0' FROM cat_blockchain_networks n JOIN cat_wallets w ON w.network_id=n.id WHERE n.id=$2 AND w.id=$6`, auth, f.reservation.NetworkID, fp, f.reservation.BasisType, f.reservation.BasisID, f.reservation.PayerWalletID)
	require.NoError(t, err)
	_, err = q.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope='{"synthetic":true}',tx_hash=$2 WHERE id=$1`, auth, hash)
	require.NoError(t, err)
	_, err = q.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET tx_hash=$2,unsigned_fingerprint=$3 WHERE id=$1`, f.reservation.ID, hash, fp)
	require.ErrorContains(t, err, "transition event")
	require.NoError(t, NewFeeBudgetManager().MarkReplacementBroadcast(f.ctx, f.reservation.ID, "fee-"+f.reservation.BasisID.String(), f.receipt.TxHash, fp, hash, 2))
	f.receipt.TxHash = hash
	f.prove(t)
	require.NoError(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
	require.Equal(t, []int{1, 1, 1, 1}, f.counts(t))
	var events int
	require.NoError(t, q.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_fee_reservation_events WHERE reservation_id=$1 AND metadata->>'newTxHash'=$2 AND transition_txid IS NOT NULL`, f.reservation.ID, hash).Scan(&events))
	require.Equal(t, 1, events)
}
func TestNetworkFeeV2InvalidPayerCannotCreateCapHold(t *testing.T) {
	f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
	f.receipt.Fee.TotalFee = types.NewRawAmountFromInt64(11)
	f.receipt.Fee.Components[0].Amount = f.receipt.Fee.TotalFee
	f.prove(t)
	f.receipt.FeeProof.PayerAddress = "0x00000000000000000000000000000000000000cd"
	for i := range f.receipt.FeeProof.Sources {
		f.receipt.FeeProof.Sources[i].Digest = f.receipt.FeeProof.Digest()
	}
	require.Error(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt))
	var status string
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT status FROM reg_crypto_fee_reservations WHERE id=$1`, f.reservation.ID).Scan(&status))
	require.Equal(t, "broadcast", status)
	f.prove(t)
	require.ErrorIs(t, NewFeeBudgetManager().Settle(f.ctx, f.reservation.ID, f.receipt), crypto.ErrFeeBudgetExceeded)
	require.NoError(t, f.tm.GetQuerier(f.ctx).QueryRow(f.ctx, `SELECT status FROM reg_crypto_fee_reservations WHERE id=$1`, f.reservation.ID).Scan(&status))
	require.Equal(t, "held", status)
	require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
}

func proveTRONNetworkFee(t *testing.T, r *crypto.ChainReceipt, payer string) {
	t.Helper()
	r.Fee.Components = []crypto.FeeComponent{{Name: "energy_fee", Amount: types.ZeroRawAmount()}, {Name: "bandwidth_fee", Amount: r.Fee.TotalFee, Quantity: 2}}
	p := crypto.NetworkFeeProof{SchemaVersion: 1, Family: crypto.ChainFamilyTRON, NetworkID: r.NetworkID, TxHash: r.TxHash, PayerAddress: payer, NativeTokenID: r.Fee.NativeAsset.TokenID, BlockHash: r.BlockHash, BlockNumber: r.BlockHeight, Status: r.Status, TotalFee: r.Fee.TotalFee, Components: r.Fee.Components, Confirmations: r.Confirmations, Finality: r.Finality, Canonicality: r.Canonicality, VerifiedAt: r.ObservedAt,
		Sources: []crypto.VerificationSource{{ProviderID: "one", TrustGroup: "one", SourceDigest: strings.Repeat("a", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: r.BlockHeight + int64(r.Confirmations) - 1}, {ProviderID: "two", TrustGroup: "two", SourceDigest: strings.Repeat("b", 64), HeadHash: strings.Repeat("c", 64), HeadHeight: r.BlockHeight + int64(r.Confirmations) - 1}}}
	for i := range p.Sources {
		p.Sources[i].Digest = p.Digest()
	}
	r.FeeProof = &p
	require.NoError(t, p.ValidateReceipt(*r))
}
func TestNetworkFeeV2OutboundPrincipalCommitsWithItsIndependentFee(t *testing.T) {
	for _, kind := range []string{"withdrawal", "treasury", "sweep"} {
		for _, status := range []crypto.ReceiptStatus{crypto.ReceiptSucceeded, crypto.ReceiptReverted} {
			t.Run(kind+"/"+string(status), func(t *testing.T) {
				payer := "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb"
				hotAddress, depositAddress := payer, ""
				if kind == "sweep" {
					hotAddress, depositAddress = "", payer
				}
				runSweepCoordinationFixtureWithAddresses(t, true, hotAddress, depositAddress, func(ctx context.Context, tm *postgres.TxManager, c sweepCoordinationFixture) {
					var task outboundapp.SettlementTask
					var r *crypto.ChainReceipt
					var service func() *outboundapp.SettlementService
					switch kind {
					case "withdrawal":
						f := newWithdrawalFinalFixture(t, ctx, tm, c)
						r = &f.receipt
						task = f.task
						service = func() *outboundapp.SettlementService { return f.service(t) }
						f.broadcast(t)
					case "treasury":
						f := newTreasuryFinalFixture(t, ctx, tm, c)
						r = &f.receipt
						task = f.task
						service = func() *outboundapp.SettlementService { return f.service(t) }
						f.broadcast(t)
					case "sweep":
						f := newSweepFinalFixture(t, ctx, tm, c, 1)
						r = &f.receipts[0]
						task = f.tasks[0]
						service = func() *outboundapp.SettlementService { return f.service(t, 0) }
						f.broadcast(t)
					}
					r.Status = status
					proveTRONNetworkFee(t, r, payer)
					require.NoError(t, service().Settle(ctx, task))
					require.NoError(t, service().Settle(ctx, task))
					var fees, principal int
					require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_network_fee_bindings b JOIN reg_crypto_fee_reservations f ON f.id=b.reservation_id WHERE f.network_id=$1),
      (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$2 AND recorder_type IN ('CryptoWithdrawal','TreasuryWithdrawal','CryptoSweep'))`, c.network, task.DocumentID).Scan(&fees, &principal))
					require.Equal(t, 1, fees)
					expected := 0
					if status == crypto.ReceiptSucceeded {
						expected = 1
						if kind == "sweep" {
							expected = 2
						}
					}
					require.Equal(t, expected, principal)
					r.Fee.TotalFee = types.NewRawAmountFromInt64(3)
					proveTRONNetworkFee(t, r, payer)
					require.Error(t, service().Settle(ctx, task), "terminal outbound replay must validate its recognized fee")
				})
			})
		}
	}
}

func TestNetworkFeeV2UnknownBroadcastCannotReleaseItsFeeReserve(t *testing.T) {
	f := newNetworkFeeFixture(t, crypto.SigningPurposeSweep)
	_, err := f.tm.GetQuerier(f.ctx).Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='released',released_at=now() WHERE id=$1`, f.reservation.ID)
	require.ErrorContains(t, err, "unknown signing outcome")
	require.Equal(t, []int{0, 0, 0, 0}, f.counts(t))
}
