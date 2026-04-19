-- +goose Up
-- sys_automation_subscribers: polymorphic binding between rules and delivery targets.
-- Acts as a "table part" (табличная часть) of automation rules.

CREATE TABLE sys_automation_subscribers (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id           UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,

    subscriber_type   VARCHAR(30)   NOT NULL,
    -- channel   → deliver via external channel (Telegram chat, email, webhook)
    -- user      → deliver to specific user (UI notification or email)
    -- role      → deliver to all users with this role
    -- doc_field → deliver to user ID extracted from document field

    -- Polymorphic reference: exactly ONE is populated based on subscriber_type
    channel_id        UUID          REFERENCES sys_automation_channels(id) ON DELETE RESTRICT,
    user_id           UUID          REFERENCES users(id) ON DELETE CASCADE,
    role_name         VARCHAR(100),
    doc_field_path    VARCHAR(255),

    -- Delivery method for user/role/doc_field subscribers (ignored for channel type)
    delivery_method   VARCHAR(30)   NOT NULL DEFAULT 'ui_notification',
    -- ui_notification | email

    -- Sort order within rule
    idx               INT           NOT NULL DEFAULT 0,

    CONSTRAINT chk_subscriber_ref CHECK (
        (subscriber_type = 'channel'   AND channel_id IS NOT NULL) OR
        (subscriber_type = 'user'      AND user_id IS NOT NULL) OR
        (subscriber_type = 'role'      AND role_name IS NOT NULL) OR
        (subscriber_type = 'doc_field' AND doc_field_path IS NOT NULL)
    )
);

CREATE INDEX idx_sys_auto_subscribers_rule
    ON sys_automation_subscribers(rule_id);

CREATE INDEX idx_sys_auto_subscribers_channel
    ON sys_automation_subscribers(channel_id)
    WHERE channel_id IS NOT NULL;

CREATE INDEX idx_sys_auto_subscribers_user
    ON sys_automation_subscribers(user_id)
    WHERE user_id IS NOT NULL;

COMMENT ON TABLE sys_automation_subscribers IS
    'Polymorphic subscribers binding rules to channels, users, roles, or document fields';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_subscribers;
