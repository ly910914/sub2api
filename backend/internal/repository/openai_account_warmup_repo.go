package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type openAIWarmupRepository struct {
	db       *sql.DB
	accounts service.AccountRepository
	cache    service.SchedulerCache
}

func (r *accountRepository) IsOpenAIWarmupAllowed(ctx context.Context, id int64) (bool, error) {
	var allowed bool
	rows, err := r.sql.QueryContext(ctx, `SELECT NOT EXISTS (
        SELECT 1 FROM accounts x LEFT JOIN openai_account_warmups w ON w.account_id=x.id
        WHERE (x.id=a.id OR x.id=a.parent_account_id) AND x.extra->'openai_warmup'->>'enabled'='true'
          AND (w.account_id IS NULL OR w.status<>'completed' OR w.config IS DISTINCT FROM x.extra->'openai_warmup'
            OR w.success_count < (w.config->>'success_threshold')::integer))
		FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, service.ErrAccountNotFound
	}
	err = rows.Scan(&allowed)
	return allowed, err
}

func NewOpenAIWarmupRepository(db *sql.DB, accounts service.AccountRepository, cache service.SchedulerCache) service.OpenAIWarmupRepository {
	return &openAIWarmupRepository{db: db, accounts: accounts, cache: cache}
}

func (r *openAIWarmupRepository) Claim(ctx context.Context) (*service.OpenAIWarmupJob, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize only the short reservation transaction, across all instances.
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(741241)`).Scan(&locked); err != nil || !locked {
		return nil, err
	}
	var running int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM openai_account_warmup_sessions WHERE lease_until > now()`).Scan(&running); err != nil || running >= 10 {
		return nil, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN openai_account_warmups w ON w.account_id=a.id
        WHERE a.deleted_at IS NULL AND a.status='active' AND a.schedulable AND a.platform='openai' AND a.type='oauth'
          AND w.config->>'enabled'='true' AND w.status NOT IN ('failed','completed','disabled')
          AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= now())
          AND (a.overload_until IS NULL OR a.overload_until <= now())
          AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= now())
          AND (NOT a.auto_pause_on_expired OR a.expires_at IS NULL OR a.expires_at > now())
          AND w.success_count + (SELECT count(*) FROM openai_account_warmup_sessions s WHERE s.account_id=a.id AND s.lease_until>now()) < (w.config->>'success_threshold')::integer
          AND EXISTS (SELECT 1 FROM generate_series(0, LEAST(CASE WHEN a.concurrency > 0 THEN a.concurrency ELSE (w.config->>'concurrency')::integer END,(w.config->>'concurrency')::integer)-1) n
            LEFT JOIN openai_account_warmup_sessions s ON s.account_id=a.id AND s.slot=n
            WHERE s.slot IS NULL OR ((s.lease_until IS NULL OR s.lease_until<=now()) AND s.next_attempt_at<=now()))
        ORDER BY w.updated_at,a.id LIMIT 1 FOR UPDATE OF a SKIP LOCKED`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job := &service.OpenAIWarmupJob{AccountID: id, LeaseToken: uuid.NewString()}
	var cfg []byte
	var status string
	var successes int
	if err = tx.QueryRowContext(ctx, `SELECT generation,config,status,success_count FROM openai_account_warmups WHERE account_id=$1 FOR UPDATE`, id).Scan(&job.Generation, &cfg, &status, &successes); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(cfg, &job.Config); err != nil {
		return nil, err
	}
	job.Config.Normalize()
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM openai_account_warmup_sessions WHERE account_id=$1 AND lease_until>now()`, id).Scan(&running); err != nil {
		return nil, err
	}
	if !job.Config.Enabled || status == "completed" || status == "failed" || status == "disabled" || successes+running >= job.Config.SuccessThreshold {
		return nil, nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO openai_account_warmup_sessions(account_id,slot) SELECT $1,n FROM generate_series(0,$2::integer-1) n ON CONFLICT DO NOTHING`, id, job.Config.Concurrency)
	if err != nil {
		return nil, err
	}
	var history []byte
	err = tx.QueryRowContext(ctx, `UPDATE openai_account_warmup_sessions SET lease_token=$2,lease_until=now()+interval '3 minutes'
        WHERE account_id=$1 AND slot=(SELECT slot FROM openai_account_warmup_sessions WHERE account_id=$1 AND slot<$3
          AND (lease_until IS NULL OR lease_until<=now()) AND next_attempt_at<=now() ORDER BY next_attempt_at,slot LIMIT 1)
        RETURNING slot,session_id::text,history`, id, job.LeaseToken, job.Config.Concurrency).Scan(&job.Slot, &job.SessionID, &history)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(history, &job.History); err != nil {
		return nil, err
	}
	job.Model = job.Config.ModelForSlot(job.Slot)
	if _, err = tx.ExecContext(ctx, `UPDATE openai_account_warmups SET status='running',updated_at=now() WHERE account_id=$1`, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *openAIWarmupRepository) Renew(ctx context.Context, job *service.OpenAIWarmupJob) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE openai_account_warmup_sessions s SET lease_until=now()+interval '3 minutes'
        FROM openai_account_warmups w,accounts a WHERE s.account_id=$1 AND s.slot=$2 AND s.lease_token=$3 AND s.lease_until>now()
          AND w.account_id=s.account_id AND w.generation=$4 AND w.config->>'enabled'='true' AND w.status IN ('running','cooling')
          AND a.id=w.account_id AND a.deleted_at IS NULL AND a.status='active' AND a.schedulable`, job.AccountID, job.Slot, job.LeaseToken, job.Generation)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *openAIWarmupRepository) Finish(ctx context.Context, job *service.OpenAIWarmupJob, result service.OpenAIWarmupResult) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, job.AccountID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var generation int64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT generation,status FROM openai_account_warmups WHERE account_id=$1 FOR UPDATE`, id).Scan(&generation, &status); err != nil {
		return err
	}
	if generation != job.Generation || status == "disabled" || status == "completed" || status == "failed" {
		return nil
	}
	var failures int
	err = tx.QueryRowContext(ctx, `SELECT consecutive_failures FROM openai_account_warmup_sessions WHERE account_id=$1 AND slot=$2 AND lease_token=$3 AND lease_until>now() FOR UPDATE`, id, job.Slot, job.LeaseToken).Scan(&failures)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	retryAt := result.RetryAt
	if retryAt.IsZero() {
		retryAt = time.Now().Add(time.Second)
	}
	success := result.Error == "" && !result.Skipped
	if success {
		failures = 0
		history, err := json.Marshal(result.History)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET history=$3,consecutive_failures=0 WHERE account_id=$1 AND slot=$2`, id, job.Slot, history)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmups SET success_count=success_count+1,input_tokens=input_tokens+$2,output_tokens=output_tokens+$3,last_error='',updated_at=now() WHERE account_id=$1`, id, result.InputTokens, result.OutputTokens)
		if err != nil {
			return err
		}
	} else if result.Error != "" && !result.Skipped {
		failures++
		status = "cooling"
		if result.Fatal || failures >= 5 {
			status = "failed"
		}
		retryAt = time.Now().Add(time.Duration(1<<min(failures, 6)) * time.Second)
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmups SET failure_count=failure_count+1,status=$2,last_error=$3,updated_at=now() WHERE account_id=$1`, id, status, result.Error)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmups SET status='cooling',last_error=$2,failure_count=failure_count+CASE WHEN $2<>'' THEN 1 ELSE 0 END,updated_at=now() WHERE account_id=$1`, id, result.Error)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET lease_token='',lease_until=NULL,next_attempt_at=$3,consecutive_failures=$4 WHERE account_id=$1 AND slot=$2`, id, job.Slot, retryAt, failures)
	if err != nil {
		return err
	}
	// A peer's retry must not invalidate healthy turns already in flight.
	if status != "failed" {
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmups SET status=CASE WHEN EXISTS(SELECT 1 FROM openai_account_warmup_sessions WHERE account_id=$1 AND lease_until>now()) THEN 'running' ELSE 'cooling' END WHERE account_id=$1`, id)
		if err != nil {
			return err
		}
	}
	if status == "failed" {
		if _, err = tx.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET lease_token='',lease_until=NULL WHERE account_id=$1`, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra WHERE id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.refresh(ctx, id)
	return nil
}

func (r *openAIWarmupRepository) Control(ctx context.Context, id int64, reset bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL AND platform='openai' AND type='oauth' AND parent_account_id IS NULL FOR UPDATE`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE openai_account_warmups SET generation=generation+1,status='pending',last_error='',completed_at=NULL,
        success_count=CASE WHEN $2 THEN 0 ELSE success_count END,failure_count=CASE WHEN $2 THEN 0 ELSE failure_count END,
        input_tokens=CASE WHEN $2 THEN 0 ELSE input_tokens END,output_tokens=CASE WHEN $2 THEN 0 ELSE output_tokens END,updated_at=now() WHERE account_id=$1`, id, reset)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrAccountNotFound
	}
	if reset {
		_, err = tx.ExecContext(ctx, `DELETE FROM openai_account_warmup_sessions WHERE account_id=$1`, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET lease_token='',lease_until=NULL,consecutive_failures=0,next_attempt_at=now() WHERE account_id=$1`, id)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra WHERE id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.refresh(ctx, id)
	return nil
}

func (r *openAIWarmupRepository) refresh(ctx context.Context, id int64) {
	// The transactional outbox remains the durable fallback on cache failure.
	if r == nil || r.cache == nil || r.accounts == nil {
		return
	}
	a, err := r.accounts.GetByID(ctx, id)
	if err == nil {
		_ = r.cache.SetAccount(ctx, a)
	}
}
