-- +goose Up
-- Migrate existing automation rules to use humanAmounts and money template function for correct currency handling

-- Replace doc.totalAmount with humanAmounts.totalAmount in CEL conditions
UPDATE sys_automation_rules
SET condition_cel = REPLACE(condition_cel, 'doc.totalAmount', 'humanAmounts.totalAmount')
WHERE condition_cel LIKE '%doc.totalAmount%';

-- Replace raw MinorUnits display with money template function in notification templates
UPDATE sys_automation_rules
SET action_template = REPLACE(action_template, '{{.doc.totalAmount}}', '{{money .doc.totalAmount 2}}')
WHERE action_template LIKE '%{{.doc.totalAmount}}%';

UPDATE sys_automation_rules
SET action_template = REPLACE(action_template, '{{ .doc.totalAmount }}', '{{money .doc.totalAmount 2}}')
WHERE action_template LIKE '%{{ .doc.totalAmount }}%';


-- +goose Down
-- Revert the basic replacements

UPDATE sys_automation_rules
SET condition_cel = REPLACE(condition_cel, 'humanAmounts.totalAmount', 'doc.totalAmount')
WHERE condition_cel LIKE '%humanAmounts.totalAmount%';

UPDATE sys_automation_rules
SET action_template = REPLACE(action_template, '{{money .doc.totalAmount 2}}', '{{.doc.totalAmount}}')
WHERE action_template LIKE '%{{money .doc.totalAmount 2}}%';
