package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketPublishesFailureWhileRetrying(t *testing.T) {
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	s, _ := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return codexModelResponse("another-model"), nil
		}
		close(secondStarted)
		select {
		case <-release:
		case <-req.Context().Done():
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	defer close(release)
	job := s.startCodexAccountTicketJob(context.Background(), 41, true)
	select {
	case <-secondStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("second attempt did not start")
	}
	status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
	require.NoError(t, err)
	require.Equal(t, "harvesting", status.State)
	require.Equal(t, 2, status.Attempts)
	require.Contains(t, status.LastError, "model does not match")
	job.cancel()
	waitCodexTicketJob(t, job)
}

func TestCodexTicketJobDeadlineEndsHarvestingAndKeepsCooldown(t *testing.T) {
	s, _ := ticketJobService(t, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}})
	account, err := s.codexTicketAccountByID(context.Background(), 41)
	require.NoError(t, err)
	job := &codexAccountTicketJob{
		running:          true,
		revision:         codexAccountTicketConfigOf(account).Revision,
		fixedFingerprint: codexTicketFixedProxyFingerprint(account),
		harvestProxyURL:  s.openAICodexTicketHarvestProxyURL(),
	}
	s.openaiCodexAccountJobs = map[int64]*codexAccountTicketJob{41: job}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s.runCodexAccountTicketJob(ctx, 41, job)
	status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
	require.NoError(t, err)
	require.Equal(t, "error", status.State)
	require.Contains(t, status.LastError, "timed out")
	require.NotNil(t, status.RetryAfter)
	require.Nil(t, s.startCodexAccountTicketJob(context.Background(), 41, false))
}

func TestCodexTicketProbeFailureKeepsReadTimeoutAndRedactsTransportErrors(t *testing.T) {
	err := validateCodexTicketCompletedModel(codexTicketErrorReader{context.DeadlineExceeded}, "target")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, codexTicketProbeFailure(http.StatusOK, err), "timed out")
	secretError := errors.New("socks5://private-user:private-password@proxy.example:3010 failed")
	message := codexTicketProbeFailure(0, secretError)
	require.Contains(t, message, "proxy connection")
	require.NotContains(t, message, "private-")
	require.Contains(t, codexTicketProbeFailure(http.StatusServiceUnavailable, secretError), "503")
	require.Contains(t, codexTicketProbeFailure(http.StatusOK, nil), "no valid STATE")
}

type codexTicketErrorReader struct{ err error }

func (r codexTicketErrorReader) Read([]byte) (int, error) { return 0, r.err }
