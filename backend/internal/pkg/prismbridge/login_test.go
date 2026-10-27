package prismbridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func loginManagerForTest(t *testing.T) *LoginManager {
	testManager(t) // A subprocess stand-in exercises stdin, HTTP and process ownership.
	m := &LoginManager{}
	t.Cleanup(m.Close)
	return m
}

func TestLoginOwnershipRedactionAndConsumption(t *testing.T) {
	m := loginManagerForTest(t)
	ctx := context.Background()
	id, _, err := m.Create(ctx, "admin:1", 42, "")
	require.NoError(t, err)
	s, err := m.get("admin:1", id)
	require.NoError(t, err)
	done := s.worker.done
	_, err = m.State(ctx, "admin:2", id, true)
	require.ErrorIs(t, err, ErrLogin)
	require.ErrorIs(t, m.Cancel("admin:2", id), ErrLogin)
	state, err := m.State(ctx, "admin:1", id, false)
	require.NoError(t, err)
	require.Equal(t, "completed", state.Status)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "cookie")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("login worker not stopped after credential capture")
	}
	called := false
	err = m.WithCookie(ctx, "admin:1", id, 43, func(string) error { called = true; return nil })
	require.ErrorIs(t, err, ErrLogin)
	require.False(t, called)
	saveErr := errors.New("database unavailable")
	err = m.WithCookie(ctx, "admin:1", id, 42, func(cookie string) error {
		require.Equal(t, "private-cookie", cookie)
		return saveErr
	})
	require.ErrorIs(t, err, saveErr)
	var successes atomic.Int32
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = m.WithCookie(ctx, "admin:1", id, 42, func(string) error { successes.Add(1); return nil })
		}()
	}
	group.Wait()
	require.Equal(t, int32(1), successes.Load())
	require.Empty(t, s.cookie)
	require.Equal(t, "consumed", s.status)
}

func TestLoginCancelExpiryAndShutdown(t *testing.T) {
	m := loginManagerForTest(t)
	ctx := context.Background()
	id, _, err := m.Create(ctx, "admin:1", 0, "")
	require.NoError(t, err)
	s, err := m.get("admin:1", id)
	require.NoError(t, err)
	done := s.worker.done
	require.NoError(t, m.Cancel("admin:1", id))
	_, err = m.State(ctx, "admin:1", id, false)
	require.ErrorIs(t, err, ErrLogin)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled browser still running")
	}
	m.sessions["expired"] = &loginSession{owner: "admin:1", expires: time.Now().Add(-time.Second)}
	_, err = m.State(ctx, "admin:1", "expired", false)
	require.ErrorIs(t, err, ErrLogin)
	m.Close()
	_, _, err = m.Create(ctx, "admin:1", 0, "")
	require.Error(t, err)
}
