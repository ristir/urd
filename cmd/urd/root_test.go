package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/daemon"
	"github.com/ristir/urd/internal/setup"
)

func noopSpawn() error { return nil }

func captureStderr(t *testing.T, f func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// t.TempDir() bakes the test name into the path, and on macOS that overruns sockaddr_un.
func shortRuntimeDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "urdsock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRunRootNonInteractiveNeverTouchesRC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", home)
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "export A=1\n" {
		t.Fatalf(".zshrc was modified without a terminal:\n%s", got)
	}
	if !strings.Contains(out.String(), " from 1 source,") {
		t.Fatalf("summary missing: %s", out.String())
	}
	if !strings.Contains(out.String(), bootBin+" hook zsh") {
		t.Fatal("instruction for manual setup missing")
	}
}

func TestRunRootInteractiveWritesHookOnYes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", home)
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("y\nn\n"), true, false, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	rc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("rc not written: %v", err)
	}
	if !strings.Contains(string(rc), bootBin+" hook zsh") {
		t.Fatalf("hook not written:\n%s", rc)
	}
}

func TestRunRootInteractiveStealsCtrlROnYes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", home)
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\ny\n"), true, false, noopSpawn); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "urd", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "steal_ctrl_r = true") {
		t.Fatalf("config does not record the answer:\n%s", data)
	}
}

func TestRunRootSpawnsDaemonWhenSocketIsDead(t *testing.T) {
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

	spawned := false
	spawn := func() error {
		spawned = true
		ln, err := net.Listen("unix", config.SocketPath())
		if err != nil {
			return err
		}
		t.Cleanup(func() { ln.Close() })
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !spawned {
		t.Fatal("daemon spawn was not attempted although the socket had no listener")
	}
	if !strings.Contains(out.String(), "daemon started") {
		t.Fatalf("missing the daemon-started notice: %s", out.String())
	}
}

func TestRunRootDoesNotClaimSuccessWhenTheDaemonCannotBind(t *testing.T) {
	home := t.TempDir()
	// Deliberately longer than sockaddr_un (104 bytes on macOS): the daemon dies on bind,
	// and cmd.Start() still returns nil.
	longDir := filepath.Join(home, strings.Repeat("d", 120))
	if err := os.MkdirAll(longDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", longDir)
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 100:0;ll\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spawn := func() error { return nil }

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "daemon started") {
		t.Fatalf("claims the daemon started although it never bound the socket: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "not answering") {
		t.Fatalf("silent about the daemon not answering: %s", errOut.String())
	}
	if strings.Contains(out.String(), "urd:") {
		t.Fatalf("diagnostics leaked into stdout: %s", out.String())
	}
	if !strings.Contains(out.String(), " from 1 source,") {
		t.Fatalf("the summary itself is gone: %s", out.String())
	}
}

func TestRunRootAlwaysAttemptsRestartEvenWhenSocketIsAlive(t *testing.T) {
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

	ln, err := net.Listen("unix", config.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	spawned := false
	spawn := func() error { spawned = true; return nil }

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !spawned {
		t.Fatal("daemon spawn was not attempted although predictability requires restarting every time")
	}
}

func TestRunRootDoesNotClaimRestartWithoutAPidFile(t *testing.T) {
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

	ln, err := net.Listen("unix", config.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	spawn := func() error { return nil }

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "restarted") {
		t.Fatalf("claims a restart it could not have performed: %s", out.String())
	}
	if !strings.Contains(out.String(), "daemon started") {
		t.Fatalf("missing the daemon notice entirely: %s", out.String())
	}
}

func TestRunRootRestartsALiveDaemonUnderANewPid(t *testing.T) {
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

	oldProc := exec.Command("sleep", "30")
	if err := oldProc.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oldProc.Process.Kill() })
	if err := writePidFile(config.PidPath(), oldProc.Process.Pid, selfPath()); err != nil {
		t.Fatal(err)
	}

	oldLn, err := net.Listen("unix", config.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	oldLnOpen := true
	defer func() {
		if oldLnOpen {
			oldLn.Close()
		}
	}()

	newPid := oldProc.Process.Pid + 999999
	spawn := func() error {
		oldLn.Close()
		oldLnOpen = false
		newLn, err := net.Listen("unix", config.SocketPath())
		if err != nil {
			return err
		}
		t.Cleanup(func() { newLn.Close() })
		return writePidFile(config.PidPath(), newPid, selfPath())
	}

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- oldProc.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the old daemon process was not stopped")
	}

	gotPid, _, ok := readPidFile(config.PidPath())
	if !ok || gotPid != newPid {
		t.Fatalf("pid file = (pid=%d ok=%v), want the new daemon's pid %d", gotPid, ok, newPid)
	}
	if !strings.Contains(out.String(), "restart") {
		t.Fatalf("output does not mention the restart: %s", out.String())
	}
	if !strings.Contains(out.String(), "cold") {
		t.Fatalf("output does not name the cold-cache cost: %s", out.String())
	}
}

// The PID matches and the binary does not: the usual sign of a reused PID.
func TestRunRootDoesNotSignalAPidFileNamingADifferentBinary(t *testing.T) {
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

	oldProc := exec.Command("sleep", "30")
	if err := oldProc.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oldProc.Process.Kill() })
	if err := writePidFile(config.PidPath(), oldProc.Process.Pid, "/definitely/not/urd"); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("unix", config.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	spawn := func() error { return nil }

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	// signal 0 on a zombie answers "alive" on macOS, so Wait with a timeout is used instead.
	waitErr := make(chan error, 1)
	go func() { waitErr <- oldProc.Wait() }()
	select {
	case err := <-waitErr:
		t.Fatalf("a pid file naming a different binary must not be signalled, but the process exited: %v", err)
	case <-time.After(daemonStopGrace * 2):
	}
	if strings.Contains(out.String(), "restart") {
		t.Fatalf("claims a restart that must not have happened: %s", out.String())
	}
}

func writeBrokenConfig(t *testing.T, home string) string {
	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("mode = [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRootBareWarnsAndContinuesOnBrokenConfig(t *testing.T) {
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
	path := writeBrokenConfig(t, home)

	var out bytes.Buffer
	var errOut bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn)
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(stderr, path) {
		t.Fatalf("warning does not name the config path: %q", stderr)
	}
	if !strings.Contains(out.String(), " from 1 source,") {
		t.Fatalf("bare urd did not continue with defaults: %s", out.String())
	}
}

func TestRunRootSetupWarnsAndContinuesOnBrokenConfig(t *testing.T) {
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
	path := writeBrokenConfig(t, home)

	var out bytes.Buffer
	var errOut bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = runRoot(&out, &errOut, strings.NewReader("n\nn\n"), true, true, noopSpawn)
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(stderr, path) {
		t.Fatalf("warning does not name the config path: %q", stderr)
	}
}

func sandbox(t *testing.T) string {
	t.Helper()
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
	return home
}

func bigZshHistory(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, ": %d:0;cmd%d\n", 1700000000+i, i)
	}
	return []byte(b.String())
}

func bigCorpusSandbox(t *testing.T, entries int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), bigZshHistory(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func rcBackups(t *testing.T, home string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(home, ".zshrc.urd-bak-*"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRunRootSetupOffersHistLimitsAboveThreshold(t *testing.T) {
	home := bigCorpusSandbox(t, setup.HistTrimThreshold+1)
	rc := filepath.Join(home, ".zshrc")
	original := "export ZSH=/omz\nsource $ZSH/oh-my-zsh.sh\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	want := setup.HistLimit(setup.HistTrimThreshold + 1)

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\ny\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	if !strings.Contains(out.String(), fmt.Sprintf("to %d?", want)) {
		t.Fatalf("question does not name the computed number %d: %s", want, out.String())
	}

	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := fmt.Sprintf("HISTSIZE=%d\nSAVEHIST=%d", want, want)
	if !strings.Contains(string(got), wantLines) {
		t.Fatalf("rc does not carry the same number offered in the question:\n%s", got)
	}
	if !strings.HasPrefix(string(got), original) {
		t.Fatalf("original .zshrc content lost:\n%s", got)
	}
	if !strings.Contains(out.String(), "urd's corpus lives in its own store") {
		t.Fatalf("missing the reassurance line: %s", out.String())
	}

	bak := rcBackups(t, home)
	if len(bak) != 1 {
		t.Fatalf("expected exactly one .zshrc backup, got %v", bak)
	}
	saved, err := os.ReadFile(bak[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != original {
		t.Fatalf("backup does not hold the pre-edit .zshrc:\n%s", saved)
	}
}

func TestRunRootSetupHistLimitsDefaultIsNo(t *testing.T) {
	home := bigCorpusSandbox(t, setup.HistTrimThreshold+1)
	rc := filepath.Join(home, ".zshrc")
	original := "export ZSH=/omz\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\n\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("default answer wrote to .zshrc:\n%s", got)
	}
	if len(rcBackups(t, home)) != 0 {
		t.Fatal("backup created although the default answer changed nothing")
	}
}

func TestRunRootSetupHistLimitsIsIdempotentAcrossRuns(t *testing.T) {
	home := bigCorpusSandbox(t, setup.HistTrimThreshold+1)
	rc := filepath.Join(home, ".zshrc")

	var out1, errOut1 bytes.Buffer
	if code := runRoot(&out1, &errOut1, strings.NewReader("n\ny\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("first run: exit %d: %s", code, out1.String())
	}
	first, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}

	var out2, errOut2 bytes.Buffer
	if code := runRoot(&out2, &errOut2, strings.NewReader("n\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("second run: exit %d: %s", code, out2.String())
	}
	if strings.Contains(out2.String(), "Raise HISTSIZE") {
		t.Fatalf("second run asked again although the limits are already set: %s", out2.String())
	}
	second, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second run changed .zshrc:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestRunRootSetupSkipsHistLimitsWhenAlreadyCustomised(t *testing.T) {
	home := bigCorpusSandbox(t, setup.HistTrimThreshold+1)
	rc := filepath.Join(home, ".zshrc")
	original := "export ZSH=/omz\nSAVEHIST=5000\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "Raise HISTSIZE") {
		t.Fatalf("asked to raise limits although SAVEHIST is already set by hand: %s", out.String())
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("hand-set SAVEHIST was touched:\n%s", got)
	}
}

func TestRunRootSetupSkipsHistLimitsBelowThreshold(t *testing.T) {
	home := sandbox(t)
	rc := filepath.Join(home, ".zshrc")

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("y\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "Raise HISTSIZE") {
		t.Fatalf("offered to raise limits although the corpus is far below the trim threshold: %s", out.String())
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "HISTSIZE") {
		t.Fatalf("HISTSIZE written although it was never offered:\n%s", got)
	}
}

func TestRunRootNonInteractiveNeverOffersHistLimits(t *testing.T) {
	home := bigCorpusSandbox(t, setup.HistTrimThreshold+1)
	rc := filepath.Join(home, ".zshrc")
	original := "export A=1\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf(".zshrc touched without a terminal:\n%s", got)
	}
	if strings.Contains(out.String(), "Raise HISTSIZE") {
		t.Fatalf("question asked without a terminal: %s", out.String())
	}
}

const handWrittenConfig = `# my urd config, hand-written
[engine]
mode = "oneshot"
idle_timeout = "2h"

[sources]
auto = true
extra = [
  "~/backups/history-archive/**/.bash_history",
  "/mnt/backup/old-laptop/.zsh_history",
]

[ui]
indicator = "below"
trigger = "hist"

[filters]
exclude = ["^history", "^urd", "^ls$", "^cd "]
`

func writeGoodConfig(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(handWrittenConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func backups(t *testing.T, home string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(home, ".config", "urd", "config.toml.urd-bak-*"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRunRootSetupNeverRewritesAnUnparsableConfig(t *testing.T) {
	home := sandbox(t)
	path := writeBrokenConfig(t, home)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	captureStderr(t, func() {
		if code := runRoot(&out, &errOut, strings.NewReader("n\nn\n"), true, true, noopSpawn); code != 0 {
			t.Fatalf("exit %d: %s", code, out.String())
		}
	})

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the config was rewritten although it could not be parsed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(errOut.String(), "was not modified") {
		t.Fatalf("silent about leaving the file alone: %q", errOut.String())
	}
}

func TestRunRootSetupLeavesAnUnchangedConfigByteIdentical(t *testing.T) {
	home := sandbox(t)
	path := writeGoodConfig(t, home)

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\nn\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != handWrittenConfig {
		t.Fatalf("two noes rewrote the config:\n%s", after)
	}
	if got := backups(t, home); len(got) != 0 {
		t.Fatalf("backup created although nothing was written: %v", got)
	}
}

func TestRunRootSetupBacksUpTheConfigBeforeStealingCtrlR(t *testing.T) {
	home := sandbox(t)
	path := writeGoodConfig(t, home)

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader("n\ny\n"), true, true, noopSpawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}

	bak := backups(t, home)
	if len(bak) != 1 {
		t.Fatalf("expected exactly one backup, got %v", bak)
	}
	saved, err := os.ReadFile(bak[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != handWrittenConfig {
		t.Fatalf("the backup does not hold the original config:\n%s", saved)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "steal_ctrl_r = true") {
		t.Fatalf("the answer was not recorded:\n%s", after)
	}
	for _, want := range []string{`trigger = "hist"`, `indicator = "below"`, "old-laptop", "history-archive", `"^ls$"`, `"^cd "`} {
		if !strings.Contains(string(after), want) {
			t.Fatalf("setting %q lost on save:\n%s", want, after)
		}
	}
	if !strings.Contains(out.String(), "backup saved as") {
		t.Fatalf("silent about the backup: %q", out.String())
	}
}

func TestRunRootNamesABadFilterPatternOnEveryRun(t *testing.T) {
	home := sandbox(t)
	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[engine]\nmode = \"oneshot\"\n\n[filters]\nexclude = [\"*bad(\", \"^history\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 3; run++ {
		var out, errOut bytes.Buffer
		if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn); code != 0 {
			t.Fatalf("run %d: exit %d: %s", run, code, out.String())
		}
		if !strings.Contains(errOut.String(), `filter pattern "*bad(" did not compile`) {
			t.Fatalf("run %d did not name the pattern; stderr was %q, stdout %q", run, errOut.String(), out.String())
		}
		if run > 1 && !strings.Contains(out.String(), "index up to date") {
			t.Fatalf("run %d rebuilt, so the read path was never exercised: %s", run, out.String())
		}
	}
}

func TestRunRootStaysQuietWhenEveryFilterCompiles(t *testing.T) {
	home := sandbox(t)
	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[engine]\nmode = \"oneshot\"\n\n[filters]\nexclude = [\"^history\", \"^urd\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		var out, errOut bytes.Buffer
		if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if errOut.Len() != 0 {
			t.Fatalf("run %d complained although every pattern compiles: %q", run, errOut.String())
		}
	}
}

// daemonWait has to cover the ~200 ms daemon.Listen spends on a stale socket.
func TestRunRootWaitsLongEnoughForAReclaimedStaleSocket(t *testing.T) {
	sandbox(t)
	sock := config.SocketPath()

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the fixture is wrong: no stale socket file left: %v", err)
	}

	lnCh := make(chan net.Listener, 1)
	spawn := func() error {
		go func() {
			l, err := daemon.Listen(sock)
			if err != nil {
				close(lnCh)
				return
			}
			lnCh <- l
		}()
		return nil
	}
	t.Cleanup(func() {
		if l, ok := <-lnCh; ok {
			l.Close()
		}
	})

	var out, errOut bytes.Buffer
	if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, spawn); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.Contains(errOut.String(), "not answering") {
		t.Fatalf("gave up before the daemon could reclaim the stale socket: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "daemon started") {
		t.Fatalf("missing the daemon-started notice: %s / %s", out.String(), errOut.String())
	}
}

func TestRunRootReportsOnlyTheSourcesItCouldRead(t *testing.T) {
	home := sandbox(t)
	arch := filepath.Join(home, "archive")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	readable := filepath.Join(arch, "host-a")
	if err := os.WriteFile(readable, []byte("#1700000001\nansible-playbook only-in-the-archive.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(arch, "host-b")
	if err := os.WriteFile(locked, []byte("#1700000002\nterraform apply only-in-the-locked-archive\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	dir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[engine]\nmode = \"oneshot\"\n\n[sources]\nauto = true\nextra = [\"" + arch + "/*\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	for run := 1; run <= 3; run++ {
		var out, errOut bytes.Buffer
		if code := runRoot(&out, &errOut, strings.NewReader(""), false, false, noopSpawn); code != 0 {
			t.Fatalf("run %d: exit %d: %s", run, code, out.String())
		}
		if !strings.Contains(out.String(), " from 2 sources,") {
			t.Fatalf("run %d counts a source it could not read: %s", run, out.String())
		}
		if !strings.Contains(errOut.String(), locked) {
			t.Fatalf("run %d does not name the lost source: %q", run, errOut.String())
		}
		if !strings.Contains(errOut.String(), "could not be read") {
			t.Fatalf("run %d does not say what happened: %q", run, errOut.String())
		}
	}
}
