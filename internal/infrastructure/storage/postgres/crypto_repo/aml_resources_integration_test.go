//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

func nativeResourceFixture(t *testing.T) (amlRoutingFixture, id.ID, id.ID) {
	t.Helper()
	f := newAMLRoutingFixture(t, false)
	target := f.input.WalletID
	native, feeWallet := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Native operating asset',$2,'TRX',6,'native')`, native, f.input.Request.NetworkID)
	require.NoError(t, err)
	feeAddress := testTRONAddress()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,custody_merchant_id,address,vault_key_id,derivation_path,wallet_role)
 VALUES($1,$1::uuid::text,'Own operating wallet',$2,$3,$4,'resource-key','resource-path','network_fee')`, feeWallet, f.input.Request.NetworkID, f.input.Request.MerchantID, feeAddress)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses)
 VALUES($1,$2,'test-native-v1','synthetic-v1','','TRX','',TRUE,6,TRUE,'synthetic native fixture','{"synthetic":true}',ARRAY['PENDING'])`, f.input.Request.NetworkID, native)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id,enabled,policy_version,provider_account_id,mapping_version)
 VALUES($1,$2,$3,TRUE,'routing-v1',$4,'test-native-v1')`, f.input.Request.MerchantID, f.input.Request.NetworkID, native, f.input.ProviderAccountID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,1,'checkpoint',1,'paused',2)`, f.input.Request.NetworkID)
	require.NoError(t, err)
	f.input.WalletID, f.input.Request.TokenID, f.input.Request.Address = feeWallet, native, feeAddress
	f.input.Request.ContractAddress, f.input.Request.EventKind, f.input.MappingVersion = "", "native_value", "test-native-v1"
	f.baseline(t, feeWallet)
	f.baseline(t, target)
	proof := nativeResourceProof(t, f, f.input.Request.Address, "operating-seed", "100")
	_, effect := f.save(t, proof)
	_, err = f.repo.RegisterAMLOperatingReceipt(f.ctx, effect, "synthetic treasury funding", "integration-test")
	require.NoError(t, err)
	f.complete(t, effect, 1000, true)
	f.rebuild(t)
	require.Equal(t, aml.Low, f.class(t, feeWallet))
	return f, target, effect
}

func nativeResourceProof(t *testing.T, f amlRoutingFixture, destination, tx, amount string) crypto.TransferVerification {
	proof := f.proof(destination, tx, 1)
	proof.Event.TokenContract, proof.Event.EventKind = "", crypto.EventKindNativeValue
	proof.Event.Amount = types.MustCryptoAmount(amount)
	proof.DecoderVersion, proof.Event.DecoderVersion = 2, 2
	for i := range proof.Sources {
		proof.Sources[i].DecoderVersion, proof.Sources[i].SourceDigest = 2, "native-source"
	}
	if len(tx) == 64 {
		proof.Event.EventIndex = 0
		require.NoError(t, proof.BindTRONAccountingIdentity())
	}
	return proof
}

func resourceFee(t *testing.T, f amlRoutingFixture, target id.ID, fundingAmount, cap string) (id.ID, id.ID) {
	t.Helper()
	return resourceFeeWithLink(t, f, target, fundingAmount, cap, true)
}

func resourceFeeWithLink(t *testing.T, f amlRoutingFixture, target id.ID, fundingAmount, cap string, linked bool) (id.ID, id.ID) {
	t.Helper()
	fee, basis := id.New(), id.New()
	purpose, basisType := "sweep", "CryptoSweepAttempt"
	if fundingAmount != "0" {
		purpose, basisType = "native_funding", "CryptoNativeFunding"
		_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_native_fundings(id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,amount_minor)
 VALUES($1,'CryptoSweepAttempt',$2,1,$3,$4,$5,$6,$7)`, basis, id.New(), f.input.Request.NetworkID, f.input.Request.TokenID, f.input.WalletID, target, fundingAmount)
		require.NoError(t, err)
	}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor)
 VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8,1,'native_funding',$9)`, fee, f.input.Request.NetworkID, f.input.WalletID, f.input.Request.TokenID, fundingAmount, basisType, basis, purpose, cap)
	require.NoError(t, err)
	if fundingAmount != "0" && linked {
		_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_native_fundings SET fee_reservation_id=$2 WHERE id=$1`, basis, fee)
		require.NoError(t, err)
	}
	return fee, basis
}

func resourceRequest(t *testing.T, f amlRoutingFixture, fee id.ID) aml.ResourceBudgetRequest {
	t.Helper()
	r := aml.ResourceBudgetRequest{FeeReservationID: fee, MerchantID: f.input.Request.MerchantID, Class: aml.Low}
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT p.revision,r.revision,r.scope_version FROM reg_crypto_aml_resource_positions p JOIN reg_crypto_wallet_risk_states r ON r.wallet_id=p.wallet_id AND r.token_id=p.token_id WHERE p.wallet_id=$1 AND p.token_id=$2`, f.input.WalletID, f.input.Request.TokenID).Scan(&r.ResourceRevision, &r.RiskRevision, &r.ScopeVersion))
	return r
}

func signResourceFunding(t *testing.T, f amlRoutingFixture, target, fee, funding id.ID, networkIdentifiers ...string) string {
	t.Helper()
	var destination, amount, cap string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT w.address,f.amount_minor::text,fee.max_fee_minor::text FROM doc_crypto_native_fundings f JOIN cat_wallets w ON w.id=f.destination_wallet_id JOIN reg_crypto_fee_reservations fee ON fee.id=f.fee_reservation_id WHERE f.id=$1 AND w.id=$2`, funding, target).Scan(&destination, &amount, &cap))
	networkIdentifier := "nile"
	if len(networkIdentifiers) > 0 {
		networkIdentifier = networkIdentifiers[0]
	}
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.input.Request.MerchantID,
		Purpose: crypto.SigningPurposeNativeFunding, BasisType: "CryptoNativeFunding", BasisID: funding, Attempt: 1, Generation: 1, ExpiresAt: time.Now().Add(time.Hour),
		NetworkID: f.input.Request.NetworkID, NetworkCode: "RESOURCE-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: networkIdentifier, SourceAddress: f.input.Request.Address, FeePayerAddress: f.input.Request.Address,
		VaultKeyID: "resource-key", DestinationAddress: destination, AssetStandard: "native", AmountMinorUnits: types.MustCryptoAmount(amount), MaxNetworkFeeMinor: types.MustCryptoAmount(cap), UnsignedFingerprint: "resource-" + funding.String(), PolicyVersion: "tron-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Payload: []byte(`{"synthetic":true}`)}
	authID, err := newHistoricalAMLAuthorization(f.ctx, auth, unsigned)
	require.NoError(t, err)
	txHash := strings.ReplaceAll(funding.String()+funding.String(), "-", "")
	signed, err := json.Marshal(crypto.SignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: 1, Generation: 1, UnsignedFingerprint: auth.UnsignedFingerprint, TxHash: txHash, Payload: []byte(`{"synthetic":true}`)})
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',tx_hash=$2,signed_envelope=$3::jsonb WHERE id=$1`, authID, txHash, string(signed))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_expected_chain_effects(signing_authorization_id,ordinal,network_id,event_kind,source_address_key,destination_address_key,token_id,contract_address_key,amount,tx_hash)
 VALUES($1,0,$2,'native_value',$3,$4,$5,'',$6,$7)`, authID, f.input.Request.NetworkID, auth.SourceAddress, destination, f.input.Request.TokenID, amount, txHash)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='broadcast',tx_hash=$2,unsigned_fingerprint=$3 WHERE id=$1`, fee, txHash, auth.UnsignedFingerprint)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_native_fundings SET status='broadcast',signing_authorization_id=$2,tx_hash=$3,unsigned_fingerprint=$4,signed_envelope=$5::jsonb WHERE id=$1`, funding, authID, txHash, auth.UnsignedFingerprint, string(signed))
	require.NoError(t, err)
	return txHash
}

func settleResourceEvidence(t *testing.T, f amlRoutingFixture, fee, funding id.ID, txHash, status, actual string, fundingPhysical bool) {
	t.Helper()
	require.NoError(t, recordResourceEvidence(f, fee, funding, txHash, status, actual, fundingPhysical))
}

func recordResourceEvidence(f amlRoutingFixture, fee, funding id.ID, txHash, status, actual string, fundingPhysical bool) error {
	receipt := crypto.ChainReceipt{NetworkID: f.input.Request.NetworkID, TxHash: txHash, Status: crypto.ReceiptStatus(status), BlockHash: strings.Repeat("a", 64), BlockHeight: 10, Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.input.Request.TokenID}, TotalFee: types.MustCryptoAmount(actual)}}
	return postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		repo := NativeFundingSettlementRepo{}
		var doc crypto.NativeFunding
		if fundingPhysical {
			var err error
			if doc, err = repo.GetNativeFundingForUpdate(ctx, funding); err != nil {
				return err
			}
			if err := repo.PrepareNativeFundingPrincipal(ctx, doc, receipt); err != nil {
				return err
			}
		}
		if err := recognizeTestNetworkFee(ctx, fee, receipt); err != nil {
			return err
		}
		if id.IsNil(funding) {
			return nil
		}
		terminal := "settled"
		if status == "reverted" {
			terminal = "failed"
		}
		if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_native_fundings SET status=$2 WHERE id=$1`, funding, terminal); err != nil {
			return err
		}
		if fundingPhysical {
			return repo.RecordNativeFundingPrincipal(ctx, doc, receipt)
		}
		return nil
	})
}

func resourceAmounts(t *testing.T, f amlRoutingFixture, target id.ID) []string {
	t.Helper()
	var source, destination string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_aml_resource_positions WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID).Scan(&source))
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_aml_resource_positions WHERE wallet_id=$1 AND token_id=$2`, target, f.input.Request.TokenID).Scan(&destination))
	return []string{source, destination}
}

func TestAMLResourceOperatingReceiptCannotCreditMerchantOrReplay(t *testing.T) {
	f, target, effect := nativeResourceFixture(t)
	first, err := f.repo.RegisterAMLOperatingReceipt(f.ctx, effect, "synthetic treasury funding", "integration-test")
	require.NoError(t, err)
	again, err := f.repo.RegisterAMLOperatingReceipt(f.ctx, effect, "synthetic treasury funding", "integration-test")
	require.NoError(t, err)
	require.Equal(t, first, again)
	_, err = f.repo.RegisterAMLOperatingReceipt(f.ctx, effect, "different evidence", "integration-test")
	require.Error(t, err)
	require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
	var credits, income int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements),(SELECT COUNT(*) FROM reg_crypto_fee_movements)`).Scan(&credits, &income))
	require.Zero(t, credits)
	require.Zero(t, income)
	var covered bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_total_inventory_covered($1,$2)`, f.input.WalletID, f.input.Request.TokenID).Scan(&covered))
	require.True(t, covered)
}

func TestAMLResourceFundingSuccessAndRevert(t *testing.T) {
	for _, status := range []string{"succeeded", "reverted"} {
		t.Run(status, func(t *testing.T) {
			f, target, seed := nativeResourceFixture(t)
			fee, funding := resourceFee(t, f, target, "20", "10")
			request := resourceRequest(t, f, fee)
			budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, request)
			require.NoError(t, err)
			require.Equal(t, "30", budget.Requirement.Total().String())
			again, err := f.repo.ReserveAMLResourceBudget(f.ctx, request)
			require.NoError(t, err)
			require.Equal(t, budget.ID, again.ID)
			txHash := signResourceFunding(t, f, target, fee, funding)
			require.Error(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "unknown result", "operator"))
			if status == "succeeded" {
				_, err = f.repo.RequestAMLRescreen(f.ctx, seed, 1, "late high after signing", "operator")
				require.NoError(t, err)
				f.complete(t, seed, 9000, true)
			}
			f.rebuild(t)
			require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target), "broadcast alone cannot debit or release resources")
			if status == "succeeded" {
				require.Error(t, recordResourceEvidence(f, fee, funding, txHash, status, "3", true), "native funding cannot commit principal before canonical transfer proof")
				require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
				var address string
				require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&address))
				proof := nativeResourceProof(t, f, address, txHash, "20")
				proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
				f.save(t, proof)
				settleResourceEvidence(t, f, fee, funding, txHash, status, "3", true)
				f.rebuild(t)
				require.Equal(t, []string{"77", "20"}, resourceAmounts(t, f, target))
				require.Equal(t, aml.High, f.class(t, target), "late high follows realized funding without suppressing its accounting")
			} else {
				settleResourceEvidence(t, f, fee, funding, txHash, status, "3", false)
				f.rebuild(t)
				require.Equal(t, []string{"97", "0"}, resourceAmounts(t, f, target), "revert burns only the proven fee")
			}
			var outcome, actual string
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status,actual_fee::text FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&outcome, &actual))
			if status == "succeeded" {
				require.Equal(t, "settled", outcome)
			} else {
				require.Equal(t, "failed", outcome)
			}
			require.Equal(t, "3", actual)
			f.rebuild(t)
			var covered bool
			require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fn_crypto_aml_total_inventory_covered($1,$2)`, f.input.WalletID, f.input.Request.TokenID).Scan(&covered))
			require.True(t, covered)
		})
	}
}

func TestAMLResourceCancellationAndInsufficientOperatingBalance(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, _ := resourceFee(t, f, target, "95", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.ErrorContains(t, err, "insufficient operating funds")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='released' WHERE id=$1`, fee)
	require.NoError(t, err)
	fee, _ = resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='released' WHERE id=$1`, fee)
	require.ErrorContains(t, err, "audited resource cancellation")
	require.NoError(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "plan replaced before signing", "operator"))
	require.NoError(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "plan replaced before signing", "operator"))
	require.ErrorContains(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "different cancellation reason", "operator"), "original reason or actor")
	require.ErrorContains(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "plan replaced before signing", "another operator"), "original reason or actor")
	var state string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_fee_reservations WHERE id=$1`, fee).Scan(&state))
	require.Equal(t, "released", state)
	require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
}

func TestAMLResourceLateHighBlocksNewAuthorization(t *testing.T) {
	f, target, effect := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	_, err = f.repo.RequestAMLRescreen(f.ctx, effect, 1, "late adverse intelligence", "operator")
	require.NoError(t, err)
	f.complete(t, effect, 9000, true)
	f.rebuild(t)
	var amount, address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount_minor::text,w.address FROM doc_crypto_native_fundings f JOIN cat_wallets w ON w.id=f.destination_wallet_id WHERE f.id=$1`, funding).Scan(&amount, &address))
	auth := crypto.SigningAuthorization{SchemaVersion: 1, AuthorizationID: id.New().String(), TenantID: id.New().String(), MerchantID: &f.input.Request.MerchantID, Purpose: crypto.SigningPurposeNativeFunding, BasisType: "CryptoNativeFunding", BasisID: funding,
		NetworkID: f.input.Request.NetworkID, NetworkCode: "RESOURCE-TEST", ChainFamily: crypto.ChainFamilyTRON, NetworkIdentifier: "nile", SourceAddress: f.input.Request.Address, VaultKeyID: "resource-key", DestinationAddress: address, AssetStandard: "native",
		AmountMinorUnits: types.MustCryptoAmount(amount), MaxNetworkFeeMinor: types.MustCryptoAmount("10"), UnsignedFingerprint: "late-high", ExpiresAt: time.Now().Add(time.Hour), PolicyVersion: "tron-v1"}
	_, err = newHistoricalAMLAuthorization(f.ctx, auth, crypto.UnsignedTxEnvelope{Payload: []byte(`{}`)})
	require.ErrorContains(t, err, "authorization is cancelled or stale")
}

func TestAMLResourceEvidenceCannotBeRolledBack(t *testing.T) {
	f, _, _ := nativeResourceFixture(t)
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, migrationsBeforeAML)
	require.ErrorContains(t, err, "cannot roll back AML resource evidence")
}

func TestAMLResourceGasCannotBorrowMerchantPrincipalOrProcessingFee(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	proof := nativeResourceProof(t, f, f.input.Request.Address, "native-merchant-payment", "100")
	proofID, effect := f.save(t, proof)
	f.postPayment(t, proof, proofID, f.input.WalletID)
	f.creditNet(t, proof.Event.TxHash)
	f.complete(t, effect, 1000, true)
	f.rebuild(t)
	fee, _ := resourceFee(t, f, target, "95", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.ErrorContains(t, err, "insufficient operating funds", "physical balance 200 includes principal and fee income")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='released' WHERE id=$1`, fee)
	require.NoError(t, err)
	fee, _ = resourceFee(t, f, target, "0", "10")
	_, err = f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='broadcast',tx_hash=$2 WHERE id=$1`, fee, strings.Repeat("fe", 32))
	require.NoError(t, err)
	settleResourceEvidence(t, f, fee, id.ID{}, strings.Repeat("fe", 32), "succeeded", "3", false)
	f.rebuild(t)
	require.Equal(t, []string{"97", "0"}, resourceAmounts(t, f, target))
	var principal, income string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT principal::text,service_fee::text FROM reg_crypto_aml_inventory_positions WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID).Scan(&principal, &income))
	require.Equal(t, []string{"90", "10"}, []string{principal, income})
	_, balances, err := f.repo.ReadMerchantRiskBalance(f.ctx, f.input.Request.MerchantID)
	require.NoError(t, err)
	for _, balance := range balances {
		if balance.Scope.TokenID == f.input.Request.TokenID {
			require.Equal(t, "90", balance.Ledger.String())
			require.Len(t, balance.Sources, 1)
			require.True(t, balance.Sources[0].Covered)
			require.False(t, balance.Sources[0].Held)
		}
	}
}

func TestAMLResourceConcurrentReserveAndUnpostedDestination(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, _ := resourceFee(t, f, target, "20", "10")
	request := resourceRequest(t, f, fee)
	results := make([]aml.ResourceBudget, 2)
	errors := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() { results[i], errors[i] = f.repo.ReserveAMLResourceBudget(f.ctx, request) })
	}
	wg.Wait()
	for _, err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, results[0].ID, results[1].ID)
	require.NoError(t, f.repo.CancelAMLResourceBudget(f.ctx, results[0].ID, "new plan required", "operator"))
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&address))
	f.save(t, nativeResourceProof(t, f, address, "unposted-native-target", "15"))
	fee, _ = resourceFee(t, f, target, "20", "10")
	_, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.ErrorContains(t, err, "destination or risk mismatch")
}

func TestAMLResourceProjectionConflictRollsBackAndRetries(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	txHash := signResourceFunding(t, f, target, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&address))
	proof := nativeResourceProof(t, f, address, txHash, "20")
	proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
	f.save(t, proof)
	settleResourceEvidence(t, f, fee, funding, txHash, "succeeded", "3", true)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_resource_positions SET amount=999 WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	progressed, err := f.repo.CaptureNextAMLInventoryAllocation(f.ctx)
	require.True(t, progressed)
	require.ErrorContains(t, err, "projection disagrees")
	var state string
	var facts, attempts int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT b.status,(SELECT COUNT(*) FROM reg_crypto_aml_resource_facts WHERE budget_id=b.id),q.attempts FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_aml_inventory_queue q ON q.basis_id=b.id AND q.kind='resource' WHERE b.id=$1`, budget.ID).Scan(&state, &facts, &attempts))
	require.Equal(t, "reserved", state)
	require.Zero(t, facts)
	require.Equal(t, 1, attempts)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_resource_positions SET amount=100 WHERE wallet_id=$1 AND token_id=$2`, f.input.WalletID, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_inventory_queue SET next_attempt_at=NOW() WHERE kind='resource' AND basis_id=$1`, budget.ID)
	require.NoError(t, err)
	f.rebuild(t)
	require.Equal(t, []string{"77", "20"}, resourceAmounts(t, f, target))
}

func TestAMLResourceOverCapAndUnknownOutcomeRetainReservation(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	signResourceFunding(t, f, target, fee, funding)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_fee_reservations SET status='held',hold_reason='proven actual fee exceeds cap' WHERE id=$1`, fee)
	require.NoError(t, err)
	f.rebuild(t)
	var state string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&state))
	require.Equal(t, "reserved", state)
	require.Error(t, f.repo.CancelAMLResourceBudget(f.ctx, budget.ID, "cannot infer final outcome", "operator"))
	require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
}

func TestAMLResourceFundingProofMustMatchFinalSettlementBlock(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	txHash := signResourceFunding(t, f, target, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&address))
	proof := nativeResourceProof(t, f, address, txHash, "20")
	proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
	proof.Event.BlockNumber = 11
	proof.Event.Confirmations = 19
	f.save(t, proof)
	require.Error(t, recordResourceEvidence(f, fee, funding, txHash, "succeeded", "3", true), "wrong inclusion cannot commit funding principal")
	f.rebuild(t)
	require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
	var state string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&state))
	require.Equal(t, "reserved", state, "a matching tx hash in another block does not prove this settlement")
}

func TestAMLResourceContradictoryRevertAndTransferProofRetainBudget(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFee(t, f, target, "20", "10")
	budget, err := f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	txHash := signResourceFunding(t, f, target, fee, funding)
	var address string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT address FROM cat_wallets WHERE id=$1`, target).Scan(&address))
	proof := nativeResourceProof(t, f, address, txHash, "20")
	proof.Event.FromAddress, proof.Event.Direction = f.input.Request.Address, crypto.TransferDirectionInternal
	f.save(t, proof)
	settleResourceEvidence(t, f, fee, funding, txHash, "reverted", "3", false)
	f.rebuild(t)
	var state string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_aml_resource_budgets WHERE id=$1`, budget.ID).Scan(&state))
	require.Equal(t, "reserved", state, "contradictory final evidence cannot release the funding commitment")
	require.Equal(t, []string{"100", "0"}, resourceAmounts(t, f, target))
}

func TestAMLJITResourceBudgetAtomicallyLinksItsFeeBeforeGuard(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	fee, funding := resourceFeeWithLink(t, f, target, "10", "2", false)
	require.NoError(t, f.repo.EnsureAMLResourceBudget(f.ctx, fee))
	require.NoError(t, f.repo.EnsureAMLResourceBudget(f.ctx, fee))
	var linked id.ID
	var budgets int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT fee_reservation_id FROM doc_crypto_native_fundings WHERE id=$1`, funding).Scan(&linked))
	require.Equal(t, fee, linked)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=$1 AND funding_id=$2`, fee, funding).Scan(&budgets))
	require.Equal(t, 1, budgets)
}
