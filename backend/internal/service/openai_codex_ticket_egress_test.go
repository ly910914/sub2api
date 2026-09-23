package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketEgressReportsOnlyValidatedTraceFields(t *testing.T) {
	for _, tc := range []struct{ name, body, ip, country string }{
		{"ipv4", "ip=203.0.113.25\nloc=GB\nother=private-value", "203.0.113.25", "GB"},
		{"ipv6", "ip=2001:db8::1\r\nloc=US\r\n", "2001:db8::1", "US"},
		{"invalid", "ip=private-value\nloc=private-value", "", ""},
		{"oversized", "ip=203.0.113.25\n" + strings.Repeat("x", 8193), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := ticketJobService(t, &codexTicketFuncUpstream{trace: func(req *http.Request, proxy string) (*http.Response, error) {
				require.Equal(t, "socks5h://user:secret@pool.example:1080", proxy)
				require.Empty(t, req.Header.Get("Authorization"))
				require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
				require.Empty(t, req.Header.Get("ChatGPT-Account-ID"))
				require.Equal(t, HTTPUpstreamProfileOpenAIHarvest, HTTPUpstreamProfileFromContext(req.Context()))
				require.True(t, req.Close)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}})
			got := s.probeCodexTicketEgress(context.Background(), ticketTestAccount(41), "socks5h://user:secret@pool.example:1080")
			require.Equal(t, tc.ip, got.IP)
			require.Equal(t, tc.country, got.Country)
			require.Equal(t, tc.ip != "", got.State == "ready")
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-value")
			require.NotContains(t, string(encoded), "secret")
		})
	}
}

func TestCodexTicketEgressFailureDoesNotExposeSecretsOrFallBackToDirect(t *testing.T) {
	calls := 0
	s, _ := ticketJobService(t, &codexTicketFuncUpstream{trace: func(*http.Request, string) (*http.Response, error) {
		calls++
		return nil, errors.New("socks5://user:private-password@pool.example:1080")
	}})
	for _, pool := range []string{"", "invalid", "socks5://user:private-password@pool.example:1080"} {
		got := s.probeCodexTicketEgress(context.Background(), ticketTestAccount(41), pool)
		require.Equal(t, "error", got.State)
		require.Empty(t, got.IP)
		encoded, err := json.Marshal(got)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-password")
	}
	require.Equal(t, 1, calls)
}

func TestCodexTicketEgressPendingCheckDoesNotDelayHarvestAndIsCanceled(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	s, _ := ticketJobService(t, &codexTicketFuncUpstream{
		trace: func(req *http.Request, _ string) (*http.Response, error) {
			close(started)
			<-req.Context().Done()
			close(stopped)
			return nil, req.Context().Err()
		},
		do: func(req *http.Request) (*http.Response, error) {
			select {
			case <-started:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			return codexTicketResponse(), nil
		},
	})
	job := s.startCodexAccountTicketJob(context.Background(), 41, true)
	waitCodexTicketJob(t, job)
	select {
	case <-stopped:
	default:
		t.Fatal("diagnostic must stop with the job")
	}
	status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
	require.NoError(t, err)
	require.True(t, status.TicketUsable)
	require.Empty(t, status.LastError)
	sample := &CodexTicketEgressStatus{State: "ready", IP: "203.0.113.1", CheckedAt: time.Now()}
	require.False(t, s.publishCodexTicketEgress(context.Background(), 41, job, sample))
}

func TestCodexTicketEgressStatusIsCopiedAndHiddenAfterPoolChange(t *testing.T) {
	s, _ := ticketJobService(t, nil)
	a, err := s.codexTicketAccountByID(context.Background(), 41)
	require.NoError(t, err)
	job := &codexAccountTicketJob{revision: codexAccountTicketConfigOf(a).Revision, fixedFingerprint: codexTicketFixedProxyFingerprint(a), harvestProxyURL: s.openAICodexTicketHarvestProxyURL(), running: true}
	s.openaiCodexAccountJobs = map[int64]*codexAccountTicketJob{41: job}
	require.True(t, s.publishCodexTicketEgress(context.Background(), 41, job, &CodexTicketEgressStatus{State: "ready", IP: "203.0.113.1", CheckedAt: time.Now()}))
	status, err := s.GetCodexAccountTicketStatus(context.Background(), 41)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.1", status.Egress.IP)
	status.Egress.IP = "changed"
	require.Equal(t, "203.0.113.1", job.egress.IP)
	job.harvestProxyURL = "http://another.example:8080"
	status, err = s.GetCodexAccountTicketStatus(context.Background(), 41)
	require.NoError(t, err)
	require.Nil(t, status.Egress)
	require.False(t, s.publishCodexTicketEgress(context.Background(), 41, job, &CodexTicketEgressStatus{State: "error"}))
}
