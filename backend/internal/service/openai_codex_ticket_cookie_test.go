package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieFreshnessAndHeaderRestore(t *testing.T) {
	now := time.Now()
	ticket := &openAICodexTicket{
		CapturedAt:       now,
		HarvestSessionID: "harvest-session",
		HarvestCookies:   []string{"a=1", "b=2"},
		HarvestCookiesAt: now,
	}
	require.True(t, codexTicketCookiesFresh(ticket, now.Add(239*time.Second)))
	require.False(t, codexTicketCookiesFresh(ticket, now.Add(240*time.Second)))
	h := http.Header{"Cookie": []string{"client=ignored"}, "session_id": []string{"client-session"}}
	restoreBoundCodexTicketHarvestIdentity(h, ticket)
	require.Equal(t, "harvest-session", h.Get("session_id"))
	require.Equal(t, "a=1; b=2", h.Get("Cookie"))
	expired := *ticket
	expired.HarvestCookiesAt = now.Add(-openAICodexCredentialTTL)
	restoreBoundCodexTicketHarvestIdentity(h, &expired)
	require.Empty(t, h.Get("Cookie"))
}

func TestCodexTicketAcceptedResponseRotatesStateAndMergesCookies(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{openAICodexTicketDefaultModel}}, nil)
	oldAt := time.Now().Add(-2 * time.Minute)
	old := verifiedTestTicket(account, 292)
	old.CapturedAt = oldAt
	old.ExpiresAt = oldAt.Add(time.Hour)
	old.HarvestSessionID = "harvest-session"
	old.HarvestCookies = []string{"stable=keep", "rotate=old"}
	old.HarvestCookiesAt = oldAt
	svc.storeOpenAICodexTicket(context.Background(), account, old)
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketToRequest(req.Context(), account, old.Model, req))
	returned := fakeCodexTicketState(292)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	resp.Header.Set(openAICodexTurnStateHeader, returned)
	resp.Header.Add("Set-Cookie", "rotate=new; Path=/; Secure")
	receipt, _ := req.Context().Value(codexTicketReceiptContextKey{}).(*codexTicketReceipt)
	svc.updateCodexTicketFromResponse(req, resp, receipt, account)
	got := svc.lookupOpenAICodexTicket(account, old.Model)
	require.Equal(t, returned, got.State)
	require.Equal(t, []string{"rotate=new", "stable=keep"}, got.HarvestCookies)
	require.True(t, got.HarvestCookiesAt.After(oldAt))
	require.Equal(t, "harvest-session", got.HarvestSessionID)
}

func TestCodexTicketRejectedResponseDoesNotRefreshCookies(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{openAICodexTicketDefaultModel}}, nil)
	oldAt := time.Now().Add(-2 * time.Minute)
	old := verifiedTestTicket(account, 292)
	old.CapturedAt = oldAt
	old.ExpiresAt = oldAt.Add(time.Hour)
	old.HarvestCookies = []string{"stable=keep"}
	old.HarvestCookiesAt = oldAt
	svc.storeOpenAICodexTicket(context.Background(), account, old)
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketToRequest(req.Context(), account, old.Model, req))
	receipt, _ := req.Context().Value(codexTicketReceiptContextKey{}).(*codexTicketReceipt)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	resp.Header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(312))
	resp.Header.Add("Set-Cookie", "stable=bad; Path=/; Secure")
	svc.updateCodexTicketFromResponse(req, resp, receipt, account)
	got := svc.lookupOpenAICodexTicket(account, old.Model)
	require.Equal(t, old.State, got.State)
	require.Equal(t, []string{"stable=keep"}, got.HarvestCookies)
	require.Equal(t, oldAt, got.HarvestCookiesAt)
}

func TestCodexTicketHarvestProbeReusesSessionAndCookies(t *testing.T) {
	account := ticketTestAccount(41)
	var requests []*http.Request
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		resp := codexTicketResponse()
		if len(requests) == 1 {
			resp.Header.Add("Set-Cookie", "edge=one; Path=/")
		}
		return resp, nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{openAICodexTicketDefaultModel}}, upstream)
	first := svc.fireCodexAccountTicketProbeWithSession(context.Background(), account, "token", account.Extra[codexAccountTicketConfigKey].(codexAccountTicketConfig).Model, "http://harvest:8080", "", "", nil, time.Second)
	require.NoError(t, first.Err)
	require.Equal(t, "edge=one", first.Cookies[0])
	second := svc.fireCodexAccountTicketProbeWithSession(context.Background(), account, "token", account.Extra[codexAccountTicketConfigKey].(codexAccountTicketConfig).Model, "http://fixed:8080", first.State, first.SessionID, first.Cookies, time.Second)
	require.NoError(t, second.Err)
	require.Len(t, requests, 2)
	require.Equal(t, first.SessionID, requests[0].Header.Get("session_id"))
	require.Equal(t, first.SessionID, requests[1].Header.Get("session_id"))
	require.Equal(t, "edge=one", requests[1].Header.Get("Cookie"))
}
