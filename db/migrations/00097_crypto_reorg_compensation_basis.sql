-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_reorg_compensations ADD COLUMN request_basis JSONB
 CHECK(request_basis IS NULL OR jsonb_typeof(request_basis)='object');
CREATE FUNCTION fn_crypto_reorg_compensation_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.request_basis IS NULL OR NEW.request_basis->'schemaVersion' IS DISTINCT FROM '1'::jsonb THEN
   RAISE EXCEPTION 'new financial reorg compensation requires request basis version 1' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'financial reorg compensation facts are immutable' USING ERRCODE='23514';
END; $$;
CREATE TRIGGER trg_crypto_reorg_compensation_immutable BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_reorg_compensations
 FOR EACH ROW EXECUTE FUNCTION fn_crypto_reorg_compensation_immutable();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_reorg_compensations WHERE request_basis IS NOT NULL) THEN
  RAISE EXCEPTION 'financial reorg request basis requires forward recovery' USING ERRCODE='23514';
 END IF;
END; $$;
DROP TRIGGER trg_crypto_reorg_compensation_immutable ON reg_crypto_reorg_compensations;
DROP FUNCTION fn_crypto_reorg_compensation_immutable();
ALTER TABLE reg_crypto_reorg_compensations DROP COLUMN request_basis;
-- +goose StatementEnd
