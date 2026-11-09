#!/usr/bin/env bash
set -euo pipefail

output_path="${1:?output path is required}"
snapshot_label="${2:?snapshot label is required}"
: "${DATABASE_URL:?DATABASE_URL is required}"

mkdir -p "$(dirname "$output_path")"

psql "$DATABASE_URL" --no-psqlrc --set ON_ERROR_STOP=1 --tuples-only --no-align \
  --set snapshot_label="$snapshot_label" > "$output_path" <<'SQL'
WITH
wallet_balances AS (
  SELECT COALESCE(jsonb_agg(to_jsonb(row_data) ORDER BY row_data.wallet_id, row_data.token_id), '[]'::jsonb) AS value
  FROM (
    SELECT wallet_id::text,
           token_id::text,
           COUNT(*) AS movement_count,
           COALESCE(SUM(CASE record_type WHEN 'receipt' THEN amount ELSE -amount END), 0)::text AS net_amount
    FROM reg_crypto_balance_movements
    GROUP BY wallet_id, token_id
  ) row_data
),
merchant_balances AS (
  SELECT COALESCE(jsonb_agg(to_jsonb(row_data) ORDER BY row_data.merchant_id, row_data.token_id), '[]'::jsonb) AS value
  FROM (
    SELECT merchant_id::text,
           token_id::text,
           COUNT(*) AS movement_count,
           COALESCE(SUM(CASE record_type WHEN 'receipt' THEN amount ELSE -amount END), 0)::text AS net_amount
    FROM reg_crypto_merchant_balance_movements
    GROUP BY merchant_id, token_id
  ) row_data
),
platform_costs AS (
  SELECT COALESCE(jsonb_agg(to_jsonb(row_data) ORDER BY row_data.network_id, row_data.cost_token_id, row_data.purpose), '[]'::jsonb) AS value
  FROM (
    SELECT network_id::text,
           cost_token_id::text,
           purpose,
           COUNT(*) AS movement_count,
           COALESCE(SUM(CASE record_type WHEN 'receipt' THEN amount ELSE -amount END), 0)::text AS net_amount
    FROM reg_crypto_platform_cost_movements
    GROUP BY network_id, cost_token_id, purpose
  ) row_data
),
custody_adjustments AS (
  SELECT COALESCE(jsonb_object_agg(status, item_count), '{}'::jsonb) AS value
  FROM (
    SELECT status, COUNT(*) AS item_count
    FROM doc_crypto_custody_adjustments
    GROUP BY status
  ) row_data
),
external_outflows AS (
  SELECT COALESCE(jsonb_agg(to_jsonb(row_data) ORDER BY row_data.status, row_data.classification), '[]'::jsonb) AS value
  FROM (
    SELECT status, COALESCE(classification, '') AS classification, COUNT(*) AS item_count
    FROM reg_crypto_external_outflows
    GROUP BY status, classification
  ) row_data
)
SELECT jsonb_pretty(jsonb_build_object(
  'schemaVersion', 61,
  'label', :'snapshot_label',
  'capturedAt', clock_timestamp(),
  'walletBalances', wallet_balances.value,
  'merchantBalances', merchant_balances.value,
  'platformCosts', platform_costs.value,
  'custodyAdjustments', custody_adjustments.value,
  'externalOutflows', external_outflows.value
))
FROM wallet_balances, merchant_balances, platform_costs, custody_adjustments, external_outflows;
SQL
