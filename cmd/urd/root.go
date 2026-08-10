package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/engine"
	"github.com/ristir/urd/internal/setup"
)

// The resolved path, not the bare name: ~/.local/bin reaches $PATH through ~/.profile,
// which only login shells read, so a bare name in a .bashrc silently binds nothing.
var bootBin = selfPath()

var detectShell = setup.Detect

// A stale socket answers ECONNREFUSED at once; the budget is for a daemon that is alive but busy.
const daemonProbe = 200 * time.Millisecond

// Long enough for the kernel to close the socket descriptor before the next daemon.Listen.
const daemonStopGrace = 200 * time.Millisecond

// Measured: a clean start is 3-12 ms, a stale socket holds daemon.Listen for ~200 ms.
const daemonWait = 500 * time.Millisecond

func daemonAlive(timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", config.SocketPath(), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// A successful fork proves nothing: with too long a socket path the daemon dies on bind
// and cmd.Start() still returns nil.
func daemonReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if daemonAlive(20 * time.Millisecond) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Detached: the daemon has to outlive the tab that started it.
func startDaemon() error {
	cmd := exec.Command(selfPath(), "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// PID on the first line, binary path on the second: bare urd identifies the daemon by both.
func writePidFile(path string, pid int, exe string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"+exe+"\n"), 0o600)
}

// A missing file or garbage in it means "nothing to stop", as does a daemon from an older version.
func readPidFile(path string) (pid int, exe string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}
	lines := strings.SplitN(strings.TrimRight(string(data), "\n"), "\n", 2)
	n, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || n <= 0 {
		return 0, "", false
	}
	if len(lines) > 1 {
		exe = strings.TrimSpace(lines[1])
	}
	return n, exe, true
}

// Signal 0 neither kills nor makes a zombie, it only checks whether a PID is alive.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Strictly by PID, never by process name: pkill -f also matches environment variables.
func stopLiveDaemon(pidPath string, grace time.Duration) {
	pid, exe, ok := readPidFile(pidPath)
	if !ok {
		return
	}
	// A pid file naming a foreign binary is left alone rather than guessed at.
	if exe != selfPath() {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return
	}
	deadline := time.Now().Add(grace)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

func ask(out io.Writer, r *bufio.Reader, question string, defaultYes bool) bool {
	suffix := " [y/N] "
	if defaultYes {
		suffix = " [Y/n] "
	}
	fmt.Fprint(out, question+suffix)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return defaultYes
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}

// The daemon start comes in as a parameter: under "go test" os.Executable() is the test binary.
func runRoot(out, errOut io.Writer, in io.Reader, interactive, forceSetup bool, spawnDaemon func() error) int {
	cfg, cfgErr := loadConfigOrWarn()

	_, info, err := engine.Load(cfg, true)
	if err != nil {
		fmt.Fprintf(errOut, "urd: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, setup.Summary(info))
	for _, w := range setup.Warnings(info) {
		fmt.Fprintln(errOut, w)
	}

	// Always, not only when the binary changed: otherwise a live daemon serves old code until idle.
	if cfg.Engine.Mode == "daemon" {
		wasAlive := daemonAlive(daemonProbe)
		prevPid, _, _ := readPidFile(config.PidPath())
		if wasAlive {
			stopLiveDaemon(config.PidPath(), daemonStopGrace)
		}
		switch err := spawnDaemon(); {
		case err != nil:
			fmt.Fprintf(errOut, "urd: could not start the daemon: %v\n", err)
		case daemonReady(daemonWait):
			// "restarted" only when the pid file really changed: the old daemon may still be answering.
			newPid, _, _ := readPidFile(config.PidPath())
			if wasAlive && newPid != 0 && newPid != prevPid {
				fmt.Fprintln(out, setup.DaemonRestartNotice())
			} else {
				fmt.Fprintln(out, "daemon started")
			}
		default:
			fmt.Fprintf(errOut, "urd: daemon was started but is not answering on %s yet; searches fall back to oneshot until it answers\n", config.SocketPath())
		}
	}

	// A seam: under "go test" the parent is the go tool, so detection would fall through to $SHELL.
	sh := detectShell()
	rc := setup.RCPath(sh)
	line := setup.HookLine(bootBin, sh)
	if !interactive {
		data, _ := os.ReadFile(rc)
		if !setup.HasHook(string(data)) {
			fmt.Fprintf(out, "add this line to the end of %s:\n  %s\n", rc, line)
		}
		return 0
	}

	reader := bufio.NewReader(in)
	data, _ := os.ReadFile(rc)
	if forceSetup || !setup.HasHook(string(data)) {
		if ask(out, reader, fmt.Sprintf("Add %s to %s?", line, rc), true) {
			changed, backup, err := setup.EnsureHook(rc, line)
			switch {
			case err != nil:
				fmt.Fprintf(errOut, "urd: could not write %s: %v\n", rc, err)
			case changed && backup != "":
				fmt.Fprintf(out, "written; backup saved as %s\n", backup)
			case changed:
				fmt.Fprintf(out, "written to %s\n", rc)
			}
		}

		if setup.NeedsHistLimits(string(data), info.Stats.Kept) {
			n := setup.HistLimit(info.Stats.Kept)
			q := fmt.Sprintf("Raise %s in %s to %d? The shell's default trims history past %d lines and your corpus already crosses it.", setup.HistLimitNames(sh), rc, n, setup.HistTrimThreshold)
			if ask(out, reader, q, false) {
				changed, backup, err := setup.EnsureHistLimits(rc, n, sh)
				switch {
				case err != nil:
					fmt.Fprintf(errOut, "urd: could not write %s: %v\n", rc, err)
				case changed && backup != "":
					fmt.Fprintf(out, "written; backup saved as %s\n", backup)
					fmt.Fprintln(out, setup.HistLimitReassurance())
				case changed:
					fmt.Fprintf(out, "written to %s\n", rc)
					fmt.Fprintln(out, setup.HistLimitReassurance())
				}
			}
		}

		// ^Xr is already zsh's history-incremental-search-backward, so Ctrl-R takes nothing away.
		dirty := false
		if ask(out, reader, "Bind Ctrl-R to urd? The native search stays on Ctrl-X r either way.", false) && !cfg.UI.StealCtrlR {
			cfg.UI.StealCtrlR = true
			dirty = true
		}
		switch {
		case cfgErr != nil:
			// cfg here is pure defaults - config.Load returns those on a parse error - and writing them
			// would erase the filters and sources of a whole file.
			fmt.Fprintf(errOut, "urd: %s was not modified because it could not be parsed\n", config.Path())
		case !dirty:
			// Save rewrites through the encoder and loses comments, so "no" must not reach a write.
		default:
			backup, err := config.SaveWithBackup(cfg)
			switch {
			case err != nil:
				fmt.Fprintf(errOut, "urd: could not save config: %v\n", err)
			case backup != "":
				fmt.Fprintf(out, "config written; backup saved as %s\n", backup)
			default:
				fmt.Fprintf(out, "config written to %s\n", config.Path())
			}
		}

		if len(cfg.Sources.Extra) == 0 {
			fmt.Fprintln(out, setup.SyncDirHint())
		}

		fmt.Fprintf(out, "done. restart the tab or run: source %s\n", rc)
	}
	return 0
}
