package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// Reuse an old semantic match even when its hash came from unordered JSON.
// Keep every historical row/hold intact. New callers serialize on the stable key.
func networkIncident(ctx context.Context, network id.ID, kind, key string, details []byte) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var incident id.ID
	var exact bool
	err := q.QueryRow(ctx, `SELECT id,payment_id IS NULL AND invoice_id IS NULL AND details=$4::jsonb
 FROM reg_crypto_security_incidents WHERE network_id=$1 AND incident_kind=$2 AND event_kind='native_value' AND event_index=0
 AND (tx_hash=$3 OR (payment_id IS NULL AND invoice_id IS NULL AND details=$4::jsonb))
 ORDER BY (tx_hash=$3) DESC,created_at,id LIMIT 1`, network, kind, key, string(details)).Scan(&incident, &exact)
	if err == nil {
		if !exact {
			return id.Nil(), apperror.NewConflict("network incident replay differs from its original evidence")
		}
		return incident, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), fmt.Errorf("load network incident: %w", err)
	}
	err = q.QueryRow(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,incident_kind,tx_hash,event_kind,event_index,details)
 VALUES($1,$2,$3,$4,'native_value',0,$5::jsonb) ON CONFLICT(network_id,incident_kind,tx_hash,event_kind,event_index) DO NOTHING RETURNING id`, id.New(), network, kind, key, string(details)).Scan(&incident)
	if errors.Is(err, pgx.ErrNoRows) {
		err = q.QueryRow(ctx, `SELECT id,payment_id IS NULL AND invoice_id IS NULL AND details=$4::jsonb FROM reg_crypto_security_incidents
 WHERE network_id=$1 AND incident_kind=$2 AND tx_hash=$3 AND event_kind='native_value' AND event_index=0`, network, kind, key, string(details)).Scan(&incident, &exact)
		if err == nil && !exact {
			return id.Nil(), apperror.NewConflict("network incident replay differs from its original evidence")
		}
	}
	if err != nil {
		return id.Nil(), fmt.Errorf("insert network incident: %w", err)
	}
	return incident, nil
}

// Incident identity belongs to the chain event and its payment. Details retain
// the first observation; each compensation generation owns its own full basis.
func paymentReorgIncident(ctx context.Context, p *crypto_payment.CryptoPayment, details []byte) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var incident id.ID
	err := q.QueryRow(ctx, `INSERT INTO reg_crypto_security_incidents(id,network_id,payment_id,incident_kind,tx_hash,event_kind,event_index,details)
 VALUES($1,$2,$3,'posted_payment_reorg',$4,$5,$6,$7::jsonb)
 ON CONFLICT(network_id,incident_kind,tx_hash,event_kind,event_index) DO NOTHING RETURNING id`, id.New(), p.NetworkID, p.ID, p.TxHash, p.EventKind, p.EventIndex, string(details)).Scan(&incident)
	if errors.Is(err, pgx.ErrNoRows) {
		var exact bool
		err = q.QueryRow(ctx, `SELECT id,COALESCE(payment_id=$5,FALSE) AND invoice_id IS NULL FROM reg_crypto_security_incidents
	 WHERE network_id=$1 AND incident_kind='posted_payment_reorg' AND tx_hash=$2 AND event_kind=$3 AND event_index=$4`, p.NetworkID, p.TxHash, p.EventKind, p.EventIndex, p.ID).Scan(&incident, &exact)
		if err == nil && !exact {
			return id.Nil(), apperror.NewConflict("financial reorg incident belongs to another payment")
		}
	}
	if err != nil {
		return id.Nil(), fmt.Errorf("insert payment reorg incident: %w", err)
	}
	return incident, nil
}
