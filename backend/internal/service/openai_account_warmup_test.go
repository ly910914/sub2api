package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func warmupTestAccount() *Account {
	return &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 3,
		Credentials: map[string]any{"access_token": "test-token"}, Extra: map[string]any{OpenAIWarmupConfigKey: OpenAIWarmupConfig{Enabled: true, Model: "gpt-5", Prompt: "Continue.", Concurrency: 2, SuccessThreshold: 3}}}
}

func TestOpenAIWarmupSchedulingGate(t *testing.T) {
	a := warmupTestAccount()
	require.False(t, a.IsSchedulable())
	require.False(t, a.IsCredentialUsableForShadow())
	a.Extra[OpenAIWarmupStateKey] = map[string]any{"status": "completed", "success_count": 2}
	require.False(t, a.IsSchedulable())
	a.Extra[OpenAIWarmupStateKey] = map[string]any{"status": "completed", "success_count": 3}
	require.True(t, a.IsSchedulable())
	a.Schedulable = false
	require.False(t, a.IsSchedulable(), "completion must not change manual scheduling")
	a.Schedulable = true
	a.Extra[OpenAIWarmupConfigKey] = OpenAIWarmupConfig{Enabled: false}
	require.True(t, a.IsSchedulable())
	a.Extra = nil
	require.True(t, a.IsSchedulable(), "legacy accounts default to disabled")
}

func TestOpenAIWarmupValidation(t *testing.T) {
	a := warmupTestAccount()
	require.NoError(t, ValidateOpenAIWarmup(a))
	configured := a.OpenAIWarmupConfig()
	a.Extra[OpenAIWarmupConfigKey] = nil
	require.NoError(t, ValidateOpenAIWarmup(a), "explicit JSON null disables warmup")
	a.Extra[OpenAIWarmupConfigKey] = configured
	a.Concurrency = 0 // account concurrency <= 0 means unlimited
	require.NoError(t, ValidateOpenAIWarmup(a))
	a.Concurrency = 3
	cfg := a.OpenAIWarmupConfig()
	cfg.Concurrency = 4
	a.Extra[OpenAIWarmupConfigKey] = cfg
	require.Error(t, ValidateOpenAIWarmup(a))
	cfg.Concurrency = 1
	a.Extra[OpenAIWarmupConfigKey] = cfg
	a.Type = AccountTypeAPIKey
	require.Error(t, ValidateOpenAIWarmup(a))
}

func TestOpenAIWarmupModelSlotsKeepConfiguredOrder(t *testing.T) {
	cfg := OpenAIWarmupConfig{Model: "legacy-model", Models: []string{"gpt-5.6-sol", "gpt-6-astra"}, Concurrency: 2}
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-6-astra"}, cfg.ModelsForSlots())
	require.Equal(t, "gpt-5.6-sol", cfg.ModelForSlot(0))
	require.Equal(t, "gpt-6-astra", cfg.ModelForSlot(1))
	require.Equal(t, "gpt-5.6-sol", cfg.ModelForSlot(2))

	legacy := OpenAIWarmupConfig{Model: "legacy-model"}
	require.Equal(t, "legacy-model", legacy.ModelForSlot(0))
}

type warmupShadowAccounts struct {
	AccountRepository
	parent *Account
}

func (r warmupShadowAccounts) GetByID(context.Context, int64) (*Account, error) {
	return r.parent, nil
}

func TestOpenAIWarmupShadowFallbackUsesParent(t *testing.T) {
	parent := warmupTestAccount()
	shadow := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID}
	repo := warmupShadowAccounts{parent: parent}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	require.Error(t, gateway.CheckOpenAIWarmupAllowed(context.Background(), shadow))

	parent.Extra[OpenAIWarmupStateKey] = map[string]any{"status": "completed", "success_count": 3}
	require.NoError(t, gateway.CheckOpenAIWarmupAllowed(context.Background(), shadow))
}

const warmupCompletedSSE = `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":12,"output_tokens":2}}}` + "\n\n"

func TestOpenAIWarmupRequiresCompleteResponse(t *testing.T) {
	for _, body := range []string{"data: [DONE]\n", `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n", `data: {"type":"response.failed","response":{"error":{"code":"upstream_error"}}}` + "\n", `data: {"type":"response.completed","response":{"status":"incomplete","output":[{}]}}` + "\n"} {
		_, _, _, err := readOpenAIWarmupResponse(strings.NewReader(body))
		require.Error(t, err)
	}
	output, in, out, err := readOpenAIWarmupResponse(strings.NewReader(warmupCompletedSSE))
	require.NoError(t, err)
	require.Len(t, output, 1)
	require.EqualValues(t, 12, in)
	require.EqualValues(t, 2, out)
}

type warmupTestUpstream struct {
	HTTPUpstream
	call func(*http.Request) (*http.Response, error)
}

func (u warmupTestUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.call(r)
}

type warmupTestAccounts struct{ AccountRepository }

func (warmupTestAccounts) UpdateExtra(context.Context, int64, map[string]any) error { return nil }

func TestOpenAIWarmupStickyMultiturnAndContextRecovery(t *testing.T) {
	var sessions []string
	var inputCounts []int
	calls := 0
	upstream := warmupTestUpstream{call: func(req *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			Input          []json.RawMessage `json:"input"`
			Store          bool              `json:"store"`
			PromptCacheKey string            `json:"prompt_cache_key"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.False(t, body.Store)
		require.NotEmpty(t, body.PromptCacheKey)
		sessions = append(sessions, req.Header.Get("Session_ID"))
		inputCounts = append(inputCounts, len(body.Input))
		if calls == 3 {
			return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"context_length_exceeded","message":"too long"}}`))}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(warmupCompletedSSE))}, nil
	}}
	cfg := &config.Config{}
	repo := warmupTestAccounts{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: repo, cfg: cfg}
	executor := &AccountTestService{accountRepo: repo, openaiGatewayService: gateway}
	a := warmupTestAccount()
	job := &OpenAIWarmupJob{SessionID: "744b9003-60b6-45fc-803c-39f73832c284", Config: a.OpenAIWarmupConfig()}
	for i := 0; i < 3; i++ {
		result, err := executor.ExecuteOpenAIWarmupTurn(context.Background(), a, job)
		require.NoError(t, err)
		job.History = result.History
	}
	require.Equal(t, []int{1, 3, 5, 3}, inputCounts)
	require.NotEmpty(t, sessions[0])
	for _, id := range sessions {
		require.Equal(t, sessions[0], id)
	}
	require.Len(t, job.History, 2)
}

func TestOpenAIWarmupHistoryBound(t *testing.T) {
	history := make([]OpenAIWarmupTurn, 25)
	for i := range history {
		history[i].Input = json.RawMessage(`{"role":"user","content":"hello"}`)
	}
	require.Len(t, trimOpenAIWarmupHistory(history), 20)
}

func TestOpenAIWarmupUsesCodexTaskPayload(t *testing.T) {
	account := warmupTestAccount()
	job := &OpenAIWarmupJob{SessionID: "warmup-session", Model: "gpt-6-astra", Config: account.OpenAIWarmupConfig(), History: []OpenAIWarmupTurn{{
		Input:  json.RawMessage(`{"role":"user","content":"Review this code"}`),
		Output: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"rs_previous","encrypted_content":"encrypted-history","summary":[]}`)},
	}}}
	upstream := warmupTestUpstream{call: func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.NotEmpty(t, body["instructions"])
		require.Equal(t, false, body["store"])
		require.Equal(t, true, body["stream"])
		items := body["input"].([]any)
		require.Len(t, items, 3)
		reasoning := items[1].(map[string]any)
		require.Equal(t, "rs_previous", reasoning["id"], "valid Codex reasoning IDs are replayed for sticky context")
		require.Equal(t, "encrypted-history", reasoning["encrypted_content"])
		content := items[2].(map[string]any)["content"].([]any)
		require.Equal(t, job.Config.Prompt, content[0].(map[string]any)["text"])
		require.Equal(t, "input_text", content[0].(map[string]any)["type"])
		require.Equal(t, CodexCanonicalUserAgent(), req.Header.Get("User-Agent"))
		require.NotEmpty(t, req.Header.Get("Originator"))
		require.NotEmpty(t, req.Header.Get("Session_ID"))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(warmupCompletedSSE))}, nil
	}}
	repo := warmupTestAccounts{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream, accountRepo: repo, cfg: &config.Config{}}
	executor := &AccountTestService{accountRepo: repo, openaiGatewayService: gateway}
	result, err := executor.ExecuteOpenAIWarmupTurn(context.Background(), account, job)
	require.NoError(t, err)
	require.True(t, result.Attempted)
	require.EqualValues(t, 12, result.InputTokens)
	require.Equal(t, "gpt-6-astra", result.UpstreamModel)
	// Preparing the next request must not mutate the persisted history.
	require.Contains(t, string(job.History[0].Output[0]), "rs_previous")
}

func TestOpenAIWarmupUsageDistinguishesAttempts(t *testing.T) {
	repo := &openAIRecordUsageLogRepoStub{}
	svc := &OpenAIAccountWarmupService{usageLogs: repo}
	job := &OpenAIWarmupJob{AccountID: 7, Model: "gpt-5.6-sol", SessionID: "session"}
	svc.recordUsage(context.Background(), job, OpenAIWarmupResult{Skipped: true})
	require.Zero(t, repo.calls)
	svc.recordUsage(context.Background(), job, OpenAIWarmupResult{Attempted: true, Error: "server_is_overloaded", Skipped: true})
	require.Equal(t, 1, repo.calls)
	require.True(t, strings.HasPrefix(repo.lastLog.RequestID, "warmup:failed:"))
	require.Equal(t, RequestTypeWarmup, repo.lastLog.RequestType)
	svc.recordUsage(context.Background(), job, OpenAIWarmupResult{Attempted: true, InputTokens: 12, OutputTokens: 2})
	require.Equal(t, 2, repo.calls)
	require.False(t, strings.HasPrefix(repo.lastLog.RequestID, "warmup:failed:"))
	require.Equal(t, 12, repo.lastLog.InputTokens)
}

func TestOpenAIWarmupStreamOverloadPreservesCode(t *testing.T) {
	_, _, _, err := readOpenAIWarmupResponse(strings.NewReader(`data: {"type":"error","error":{"code":"server_is_overloaded","message":"Please retry"}}` + "\n\n"))
	var streamError *warmupStreamError
	require.ErrorAs(t, err, &streamError)
	require.Equal(t, "server_is_overloaded", streamError.Code)
}

func TestOpenAIWarmupExecuteCanceledContextSkips(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &OpenAIAccountWarmupService{}
	result := service.execute(ctx, &OpenAIWarmupJob{AccountID: 1})
	require.True(t, result.Skipped)
	require.False(t, result.Fatal)
}

func TestOpenAIWarmupStartStopIsIdempotent(t *testing.T) {
	repo := &warmupLifecycleRepo{}
	service := NewOpenAIAccountWarmupService(repo, nil, nil, nil)
	service.Start()
	service.Start()
	service.Stop()
	service.Stop()
}

type warmupLifecycleRepo struct{}

func (*warmupLifecycleRepo) Claim(context.Context) (*OpenAIWarmupJob, error) { return nil, nil }
func (*warmupLifecycleRepo) Renew(context.Context, *OpenAIWarmupJob) (bool, error) {
	return true, nil
}
func (*warmupLifecycleRepo) Finish(context.Context, *OpenAIWarmupJob, OpenAIWarmupResult) error {
	return nil
}
func (*warmupLifecycleRepo) Control(context.Context, int64, bool) error { return nil }
