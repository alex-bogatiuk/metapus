package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
	"os"
	"strings"
)

// No RPC, signing, mutation or financial reconciliation is used by diagnostics.
func runTransferEvidence(ctx context.Context, pool *postgres.Pool, hash string) error {
	hash = strings.ToLower(strings.TrimPrefix(hash, "0x"))
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		return fmt.Errorf("-tx requires a complete transaction hash")
	}
	rows, err := pool.Unwrap().Query(ctx, `SELECT jsonb_build_object(
 'paymentId',p.id,'network',n.code,'eventKind',p.event_kind,'eventIndex',p.event_index,
 'status',p.status,'posted',p.posted,'postedVersion',p.posted_version,'confirmations',p.confirmations,'requiredConfirmations',p.required_confs,
 'verificationId',p.verification_id,'cutoverHeight',c.start_height,'mode',c.mode,
 'waitingReason',CASE WHEN p.posted THEN NULL WHEN c.mode='paused' THEN 'ingestion_paused'
 WHEN c.last_error_code<>'' THEN c.last_error_code WHEN v.id IS NULL THEN 'transfer_evidence_pending'
 WHEN v.confirmations<p.required_confs THEN 'confirmation_policy_pending' ELSE 'inbox_processing' END,
 'evidence',v.evidence)
 FROM doc_crypto_payments p JOIN cat_blockchain_networks n ON n.id=p.network_id
 LEFT JOIN sys_crypto_block_ingestion c ON c.network_id=p.network_id
 LEFT JOIN LATERAL (SELECT id,confirmations,evidence FROM reg_crypto_transfer_verifications
 WHERE network_id=p.network_id AND tx_hash=p.tx_hash AND event_kind=p.event_kind AND event_index=p.event_index
 ORDER BY confirmations DESC,verified_at DESC LIMIT 1) v ON TRUE
 WHERE lower(replace(p.tx_hash,'0x',''))=$1 ORDER BY p.event_kind,p.event_index`, hash)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		items = append(items, json.RawMessage(raw))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(items)
}
