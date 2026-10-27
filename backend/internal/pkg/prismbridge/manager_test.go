package prismbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("PRISM_MANAGER_TEST_PROCESS") == "1" {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		var cfg map[string]string
		if json.Unmarshal([]byte(line), &cfg) != nil {
			os.Exit(2)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(3)
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+cfg["key"] {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(os.Args[len(os.Args)-1], "login.py") {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "cookie": "private-cookie", "frame": "private-frame", "ok": true})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"profile": cfg["profile"], "args": os.Args, "cookie": cfg["cookie"], "proxy": cfg["proxy"]})
		})}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]int{"port": listener.Addr().(*net.TCPAddr).Port})
		go server.Serve(listener)
		_, _ = io.Copy(io.Discard, reader)
		_ = server.Close()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("SUB2API_PRISM_PYTHON", exe)
	t.Setenv("SUB2API_PRISM_DATA_DIR", t.TempDir())
	t.Setenv("PRISM_MANAGER_TEST_PROCESS", "1")
	m := &Manager{}
	t.Cleanup(func() {
		m.mu.Lock()
		var done []chan struct{}
		for _, w := range m.workers {
			done = append(done, w.done)
		}
		m.mu.Unlock()
		m.Close()
		for _, ch := range done {
			select {
			case <-ch:
			case <-time.After(10 * time.Second):
				t.Error("worker did not exit")
			}
		}
	})
	return m
}

func TestManagedWorkersIsolationAndLifecycle(t *testing.T) {
	m := testManager(t)
	first, err := m.Acquire(context.Background(), 1, "secret-one", "http://proxy:8080")
	require.NoError(t, err)
	_, err = m.Acquire(context.Background(), 1, "secret-one", "")
	require.ErrorContains(t, err, "busy")
	second, err := m.Acquire(context.Background(), 2, "secret-two", "")
	require.NoError(t, err)
	require.NotEqual(t, first.URL, second.URL)
	require.NotEqual(t, first.Key, second.Key)
	req, _ := http.NewRequest("GET", first.URL, nil)
	req.Header.Set("Authorization", "Bearer "+first.Key)
	resp, err := Client.Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	var payload struct {
		Args          []string
		Cookie, Proxy string
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, "secret-one", payload.Cookie)
	require.Equal(t, "http://proxy:8080", payload.Proxy)
	require.NotContains(t, strings.Join(payload.Args, " "), "secret-one")
	first.Release()
	same, err := m.Acquire(context.Background(), 1, "secret-one", "http://proxy:8080")
	require.NoError(t, err)
	require.Equal(t, first.URL, same.URL)
	same.Release()
	rotated, err := m.Acquire(context.Background(), 1, "secret-new", "")
	require.NoError(t, err)
	require.NotEqual(t, first.Key, rotated.Key)
	select {
	case <-first.worker.done:
	case <-time.After(10 * time.Second):
		t.Fatal("old credential process not stopped")
	}
	rotated.Abort()
	select {
	case <-rotated.worker.done:
	case <-time.After(10 * time.Second):
		t.Fatal("aborted process not stopped")
	}
	second.Release()
}

func TestManagedWorkerCapacityAndIdleEviction(t *testing.T) {
	m := testManager(t)
	t.Setenv("SUB2API_PRISM_MAX_WORKERS", "1")
	first, err := m.Acquire(context.Background(), 1, "cookie", "")
	require.NoError(t, err)
	_, err = m.Acquire(context.Background(), 2, "cookie", "")
	require.ErrorContains(t, err, "limit")
	first.Release()
	second, err := m.Acquire(context.Background(), 2, "cookie", "")
	require.NoError(t, err)
	select {
	case <-first.worker.done:
	case <-time.After(10 * time.Second):
		t.Fatal("idle worker not evicted")
	}
	second.Release()
	m.stopIdle(2, second.worker)
	select {
	case <-second.worker.done:
	case <-time.After(10 * time.Second):
		t.Fatal("idle worker not stopped")
	}
}
