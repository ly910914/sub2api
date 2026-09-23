package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func trimOpenAIWarmupHistory(history []OpenAIWarmupTurn) []OpenAIWarmupTurn {
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	for len(history) > 0 {
		data, _ := json.Marshal(history)
		if len(data) <= 1<<20 {
			break
		}
		history = history[1:]
	}
	return history
}

func (s *AccountTestService) ExecuteOpenAIWarmupTurn(ctx context.Context, account *Account, job *OpenAIWarmupJob) (OpenAIWarmupResult, error) {
	var result OpenAIWarmupResult
	gateway := s.openaiGatewayService
	if gateway == nil {
		return result, errors.New("OpenAI gateway unavailable")
	}
	history := trimOpenAIWarmupHistory(job.History)
	model := strings.TrimSpace(job.Model)
	if model == "" {
		model = job.Config.ModelForSlot(job.Slot)
	}
	agentRecoveryTried := false
	input, _ := json.Marshal(map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": job.Config.Prompt}}})
	for {
		items := make([]any, 0, len(history)*2+1)
		appendItem := func(raw json.RawMessage) error {
			var item any
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			items = append(items, item)
			return nil
		}
		for _, turn := range history {
			if err := appendItem(turn.Input); err != nil {
				return result, fmt.Errorf("decode warmup history: %w", err)
			}
			for _, output := range turn.Output {
				if err := appendItem(output); err != nil {
					return result, fmt.Errorf("decode warmup output: %w", err)
				}
			}
		}
		if err := appendItem(input); err != nil {
			return result, err
		}
		upstreamModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(model))
		// Match the v2.8.0 account test probe: this direct Codex payload is much
		// closer to the official worker than passing through the public gateway.
		payload := createOpenAITestPayload(upstreamModel, true)
		payload["input"] = items
		payload["prompt_cache_key"] = job.SessionID
		body, err := json.Marshal(payload)
		if err != nil {
			return result, err
		}
		if sanitized, changed, sanitizeErr := sanitizeOpenAIResponsesInputItemIDs(body); sanitizeErr == nil && changed {
			body = sanitized
		}
		inbound, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(body))
		if err != nil {
			return result, err
		}
		inbound = inbound.WithContext(WithHTTPUpstreamProfile(inbound.Context(), HTTPUpstreamProfileOpenAI))
		inbound.Host = "chatgpt.com"
		inbound.Header.Set("Content-Type", "application/json")
		inbound.Header.Set("Accept", "text/event-stream")
		inbound.Header.Set("OpenAI-Beta", "responses=experimental")
		canonical := resolveCodexOutboundIdentity("")
		inbound.Header.Set("Originator", canonical.originator)
		if customUA := strings.TrimSpace(account.GetOpenAIUserAgent()); customUA != "" {
			inbound.Header.Set("User-Agent", customUA)
		} else {
			inbound.Header.Set("User-Agent", canonical.userAgent)
		}
		setOpenAIChatGPTAccountHeaders(inbound.Header, account)
		enforceCodexIdentityHeadersWithUA(inbound.Header, account.GetOpenAIUserAgent())
		inbound.Header.Set("Session_ID", job.SessionID)
		inbound.Header.Set("Conversation_ID", job.SessionID)
		if len(history) > 0 {
			if state := history[len(history)-1].TurnState; validCodexTicketState(state) {
				inbound.Header.Set(openAICodexTurnStateHeader, state)
			}
		}
		token, _, err := gateway.GetAccessToken(ctx, account)
		if err != nil {
			return result, err
		}
		if account.IsOpenAIAgentIdentity() {
			authHeaders, authErr := buildAgentIdentityAuthenticationHeaders(ctx, s.accountRepo, s.agentIdentityWS, &s.agentIdentityTaskMu, account)
			if authErr != nil {
				return result, authErr
			}
			for key, values := range authHeaders {
				for _, value := range values {
					inbound.Header.Add(key, value)
				}
			}
		} else {
			inbound.Header.Set("Authorization", "Bearer "+token)
		}
		account.ApplyHeaderOverrides(inbound.Header)
		proxyURL := ""
		if account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		result.Attempted = true
		result.UpstreamModel = upstreamModel
		var response *http.Response
		if s.httpUpstream != nil {
			response, err = s.doOpenAIAccountTestUpstream(inbound, proxyURL, account, true)
		} else {
			// Unit test fixtures historically attach the upstream to the gateway
			// only; retain that fallback while production uses the account-test
			// transport above.
			response, err = gateway.doOpenAIUpstream(inbound, proxyURL, account)
		}
		if err != nil {
			return result, err
		}
		if updates, err := extractOpenAICodexProbeUpdates(response); err == nil && len(updates) > 0 && s.accountRepo != nil {
			_ = s.accountRepo.UpdateExtra(ctx, account.ID, updates)
		}
		if response.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			raw = redactAgentIdentitySensitiveBodyForAccount(ctx, s.accountRepo, account, raw)
			if !agentRecoveryTried && account.IsOpenAIAgentIdentity() && isAgentIdentityTaskInvalidHTTPResponse(response.StatusCode, raw) {
				agentRecoveryTried = true
				if err := ensureAgentIdentityTaskForAccount(ctx, s.accountRepo, s.agentIdentityWS, &s.agentIdentityTaskMu, account, account.GetCredential("task_id")); err != nil {
					return result, err
				}
				continue
			}
			if response.StatusCode == http.StatusTooManyRequests {
				s.reconcileOpenAI429State(ctx, account, response.Header, raw)
			}
			message := extractUpstreamErrorMessage(raw)
			if strings.Contains(strings.ToLower(string(raw)), "context_length_exceeded") && len(history) > 0 {
				history = history[len(history)-len(history)/2:]
				continue
			}
			return result, &warmupUpstreamError{Status: response.StatusCode, Message: message}
		}
		output, inTokens, outTokens, err := readOpenAIWarmupResponse(response.Body)
		response.Body.Close()
		if err != nil {
			if strings.Contains(err.Error(), "context_length_exceeded") && len(history) > 0 {
				history = history[len(history)-len(history)/2:]
				continue
			}
			return result, err
		}
		turn := OpenAIWarmupTurn{Input: input, Output: output}
		if state := response.Header.Get(openAICodexTurnStateHeader); validCodexTicketState(state) {
			turn.TurnState = state
		}
		result.History = trimOpenAIWarmupHistory(append(history, turn))
		result.InputTokens = inTokens
		result.OutputTokens = outTokens
		return result, nil
	}
}

func readOpenAIWarmupResponse(body io.Reader) ([]json.RawMessage, int64, int64, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, 8<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "[DONE]" {
			break
		}
		var event struct {
			Type  string `json:"type"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Response struct {
				Status string            `json:"status"`
				Output []json.RawMessage `json:"output"`
				Error  struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Usage struct {
					Input  int64 `json:"input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, 0, 0, fmt.Errorf("invalid warmup stream: %w", err)
		}
		switch event.Type {
		case "response.completed", "response.done":
			if event.Response.Status != "completed" || len(event.Response.Output) == 0 {
				return nil, 0, 0, errors.New("warmup response did not complete with output")
			}
			return event.Response.Output, event.Response.Usage.Input, event.Response.Usage.Output, nil
		case "response.failed", "response.incomplete":
			return nil, 0, 0, &warmupStreamError{Code: event.Response.Error.Code, Message: event.Response.Error.Message}
		case "error":
			return nil, 0, 0, &warmupStreamError{Code: event.Error.Code, Message: event.Error.Message}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, 0, err
	}
	return nil, 0, 0, errors.New("warmup stream ended before a completed response")
}

type warmupStreamError struct{ Code, Message string }

func (e *warmupStreamError) Error() string {
	return fmt.Sprintf("warmup stream error: %s %s", e.Code, e.Message)
}
