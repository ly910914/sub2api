package service

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketForbiddenRetriesAndSavesVerifiedTicket(t *testing.T) {
	for _, stage := range []string{"harvest", "business"} {
		t.Run(stage, func(t *testing.T) {
			var probes atomic.Int64
			s, repo := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				business := req.Header.Get(openAICodexTurnStateHeader) != ""
				if business == (stage == "business") && probes.Add(1) <= 2 {
					response.StatusCode = http.StatusForbidden
				}
				return response, nil
			}})
			job := s.startCodexAccountTicketJob(context.Background(), 41, true)
			waitCodexTicketJob(t, job)
			require.GreaterOrEqual(t, probes.Load(), int64(3))
			require.Empty(t, job.lastError)
			require.True(t, job.retryAfter.IsZero())
			status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
			require.NoError(t, err)
			require.True(t, status.TicketUsable)
			account, err := repo.GetByID(context.Background(), 41)
			require.NoError(t, err)
			ticket := parseOpenAICodexTicketFromAny(41, status.Model, account.Extra[openAICodexTicketExtraKey(status.Model)])
			require.NotNil(t, ticket)
			require.True(t, ticket.Verified)
		})
	}
}

func TestCodexTicketPersistentForbiddenWaitsForRoundDeadline(t *testing.T) {
	for _, stage := range []string{"harvest", "business"} {
		t.Run(stage, func(t *testing.T) {
			var probes atomic.Int64
			s, _ := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				business := req.Header.Get(openAICodexTurnStateHeader) != ""
				if business == (stage == "business") {
					probes.Add(1)
					response.StatusCode = http.StatusForbidden
				}
				return response, nil
			}})
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			s.openaiCodexTicketContext = ctx
			job := s.startCodexAccountTicketJob(context.Background(), 41, true)
			require.Eventually(t, func() bool {
				status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
				return err == nil && status.State == "harvesting" && status.RetryAfter == nil && strings.Contains(status.LastError, "403")
			}, time.Second, 5*time.Millisecond)
			waitCodexTicketJob(t, job)
			require.GreaterOrEqual(t, probes.Load(), int64(3))
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			require.Contains(t, job.lastError, "timed out")
			require.WithinDuration(t, time.Now().Add(3*time.Minute), job.retryAfter, time.Second)
			require.Nil(t, s.startCodexAccountTicketJob(context.Background(), 41, false))
		})
	}
}
