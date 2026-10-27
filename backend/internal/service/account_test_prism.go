package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/prismbridge"
	"github.com/gin-gonic/gin"
)

func (s *AccountTestService) testPrismAccount(c *gin.Context, account *Account, model, prompt, mode string) error {
	if mode != "" && mode != "text" {
		return s.sendErrorAndEnd(c, "Prism supports normal text tests only")
	}
	if model == "" {
		model = "gpt-6.1-sol"
	}
	model = account.GetMappedModel(model)
	if prompt == "" {
		prompt = "Reply briefly with hello."
	}
	cookie, err := normalizePrismCookie(account.GetCredential(prismCookieKey))
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	proxy := ""
	if account.ProxyID != nil {
		if account.Proxy == nil {
			return s.sendErrorAndEnd(c, "Prism account proxy is unavailable")
		}
		proxy = account.Proxy.URL()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Minute)
	defer cancel()
	lease, err := prismbridge.Default.Acquire(ctx, account.ID, cookie, proxy)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	defer lease.Release()
	body, _ := json.Marshal(map[string]any{"model": model, "input": prompt, "stream": true})
	req, err := http.NewRequestWithContext(ctx, "POST", lease.URL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return s.sendErrorAndEnd(c, "Cannot create Prism test")
	}
	req.Header.Set("Authorization", "Bearer "+lease.Key)
	req.Header.Set("Content-Type", "application/json")
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Writer.Flush()
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
	resp, err := prismbridge.Client.Do(req)
	if err != nil {
		prismbridge.Default.Stop(account.ID)
		return s.sendErrorAndEnd(c, "Prism test connection failed; no retry was attempted")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return s.sendErrorAndEnd(c, "Prism rejected the test request; check the login and selected model")
	}
	return s.processOpenAIStream(c, resp.Body)
}
