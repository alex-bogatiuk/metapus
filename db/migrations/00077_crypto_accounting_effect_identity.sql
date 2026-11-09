-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Independent of transfer evidence/signing byte schemas. The preimage matches
-- AccountingEffectIdentity.Key and is protected by cross-language golden tests.
CREATE FUNCTION fn_crypto_accounting_identity_bytes(
    family TEXT, network UUID, tx TEXT, kind TEXT, ordinal BIGINT, trace TEXT
) RETURNS TEXT LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT format(
        '{"schemaVersion":1,"family":"%s","networkId":"%s","txHash":"%s","eventKind":"%s","ordinal":%s%s}',
        family,network,tx,kind,ordinal,
        CASE WHEN trace='' THEN '' ELSE format(',"traceHash":"%s"',trace) END
    );
$$;

CREATE TABLE reg_crypto_accounting_effects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    schema_version INT NOT NULL DEFAULT 1 CHECK(schema_version=1),
    family TEXT NOT NULL CHECK(family IN ('tron','evm')),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash TEXT NOT NULL CHECK(tx_hash ~ '^[0-9a-f]{64}$'),
    event_kind TEXT NOT NULL CHECK(event_kind IN ('native_value','contract_log','internal_value')),
    effect_ordinal BIGINT NOT NULL CHECK(effect_ordinal>=0),
    trace_hash TEXT NOT NULL DEFAULT '',
    logical_key TEXT GENERATED ALWAYS AS (
        'accounting-effect.v1:' || encode(digest(
            fn_crypto_accounting_identity_bytes(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash),
            'sha256'),'hex')
    ) STORED UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK((event_kind='internal_value' AND family='tron' AND trace_hash ~ '^[0-9a-f]{64}$')
       OR (event_kind<>'internal_value' AND trace_hash='')),
    CHECK(NOT(family='evm' AND event_kind='native_value') OR effect_ordinal=0),
    UNIQUE(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash)
);

CREATE TABLE reg_crypto_accounting_effect_occurrences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    effect_id UUID NOT NULL REFERENCES reg_crypto_accounting_effects(id),
    block_number BIGINT NOT NULL CHECK(block_number>=0),
    block_hash TEXT NOT NULL CHECK(block_hash ~ '^[0-9a-f]{64}$'),
    economics_hash TEXT NOT NULL CHECK(economics_hash ~ '^[0-9a-f]{64}$'),
    source_address TEXT NOT NULL CHECK(source_address<>''),
    destination_address TEXT NOT NULL CHECK(destination_address<>''),
    contract_address TEXT NOT NULL,
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(effect_id,block_hash)
);
CREATE INDEX idx_crypto_accounting_occurrence_block
    ON reg_crypto_accounting_effect_occurrences(block_number,effect_id);

-- One persisted proof cannot be rebound to another logical effect or occurrence.
-- This is evidence indexing only: it neither admits custody nor marks a
-- canonical generation as financially recognized.
CREATE TABLE reg_crypto_accounting_effect_evidence (
    verification_id UUID PRIMARY KEY REFERENCES reg_crypto_transfer_verifications(id),
    occurrence_id UUID NOT NULL REFERENCES reg_crypto_accounting_effect_occurrences(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_crypto_accounting_evidence_occurrence
    ON reg_crypto_accounting_effect_evidence(occurrence_id,verification_id);

CREATE FUNCTION fn_crypto_accounting_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'accounting effect evidence is immutable' USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER trg_crypto_accounting_effect_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();
CREATE TRIGGER trg_crypto_accounting_occurrence_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effect_occurrences
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();
CREATE TRIGGER trg_crypto_accounting_evidence_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effect_evidence
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();

CREATE FUNCTION fn_crypto_accounting_evidence_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof reg_crypto_transfer_verifications;
    occurrence reg_crypto_accounting_effect_occurrences;
    effect reg_crypto_accounting_effects;
    identity JSONB;
    event JSONB;
    from_key TEXT;
    to_key TEXT;
    contract_key TEXT;
BEGIN
    SELECT * INTO STRICT proof FROM reg_crypto_transfer_verifications WHERE id=NEW.verification_id;
    SELECT * INTO STRICT occurrence FROM reg_crypto_accounting_effect_occurrences WHERE id=NEW.occurrence_id;
    SELECT * INTO STRICT effect FROM reg_crypto_accounting_effects WHERE id=occurrence.effect_id FOR UPDATE;
    identity:=proof.evidence->'accountingIdentity';
    event:=proof.evidence->'event';
    from_key:=event->>'fromAddress';
    to_key:=event->>'toAddress';
    contract_key:=event->>'tokenContract';
    IF effect.family='evm' THEN
        from_key:=lower(from_key);
        to_key:=lower(to_key);
        contract_key:=lower(contract_key);
    END IF;
    IF identity IS NULL OR jsonb_typeof(identity)<>'object'
       OR identity IS DISTINCT FROM
          (jsonb_build_object('schemaVersion',1,'family',effect.family,'networkId',effect.network_id,
             'txHash',effect.tx_hash,'eventKind',effect.event_kind,'ordinal',effect.effect_ordinal)
           || CASE WHEN effect.trace_hash='' THEN '{}'::jsonb ELSE jsonb_build_object('traceHash',effect.trace_hash) END)
       OR proof.network_id<>effect.network_id
       OR regexp_replace(lower(proof.tx_hash),'^0x','')<>effect.tx_hash
       OR proof.event_kind<>effect.event_kind
       OR proof.block_number<>occurrence.block_number
       OR regexp_replace(lower(proof.block_hash),'^0x','')<>occurrence.block_hash
       OR (event->>'networkId') IS DISTINCT FROM effect.network_id::text
       OR regexp_replace(lower(event->>'txHash'),'^0x','') IS DISTINCT FROM effect.tx_hash
       OR (event->>'eventKind') IS DISTINCT FROM effect.event_kind
       OR (event->>'eventIndex')::bigint IS DISTINCT FROM proof.event_index
       OR (event->>'blockNumber')::bigint IS DISTINCT FROM occurrence.block_number
       OR regexp_replace(lower(event->>'blockHash'),'^0x','') IS DISTINCT FROM occurrence.block_hash
       OR (event->>'amount')::numeric IS DISTINCT FROM occurrence.amount
       OR from_key IS DISTINCT FROM occurrence.source_address
       OR to_key IS DISTINCT FROM occurrence.destination_address
       OR contract_key IS DISTINCT FROM occurrence.contract_address
       OR occurrence.economics_hash<>encode(digest(format(
          '{"schemaVersion":1,"identity":%s,"from":%s,"to":%s,"contract":%s,"amount":%s}',
          fn_crypto_accounting_identity_bytes(effect.family,effect.network_id,effect.tx_hash,
             effect.event_kind,effect.effect_ordinal,effect.trace_hash),
          to_jsonb(from_key)::text,to_jsonb(to_key)::text,to_jsonb(contract_key)::text,
          to_jsonb(occurrence.amount::text)::text),'sha256'),'hex')
       OR (event->>'canonicality') IS DISTINCT FROM 'canonical'
       OR (event->>'finality') IS DISTINCT FROM 'solidified'
       OR (proof.evidence->>'outcome') IS DISTINCT FROM 'verified'
       OR (proof.evidence->>'executionStatus') IS DISTINCT FROM 'succeeded'
       OR EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence prior
          JOIN reg_crypto_transfer_verifications previous ON previous.id=prior.verification_id
          WHERE prior.occurrence_id=NEW.occurrence_id AND previous.event_index<>proof.event_index)
       OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=effect.network_id AND n.chain_family=effect.family)
       OR (effect.event_kind='contract_log')<>(occurrence.contract_address<>'')
       OR (effect.family='tron' AND
            ((proof.evidence->>'decoderVersion')::int IS DISTINCT FROM 2
             OR (effect.event_kind<>'internal_value' AND proof.event_index<>effect.effect_ordinal)
             OR (effect.event_kind='internal_value' AND
                 (regexp_replace(lower(event->>'traceHash'),'^0x','') IS DISTINCT FROM effect.trace_hash
                  OR (event->>'callValueIndex')::bigint IS DISTINCT FROM effect.effect_ordinal))))
       OR (effect.family='evm' AND
            ((proof.evidence->>'decoderVersion')::int IS DISTINCT FROM 1
             OR proof.event_index<effect.effect_ordinal
             OR (effect.event_kind='native_value' AND proof.event_index<>0))) THEN
        RAISE EXCEPTION 'accounting identity does not match verified occurrence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_accounting_evidence_guard
    BEFORE INSERT ON reg_crypto_accounting_effect_evidence
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_evidence_guard();

-- No detached, unproven identity/occurrence can survive a commit.
CREATE FUNCTION fn_crypto_accounting_coverage_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_accounting_effects' THEN
        IF NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o
            JOIN reg_crypto_accounting_effect_evidence e ON e.occurrence_id=o.id WHERE o.effect_id=NEW.id) THEN
            RAISE EXCEPTION 'accounting effect requires verified occurrence' USING ERRCODE='23514';
        END IF;
    ELSIF NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence e WHERE e.occurrence_id=NEW.id) THEN
        RAISE EXCEPTION 'accounting occurrence requires verified evidence' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_accounting_effect_coverage
    AFTER INSERT ON reg_crypto_accounting_effects DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_coverage_guard();
CREATE CONSTRAINT TRIGGER trg_crypto_accounting_occurrence_coverage
    AFTER INSERT ON reg_crypto_accounting_effect_occurrences DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_coverage_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence) THEN
        RAISE EXCEPTION 'accounting evidence history requires a forward-compatible binary';
    END IF;
END;
$$;
DROP TABLE reg_crypto_accounting_effect_evidence;
DROP TABLE reg_crypto_accounting_effect_occurrences;
DROP TABLE reg_crypto_accounting_effects;
DROP FUNCTION fn_crypto_accounting_coverage_guard();
DROP FUNCTION fn_crypto_accounting_evidence_guard();
DROP FUNCTION fn_crypto_accounting_immutable();
DROP FUNCTION fn_crypto_accounting_identity_bytes(TEXT,UUID,TEXT,TEXT,BIGINT,TEXT);
-- +goose StatementEnd
