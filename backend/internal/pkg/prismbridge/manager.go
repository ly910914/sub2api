// Package prismbridge manages isolated, local Prism browser workers.
package prismbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/bridge.py scripts/managed.py scripts/login.py scripts/LICENSE
var scripts embed.FS

// Default owns workers for gateway, account tests and administrative updates.
var Default = &Manager{}

type Manager struct {
	mu      sync.Mutex
	workers map[int64]*worker
	closed  bool
}

type worker struct {
	fingerprint [32]byte
	busy        bool
	timer       *time.Timer
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	done        chan struct{}
	url, key    string
}

type Lease struct {
	URL, Key string
	manager  *Manager
	id       int64
	worker   *worker
	once     sync.Once
}

// Abort discards an uncertain worker without replaying its in-flight turn.
func (l *Lease) Abort() {
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if l.manager.workers[l.id] == l.worker {
		l.manager.stopLocked(l.id)
	}
}

// Client never honors machine HTTP proxies or redirects for loopback traffic.
var Client = &http.Client{
	Transport:     &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (l *Lease) Release() {
	l.once.Do(func() {
		m := l.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.workers[l.id] != l.worker {
			return
		}
		l.worker.busy = false
		l.worker.timer = time.AfterFunc(10*time.Minute, func() { m.stopIdle(l.id, l.worker) })
	})
}

func (m *Manager) stopIdle(id int64, w *worker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.workers[id] == w && !w.busy {
		m.stopLocked(id)
	}
}

func (m *Manager) stopLocked(id int64) {
	w := m.workers[id]
	if w == nil {
		return
	}
	delete(m.workers, id)
	if w.timer != nil {
		w.timer.Stop()
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

func (m *Manager) Stop(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(id)
}

func (m *Manager) Close() {
	if m == Default {
		Logins.Close()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for id := range m.workers {
		m.stopLocked(id)
	}
}

// Acquire admits one turn per account. It never retries a submitted turn.
func (m *Manager) Acquire(ctx context.Context, id int64, cookie, proxy string) (*Lease, error) {
	if id <= 0 || cookie == "" {
		return nil, errors.New("Prism requires a saved account and imported login credentials")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("Prism manager is shutting down")
	}
	if m.workers == nil {
		m.workers = make(map[int64]*worker)
	}
	fingerprint := sha256.Sum256([]byte(cookie + "\x00" + proxy))
	w := m.workers[id]
	if w != nil {
		select {
		case <-w.done:
			m.stopLocked(id)
			w = nil
		default:
		}
	}
	if w != nil && w.busy {
		return nil, errors.New("Prism account is busy; wait for its current turn to finish")
	}
	if w != nil && w.fingerprint != fingerprint {
		m.stopLocked(id)
		w = nil
	}
	if w == nil {
		limit := 4
		if n, err := strconv.Atoi(os.Getenv("SUB2API_PRISM_MAX_WORKERS")); err == nil && n > 0 {
			limit = n
		}
		if len(m.workers) >= limit {
			for otherID, other := range m.workers {
				if !other.busy {
					m.stopLocked(otherID)
					break
				}
			}
			if len(m.workers) >= limit {
				return nil, errors.New("Prism browser limit reached; try again when a turn completes")
			}
		}
		var err error
		w, err = start(ctx, id, fingerprint, cookie, proxy)
		if err != nil {
			return nil, err
		}
		m.workers[id] = w
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.busy = true
	return &Lease{URL: w.url, Key: w.key, manager: m, id: id, worker: w}, nil
}

func start(ctx context.Context, id int64, fingerprint [32]byte, cookie, proxy string) (*worker, error) {
	return startRuntime(ctx, id, fingerprint, cookie, proxy, "managed.py")
}

func startRuntime(ctx context.Context, id int64, fingerprint [32]byte, cookie, proxy, entry string) (*worker, error) {
	root := os.Getenv("SUB2API_PRISM_DATA_DIR")
	if root == "" {
		root = filepath.Join("data", "prism")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Versioned script directory allows upgrades without rewriting active files.
	source, _ := scripts.ReadFile("scripts/" + entry)
	vendor, _ := scripts.ReadFile("scripts/bridge.py")
	digest := sha256.Sum256(append(source, vendor...))
	scriptDir := filepath.Join(root, "runtime-"+hex.EncodeToString(digest[:8]))
	if err = os.MkdirAll(scriptDir, 0700); err != nil {
		return nil, err
	}
	for _, name := range []string{"bridge.py", "managed.py", "login.py", "LICENSE"} {
		data, _ := scripts.ReadFile("scripts/" + name)
		if err = os.WriteFile(filepath.Join(scriptDir, name), data, 0600); err != nil {
			return nil, err
		}
	}
	// Fresh profile per process prevents concurrent old/new browser locks and
	// stale anonymous cookies; durable continuation metadata remains per account.
	accountDir := filepath.Join(root, strconv.FormatInt(id, 10), hex.EncodeToString(fingerprint[:8]))
	if err = os.MkdirAll(accountDir, 0700); err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp(accountDir, "profile-")
	if err != nil {
		return nil, err
	}
	python := os.Getenv("SUB2API_PRISM_PYTHON")
	if python == "" {
		python = "python3"
		if runtime.GOOS == "windows" {
			python = "python"
		}
	}
	cmd := exec.Command(python, "-u", filepath.Join(scriptDir, entry))
	configureProcess(cmd)
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONDONTWRITEBYTECODE=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start Prism runtime: install Python and Playwright or use the Prism image: %w", err)
	}
	cleanupProcess, err := ownProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	keyBytes := make([]byte, 32)
	if _, err = rand.Read(keyBytes); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cleanupProcess()
		return nil, err
	}
	w := &worker{fingerprint: fingerprint, cmd: cmd, stdin: stdin, done: make(chan struct{}), key: hex.EncodeToString(keyBytes)}
	go func() {
		_ = cmd.Wait()
		cleanupProcess()
		// profile is a fresh MkdirTemp child of accountDir, never a caller path.
		_ = os.RemoveAll(profile)
		close(w.done)
	}()
	cfg := map[string]string{"cookie": cookie, "proxy": proxy, "profile": profile, "key": w.key, "browser": os.Getenv("SUB2API_PRISM_BROWSER")}
	if err = json.NewEncoder(stdin).Encode(cfg); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return nil, errors.New("Prism runtime initialization failed")
	}
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(io.LimitReader(stdout, 4096)).ReadString('\n'); ready <- line }()
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	var line string
	select {
	case line = <-ready:
	case <-ctx.Done():
		err = ctx.Err()
	case <-timeout.C:
		err = errors.New("Prism runtime startup timed out")
	}
	var endpoint struct {
		Port int `json:"port"`
	}
	if err != nil || json.Unmarshal([]byte(strings.TrimSpace(line)), &endpoint) != nil || endpoint.Port <= 0 || endpoint.Port > 65535 {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("Prism runtime unavailable: install Python, Playwright and Chromium; see docs/PRISM.md")
	}
	w.url = "http://127.0.0.1:" + strconv.Itoa(endpoint.Port)
	return w, nil
}
