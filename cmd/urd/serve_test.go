package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/protocol"
	"github.com/ristir/urd/internal/query"
)

func writeConfig(t *testing.T, home, exclude string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`[engine]
mode = "daemon"
idle_timeout = "1s"

[sources]
auto = true

[ui]
indicator = "suffix"
trigger = "urd"

[filters]
exclude = [%s]
`, exclude)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func askDaemon(t *testing.T, q string) (query.Result, error) {
	t.Helper()
	conn, err := net.DialTimeout("unix", config.SocketPath(), 500*time.Millisecond)
	if err != nil {
		return query.Result{}, err
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "Q 0 %s\n", q); err != nil {
		return query.Result{}, err
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	return protocol.DecodeResponse(bufio.NewReader(conn))
}

func TestServeWritesItsOwnPidFileAndRemovesItOnExit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, home, "")

	done := make(chan int, 1)
	go func() { done <- runServe() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := askDaemon(t, "ll"); err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("the daemon never answered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	pid, _, ok := readPidFile(config.PidPath())
	if !ok || pid != os.Getpid() {
		t.Fatalf("pid file = (pid=%d ok=%v), want this process's pid %d", pid, ok, os.Getpid())
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon did not exit on idle")
	}
	if _, err := os.Stat(config.PidPath()); !os.IsNotExist(err) {
		t.Fatalf("pid file survived a clean exit: err=%v", err)
	}
}

func TestDaemonNoticesChangedFiltersOnDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"),
		[]byte(": 100:0;kubectl get pods\n: 200:0;ansible-playbook rate-limit.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, home, `"kubectl"`)

	saved := refreshInterval
	refreshInterval = 100 * time.Millisecond
	t.Cleanup(func() { refreshInterval = saved })

	done := make(chan int, 1)
	go func() { done <- runServe() }()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the daemon did not exit on idle")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	var res query.Result
	var err error
	for {
		res, err = askDaemon(t, "kubectl")
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the daemon never answered: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res.Total != 0 {
		t.Fatalf("kubectl is excluded by the config but the daemon found it: %+v", res)
	}

	writeConfig(t, home, "")

	deadline = time.Now().Add(5 * time.Second)
	for {
		res, err = askDaemon(t, "kubectl")
		if err == nil && res.Total == 1 {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the filter was removed from the config but the daemon still answers %+v (err %v)", res, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
