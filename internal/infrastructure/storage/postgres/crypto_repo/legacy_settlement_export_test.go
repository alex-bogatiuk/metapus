//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type LegacySettlementFixture struct {
	Ctx     context.Context
	Pool    *pgxpool.Pool
	Task    outboundapp.SettlementTask
	Receipt crypto.ChainReceipt
}

func NewLegacySettlementFixture(t *testing.T, kind string, seedPhysical bool) LegacySettlementFixture {
	t.Helper()
	b := newExpectedRouteFixture(t, "")
	f := LegacySettlementFixture{Ctx: b.ctx, Pool: b.pool, Task: outboundapp.SettlementTask{Kind: kind, DocumentID: id.New(), NetworkID: b.event.NetworkID, OperationTokenID: b.asset, SourceWalletID: b.source, HotWalletID: b.destination, Amount: types.MustCryptoAmount("100"), TxHash: "0x" + strings.Repeat("d", 64), PostedVersion: 1, AccountingModelVersion: 1}}
	merchant, native := id.New(), id.New()
	_, err := b.pool.Exec(b.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Legacy settlement merchant')`, merchant)
	require.NoError(t, err)
	_, err = b.pool.Exec(b.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) SELECT $1,$1::uuid::text,'Legacy native',id,native_token_symbol,native_decimals,'native' FROM cat_blockchain_networks WHERE id=$2`, native, b.event.NetworkID)
	require.NoError(t, err)
	f.Receipt = crypto.ChainReceipt{NetworkID: f.Task.NetworkID, TxHash: f.Task.TxHash, Status: crypto.ReceiptReverted, BlockHash: "block", BlockHeight: 10, Confirmations: 20, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified, ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: native}, TotalFee: types.ZeroCryptoAmount()}}
	physicalKind := ""
	switch kind {
	case outboundapp.SettlementWithdrawal:
		f.Task.RequestID, f.Task.MerchantID = id.New(), merchant
		_, err = b.pool.Exec(b.ctx, `INSERT INTO doc_withdrawal_requests(id,number,merchant_id,token_id,amount,dest_address,status,posted,posted_version) VALUES($1,$1::uuid::text,$2,$3,100,'external','broadcast',TRUE,1)`, f.Task.RequestID, merchant, b.asset)
		require.NoError(t, err)
		_, err = b.pool.Exec(b.ctx, `INSERT INTO doc_crypto_withdrawals(accounting_model_version,id,number,basis_type,basis_id,posted,posted_version,merchant_id,token_id,source_wallet_id,dest_address,amount,tx_hash,status,settlement_status)
 VALUES(1,$1,$1::uuid::text,'WithdrawalRequest',$2,TRUE,1,$3,$4,$5,'external',100,$6,'broadcast','repairing')`, f.Task.DocumentID, f.Task.RequestID, merchant, b.asset, b.source, f.Task.TxHash)
		require.NoError(t, err)
		_, err = b.pool.Exec(b.ctx, `UPDATE doc_withdrawal_requests SET withdrawal_id=$2 WHERE id=$1`, f.Task.RequestID, f.Task.DocumentID)
		require.NoError(t, err)
		_, err = b.pool.Exec(b.ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) VALUES($1,$2,'WithdrawalRequest',1,now()-interval '1 day','expense',$3,$4,100)`, id.New(), f.Task.RequestID, merchant, b.asset)
		require.NoError(t, err)
		physicalKind = "CryptoWithdrawal"
	case outboundapp.SettlementSweep:
		f.Task.LineID = id.New()
		_, err = b.pool.Exec(b.ctx, `INSERT INTO doc_crypto_sweeps(accounting_model_version,id,number,posted,posted_version,token_id,hot_wallet_id,total_amount,status) VALUES(1,$1,$1::uuid::text,TRUE,1,$2,$3,100,'broadcast')`, f.Task.DocumentID, b.asset, b.destination)
		require.NoError(t, err)
		_, err = b.pool.Exec(b.ctx, `INSERT INTO doc_crypto_sweep_lines(line_id,document_id,line_no,wallet_id,amount,tx_hash,settlement_status) VALUES($1,$2,1,$3,100,$4,'repairing')`, f.Task.LineID, f.Task.DocumentID, b.source, f.Task.TxHash)
		require.NoError(t, err)
		physicalKind = "CryptoSweep"
	case outboundapp.SettlementTreasury:
		address := id.New()
		_, err = b.pool.Exec(b.ctx, `INSERT INTO reg_treasury_withdrawal_addresses(id,network_id,address,address_key,label) VALUES($1,$2,'0x1111111111111111111111111111111111111111','0x1111111111111111111111111111111111111111','Legacy external')`, address, b.event.NetworkID)
		require.NoError(t, err)
		_, err = b.pool.Exec(b.ctx, `INSERT INTO doc_treasury_withdrawals(accounting_model_version,id,number,posted,posted_version,token_id,source_wallet_id,dest_address,address_id,amount,tx_hash,status,settlement_status) VALUES(1,$1,$1::uuid::text,TRUE,1,$2,$3,'0x1111111111111111111111111111111111111111',$4,100,$5,'broadcast','repairing')`, f.Task.DocumentID, b.asset, b.source, address, f.Task.TxHash)
		require.NoError(t, err)
		for _, amount := range []string{"40", "60"} {
			_, err = b.pool.Exec(b.ctx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount) VALUES($1,$2,'TreasuryWithdrawal',1,now()-interval '1 day','expense',$3,$4,'processing',$5)`, id.New(), f.Task.DocumentID, merchant, b.asset, amount)
			require.NoError(t, err)
		}
		physicalKind = "TreasuryWithdrawal"
	}
	if seedPhysical {
		_, err = b.pool.Exec(b.ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,$3,1,now()-interval '1 day','expense',$4,$5,100)`, id.New(), f.Task.DocumentID, physicalKind, b.source, b.asset)
		require.NoError(t, err)
		if kind == outboundapp.SettlementSweep {
			_, err = b.pool.Exec(b.ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'CryptoSweep',1,now()-interval '1 day','receipt',$3,$4,100)`, id.New(), f.Task.DocumentID, b.destination, b.asset)
			require.NoError(t, err)
		}
	}
	return f
}

func (f LegacySettlementFixture) AssertLockOrder(t *testing.T, first, next, writer func(context.Context) error) {
	t.Helper()
	assertAccountingLockOrder(t, f.Ctx, f.Pool, first, next, writer)
}
