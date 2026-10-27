package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func prismTestCookie(exp int64) string {
	return "prism_oai_access_token=header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp))) + ".signature"
}

func TestPrismCredentialsAndCapabilities(t *testing.T) {
	cookie := prismTestCookie(time.Now().Add(time.Hour).Unix())
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{prismEnabledKey: true}, Credentials: map[string]any{prismCookieKey: fmt.Sprintf(`{"cookie":%q}`, cookie)}}
	require.NoError(t, validatePrismAccount(a))
	require.Equal(t, cookie, a.GetCredential(prismCookieKey))
	require.True(t, IsSensitiveCredentialKey(prismCookieKey))
	require.Equal(t, cookie, MergePreservingSensitiveCreds(a.Credentials, map[string]any{"model_mapping": map[string]any{}})[prismCookieKey])
	require.True(t, a.IsPrismEnabled())
	require.True(t, a.IsOpenAIWSForceHTTPEnabled())
	require.Equal(t, OpenAIWSIngressModeOff, a.ResolveOpenAIResponsesWebSocketV2Mode("ctx_pool"))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.False(t, a.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic))
	require.True(t, a.IsModelSupported("gpt-6.1-sol"))
	require.False(t, a.IsModelSupported("gpt-image-1"))
	a.Extra[prismEnabledKey] = false
	require.False(t, a.IsPrismEnabled())
	a.Credentials[prismCookieKey] = prismTestCookie(1)
	require.NoError(t, validatePrismAccount(a), "expired login must not prevent disabling")
	a.Extra[prismEnabledKey] = true
	require.Error(t, validatePrismAccount(a))
	a.Extra[prismEnabledKey] = "true"
	require.Error(t, validatePrismAccount(a))
}

func TestPrismIngressDoesNotUseOAuthWhenLoginMissing(t *testing.T) {
	for _, protocol := range []string{"responses", "chat", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			c := newPrismTestContext()
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{prismEnabledKey: true}}
			svc := &OpenAIGatewayService{}
			body := []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hello"}],"input":"hello","max_tokens":100}`)
			var err error
			switch protocol {
			case "responses":
				_, err = svc.Forward(context.Background(), c, a, body)
			case "chat":
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
			case "messages":
				_, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
			}
			require.ErrorContains(t, err, "Prism")
			require.Equal(t, 400, c.Writer.Status())
		})
	}
}

func newPrismTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	return c
}

func TestPrismNativeResponsePreservesToolsAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			payload := `{"id":"resp_prism","status":"completed","model":"gpt-6.1-sol","output":[{"type":"custom_tool_call","name":"apply_patch","input":"*** patch"}],"usage":{"input_tokens":13,"output_tokens":7,"input_tokens_details":{"cached_tokens":4}}}`
			raw := payload
			if stream {
				raw = ": ping\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\n"
			}
			c := newPrismTestContext()
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
			result, err := (&OpenAIGatewayService{}).handlePrismResponse(c, resp, []byte(fmt.Sprintf(`{"stream":%t}`, stream)), "responses", "alias", "gpt-6.1-sol", time.Now(), func() { t.Error("unexpected abort") })
			require.NoError(t, err)
			require.Equal(t, 13, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			require.Equal(t, 4, result.Usage.CacheReadInputTokens)
			require.Equal(t, "gpt-6.1-sol", result.UpstreamResponseModel)
			require.Equal(t, "resp_prism", result.ResponseID)
			require.Equal(t, len(raw), c.Writer.Size())
		})
	}
}

func TestPrismFailedAndTruncatedStreams(t *testing.T) {
	for _, raw := range []string{
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"expired\"}}}\n\n",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
	} {
		aborted := false
		_, err := (&OpenAIGatewayService{}).handlePrismResponse(newPrismTestContext(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, []byte(`{"stream":true}`), "responses", "model", "model", time.Now(), func() { aborted = true })
		require.Error(t, err)
		require.True(t, aborted)
	}
}

func TestPrismMessagesStreamDoesNotMaskErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	raw := ": ping\n\ndata: {\"error\":{\"message\":\"expired login\"}}\n\ndata: [DONE]\n\n"
	aborted := false
	_, err := (&OpenAIGatewayService{}).handlePrismResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, []byte(`{"stream":true}`), "messages", "model", "model", time.Now(), func() { aborted = true })
	require.Error(t, err)
	require.True(t, aborted)
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), ": ping")
	require.NotContains(t, recorder.Body.String(), "message_stop")
}

func TestPrismImageInputIsolation(t *testing.T) {
	require.NoError(t, validatePrismImages(gjson.Parse(`{"input":[{"type":"input_image","image_url":"data:image/png;base64,YQ=="}]}`)))
	for _, url := range []string{"http://127.0.0.1/admin", "file:///etc/passwd", "/etc/passwd", "https://example.com/image.png"} {
		require.Error(t, validatePrismImages(gjson.Parse(fmt.Sprintf(`{"messages":[{"content":[{"type":"image_url","image_url":{"url":%q}}]}]}`, url))))
	}
}
