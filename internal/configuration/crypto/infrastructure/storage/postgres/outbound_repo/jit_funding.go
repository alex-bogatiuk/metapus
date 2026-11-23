package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

const nativeFundingBasisType = crypto.NativeFundingBasisType

var (
	ErrNativeFundingPending = outboundapp.ErrNativeFundingPending
	ErrNativeFundingHeld    = outboundapp.ErrNativeFundingHeld
)

type nativeFundingRecord = crypto.NativeFunding

func loadNativeFundingWallet(ctx context.Context, walletID, networkID id.ID, requiredRole string) (HotWallet, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var wallet HotWallet
	var actualNetwork id.ID
	var role string
	err := q.QueryRow(ctx, `
		SELECT id,address,vault_key_id,network_id,wallet_role
		FROM cat_wallets
		WHERE id=$1 AND is_active=TRUE AND deletion_mark=FALSE
	`, walletID).Scan(&wallet.ID, &wallet.Address, &wallet.VaultKeyID, &actualNetwork, &role)
	if err != nil {
		return wallet, fmt.Errorf("load managed wallet: %w", err)
	}
	if actualNetwork != networkID || strings.TrimSpace(wallet.Address) == "" || strings.TrimSpace(wallet.VaultKeyID) == "" {
		return wallet, fmt.Errorf("wallet network or signing identity mismatch")
	}
	if requiredRole != "" && role != requiredRole {
		return wallet, fmt.Errorf("wallet role must be %s", requiredRole)
	}
	return wallet, nil
}

func lookupNativeFunding(ctx context.Context, basisType string, basisID id.ID, attempt int) (nativeFundingRecord, bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var record nativeFundingRecord
	err := q.QueryRow(ctx, `
		SELECT id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,
		       amount_minor,fee_reservation_id,signing_authorization_id,unsigned_fingerprint,tx_hash,status,last_error
		FROM doc_crypto_native_fundings
		WHERE basis_type=$1 AND basis_id=$2 AND ($3=0 OR attempt=$3) ORDER BY attempt DESC LIMIT 1
	`, basisType, basisID, attempt).Scan(
		&record.ID, &record.BasisType, &record.BasisID, &record.Attempt, &record.NetworkID, &record.NativeTokenID,
		&record.SourceWalletID, &record.DestinationWalletID, &record.Amount, &record.FeeReservationID,
		&record.SigningAuthorizationID, &record.UnsignedFingerprint, &record.TxHash, &record.Status, &record.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, false, nil
	}
	if err != nil {
		return record, false, fmt.Errorf("lookup native funding: %w", err)
	}
	return record, true, nil
}

func getOrCreateNativeFunding(ctx context.Context, candidate nativeFundingRecord) (record nativeFundingRecord, err error) {
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, candidate.SourceWalletID, candidate.DestinationWalletID); err != nil {
			return err
		}
		var err error
		record, err = insertOrReadNativeFunding(ctx, candidate)
		return err
	})
	return record, err
}

func insertOrReadNativeFunding(ctx context.Context, candidate nativeFundingRecord) (nativeFundingRecord, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var record nativeFundingRecord
	err := q.QueryRow(ctx, `
		INSERT INTO doc_crypto_native_fundings (
			id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,amount_minor,status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'created')
		ON CONFLICT (basis_type,basis_id,attempt) DO NOTHING
		RETURNING id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,
		          amount_minor,fee_reservation_id,signing_authorization_id,unsigned_fingerprint,tx_hash,status,last_error
	`, candidate.ID, candidate.BasisType, candidate.BasisID, candidate.Attempt, candidate.NetworkID,
		candidate.NativeTokenID, candidate.SourceWalletID, candidate.DestinationWalletID, candidate.Amount).Scan(
		&record.ID, &record.BasisType, &record.BasisID, &record.Attempt, &record.NetworkID, &record.NativeTokenID,
		&record.SourceWalletID, &record.DestinationWalletID, &record.Amount, &record.FeeReservationID,
		&record.SigningAuthorizationID, &record.UnsignedFingerprint, &record.TxHash, &record.Status, &record.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		var found bool
		record, found, err = lookupNativeFunding(ctx, candidate.BasisType, candidate.BasisID, candidate.Attempt)
		if err == nil && !found {
			return record, apperror.NewConflict("native funding intent changed during creation")
		}
	}
	if err != nil {
		return record, fmt.Errorf("get or create native funding: %w", err)
	}
	if record.NetworkID != candidate.NetworkID || record.NativeTokenID != candidate.NativeTokenID ||
		record.SourceWalletID != candidate.SourceWalletID || record.DestinationWalletID != candidate.DestinationWalletID || !record.Amount.Equal(candidate.Amount) {
		return record, apperror.NewConflict("native funding replay differs from its immutable intent")
	}
	return record, nil
}

func linkNativeFundingReservation(ctx context.Context, fundingID, reservationID id.ID) error {
	return mutateNativeFunding(ctx, fundingID, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var valid bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
			JOIN reg_crypto_fee_reservations fee ON fee.id=$2 AND fee.basis_type='CryptoNativeFunding' AND fee.basis_id=f.id
			AND fee.purpose='native_funding' AND fee.payer_wallet_id=f.source_wallet_id AND fee.network_id=f.network_id
			AND fee.native_token_id=f.native_token_id AND fee.reserved_principal_minor=f.amount_minor WHERE f.id=$1)`, fundingID, reservationID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return apperror.NewConflict("native funding reservation belongs to another intent")
		}
		tag, err := q.Exec(ctx, `
			UPDATE doc_crypto_native_fundings SET fee_reservation_id=$2,status='reserved',last_error=''
			WHERE id=$1 AND status IN ('created','reserved') AND (fee_reservation_id IS NULL OR fee_reservation_id=$2)
		`, fundingID, reservationID)
		if err != nil {
			return fmt.Errorf("link native funding fee reservation: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("native funding fee reservation state mismatch")
		}
		return nil
	})
}

func retryNativeFundingBeforeAuthorization(ctx context.Context, fundingID id.ID) error {
	return mutateNativeFunding(ctx, fundingID, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		tag, err := q.Exec(ctx, `
			UPDATE doc_crypto_native_fundings
			SET status='created',last_error='',updated_at=NOW()
			WHERE id=$1
			  AND status='held'
			  AND fee_reservation_id IS NULL
			  AND signing_authorization_id IS NULL
			  AND unsigned_fingerprint=''
			  AND tx_hash=''
		`, fundingID)
		if err != nil {
			return fmt.Errorf("retry native funding before authorization: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("native funding retry state mismatch")
		}
		return nil
	})
}

func markNativeFundingStatus(ctx context.Context, fundingID id.ID, status, reason, fingerprint string, authID *id.ID) error {
	return mutateNativeFunding(ctx, fundingID, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		tag, err := q.Exec(ctx, `
			UPDATE doc_crypto_native_fundings
			SET status=$2,last_error=$3,
			    unsigned_fingerprint=CASE WHEN $4='' THEN unsigned_fingerprint ELSE $4 END,
			    signing_authorization_id=COALESCE($5,signing_authorization_id)
			WHERE id=$1 AND status NOT IN ('settled','failed')
		`, fundingID, status, statusFailedReason(errors.New(reason)), fingerprint, authID)
		if err != nil {
			return fmt.Errorf("update native funding status: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("native funding status mismatch")
		}
		return nil
	})
}

func markNativeFundingBroadcast(ctx context.Context, fundingID id.ID, txHash, fingerprint string) error {
	return mutateNativeFunding(ctx, fundingID, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		tag, err := q.Exec(ctx, `
			UPDATE doc_crypto_native_fundings
			SET status='broadcast',tx_hash=$2,unsigned_fingerprint=$3,last_error=''
			WHERE id=$1 AND status IN ('reserved','signed','held','broadcast')
			  AND (tx_hash='' OR tx_hash=$2)
			  AND (unsigned_fingerprint='' OR unsigned_fingerprint=$3)
		`, fundingID, txHash, fingerprint)
		if err != nil {
			return fmt.Errorf("mark native funding broadcast: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("native funding broadcast identity mismatch")
		}
		return nil
	})
}

type NativeFundingSettlementRepo = crypto_repo.NativeFundingSettlementRepo
