package admin

import (
	"context"
	"errors"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/prismbridge"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) CreatePrismLogin(c *gin.Context) {
	var req struct {
		AccountID int64  `json:"account_id"`
		ProxyID   *int64 `json:"proxy_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.AccountID < 0 {
		response.BadRequest(c, "Invalid Prism login request")
		return
	}
	if req.AccountID > 0 {
		a, err := h.adminService.GetAccount(c.Request.Context(), req.AccountID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if a.Platform != service.PlatformOpenAI || a.IsShadow() {
			response.BadRequest(c, "Prism requires an independent OpenAI account")
			return
		}
		// The account's saved proxy is the fallback for login. The editor may
		// pass a positive unsaved selection when the operator changed it first.
		if req.ProxyID == nil || *req.ProxyID <= 0 {
			req.ProxyID = a.ProxyID
		}
	}
	proxy := ""
	if req.ProxyID != nil && *req.ProxyID > 0 {
		p, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		proxy = p.URL()
	}
	id, expires, err := prismbridge.Logins.Create(c.Request.Context(), adminActorScope(c), req.AccountID, proxy)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"session_id": id, "login_url": "/admin/prism-login#" + id, "expires_at": expires})
}

func (h *AccountHandler) GetPrismLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	state, err := prismbridge.Logins.State(c.Request.Context(), adminActorScope(c), c.Param("session"), c.Query("frame") == "1")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, state)
}

func (h *AccountHandler) PrismLoginAction(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var action map[string]any
	if c.ShouldBindJSON(&action) != nil {
		response.BadRequest(c, "Invalid browser action")
		return
	}
	if err := prismbridge.Logins.Action(c.Request.Context(), adminActorScope(c), c.Param("session"), action); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"ok": true})
}

func (h *AccountHandler) CancelPrismLogin(c *gin.Context) {
	if err := prismbridge.Logins.Cancel(adminActorScope(c), c.Param("session")); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Keeps the login ticket out of stored credentials and the raw cookie out of
// the request/idempotency payload. Normal service validation still applies.
func withPrismLogin(ctx context.Context, c *gin.Context, session string, id int64, credentials map[string]any, save func(map[string]any) error) error {
	if session == "" {
		return save(credentials)
	}
	if _, exists := credentials["prism_cookie"]; exists {
		return infraerrors.BadRequest("PRISM_LOGIN_CONFLICT", "Use either Prism login or imported credentials")
	}
	err := prismbridge.Logins.WithCookie(ctx, adminActorScope(c), session, id, func(cookie string) error {
		merged := make(map[string]any, len(credentials)+1)
		for k, v := range credentials {
			merged[k] = v
		}
		merged["prism_cookie"] = cookie
		return save(merged)
	})
	if errors.Is(err, prismbridge.ErrLogin) {
		return infraerrors.BadRequest("PRISM_LOGIN_INVALID", err.Error())
	}
	return err
}
