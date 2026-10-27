package prismbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

var Logins = &LoginManager{}

var ErrLogin = errors.New("Prism login unavailable, expired or belongs to another account; generate a new login link")

type LoginManager struct {
	mu       sync.Mutex
	sessions map[string]*loginSession
	closed   bool
}

type loginSession struct {
	mu      sync.Mutex
	owner   string
	account int64
	expires time.Time
	worker  *worker
	cookie  string
	status  string
}

type LoginState struct {
	Status string `json:"status"`
	Frame  string `json:"frame,omitempty"`
	Origin string `json:"origin,omitempty"`
}

func (m *LoginManager) Create(ctx context.Context, owner string, account int64, proxy string) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.sessions) >= 4 {
		return "", time.Time{}, errors.New("Prism login browser limit reached; close an existing login or wait for expiry")
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*loginSession)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", time.Time{}, err
	}
	id := hex.EncodeToString(key)
	w, err := startRuntime(ctx, 0, sha256.Sum256(key), "", proxy, "login.py")
	if err != nil {
		return "", time.Time{}, err
	}
	s := &loginSession{owner: owner, account: account, worker: w, expires: time.Now().Add(10 * time.Minute)}
	m.sessions[id] = s
	time.AfterFunc(time.Until(s.expires), func() { m.remove(id, s) })
	return id, s.expires, nil
}

func stopLoginWorker(w *worker) {
	if w == nil {
		return
	}
	_ = w.stdin.Close()
	go func() {
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
			_ = w.cmd.Process.Kill()
		}
	}()
}

func (m *LoginManager) remove(id string, s *loginSession) {
	m.mu.Lock()
	if m.sessions[id] == s {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	stopLoginWorker(s.worker)
	s.worker, s.cookie, s.status = nil, "", "expired"
}

func (m *LoginManager) get(owner, id string) (*loginSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil || s.owner != owner || !time.Now().Before(s.expires) {
		return nil, ErrLogin
	}
	return s, nil
}

func loginRequest(ctx context.Context, w *worker, path string, data any, out any) error {
	if w == nil {
		return ErrLogin
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return ErrLogin
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url+path, bytes.NewReader(payload))
	if err != nil {
		return ErrLogin
	}
	req.Header.Set("Authorization", "Bearer "+w.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return ErrLogin
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrLogin
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out) != nil {
		return ErrLogin
	}
	return nil
}

func (s *loginSession) state(ctx context.Context) (LoginState, error) {
	if s.status != "" {
		return LoginState{Status: s.status}, nil
	}
	var result struct {
		LoginState
		Cookie string `json:"cookie"`
	}
	if err := loginRequest(ctx, s.worker, "/state", map[string]string{}, &result); err != nil {
		return LoginState{}, err
	}
	if result.Status == "completed" || result.Status == "failed" {
		s.status, s.cookie = result.Status, result.Cookie
		stopLoginWorker(s.worker)
		s.worker = nil
	}
	return result.LoginState, nil // Cookie is not a field of the public state type.
}

func (m *LoginManager) State(ctx context.Context, owner, id string, frame bool) (LoginState, error) {
	s, err := m.get(owner, id)
	if err != nil {
		return LoginState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.state(ctx)
	if !frame {
		state.Frame = ""
	}
	return state, err
}

func (m *LoginManager) Action(ctx context.Context, owner, id string, action map[string]any) error {
	s, err := m.get(owner, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result struct {
		OK bool `json:"ok"`
	}
	if err = loginRequest(ctx, s.worker, "/action", action, &result); err != nil {
		return err
	}
	if !result.OK {
		return errors.New("Prism browser action failed; wait for the page and try again")
	}
	return nil
}

// WithCookie serializes redemption. Failed database saves leave the login
// retryable; success consumes it, so a draft cannot create multiple accounts.
func (m *LoginManager) WithCookie(ctx context.Context, owner, id string, account int64, save func(string) error) error {
	s, err := m.get(owner, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account != account || !time.Now().Before(s.expires) {
		return ErrLogin
	}
	state, err := s.state(ctx)
	if err != nil || state.Status != "completed" || s.cookie == "" {
		return ErrLogin
	}
	if err := save(s.cookie); err != nil {
		return err
	}
	s.cookie, s.status = "", "consumed"
	return nil
}

func (m *LoginManager) Cancel(owner, id string) error {
	s, err := m.get(owner, id)
	if err != nil {
		return err
	}
	m.remove(id, s)
	return nil
}

func (m *LoginManager) Close() {
	m.mu.Lock()
	m.closed = true
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()
	for id, s := range sessions {
		m.remove(id, s)
	}
}
