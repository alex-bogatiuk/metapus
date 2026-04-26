-- +goose Up
-- sys_notifications: in-app notification messages for users.

CREATE TABLE sys_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'error', 'success')),
    link VARCHAR(255),
    is_read BOOLEAN NOT NULL DEFAULT false,
    attributes JSONB DEFAULT '{}'::jsonb,
    
    version INT NOT NULL DEFAULT 1,
    deletion_mark BOOLEAN NOT NULL DEFAULT false,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current()
);

CREATE INDEX idx_sys_notifications_user_unread ON sys_notifications(user_id) WHERE is_read = false AND deletion_mark = false;
CREATE INDEX idx_sys_notifications_created_at_desc ON sys_notifications(created_at DESC);
CREATE INDEX idx_sys_notifications_txid ON sys_notifications(_txid);

CREATE TRIGGER update_sys_notifications_modtime
    BEFORE UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_notifications_txid
    BEFORE INSERT OR UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_notifications
    BEFORE UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- +goose Down
DROP TABLE IF EXISTS sys_notifications;
