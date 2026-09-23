package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const OpenAIWarmupConfigKey = "openai_warmup"
const OpenAIWarmupStateKey = "openai_warmup_state"

type OpenAIWarmupConfig struct {
	Enabled          bool     `json:"enabled"`
	Model            string   `json:"model"`
	Models           []string `json:"models,omitempty"`
	Concurrency      int      `json:"concurrency"`
	Prompt           string   `json:"prompt"`
	SuccessThreshold int      `json:"success_threshold"`
}

// Normalize keeps the legacy model/concurrency fields usable while allowing
// newer clients to configure a fixed list of model sessions. One concurrency
// slot is assigned to each model in order and then repeats when the threshold
// requires more turns than the configured slots.
func (c *OpenAIWarmupConfig) Normalize() {
	if c == nil {
		return
	}
	models := make([]string, 0, len(c.Models)+1)
	for _, model := range c.Models {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	if len(models) == 0 && strings.TrimSpace(c.Model) != "" {
		models = append(models, strings.TrimSpace(c.Model))
	}
	c.Models = models
	if len(models) > 0 {
		c.Model = models[0]
	}
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
}

func (c OpenAIWarmupConfig) ModelsForSlots() []string {
	c.Normalize()
	return c.Models
}

func (c OpenAIWarmupConfig) ModelForSlot(slot int) string {
	models := c.ModelsForSlots()
	if len(models) == 0 {
		return strings.TrimSpace(c.Model)
	}
	if slot < 0 {
		slot = 0
	}
	return models[slot%len(models)]
}

func (a *Account) OpenAIWarmupConfig() OpenAIWarmupConfig {
	var cfg OpenAIWarmupConfig
	if a != nil && a.Extra[OpenAIWarmupConfigKey] != nil {
		raw, _ := json.Marshal(a.Extra[OpenAIWarmupConfigKey])
		_ = json.Unmarshal(raw, &cfg)
	}
	cfg.Normalize()
	return cfg
}

func (a *Account) IsWarmupBlocked() bool {
	if a == nil || a.Platform != PlatformOpenAI {
		return false
	}
	cfg := a.OpenAIWarmupConfig()
	if !cfg.Enabled {
		return false
	}
	var state struct {
		Status       string `json:"status"`
		SuccessCount int    `json:"success_count"`
	}
	raw, _ := json.Marshal(a.Extra[OpenAIWarmupStateKey])
	_ = json.Unmarshal(raw, &state)
	return state.Status != "completed" || cfg.SuccessThreshold <= 0 || state.SuccessCount < cfg.SuccessThreshold
}

func ValidateOpenAIWarmup(a *Account) error {
	if a == nil {
		return nil
	}
	raw, exists := a.Extra[OpenAIWarmupConfigKey]
	if !exists {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return infraerrors.BadRequest("INVALID_WARMUP", "Invalid warmup configuration")
	}
	// A JSON null is the account-level way to turn warmup off while retaining
	// the previous task configuration; the database trigger performs that
	// preservation when the account row is written.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var cfg OpenAIWarmupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return infraerrors.BadRequest("INVALID_WARMUP", "Invalid warmup configuration")
	}
	if !cfg.Enabled {
		return nil
	}
	if a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.IsCredentialShadow() {
		return infraerrors.BadRequest("INVALID_WARMUP", "Warmup requires an OpenAI OAuth parent account")
	}
	cfg.Normalize()
	models := cfg.ModelsForSlots()
	if len(models) == 0 || strings.TrimSpace(cfg.Prompt) == "" || (a.Concurrency > 0 && cfg.Concurrency > a.Concurrency) || cfg.SuccessThreshold < 1 {
		return infraerrors.BadRequest("INVALID_WARMUP", "Warmup requires model, prompt, positive threshold and concurrency within account capacity")
	}
	for _, model := range models {
		if !a.IsModelSupported(model) || isOpenAIImageModel(a.GetMappedModel(model)) {
			return infraerrors.BadRequest("INVALID_WARMUP_MODEL", "Warmup requires supported text models")
		}
	}
	return nil
}

type OpenAIWarmupTurn struct {
	Input     json.RawMessage   `json:"input"`
	Output    []json.RawMessage `json:"output"`
	TurnState string            `json:"turn_state,omitempty"`
}

type OpenAIWarmupJob struct {
	AccountID  int64
	Generation int64
	Slot       int
	LeaseToken string
	SessionID  string
	Model      string
	Config     OpenAIWarmupConfig
	History    []OpenAIWarmupTurn
}

type OpenAIWarmupResult struct {
	Attempted     bool
	UpstreamModel string
	History       []OpenAIWarmupTurn
	InputTokens   int64
	OutputTokens  int64
	Error         string
	Fatal         bool
	RetryAt       time.Time
	Skipped       bool
	DurationMs    int
}

type OpenAIWarmupRepository interface {
	Claim(context.Context) (*OpenAIWarmupJob, error)
	Renew(context.Context, *OpenAIWarmupJob) (bool, error)
	Finish(context.Context, *OpenAIWarmupJob, OpenAIWarmupResult) error
	Control(context.Context, int64, bool) error
}

type OpenAIWarmupSchedulingRepository interface {
	IsOpenAIWarmupAllowed(context.Context, int64) (bool, error)
}

func (s *OpenAIGatewayService) CheckOpenAIWarmupAllowed(ctx context.Context, account *Account) error {
	if account == nil || account.Platform != PlatformOpenAI || !account.IsOAuth() {
		return nil
	}
	allowed := false
	if s != nil {
		if repo, ok := s.accountRepo.(OpenAIWarmupSchedulingRepository); ok {
			var err error
			allowed, err = repo.IsOpenAIWarmupAllowed(ctx, account.ID)
			if err != nil {
				return fmt.Errorf("check account warmup: %w", err)
			}
		} else {
			target := account
			if account.IsCredentialShadow() && account.ParentAccountID != nil && s.accountRepo != nil {
				parent, err := s.accountRepo.GetByID(ctx, *account.ParentAccountID)
				if err != nil {
					return fmt.Errorf("resolve warmup parent: %w", err)
				}
				if parent == nil {
					return infraerrors.ServiceUnavailable("ACCOUNT_WARMING_UP", "Account warmup parent is unavailable")
				}
				target = parent
			}
			allowed = !target.IsWarmupBlocked()
		}
	} else {
		allowed = !account.IsWarmupBlocked()
	}
	if !allowed {
		return infraerrors.ServiceUnavailable("ACCOUNT_WARMING_UP", "Account warmup has not completed")
	}
	return nil
}

type OpenAIAccountWarmupService struct {
	repo        OpenAIWarmupRepository
	accounts    AccountRepository
	executor    *AccountTestService
	concurrency *ConcurrencyService
	usageLogs   UsageLogRepository
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	started     bool
	wg          sync.WaitGroup
}

func NewOpenAIAccountWarmupService(repo OpenAIWarmupRepository, accounts AccountRepository, executor *AccountTestService, concurrency *ConcurrencyService) *OpenAIAccountWarmupService {
	return &OpenAIAccountWarmupService{repo: repo, accounts: accounts, executor: executor, concurrency: concurrency}
}

func (s *OpenAIAccountWarmupService) SetUsageLogRepository(repo UsageLogRepository) {
	if s != nil {
		s.usageLogs = repo
	}
}

func (s *OpenAIAccountWarmupService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.started {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true
	for i := 0; i < 10; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				job, err := s.repo.Claim(ctx)
				if err != nil {
					if ctx.Err() == nil {
						slog.Error("warmup claim failed", "error", err)
					}
					continue
				}
				if job != nil {
					s.run(ctx, job)
				}
			}
		}()
	}
}

func (s *OpenAIAccountWarmupService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if !s.started || s.cancel == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.cancel = nil
	s.started = false
}

func (s *OpenAIAccountWarmupService) Control(ctx context.Context, accountID int64, reset bool) error {
	return s.repo.Control(ctx, accountID, reset)
}

func (s *OpenAIAccountWarmupService) run(parent context.Context, job *OpenAIWarmupJob) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	renewStop := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-renewStop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			valid, err := s.repo.Renew(ctx, job)
			if err != nil || !valid {
				cancel()
				return
			}
		}
	}()
	result := s.execute(ctx, job)
	if result.DurationMs <= 0 {
		result.DurationMs = int(time.Since(started).Milliseconds())
	}
	close(renewStop)
	cancel()
	<-renewDone
	if parent.Err() != nil {
		result.Skipped = true
		if result.Error == "" {
			result.Error = "warmup interrupted by service shutdown"
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	if err := s.repo.Finish(finishCtx, job, result); err != nil {
		slog.Error("warmup progress commit failed", "account_id", job.AccountID, "error", err)
	}
	s.recordUsage(finishCtx, job, result)
	if result.Error != "" {
		slog.Warn("warmup turn failed", "account_id", job.AccountID, "model", job.Model, "slot", job.Slot, "attempted", result.Attempted, "error", result.Error)
	}
}

func (s *OpenAIAccountWarmupService) recordUsage(ctx context.Context, job *OpenAIWarmupJob, result OpenAIWarmupResult) {
	if s == nil || s.usageLogs == nil || job == nil || !result.Attempted {
		return
	}
	duration := result.DurationMs
	requestID := "warmup:" + uuid.NewString()
	if result.Error != "" || result.Skipped {
		requestID = "warmup:failed:" + uuid.NewString()
	}
	model := job.Model
	if model == "" {
		model = job.Config.ModelForSlot(job.Slot)
	}
	_, err := s.usageLogs.Create(ctx, &UsageLog{
		AccountID:        job.AccountID,
		RequestID:        requestID,
		Model:            model,
		RequestedModel:   model,
		UpstreamModel:    warmupStringPtr(result.UpstreamModel),
		InputTokens:      int(result.InputTokens),
		OutputTokens:     int(result.OutputTokens),
		RequestType:      RequestTypeWarmup,
		DurationMs:       &duration,
		RateMultiplier:   1,
		UserAgent:        warmupStringPtr(CodexCanonicalUserAgent()),
		InboundEndpoint:  warmupStringPtr("/v1/responses"),
		UpstreamEndpoint: warmupStringPtr("/backend-api/codex/responses"),
		SessionID:        warmupStringPtr(job.SessionID),
		CreatedAt:        time.Now(),
	})
	if err != nil {
		slog.Warn("warmup usage log failed", "account_id", job.AccountID, "model", model, "error", err)
	}
}

func warmupStringPtr(value string) *string { return &value }

func (s *OpenAIAccountWarmupService) execute(ctx context.Context, job *OpenAIWarmupJob) OpenAIWarmupResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return OpenAIWarmupResult{Skipped: true, RetryAt: time.Now().Add(30 * time.Second)}
	}
	if s == nil || job == nil || s.accounts == nil || s.executor == nil || s.concurrency == nil {
		return OpenAIWarmupResult{Skipped: true, RetryAt: time.Now().Add(30 * time.Second)}
	}
	account, err := s.accounts.GetByID(ctx, job.AccountID)
	if err != nil || account == nil {
		return OpenAIWarmupResult{Skipped: true}
	}
	// Check ordinary health with only this task's scheduling gate removed.
	healthy := *account
	healthy.Extra = cloneWarmupExtra(account.Extra)
	delete(healthy.Extra, OpenAIWarmupConfigKey)
	if !healthy.IsSchedulable() {
		return OpenAIWarmupResult{Skipped: true, RetryAt: time.Now().Add(30 * time.Second)}
	}
	if err := ValidateOpenAIWarmup(account); err != nil {
		return OpenAIWarmupResult{Error: err.Error(), Fatal: true}
	}
	if gateway := s.executor.openaiGatewayService; gateway != nil {
		if gateway.isOpenAIAccountBlockedBySchedulingThreshold(ctx, &healthy) || gateway.isOpenAIAccountRequestRuntimeBlocked(&healthy, job.Model, false) || gateway.isOpenAIProxyStreamQuarantined(ctx, &healthy) {
			return OpenAIWarmupResult{Skipped: true, RetryAt: time.Now().Add(30 * time.Second)}
		}
	}
	slot, err := s.concurrency.AcquireAccountSlot(ctx, account.ID, account.Concurrency)
	if err != nil || slot == nil || !slot.Acquired {
		return OpenAIWarmupResult{Skipped: true}
	}
	defer slot.ReleaseFunc()
	valid, err := s.repo.Renew(ctx, job)
	if err != nil || !valid {
		return OpenAIWarmupResult{Skipped: true}
	}
	result, err := s.executor.ExecuteOpenAIWarmupTurn(ctx, account, job)
	if err == nil {
		return result
	}
	if ctx.Err() != nil {
		result.Skipped = true
		result.RetryAt = time.Now().Add(30 * time.Second)
		result.Error = ctx.Err().Error()
		return result
	}
	var upstream *warmupUpstreamError
	result.Error = truncateString(sanitizeUpstreamErrorMessage(err.Error()), 1024)
	var streamErr *warmupStreamError
	if errors.As(err, &streamErr) && (streamErr.Code == "server_is_overloaded" || streamErr.Code == "rate_limit_exceeded" || streamErr.Code == "server_error") {
		result.Skipped = true
		result.RetryAt = time.Now().Add(time.Minute)
	}
	if strings.Contains(err.Error(), "context_length_exceeded") {
		result.Fatal = true
	}
	if errors.As(err, &upstream) {
		result.Fatal = upstream.Status >= 400 && upstream.Status < 500 && upstream.Status != 429
		if upstream.Status == 429 {
			result.Skipped = true
			result.RetryAt = time.Now().Add(time.Minute)
		}
	}
	return result
}

func cloneWarmupExtra(extra map[string]any) map[string]any {
	result := make(map[string]any, len(extra))
	for key, value := range extra {
		result[key] = value
	}
	return result
}

type warmupUpstreamError struct {
	Status  int
	Message string
}

func (e *warmupUpstreamError) Error() string {
	return fmt.Sprintf("Warmup HTTP %d: %s", e.Status, e.Message)
}
