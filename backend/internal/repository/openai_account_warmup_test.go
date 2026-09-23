package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOpenAIWarmupSchedulerProjection(t *testing.T) {
	extra := map[string]any{
		service.OpenAIWarmupConfigKey: service.OpenAIWarmupConfig{Enabled: true, Model: "gpt-5", Prompt: "private prompt", Concurrency: 2, SuccessThreshold: 3},
		service.OpenAIWarmupStateKey:  map[string]any{"status": "running", "success_count": 2, "last_error": "private error"},
	}
	projected := filterSchedulerExtra(extra)
	a := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Extra: projected}
	require.False(t, a.IsSchedulable())
	require.NotContains(t, projected[service.OpenAIWarmupConfigKey], "prompt")
	require.NotContains(t, projected[service.OpenAIWarmupStateKey], "last_error")
	extra[service.OpenAIWarmupStateKey] = map[string]any{"status": "completed", "success_count": 3}
	a.Extra = filterSchedulerExtra(extra)
	require.True(t, a.IsSchedulable())
}
