package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const prismEnabledKey = "prism_enabled"
const prismCookieKey = "prism_cookie"

func (a *Account) IsPrismEnabled() bool {
	if a == nil || a.Platform != PlatformOpenAI || a.IsShadow() {
		return false
	}
	enabled, _ := a.Extra[prismEnabledKey].(bool)
	return enabled
}

// Accept either bridge.py login's auth.json or its cookie field. Never include
// the supplied value in errors (credentials are also redacted from admin DTOs).
func normalizePrismCookie(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var auth struct {
			Cookie string `json:"cookie"`
		}
		if json.Unmarshal([]byte(raw), &auth) != nil {
			return "", infraerrors.BadRequest("PRISM_AUTH_INVALID", "Prism auth.json is invalid")
		}
		raw = strings.TrimSpace(auth.Cookie)
	}
	if len(raw) > 65536 || strings.ContainsAny(raw, "\r\n") {
		return "", infraerrors.BadRequest("PRISM_AUTH_INVALID", "Prism cookie is invalid")
	}
	for _, item := range strings.Split(raw, ";") {
		name, token, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || name != "prism_oai_access_token" || token == "" {
			continue
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			break
		}
		claims, err := base64.RawURLEncoding.DecodeString(parts[1])
		var payload struct {
			Exp int64 `json:"exp"`
		}
		if err != nil || json.Unmarshal(claims, &payload) != nil {
			break
		}
		if payload.Exp <= time.Now().Unix() {
			return "", infraerrors.BadRequest("PRISM_AUTH_EXPIRED", "Prism login expired; generate a new Prism login link")
		}
		return raw, nil
	}
	return "", infraerrors.BadRequest("PRISM_AUTH_REQUIRED", "Complete Prism login using a generated login link; a Codex OAuth token is not a Prism login")
}

func validatePrismAccount(a *Account) error {
	raw, exists := a.Extra[prismEnabledKey]
	if exists && raw != nil {
		if _, ok := raw.(bool); !ok {
			return infraerrors.BadRequest("PRISM_FLAG_INVALID", "prism_enabled must be a boolean")
		}
	}
	enabled, _ := raw.(bool)
	if enabled && (a.Platform != PlatformOpenAI || a.IsShadow() || (a.Type != AccountTypeOAuth && a.Type != AccountTypeAPIKey && a.Type != AccountTypeSetupToken)) {
		return infraerrors.BadRequest("PRISM_ACCOUNT_INVALID", "Prism requires an independent OpenAI account")
	}
	if cookie, ok := a.Credentials[prismCookieKey]; ok {
		value, valid := cookie.(string)
		if !valid {
			return infraerrors.BadRequest("PRISM_AUTH_INVALID", "Prism credentials must be a string")
		}
		if value != "" && (enabled || strings.HasPrefix(strings.TrimSpace(value), "{")) {
			normalized, err := normalizePrismCookie(value)
			if err != nil {
				return err
			}
			a.Credentials[prismCookieKey] = normalized
		}
	}
	if enabled && strings.TrimSpace(a.GetCredential(prismCookieKey)) == "" {
		return infraerrors.BadRequest("PRISM_AUTH_REQUIRED", "Complete Prism login before enabling Prism")
	}
	return nil
}
