// Package receipttest records custody receipts for integration tests the way
// the worker does: an independently proven final transfer is admitted through
// the receipt intake service, so every deferred accounting guard applies.
package receipttest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_receipt_suspense"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
)

// Proofs persists an independent transfer verification.
type Proofs interface {
	SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error)
}

// Deps are the crypto repository adapters the intake service runs on.
type Deps struct {
	Proofs Proofs
	Repo   cryptoapp.ReceiptIntakeRepository
	Guard  crypto.CustodyEffectGuard
}

// Transfer describes the incoming transfer; the destination address, token
// contract and network come from the wallet and token catalogs.
type Transfer struct {
	WalletID, TokenID id.ID
	TxHash            string
	EventIndex        int64
	FromAddress       string
	Amount            string
	BlockNumber       int64
	BlockHash         string
	Reason            crypto_custody_receipt.Reason
}

// Record accounts the transfer as a custody receipt and returns the receipt
// and the proof it rests on.
func Record(t testing.TB, ctx context.Context, deps Deps, transfer Transfer) (receiptID, proofID id.ID) {
	t.Helper()
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var network id.ID
	var family, address, contract string
	require.NoError(t, q.QueryRow(ctx, `SELECT w.network_id,n.chain_family,w.address,COALESCE(t.contract_address,'')
		FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id JOIN cat_tokens t ON t.id=$2 WHERE w.id=$1`,
		transfer.WalletID, transfer.TokenID).Scan(&network, &family, &address, &contract))
	from, blockHash := transfer.FromAddress, transfer.BlockHash
	if from == "" {
		from = "0x00000000000000000000000000000000000000AB"
	}
	if blockHash == "" {
		blockHash = strings.Repeat("b", 64)
	}
	if transfer.Reason == "" {
		transfer.Reason = crypto_custody_receipt.ReasonMatched
	}
	decoder, err := crypto.TransferEvidenceSchemaFor(crypto.ChainFamily(family))
	require.NoError(t, err)
	if decoder == crypto.TRONTransferEvidenceSchema {
		defer admitCurrentTRONEvidence(t, ctx, network)()
	}
	kind := crypto.EventKindContractLog
	if contract == "" {
		kind = crypto.EventKindNativeValue
	}
	event := crypto.BlockchainEvent{
		NetworkID: network, TxHash: transfer.TxHash, EventKind: kind, EventIndex: transfer.EventIndex, DecoderVersion: decoder,
		FromAddress: from, ToAddress: address, TokenContract: contract, Amount: types.MustRawAmount(transfer.Amount),
		BlockNumber: transfer.BlockNumber, BlockHash: blockHash, Confirmations: 120,
		Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical,
		EventType: crypto.EventTypeTransfer, Direction: crypto.TransferDirectionIncoming,
	}
	identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamily(family), transfer.EventIndex)
	require.NoError(t, err)
	sources := []crypto.VerificationSource{
		{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "receipt", HeadHeight: event.BlockNumber + 119, HeadHash: "head"},
		{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "receipt", HeadHeight: event.BlockNumber + 119, HeadHash: "head"},
	}
	if decoder == crypto.TRONTransferEvidenceSchema {
		for i := range sources {
			sources[i].DecoderVersion, sources[i].SourceDigest = decoder, "receipt"
		}
	}
	proofID, err = deps.Proofs.SaveVerification(ctx, crypto.TransferVerification{
		AccountingIdentity: &identity, Event: event, DecoderVersion: decoder, Sources: sources, MatchingEffects: 1,
		VerifiedAt: time.Now().UTC(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded,
	})
	require.NoError(t, err)
	event.VerificationID = &proofID
	engine := posting.NewEngine(nil)
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})
	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo())))
	service := cryptoapp.NewReceiptIntakeService(postgres.MustGetTxManager(ctx), deps.Repo, deps.Guard,
		engine, numerator.New(), postgres.NewOutboxPublisher(), time.Now)
	doc, err := service.Record(ctx, cryptoapp.RecordCustodyReceiptCommand{
		Basis: crypto.AccountingTransferBasis{NetworkID: network, WalletID: transfer.WalletID, TokenID: transfer.TokenID,
			TxHash: event.TxHash, EventKind: event.EventKind, EventIndex: event.EventIndex, FromAddress: event.FromAddress,
			RecordType: entity.RecordTypeReceipt, Amount: event.Amount, BlockNumber: event.BlockNumber, BlockHash: event.BlockHash,
			RequiredConfs: 1, VerificationID: &proofID},
		Event: event, Reason: transfer.Reason,
	})
	require.NoError(t, err)
	return doc.ID, proofID
}

// admitCurrentTRONEvidence lets a fixture network accept current TRON receipt
// evidence: an unregistered reader is registered in shadow mode and a paused
// one resumes, both only until the receipt is recorded.
func admitCurrentTRONEvidence(t testing.TB, ctx context.Context, network id.ID) func() {
	t.Helper()
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var mode string
	err := q.QueryRow(ctx, `SELECT mode FROM sys_crypto_block_ingestion WHERE network_id=$1`, network).Scan(&mode)
	if err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows)
		_, err = q.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version)
			VALUES($1,1,'checkpoint',1,'shadow',2)`, network)
		require.NoError(t, err)
		return func() {
			withoutIngestionGuards(t, ctx, `DELETE FROM sys_crypto_block_ingestion WHERE network_id='`+network.String()+`'`)
		}
	}
	if mode != "paused" {
		return func() {}
	}
	withoutIngestionGuards(t, ctx, `UPDATE sys_crypto_block_ingestion SET mode='shadow' WHERE network_id='`+network.String()+`'`)
	return func() {
		withoutIngestionGuards(t, ctx, `UPDATE sys_crypto_block_ingestion SET mode='paused' WHERE network_id='`+network.String()+`'`)
	}
}

func withoutIngestionGuards(t testing.TB, ctx context.Context, statement string) {
	t.Helper()
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `ALTER TABLE sys_crypto_block_ingestion DISABLE TRIGGER USER;
		`+statement+`;
		ALTER TABLE sys_crypto_block_ingestion ENABLE TRIGGER USER`)
	require.NoError(t, err)
}
