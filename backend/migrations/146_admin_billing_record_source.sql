-- 为已存在的管理员手工账单表补充订单来源字段。
-- 新安装环境的 145 已包含该字段；这里用于兼容已执行过 145 的数据库。

ALTER TABLE admin_billing_records
    ADD COLUMN IF NOT EXISTS source VARCHAR(100) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS adminbillingrecord_source
    ON admin_billing_records (source) WHERE deleted_at IS NULL;
