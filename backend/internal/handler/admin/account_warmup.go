package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h *AccountHandler) SetWarmupService(s *service.OpenAIAccountWarmupService) { h.warmupService = s }
func (h *AccountHandler) RetryWarmup(c *gin.Context)                             { h.controlWarmup(c, false) }
func (h *AccountHandler) ResetWarmup(c *gin.Context)                             { h.controlWarmup(c, true) }
func (h *AccountHandler) controlWarmup(c *gin.Context, reset bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.warmupService == nil {
		response.Error(c, 503, "Warmup service unavailable")
		return
	}
	if err = h.warmupService.Control(c.Request.Context(), id, reset); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, account.Extra[service.OpenAIWarmupStateKey])
}

func withWarmupConfig(extra map[string]any, cfg *service.OpenAIWarmupConfig) map[string]any {
	if cfg == nil {
		return extra
	}
	normalized := *cfg
	normalized.Normalize()
	if extra == nil {
		extra = make(map[string]any)
	}
	extra[service.OpenAIWarmupConfigKey] = normalized
	return extra
}
