//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWarmupLifecycle(t *testing.T) {
	ctx := context.Background()
	db := integrationDB
	repo := &openAIWarmupRepository{db: db}
	accounts := newAccountRepositoryWithSQL(integrationEntClient, db, nil)
	var id int64
	cfg := service.OpenAIWarmupConfig{Enabled: true, Model: "gpt-5", Prompt: "Continue", Concurrency: 2, SuccessThreshold: 3}
	raw, _ := json.Marshal(map[string]any{service.OpenAIWarmupConfigKey: cfg})
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,concurrency,status,schedulable,extra) VALUES('warmup-test','openai','oauth',3,'active',true,$1) RETURNING id`, raw).Scan(&id))
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, id) })
	assertAllowed := func(want bool) {
		t.Helper()
		actual, err := accounts.IsOpenAIWarmupAllowed(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, actual)
	}
	claim := func() *service.OpenAIWarmupJob {
		t.Helper()
		job, err := repo.Claim(ctx)
		require.NoError(t, err)
		require.NotNil(t, job)
		require.Equal(t, id, job.AccountID)
		return job
	}
	ready := func() {
		t.Helper()
		_, err := db.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET next_attempt_at=now()-interval '1 second' WHERE account_id=$1`, id)
		require.NoError(t, err)
	}
	result := service.OpenAIWarmupResult{History: []service.OpenAIWarmupTurn{{Input: json.RawMessage(`{"role":"user","content":"Continue"}`), Output: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"OK"}`)}}}, InputTokens: 4, OutputTokens: 1}
	assertAllowed(false)
	first, second := claim(), claim()
	require.NotEqual(t, first.SessionID, second.SessionID)
	empty, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, empty)
	require.NoError(t, repo.Finish(ctx, first, result))
	require.NoError(t, repo.Finish(ctx, first, result), "duplicate delivery must not increment twice")
	assertAllowed(false)
	ready()
	third := claim()
	require.Equal(t, first.SessionID, third.SessionID)
	require.Len(t, third.History, 1)
	empty, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, empty, "reserve only remaining successes")
	require.NoError(t, repo.Finish(ctx, second, result))
	assertAllowed(false)
	require.NoError(t, repo.Finish(ctx, third, result))
	assertAllowed(true)
	var activeLeases int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM openai_account_warmup_sessions WHERE account_id=$1 AND lease_until>now()`, id).Scan(&activeLeases))
	require.Zero(t, activeLeases, "completion must release remaining warmup leases")
	// Ordinary account edits cannot forge or erase server-owned progress.
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_build_object('openai_warmup_state',jsonb_build_object('status','failed','success_count',0)) WHERE id=$1`, id)
	require.NoError(t, err)
	assertAllowed(true)
	require.NoError(t, repo.Control(ctx, id, true))
	assertAllowed(false)
	stale := claim()
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup,enabled}','false') WHERE id=$1`, id)
	require.NoError(t, err)
	assertAllowed(true)
	valid, err := repo.Renew(ctx, stale)
	require.NoError(t, err)
	require.False(t, valid)
	require.NoError(t, repo.Finish(ctx, stale, result))
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup,enabled}','true') WHERE id=$1`, id)
	require.NoError(t, err)
	assertAllowed(false)
	resumed := claim()
	require.Equal(t, stale.SessionID, resumed.SessionID)
	require.Empty(t, resumed.History)
	require.NoError(t, repo.Finish(ctx, resumed, service.OpenAIWarmupResult{Error: "timeout", Fatal: true}))
	empty, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, empty)
	require.NoError(t, repo.Control(ctx, id, false))
	recovered := claim()
	require.NoError(t, repo.Finish(ctx, recovered, result))
	// Lowering the target releases the gate without changing manual scheduling.
	_, err = db.ExecContext(ctx, `UPDATE accounts SET schedulable=false,extra=jsonb_set(extra,'{openai_warmup,success_threshold}','1') WHERE id=$1`, id)
	require.NoError(t, err)
	assertAllowed(true)
	a, err := accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.False(t, a.IsSchedulable())
	_, err = db.ExecContext(ctx, `UPDATE accounts SET schedulable=true,extra=jsonb_set(extra,'{openai_warmup,success_threshold}','2') WHERE id=$1`, id)
	require.NoError(t, err)
	assertAllowed(false)
	job := claim()
	_, err = db.ExecContext(ctx, `UPDATE openai_account_warmup_sessions SET lease_until=$2 WHERE account_id=$1`, id, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	successor := claim()
	require.NotEqual(t, job.LeaseToken, successor.LeaseToken)
	require.NoError(t, repo.Finish(ctx, job, result))
	assertAllowed(false)
	require.NoError(t, repo.Finish(ctx, successor, result))
	assertAllowed(true)
}

func TestOpenAIWarmupConcurrentReservations(t *testing.T) {
	ctx := context.Background()
	db := integrationDB
	var id int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,concurrency,status,schedulable,extra)
        VALUES('warmup-concurrent','openai','oauth',20,'active',true,
        '{"openai_warmup":{"enabled":true,"model":"gpt-5","prompt":"Hi","concurrency":20,"success_threshold":20}}') RETURNING id`).Scan(&id))
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, id) })
	repos := []*openAIWarmupRepository{{db: db}, {db: db}}
	jobs := make(chan *service.OpenAIWarmupJob, 40)
	errs := make(chan error, 40)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job, err := repos[i%2].Claim(ctx)
			if err != nil {
				errs <- err
			}
			if job != nil {
				jobs <- job
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	// Fill any slots skipped by nonblocking lock acquisition.
	for len(jobs) < 10 {
		job, err := repos[0].Claim(ctx)
		require.NoError(t, err)
		require.NotNil(t, job)
		jobs <- job
	}
	require.Len(t, jobs, 10, "the cap is shared by independent service instances")
	overflow, err := repos[1].Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, overflow)
	close(jobs)
	seen := map[int]bool{}
	commits := make(chan error, 20)
	for job := range jobs {
		require.False(t, seen[job.Slot])
		seen[job.Slot] = true
		for n := 0; n < 2; n++ {
			wg.Add(1)
			go func(job *service.OpenAIWarmupJob) {
				defer wg.Done()
				commits <- repos[1].Finish(ctx, job, service.OpenAIWarmupResult{History: []service.OpenAIWarmupTurn{}})
			}(job)
		}
	}
	wg.Wait()
	close(commits)
	for err := range commits {
		require.NoError(t, err)
	}
	var successes int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT success_count FROM openai_account_warmups WHERE account_id=$1`, id).Scan(&successes))
	require.Equal(t, 10, successes, "each reservation commits at most once")
}

func TestOpenAIWarmupConfigPreservationAndShadow(t *testing.T) {
	ctx := context.Background()
	db := integrationDB
	var id, shadowID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,concurrency,status,schedulable,extra)
        VALUES('warmup-config','openai','oauth',2,'active',true,
        '{"openai_warmup":{"enabled":true,"model":"gpt-5","prompt":"Original","concurrency":1,"success_threshold":2}}') RETURNING id`).Scan(&id))
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, id) })
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,status,schedulable,parent_account_id,quota_dimension)
        VALUES('warmup-shadow','openai','oauth','active',true,$1,'spark') RETURNING id`, id).Scan(&shadowID))
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, shadowID) })
	repo := &openAIWarmupRepository{db: db}
	accounts := newAccountRepositoryWithSQL(integrationEntClient, db, nil)
	allowed, err := accounts.IsOpenAIWarmupAllowed(ctx, shadowID)
	require.NoError(t, err)
	require.False(t, allowed)
	job, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	result := service.OpenAIWarmupResult{History: []service.OpenAIWarmupTurn{{Input: json.RawMessage(`{"role":"user","content":"Original"}`)}}}
	require.NoError(t, repo.Finish(ctx, job, result))
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup}','{"enabled":false,"model":"","prompt":"","concurrency":1,"success_threshold":0}') WHERE id=$1`, id)
	require.NoError(t, err)
	account, err := accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Original", account.OpenAIWarmupConfig().Prompt)
	allowed, err = accounts.IsOpenAIWarmupAllowed(ctx, shadowID)
	require.NoError(t, err)
	require.True(t, allowed)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup}','null'::jsonb) WHERE id=$1`, id)
	require.NoError(t, err)
	account, err = accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.False(t, account.OpenAIWarmupConfig().Enabled)
	require.Equal(t, "Original", account.OpenAIWarmupConfig().Prompt)
	allowed, err = accounts.IsOpenAIWarmupAllowed(ctx, shadowID)
	require.NoError(t, err)
	require.True(t, allowed)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup,enabled}','true') WHERE id=$1`, id)
	require.NoError(t, err)
	resumed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, resumed)
	require.Equal(t, job.SessionID, resumed.SessionID)
	require.Len(t, resumed.History, 1)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{openai_warmup,prompt}','"Replacement"') WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, repo.Finish(ctx, resumed, result))
	replacement, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, replacement)
	require.NotEqual(t, job.SessionID, replacement.SessionID)
	require.Empty(t, replacement.History)
	var successes int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT success_count FROM openai_account_warmups WHERE account_id=$1`, id).Scan(&successes))
	require.Zero(t, successes)
}
