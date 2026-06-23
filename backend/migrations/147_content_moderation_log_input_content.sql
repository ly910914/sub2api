-- 为风控记录补充可维护字段：输入哈希和脱敏审核内容。

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS input_hash VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS input_text TEXT NOT NULL DEFAULT '';

UPDATE content_moderation_logs
SET input_text = input_excerpt
WHERE input_text = '' AND input_excerpt <> '';

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_input_hash
    ON content_moderation_logs(input_hash);
