package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketBlockedSaveRepo struct {
	*codexTicketRefreshRepo
	started chan struct{}
	release chan struct{}
	writes  atomic.Int64
}

func (r *codexTicketBlockedSaveRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if r.writes.Add(1) == 1 {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.codexTicketRefreshRepo.UpdateExtra(ctx, id, updates)
}

func TestCodexTicketConcurrentHarvestSavesOnceBeforeCancelingSibling(t *testing.T) {
	for _, lateCandidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending-sibling", true: "two-candidates"}[lateCandidate], func(t *testing.T) {
			bothStarted := make(chan struct{})
			siblingCanceled := make(chan struct{})
			var harvests, replays atomic.Int64
			s, repo := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				if req.Header.Get(openAICodexTurnStateHeader) != "" {
					replays.Add(1)
					return codexTicketResponse(), nil
				}
				n := harvests.Add(1)
				if n == 2 {
					close(bothStarted)
				}
				select {
				case <-bothStarted:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
				if n == 2 && !lateCandidate {
					<-req.Context().Done()
					close(siblingCanceled)
					// A late failure must not overwrite a saved success or add cooldown.
					return nil, req.Context().Err()
				}
				return codexTicketResponse(), nil
			}})
			blocked := &codexTicketBlockedSaveRepo{codexTicketRefreshRepo: repo, started: make(chan struct{}), release: make(chan struct{})}
			s.accountRepo = blocked
			job := s.startCodexAccountTicketJob(context.Background(), 41, true)
			select {
			case <-blocked.started:
			case <-time.After(3 * time.Second):
				t.Fatal("both workers must harvest before verification")
			}
			select {
			case <-siblingCanceled:
				t.Fatal("sibling canceled before database save")
			default:
			}
			close(blocked.release)
			waitCodexTicketJob(t, job)
			require.Equal(t, int64(2), harvests.Load())
			require.Equal(t, int64(1), replays.Load())
			require.Equal(t, int64(1), blocked.writes.Load())
			require.Empty(t, job.lastError)
			require.True(t, job.retryAfter.IsZero())
			if !lateCandidate {
				<-siblingCanceled
			}
			account, err := repo.GetByID(context.Background(), 41)
			require.NoError(t, err)
			restart := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			restart.accountRepo = repo
			ticket := restart.lookupOpenAICodexTicket(account, openAICodexTicketDefaultModel)
			require.NotNil(t, ticket, "saved ticket must survive losing the process cache")
			require.True(t, ticket.Verified)
		})
	}
}

func TestCodexTicketRetriesBeyondEightUntilDeadlineThenCoolsDown(t *testing.T) {
	require.Equal(t, 5*time.Minute, codexTicketJobTimeout)
	require.Equal(t, 3*time.Minute, codexTicketRetryCooldown)
	var calls atomic.Int64
	s, _ := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return codexModelResponse("another-model"), nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 4500*time.Millisecond)
	defer cancel()
	s.openaiCodexTicketContext = ctx
	job := s.startCodexAccountTicketJob(context.Background(), 41, true)
	waitCodexTicketJob(t, job)
	require.Greater(t, calls.Load(), int64(8))
	require.Equal(t, int(calls.Load()), job.attempts)
	require.Contains(t, job.lastError, "timed out")
	require.WithinDuration(t, time.Now().Add(3*time.Minute), job.retryAfter, time.Second)
	require.Nil(t, s.startCodexAccountTicketJob(context.Background(), 41, false))
	s.openaiCodexAccountMu.Lock()
	job.retryAfter = time.Now().Add(-time.Second)
	s.openaiCodexAccountMu.Unlock()
	s.openaiCodexTicketContext = context.Background()
	next := s.startCodexAccountTicketJob(context.Background(), 41, false)
	require.NotNil(t, next, "automatic retry resumes after cooldown")
	require.NotSame(t, job, next)
	next.cancel()
	waitCodexTicketJob(t, next)
}
