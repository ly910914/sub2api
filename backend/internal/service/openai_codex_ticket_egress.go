package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var codexTicketHarvestBodyKeys = []string{"prompt_cache_key", "client_metadata", "device_id"}

func (s *OpenAIGatewayService) harvestPinnedSessionForModel(ctx context.Context, account *Account, model string) string {
	if s == nil || account == nil || !s.openAICodexTicketEnabledContext(ctx) {
		return ""
	}
	live, err := s.codexTicketLiveAccount(ctx, account)
	if err != nil || live == nil {
		return ""
	}
	ticket := s.lookupOpenAICodexTicket(live, model)
	if ticket == nil || !ticket.validFor(live, codexAccountTicketConfigOf(live), time.Now()) {
		return ""
	}
	return strings.TrimSpace(ticket.HarvestSessionID)
}

// Harvest probes are minted with only session_id. Keeping the regular client
// cache/device identity in a ticket-bound request can make the upstream treat
// it as a different conversation and return the 312 refresh signal.
func (s *OpenAIGatewayService) pinHarvestIdentityBodyForModel(ctx context.Context, account *Account, model string, body []byte) ([]byte, error) {
	if s == nil || len(body) == 0 || s.harvestPinnedSessionForModel(ctx, account, model) == "" {
		return body, nil
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return body, nil
	}
	changed := false
	for _, key := range codexTicketHarvestBodyKeys {
		if _, ok := object[key]; ok {
			delete(object, key)
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(object); err != nil {
		return body, err
	}
	return bytes.TrimSpace(out.Bytes()), nil
}

func replaceCodexTicketRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
}

// restoreBoundCodexTicketHarvestIdentity reapplies the identity created by
// the successful harvest probe. Request builders may derive a normal isolated
// session after ticket injection; this final pass must win for managed tickets.
func restoreBoundCodexTicketHarvestIdentity(h http.Header, ticket *openAICodexTicket) {
	if h == nil || ticket == nil {
		return
	}
	if ticket.HarvestSessionID != "" {
		h.Set("session_id", ticket.HarvestSessionID)
	}
	h.Del("Cookie")
	if codexTicketCookiesFresh(ticket, time.Now()) {
		h.Set("Cookie", strings.Join(ticket.HarvestCookies, "; "))
	}
}

func (s *OpenAIGatewayService) restoreBoundCodexTicketHarvestIdentity(ctx context.Context, h http.Header, account *Account) {
	if s == nil || h == nil || account == nil || !s.openAICodexTicketEnabledContext(ctx) {
		return
	}
	live, err := s.codexTicketLiveAccount(ctx, account)
	if err != nil || live == nil {
		return
	}
	state := strings.TrimSpace(h.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	for _, model := range s.openAICodexTicketConfig().Models {
		ticket := s.lookupOpenAICodexTicket(live, model)
		if ticket != nil && ticket.State == state && ticket.validFor(live, codexAccountTicketConfigOf(live), time.Now()) {
			restoreBoundCodexTicketHarvestIdentity(h, ticket)
			return
		}
	}
}

const codexTicketEgressTraceURL = "https://chatgpt.com/cdn-cgi/trace"

// This is an independent connection sample, not the egress of a ticket request.
type CodexTicketEgressStatus struct {
	State     string    `json:"state"`
	IP        string    `json:"ip,omitempty"`
	Country   string    `json:"country,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func (s *OpenAIGatewayService) monitorCodexTicketEgress(ctx context.Context, id int64, job *codexAccountTicketJob) {
	for ctx.Err() == nil {
		account, err := s.codexTicketAccountByID(ctx, id)
		if err != nil || !s.openAICodexTicketEnabledContext(ctx) || !codexAccountTicketConfigOf(account).Enabled {
			return
		}
		if !s.publishCodexTicketEgress(ctx, id, job, nil) {
			return
		}
		sample := s.probeCodexTicketEgress(ctx, account, freshCodexTicketProxyURL(job.harvestProxyURL))
		if !s.publishCodexTicketEgress(ctx, id, job, &sample) {
			return
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *OpenAIGatewayService) publishCodexTicketEgress(ctx context.Context, id int64, job *codexAccountTicketJob, sample *CodexTicketEgressStatus) bool {
	s.openaiCodexAccountMu.Lock()
	defer s.openaiCodexAccountMu.Unlock()
	if ctx.Err() != nil || s.openaiCodexAccountJobs[id] != job || !job.running || s.openAICodexTicketHarvestProxyURLContext(ctx) != job.harvestProxyURL {
		return false
	}
	if sample != nil {
		copySample := *sample
		job.egress = &copySample
	}
	return true
}

func (s *OpenAIGatewayService) probeCodexTicketEgress(ctx context.Context, account *Account, proxyURL string) CodexTicketEgressStatus {
	result := CodexTicketEgressStatus{State: "error", CheckedAt: time.Now()}
	// No direct fallback: this diagnostic must only report the configured pool.
	if strings.TrimSpace(proxyURL) == "" || ValidateOpenAICodexTicketHarvestProxyURL(proxyURL) != nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAIHarvest), http.MethodGet, codexTicketEgressTraceURL, nil)
	if err != nil {
		return result
	}
	req.Close = true
	resp, err := s.doCodexTicketProbe(req, account, proxyURL, true)
	if err != nil || resp == nil {
		return result
	}
	if resp.Body == nil {
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil || len(body) > 8192 {
		return result
	}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			if ip, err := netip.ParseAddr(value); err == nil && ip.Zone() == "" {
				result.IP = ip.String()
			}
		case "loc":
			if len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z' {
				result.Country = value
			}
		}
	}
	if result.IP != "" {
		result.State = "ready"
	}
	return result
}
