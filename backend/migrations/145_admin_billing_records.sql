-- 管理员账单统计：手工记账每笔订单的成本与利润，按 person_name 归属（自由文本，不关联 users）。
-- 营收 = cost + profit（仅展示，不存储）。软删除：deleted_at IS NULL 为活跃记录。

CREATE TABLE IF NOT EXISTS admin_billing_records (
    id           BIGSERIAL PRIMARY KEY,
    person_name  VARCHAR(100) NOT NULL,
    cost         DECIMAL(20,2) NOT NULL DEFAULT 0,
    profit       DECIMAL(20,2) NOT NULL DEFAULT 0,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    note         TEXT,
    created_by   BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS adminbillingrecord_person_name
    ON admin_billing_records (person_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS adminbillingrecord_occurred_at
    ON admin_billing_records (occurred_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS adminbillingrecord_created_at
    ON admin_billing_records (created_at) WHERE deleted_at IS NULL;
