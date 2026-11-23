//go:build integration

package crypto_worker

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func custodyTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_ADMIN_DSN is required for isolated custody tests")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	name := "custody_test_" + id.Compact(id.New())
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	address, err := url.Parse(dsn)
	require.NoError(t, err)
	address.Path = "/" + name
	_, err = sdktest.Migrate(address.String(), cryptoschema.Migration())
	require.NoError(t, err)
	pool, err := sdktest.OpenPool(t.Context(), address.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return tenant.WithTxManager(t.Context(), sdktest.NewTxManager(pool)), pool
}

type custodyFixture struct {
	ctx                  context.Context
	pool                 *pgxpool.Pool
	network, token, a, b id.ID
}

func newCustodyFixture(t *testing.T) custodyFixture {
	t.Helper()
	ctx, pool := custodyTestDatabase(t)
	f := custodyFixture{ctx: ctx, pool: pool, network: id.New(), token: id.New(), a: id.New(), b: id.New()}
	for _, merchant := range []id.ID{f.a, f.b} {
		_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Custody test')`, merchant, merchant.String())
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
 VALUES($1,$2,'Custody network','evm',$2,'ETH',18)`, f.network, f.network.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$2,'Custody token',$3,'ETH',18,'native')`, f.token, f.token.String(), f.network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE sys_crypto_network_policies SET enabled=FALSE,per_transaction_max_fee_minor=100,daily_network_fee_budget_minor=10000 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	return f
}

func (f custodyFixture) wallet(t *testing.T, owner *id.ID, role, status, balance string) id.ID {
	t.Helper()
	walletID := id.New()
	tier := "hot"
	if role == "merchant_pool" {
		tier = "pool"
	}
	address := "0x" + id.Compact(walletID) + "00000000"
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,derivation_path,custody_merchant_id,wallet_role,tier,status,chain_activation_status)
 VALUES($1,$2,'Custody wallet',$3,$4,$2,$2,$5,$6,$7,$8,'active')`, walletID, walletID.String(), f.network, address, owner, role, tier, status)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,$3)`, walletID, f.token, balance)
	require.NoError(t, err)
	return walletID
}

func (f custodyFixture) enforce(t *testing.T) {
	t.Helper()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, f.network)
	require.NoError(t, err)
}

func (f custodyFixture) request(t *testing.T, merchant id.ID, amount string) id.ID {
	t.Helper()
	requestID := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_withdrawal_requests(id,merchant_id,token_id,amount,dest_address,status) VALUES($1,$2,$3,$4,'0x1111111111111111111111111111111111111111','approved')`, requestID, merchant, f.token, amount)
	require.NoError(t, err)
	return requestID
}

func TestCustodyWithdrawalNeverBorrowsAndKeepsForeignReservation(t *testing.T) {
	f := newCustodyFixture(t)
	own := f.wallet(t, &f.a, "liquidity_hot", "free", "100")
	foreign := f.wallet(t, &f.b, "liquidity_hot", "free", "100000")
	f.wallet(t, nil, "liquidity_hot", "free", "1000000")
	f.enforce(t)
	repo := NewOutboundRepo()
	request := f.request(t, f.a, "70")
	selected, err := repo.ReserveHotWalletForWithdrawal(f.ctx, request, f.a, f.network, f.token, types.MustRawAmount("70"))
	require.NoError(t, err)
	require.Equal(t, own, selected.ID)
	replay, err := repo.ReserveHotWalletForWithdrawal(f.ctx, request, f.a, f.network, f.token, types.MustRawAmount("70"))
	require.NoError(t, err)
	require.Equal(t, selected.ID, replay.ID)
	_, err = repo.ReserveHotWalletForWithdrawal(f.ctx, f.request(t, f.a, "40"), f.a, f.network, f.token, types.MustRawAmount("40"))
	require.ErrorIs(t, err, ErrNoHotWalletLiquidity)
	_, err = repo.ReserveHotWalletForWithdrawal(f.ctx, request, f.b, f.network, f.token, types.MustRawAmount("70"))
	require.ErrorIs(t, err, ErrMerchantCustody)
	bad := f.request(t, f.a, "10")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_balance_reservations(wallet_id,token_id,amount,purpose,document_type,document_id) VALUES($1,$2,10,'withdrawal','WithdrawalRequest',$3)`, foreign, f.token, bad)
	require.NoError(t, err)
	_, err = repo.ReserveHotWalletForWithdrawal(f.ctx, bad, f.a, f.network, f.token, types.MustRawAmount("10"))
	require.ErrorIs(t, err, ErrMerchantCustody)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_reservations WHERE document_id=$1`, bad).Scan(&count))
	require.Equal(t, 1, count, "never create a replacement source after hiding a foreign reservation")
}

func TestCustodySweepAndFundingStayInsideOwner(t *testing.T) {
	f := newCustodyFixture(t)
	origin := f.wallet(t, &f.a, "merchant_pool", "sweep_pending", "100")
	collector := f.wallet(t, &f.a, "liquidity_hot", "free", "0")
	f.wallet(t, &f.b, "liquidity_hot", "free", "100000")
	f.wallet(t, nil, "liquidity_hot", "free", "1000000")
	f.wallet(t, nil, "network_fee", "free", "1000000")
	ownFee := f.wallet(t, &f.a, "network_fee", "free", "1000")
	f.wallet(t, &f.b, "network_fee", "free", "1000")
	f.enforce(t)
	tasks, err := NewOutboundRepo().ClaimSweepPendingWallets(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, origin, tasks[0].WalletID)
	require.Equal(t, collector, tasks[0].HotWallet().ID)
	funding, err := custodyService().ResolveNativeFundingSource(f.ctx, f.network, origin)
	require.NoError(t, err)
	require.Equal(t, ownFee, funding.ID)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, ownFee)
	require.NoError(t, err)
	_, err = custodyService().ResolveNativeFundingSource(f.ctx, f.network, origin)
	require.ErrorIs(t, err, ErrMerchantCustody)
	// An owner without a collector is held rather than swept into another pool.
	foreignOrigin := f.wallet(t, &f.b, "merchant_pool", "sweep_pending", "100")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE custody_merchant_id=$1 AND wallet_role='liquidity_hot'`, f.b)
	require.NoError(t, err)
	tasks, err = NewOutboundRepo().ClaimSweepPendingWallets(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, tasks, fmt.Sprint(foreignOrigin))
}

func TestFundingReadinessRequiresFeeCapitalForEveryCustodyOwner(t *testing.T) {
	f := newCustodyFixture(t)
	f.wallet(t, &f.a, "merchant_pool", "free", "100")
	f.wallet(t, &f.a, "network_fee", "free", "1000")
	f.wallet(t, &f.b, "merchant_pool", "free", "100")
	f.wallet(t, nil, "network_fee", "free", "1000000")
	repo := crypto_repo.NewNetworkPolicyRepo()
	minimum := types.NewRawAmountFromInt64(10)
	ready, err := repo.NetworkFundingSourcesReady(f.ctx, f.network, minimum)
	require.NoError(t, err)
	require.False(t, ready, "platform and another merchant cannot supply the missing owner's fee source")
	f.wallet(t, &f.b, "network_fee", "free", "9")
	ready, err = repo.NetworkFundingSourcesReady(f.ctx, f.network, minimum)
	require.NoError(t, err)
	require.False(t, ready, "a registered source must have its own minimum capital")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_balance_balances SET amount=10 WHERE wallet_id=(SELECT id FROM cat_wallets WHERE network_id=$1 AND custody_merchant_id=$2 AND wallet_role='network_fee') AND token_id=$3`, f.network, f.b, f.token)
	require.NoError(t, err)
	ready, err = repo.NetworkFundingSourcesReady(f.ctx, f.network, minimum)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestCustodyAllocationNeverReusesAnotherMerchantAddress(t *testing.T) {
	f := newCustodyFixture(t)
	own := f.wallet(t, &f.a, "merchant_pool", "free", "0")
	foreign := f.wallet(t, &f.b, "merchant_pool", "free", "0")
	f.wallet(t, nil, "merchant_pool", "free", "0")
	f.enforce(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE,needs_review=FALSE,max_total_wallets=10,max_persistent_addresses_per_merchant=10,max_active_invoice_leases_per_merchant=10 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	repo := crypto_catalog_repo.NewWalletRepo()
	assigned, err := repo.AssignPersistentAddress(f.ctx, f.a, f.network, "customer")
	require.NoError(t, err)
	require.Equal(t, own, assigned.ID)
	replay, err := repo.AssignPersistentAddress(f.ctx, f.a, f.network, "customer")
	require.NoError(t, err)
	require.Equal(t, own, replay.ID)
	other, err := repo.AssignPersistentAddress(f.ctx, f.b, f.network, "customer")
	require.NoError(t, err)
	require.Equal(t, foreign, other.ID)
	_, err = repo.LeaseForInvoice(f.ctx, f.a, id.New(), f.network, f.token, time.Now().Add(time.Hour))
	require.Error(t, err, "no owned address remains; shared free address cannot be used")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET merchant_id=$2 WHERE id=$1`, own, f.b)
	require.ErrorContains(t, err, "crosses merchant")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET custody_merchant_id=$2 WHERE id=$1`, own, f.b)
	require.ErrorContains(t, err, "immutable")
}

func TestCustodyAuthorizerRejectsCrossOwnerBeforeSigning(t *testing.T) {
	f := newCustodyFixture(t)
	pool := f.wallet(t, &f.a, "merchant_pool", "free", "100")
	hot := f.wallet(t, &f.a, "liquidity_hot", "free", "100")
	foreign := f.wallet(t, &f.b, "liquidity_hot", "free", "100")
	fee := f.wallet(t, &f.a, "network_fee", "free", "100")
	f.enforce(t)
	service := &SigningAuthorizationService{}
	ownWallet, err := (custodyStore{}).LoadWallet(f.ctx, pool)
	require.NoError(t, err)
	ownHot, err := (custodyStore{}).LoadWallet(f.ctx, hot)
	require.NoError(t, err)
	foreignHot, err := (custodyStore{}).LoadWallet(f.ctx, foreign)
	require.NoError(t, err)
	ownFee, err := (custodyStore{}).LoadWallet(f.ctx, fee)
	require.NoError(t, err)
	for _, tc := range []struct {
		purpose             crypto.SigningPurpose
		source, destination outboundapp.CustodyWallet
		merchant            *id.ID
		ok                  bool
	}{
		{crypto.SigningPurposeSweep, ownWallet, ownHot, nil, true}, {crypto.SigningPurposeSweep, ownWallet, foreignHot, nil, false},
		{crypto.SigningPurposeNativeFunding, ownFee, ownWallet, nil, true}, {crypto.SigningPurposeActivation, ownFee, foreignHot, nil, false},
		{crypto.SigningPurposeTreasury, ownHot, foreignHot, nil, false}, {crypto.SigningPurposeWithdrawal, foreignHot, ownWallet, &f.a, false},
		{crypto.SigningPurposeWithdrawal, ownHot, foreignHot, &f.a, false},
	} {
		_, err := service.PrepareTransferCustody(f.ctx, crypto.TransferIntent{Purpose: tc.purpose, Network: crypto.NetworkRef{ID: f.network}, SourceWalletID: tc.source.ID, SourceAddress: tc.source.Address, DestinationAddress: tc.destination.Address}, tc.merchant)
		if tc.ok {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrMerchantCustody)
		}
	}
	auth := crypto.SigningAuthorization{NetworkID: f.network, SourceAddress: ownWallet.Address, VaultKeyID: ownWallet.VaultKeyID, DestinationAddress: ownHot.Address, Purpose: crypto.SigningPurposeSweep}
	require.ErrorIs(t, service.CheckCustody(f.ctx, auth), ErrMerchantCustody, "old unbound authorization cannot be relabelled for merchant custody")
	auth.MerchantID = &f.a
	require.NoError(t, service.CheckCustody(f.ctx, auth))
	require.ErrorContains(t, service.CheckCustodySigner(f.ctx, crypto.NetworkRef{ID: f.network}, readinessTestSigner{}), "no merchant custody support")
}

func TestCustodyPolicyRequiresPausedNetworkAndCannotDowngrade(t *testing.T) {
	f := newCustodyFixture(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_custody_policies(network_id,enforced) VALUES($1,TRUE)`, f.network)
	require.ErrorContains(t, err, "pause network")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	f.enforce(t)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_custody_policies SET enforced=FALSE,version=version+1 WHERE network_id=$1`, f.network)
	require.ErrorContains(t, err, "cannot be downgraded")
	_, err = f.pool.Exec(f.ctx, `DELETE FROM sys_crypto_custody_policies WHERE network_id=$1`, f.network)
	require.ErrorContains(t, err, "cannot be deleted")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_custody_policies SET version=version+2 WHERE network_id=$1`, f.network)
	require.ErrorContains(t, err, "new version")
	f.wallet(t, &f.a, "network_fee", "free", "0")
	f.wallet(t, &f.b, "network_fee", "free", "0")
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,custody_merchant_id,wallet_role,tier) VALUES($1,$2,'Duplicate fee',$3,$2,$4,'network_fee','hot')`, id.New(), id.New().String(), f.network, f.a)
	require.Error(t, err, "only one active fee wallet per merchant/network")
}

type custodyReceiptDriver struct{ fundingReceiptDriver }

func (d custodyReceiptDriver) Capabilities() crypto.ChainFamilyCapabilities {
	c := d.fundingReceiptDriver.Capabilities()
	c.Family = crypto.ChainFamilyEVM
	return c
}

func TestCustodyLegacySignedSweepRecoversAfterIsolationWithoutSigner(t *testing.T) {
	f := newCustodyFixture(t)
	origin := f.wallet(t, &f.a, "merchant_pool", "sweeping", "1000")
	foreignCollector := f.wallet(t, &f.b, "liquidity_hot", "free", "0")
	from, err := (custodyStore{}).LoadWallet(f.ctx, origin)
	require.NoError(t, err)
	to, err := (custodyStore{}).LoadWallet(f.ctx, foreignCollector)
	require.NoError(t, err)
	attemptID := id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,claimed_at,expires_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,100,'held',NOW(),NOW()+interval '1 hour',NOW()-interval '10 minutes')`, attemptID, origin, f.token, f.network, foreignCollector, from.Address, to.Address)
	require.NoError(t, err)
	auth := crypto.SigningAuthorization{SchemaVersion: 2, AuthorizationID: id.New().String(), TenantID: id.New().String(), Purpose: crypto.SigningPurposeSweep,
		BasisType: "CryptoSweepAttempt", BasisID: attemptID, Attempt: 1, Generation: 1, NetworkSequence: "0", ExpiresAt: time.Now().Add(time.Hour),
		NetworkID: f.network, NetworkCode: f.network.String(), ChainFamily: crypto.ChainFamilyEVM, NetworkIdentifier: f.network.String(), SourceAddress: from.Address, FeePayerAddress: from.Address,
		VaultKeyID: from.VaultKeyID, DestinationAddress: to.Address, AssetStandard: "native", AmountMinorUnits: types.MustRawAmount("100"), MaxNetworkFeeMinor: types.MustRawAmount("10"), UnsignedFingerprint: "historical-fingerprint", PolicyVersion: "evm-v1"}
	unsigned := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, NetworkSequence: "0", Payload: []byte(`{"historical":true}`)}
	repo := NewOutboundRepo()
	_, err = repo.InsertSigningAuthorization(f.ctx, auth, "historical-jws", unsigned)
	require.NoError(t, err)
	signed := crypto.SignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: 1, UnsignedFingerprint: auth.UnsignedFingerprint, Generation: 1, NetworkSequence: "0", TxHash: "historical-tx", Payload: []byte(`{"historical":true}`)}
	require.NoError(t, repo.MarkSigningAuthorizationSigned(f.ctx, auth.AuthorizationID, signed))
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status,unsigned_fingerprint)
 VALUES($1,$2,$3,$3,$4,$5,'sweep',1,'native_burn',10,'held',$6)`, f.network, origin, f.token, "CryptoSweepAttempt", attemptID, auth.UnsignedFingerprint)
	require.NoError(t, err)
	f.enforce(t)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET is_active=FALSE WHERE id=$1`, origin)
	require.NoError(t, err)
	// The recovery claim deliberately ignores current source eligibility.
	tasks, err := repo.ClaimRunnableSweepAttempts(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.True(t, tasks[0].RecoveryOnly)
	registry := crypto.NewChainDriverRegistry()
	network := crypto.NetworkRef{ID: f.network, Code: f.network.String(), Family: crypto.ChainFamilyEVM, NetworkIdentifier: f.network.String(), NativeTokenID: f.token, NativeDecimals: 18, RequiredConfirmations: 1, FinalityMode: crypto.FinalityByConfirmations}
	receipt := crypto.ChainReceipt{NetworkID: f.network, TxHash: signed.TxHash, Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized}
	driver := custodyReceiptDriver{fundingReceiptDriver{network: network, receipt: receipt}}
	require.NoError(t, registry.Register(driver))
	// Both transaction builder and broadcaster are nil. Recovery may only read.
	executor := &SweepExecutor{drivers: registry, outboundRepo: repo, feeBudget: NewFeeBudgetManager()}
	result, err := executor.application().BroadcastLine(f.ctx, tasks[0], tasks[0].HotWallet(), id.New())
	require.NoError(t, err)
	require.Equal(t, signed.TxHash, result.TxHash)
	var status string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT status FROM reg_crypto_signing_authorizations WHERE authorization_id=$1`, auth.AuthorizationID).Scan(&status))
	require.Equal(t, "broadcast", status)
	var merchant *id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT merchant_id FROM reg_crypto_signing_authorizations WHERE authorization_id=$1`, auth.AuthorizationID).Scan(&merchant))
	require.Nil(t, merchant, "historical authorization must never be relabelled")
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_signing_authorizations WHERE basis_id=$1`, attemptID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestCustodyTreasuryOnlyReservesPlatformTreasuryWallet(t *testing.T) {
	f := newCustodyFixture(t)
	platform := f.wallet(t, nil, "treasury", "free", "1000")
	merchant := f.wallet(t, &f.a, "liquidity_hot", "free", "1000000")
	f.wallet(t, nil, "liquidity_hot", "free", "100000")
	f.enforce(t)
	wallets, err := crypto_repo.NewTreasuryWorkflowRepo().ListTreasurySourceWallets(f.ctx, f.token)
	require.NoError(t, err)
	require.Len(t, wallets, 1)
	require.Equal(t, platform, wallets[0].ID)
	addressID := id.New()
	destination := "0x1111111111111111111111111111111111111111"
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_treasury_withdrawal_addresses(id,network_id,address,address_key) VALUES($1,$2,$3,$3)`, addressID, f.network, destination)
	require.NoError(t, err)
	makeTask := func(source id.ID) TreasuryTask {
		task := TreasuryTask{WithdrawalID: id.New(), TokenID: f.token, NetworkID: f.network, SourceWalletID: source, Amount: types.MustRawAmount("100"), DestAddress: destination}
		_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_treasury_withdrawals(id,token_id,source_wallet_id,dest_address,address_id,amount,status) VALUES($1,$2,$3,$4,$5,100,'approved')`, task.WithdrawalID, task.TokenID, source, destination, addressID)
		require.NoError(t, err)
		return task
	}
	repo := NewOutboundRepo()
	task := makeTask(platform)
	selected, err := repo.ReserveHotWalletForTreasury(f.ctx, task)
	require.NoError(t, err)
	require.Equal(t, platform, selected.ID)
	_, err = repo.ReserveHotWalletForTreasury(f.ctx, makeTask(merchant))
	require.ErrorIs(t, err, ErrNoHotWalletLiquidity)
	wrong := task
	wrong.Amount = types.MustRawAmount("101")
	_, err = repo.ReserveHotWalletForTreasury(f.ctx, wrong)
	require.ErrorIs(t, err, ErrMerchantCustody)
}
