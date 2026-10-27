package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPrismLoginDoesNotAuditPasswordsOrKeystrokes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditCaptureRepository{}
	svc := service.NewAuditLogService(repo, nil)
	svc.Start()
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAuditLogMiddleware(svc)))
	r.POST("/api/v1/admin/prism/login/:session/action", func(c *gin.Context) {
		var body map[string]any
		require.NoError(t, c.ShouldBindJSON(&body))
		require.Equal(t, "canary-password", body["text"])
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/prism/login/session/action", strings.NewReader(`{"type":"text","text":"canary-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	svc.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 1)
	require.Equal(t, "<credential-bearing body omitted>", repo.logs[0].RequestBody)
	require.NotContains(t, service.RedactAuditBody([]byte(`{"prism_login_session":"canary-session"}`), "application/json"), "canary-session")
}
