-- +goose Up
-- Client extension: Vehicle catalog

CREATE TABLE IF NOT EXISTS cat_vehicles (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code          TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    parent_id     UUID REFERENCES cat_vehicles(id),
    is_folder     BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version       INT NOT NULL DEFAULT 1,
    attributes    JSONB NOT NULL DEFAULT '{}',

    -- Vehicle-specific fields
    plate_number  TEXT NOT NULL DEFAULT '',
    brand         TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    year          INT NOT NULL DEFAULT 0,
    vin           TEXT,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    description   TEXT,

    -- CDC fields (required by Go infrastructure)
    _deleted_at   TIMESTAMPTZ,
    _txid         BIGINT NOT NULL DEFAULT txid_current(),

    -- Timestamps
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Code uniqueness
CREATE UNIQUE INDEX IF NOT EXISTS idx_cat_vehicles_code_unique
    ON cat_vehicles (code) WHERE _deleted_at IS NULL AND code != '';

-- Keyset pagination index
CREATE INDEX IF NOT EXISTS idx_cat_vehicles_keyset
    ON cat_vehicles (created_at, id) WHERE _deleted_at IS NULL;

-- Plate number uniqueness
CREATE UNIQUE INDEX IF NOT EXISTS idx_cat_vehicles_plate_unique
    ON cat_vehicles (plate_number) WHERE _deleted_at IS NULL AND plate_number != '';

-- CDC trigger
CREATE TRIGGER tr_cat_vehicles_cdc
    BEFORE UPDATE ON cat_vehicles
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

-- +goose Down
DROP TABLE IF EXISTS cat_vehicles CASCADE;
