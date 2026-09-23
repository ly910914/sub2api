CREATE TABLE IF NOT EXISTS openai_account_warmups (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    generation BIGINT NOT NULL DEFAULT 1,
    config JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS openai_account_warmup_sessions (
    account_id BIGINT NOT NULL REFERENCES openai_account_warmups(account_id) ON DELETE CASCADE,
    slot INTEGER NOT NULL,
    session_id UUID NOT NULL DEFAULT gen_random_uuid(),
    history JSONB NOT NULL DEFAULT '[]',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    PRIMARY KEY (account_id, slot)
);
CREATE INDEX IF NOT EXISTS openai_warmup_session_leases ON openai_account_warmup_sessions(lease_until) WHERE lease_until IS NOT NULL;

-- Every account write path (including imports and bulk edits) shares the same
-- transaction boundary. Runtime progress is projected from the task, never trusted
-- from an account edit. Lock ordering is always account -> task -> session.
CREATE OR REPLACE FUNCTION sync_openai_account_warmup() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    cfg JSONB;
    task openai_account_warmups%ROWTYPE;
    changed BOOLEAN;
    restart BOOLEAN;
    enabled BOOLEAN;
BEGIN
    NEW.extra := COALESCE(NEW.extra, '{}'::jsonb);
    IF TG_OP = 'UPDATE' THEN
        IF NOT (NEW.extra ? 'openai_warmup') AND OLD.extra ? 'openai_warmup' THEN
            NEW.extra := NEW.extra || jsonb_build_object('openai_warmup', OLD.extra->'openai_warmup');
        ELSIF (NEW.extra ? 'openai_warmup')
              AND NEW.extra->'openai_warmup' = 'null'::jsonb
              AND OLD.extra ? 'openai_warmup' THEN
            -- Treat an explicit JSON null as the account-level switch-off while
            -- retaining the previous model, prompt, and progress target.
            NEW.extra := NEW.extra || jsonb_build_object(
                'openai_warmup',
                COALESCE(OLD.extra->'openai_warmup', '{}'::jsonb)
                    || jsonb_build_object('enabled', false));
        END IF;
    END IF;
    cfg := NEW.extra->'openai_warmup';
    NEW.extra := NEW.extra - 'openai_warmup_state';
    IF cfg IS NULL OR cfg = 'null'::jsonb THEN
        RETURN NEW;
    END IF;
    IF jsonb_typeof(cfg) <> 'object' OR jsonb_typeof(cfg->'enabled') IS DISTINCT FROM 'boolean' THEN
        RAISE EXCEPTION 'Invalid warmup configuration' USING ERRCODE = '23514';
    END IF;
    enabled := (cfg->>'enabled')::boolean;
    IF enabled THEN
        IF NEW.platform <> 'openai' OR NEW.type <> 'oauth' OR NEW.parent_account_id IS NOT NULL THEN
            RAISE EXCEPTION 'Warmup requires an OpenAI OAuth parent account' USING ERRCODE = '23514';
        END IF;
        IF COALESCE(length(trim(cfg->>'model')), 0) = 0 OR COALESCE(length(trim(cfg->>'prompt')), 0) = 0
           OR COALESCE((cfg->>'concurrency')::integer, 0) < 1
           OR (NEW.concurrency > 0 AND (cfg->>'concurrency')::integer > NEW.concurrency)
           OR COALESCE((cfg->>'success_threshold')::integer, 0) < 1 THEN
            RAISE EXCEPTION 'Warmup requires model, prompt, positive threshold and concurrency within account capacity' USING ERRCODE = '23514';
        END IF;
    END IF;
    SELECT * INTO task FROM openai_account_warmups WHERE account_id = NEW.id FOR UPDATE;
    IF NOT FOUND THEN
        INSERT INTO openai_account_warmups(account_id, config) VALUES(NEW.id, cfg) RETURNING * INTO task;
    ELSE
        -- A bulk switch-off must retain each account's own model/history/target.
        IF NOT enabled THEN
            cfg := task.config || jsonb_build_object('enabled', false);
            NEW.extra := jsonb_set(NEW.extra, '{openai_warmup}', cfg);
        END IF;
        changed := task.config IS DISTINCT FROM cfg;
        restart := task.config->>'model' IS DISTINCT FROM cfg->>'model'
                OR task.config->>'prompt' IS DISTINCT FROM cfg->>'prompt';
        IF changed THEN
            UPDATE openai_account_warmups SET config = cfg, generation = generation + 1,
                success_count = CASE WHEN restart THEN 0 ELSE success_count END,
                failure_count = CASE WHEN restart THEN 0 ELSE failure_count END,
                input_tokens = CASE WHEN restart THEN 0 ELSE input_tokens END,
                output_tokens = CASE WHEN restart THEN 0 ELSE output_tokens END,
                completed_at = NULL, status = 'pending', last_error = '', updated_at = now()
            WHERE account_id = NEW.id RETURNING * INTO task;
            IF restart THEN
                DELETE FROM openai_account_warmup_sessions WHERE account_id = NEW.id;
            ELSE
                UPDATE openai_account_warmup_sessions SET lease_token = '', lease_until = NULL,
                    consecutive_failures = 0, next_attempt_at = now() WHERE account_id = NEW.id;
            END IF;
        END IF;
    END IF;
    IF NOT enabled THEN
        task.status := 'disabled';
    ELSIF task.success_count >= (cfg->>'success_threshold')::integer THEN
        task.status := 'completed';
        task.completed_at := COALESCE(task.completed_at, now());
    ELSIF task.status IN ('completed', 'disabled') THEN
        task.status := 'pending';
        task.completed_at := NULL;
    END IF;
    IF task.status = 'completed' THEN
        UPDATE openai_account_warmup_sessions
        SET lease_token = '', lease_until = NULL
        WHERE account_id = NEW.id;
    END IF;
    UPDATE openai_account_warmups SET status = task.status, completed_at = task.completed_at WHERE account_id = NEW.id;
    NEW.extra := NEW.extra || jsonb_build_object('openai_warmup_state', jsonb_build_object(
        'status', task.status, 'success_count', task.success_count, 'failure_count', task.failure_count,
        'success_threshold', cfg->'success_threshold', 'last_error', task.last_error,
        'completed_at', task.completed_at, 'input_tokens', task.input_tokens, 'output_tokens', task.output_tokens));
    IF TG_OP = 'INSERT' THEN
        INSERT INTO scheduler_outbox(event_type, account_id) VALUES ('account_changed', NEW.id);
    ELSIF OLD.extra->'openai_warmup_state' IS DISTINCT FROM NEW.extra->'openai_warmup_state'
       OR OLD.extra->'openai_warmup' IS DISTINCT FROM cfg THEN
        INSERT INTO scheduler_outbox(event_type, account_id) VALUES ('account_changed', NEW.id);
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS account_warmup_sync ON accounts;
CREATE TRIGGER account_warmup_sync BEFORE INSERT OR UPDATE OF extra, concurrency, platform, type, parent_account_id
ON accounts FOR EACH ROW EXECUTE FUNCTION sync_openai_account_warmup();
