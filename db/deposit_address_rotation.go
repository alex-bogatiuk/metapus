package dbassets

import (
	"fmt"
	"strings"
)

// DepositAddressRotationUpgradeSQL is an explicit, additive operator upgrade
// for databases that already applied the owning baseline migration. Run the
// returned SQL in one transaction; it never resets data or goose history.
func DepositAddressRotationUpgradeSQL() (string, error) {
	raw, err := Files.ReadFile("migrations/00024_merchant_wallet.sql")
	if err != nil {
		return "", err
	}
	_, section, ok := strings.Cut(string(raw), "-- DEPOSIT_ADDRESS_ROTATION_SCHEMA_BEGIN")
	if !ok {
		return "", fmt.Errorf("deposit rotation schema start marker missing")
	}
	section, _, ok = strings.Cut(section, "-- DEPOSIT_ADDRESS_ROTATION_SCHEMA_END")
	if !ok {
		return "", fmt.Errorf("deposit rotation schema end marker missing")
	}
	section = strings.ReplaceAll(section, "CREATE TABLE reg_crypto_deposit_address_rotations", "CREATE TABLE IF NOT EXISTS reg_crypto_deposit_address_rotations")
	section = strings.ReplaceAll(section, "CREATE INDEX idx_deposit_rotations_", "CREATE INDEX IF NOT EXISTS idx_deposit_rotations_")
	return `SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE cat_wallets ADD COLUMN IF NOT EXISTS deposit_assigned_at TIMESTAMPTZ;
ALTER TABLE cat_wallets ADD COLUMN IF NOT EXISTS deposit_retired_at TIMESTAMPTZ;
ALTER TABLE cat_wallets DROP CONSTRAINT IF EXISTS chk_deposit_address_retired;
ALTER TABLE cat_wallets ADD CONSTRAINT chk_deposit_address_retired CHECK (deposit_retired_at IS NULL OR
 (allocation_mode='persistent' AND merchant_id IS NOT NULL AND customer_id<>''
  AND status IN ('assigned','frozen') AND is_active AND NOT deletion_mark AND _deleted_at IS NULL));
DROP INDEX IF EXISTS idx_cat_wallets_persistent_customer;
CREATE UNIQUE INDEX idx_cat_wallets_persistent_customer ON cat_wallets(merchant_id,network_id,customer_id)
 WHERE allocation_mode='persistent' AND deletion_mark=FALSE AND customer_id<>'' AND deposit_retired_at IS NULL;
DROP TRIGGER IF EXISTS trg_deposit_address_retirement_guard ON cat_wallets;
DO $$ BEGIN
 IF to_regclass('reg_crypto_deposit_address_rotations') IS NOT NULL THEN
  DROP TRIGGER IF EXISTS trg_deposit_rotation_immutable ON reg_crypto_deposit_address_rotations;
  DROP TRIGGER IF EXISTS trg_deposit_rotation_identity ON reg_crypto_deposit_address_rotations;
 END IF;
END $$;
` + section + `
DROP INDEX IF EXISTS idx_crypto_inbound_chain_events_single_review;
CREATE INDEX idx_crypto_inbound_chain_events_single_review ON reg_crypto_inbound_chain_events(match_status,created_at,id)
 WHERE match_reason IN ('additional_single_payment','retired_deposit_address') AND _deleted_at IS NULL;
`, nil
}
