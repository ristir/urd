package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/engine"
)

func home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

type Shell string

const (
	Zsh  Shell = "zsh"
	Bash Shell = "bash"
)

// Detect names the shell that started this process: $ZSH_VERSION and $BASH_VERSION are
// shell variables and never reach a child, and $SHELL is the login shell, not the asking one.
func Detect() Shell {
	if s := shellOf(parentName()); s != "" {
		return s
	}
	if s := shellOf(filepath.Base(os.Getenv("SHELL"))); s != "" {
		return s
	}
	return Zsh
}

func shellOf(name string) Shell {
	switch {
	case strings.Contains(name, "zsh"):
		return Zsh
	case strings.Contains(name, "bash"):
		return Bash
	}
	return ""
}

// parentName reads the parent's command name: /proc where there is one, ps otherwise.
func parentName() string {
	pid := strconv.Itoa(os.Getppid())
	if data, err := os.ReadFile("/proc/" + pid + "/comm"); err == nil {
		return strings.TrimSpace(string(data))
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", pid).Output()
	if err != nil {
		return ""
	}
	// A login shell shows up as "-zsh", and ps prints the full path for others.
	return strings.TrimSpace(filepath.Base(strings.TrimPrefix(strings.TrimSpace(string(out)), "-")))
}

// ZDOTDIR moves zsh's rc file; bash has no equivalent.
func RCPath(sh Shell) string {
	if sh == Bash {
		return filepath.Join(home(), ".bashrc")
	}
	if z := os.Getenv("ZDOTDIR"); z != "" {
		return filepath.Join(z, ".zshrc")
	}
	return filepath.Join(home(), ".zshrc")
}

func HookLine(bin string, sh Shell) string {
	return fmt.Sprintf(`eval "$(%s hook %s)"`, bin, sh)
}

// Any mention rather than the exact line: the user may have edited it, and a duplicate is worse.
func HasHook(content string) bool {
	return strings.Contains(content, "urd hook")
}

func appendUnique(rcPath, content string, has func(string) bool) (bool, string, error) {
	data, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return false, "", err
	}
	if has(string(data)) {
		return false, "", nil
	}

	backup := ""
	if len(data) > 0 {
		backup = fmt.Sprintf("%s.urd-bak-%s", rcPath, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return false, "", err
		}
	}

	out := string(data)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += content + "\n"
	if err := os.WriteFile(rcPath, []byte(out), 0o644); err != nil {
		return false, backup, err
	}
	return true, backup, nil
}

// The end is mandatory: oh-my-zsh and plugins bind keys when sourced, our widget must come after.
func EnsureHook(rcPath, line string) (bool, string, error) {
	return appendUnique(rcPath, line, HasHook)
}

// zshoptions(1): a histfile is trimmed past $SAVEHIST by 20%, so 12000 at the oh-my-zsh default of
// 10000. bash truncates at HISTFILESIZE with no margin at all.
const HistTrimThreshold = 12000

func HasHistLimits(content string) bool {
	return strings.Contains(content, "HISTSIZE") || strings.Contains(content, "SAVEHIST")
}

func NeedsHistLimits(rcContent string, corpusEntries int) bool {
	return !HasHistLimits(rcContent) && corpusEntries > HistTrimThreshold
}

func HistLimit(corpusEntries int) int {
	if corpusEntries < 1 {
		corpusEntries = 1
	}
	const step = 10000
	doubled := corpusEntries * 2
	return (doubled + step - 1) / step * step
}

// HISTSIZE must not be under the file limit, so both get one number. bash spells that HISTFILESIZE;
// SAVEHIST in a .bashrc would sit there meaning nothing.
func HistLimitLines(n int, sh Shell) string {
	if sh == Bash {
		return fmt.Sprintf("HISTSIZE=%d\nHISTFILESIZE=%d", n, n)
	}
	return fmt.Sprintf("HISTSIZE=%d\nSAVEHIST=%d", n, n)
}

func HistLimitNames(sh Shell) string {
	if sh == Bash {
		return "HISTSIZE/HISTFILESIZE"
	}
	return "HISTSIZE/SAVEHIST"
}

func EnsureHistLimits(rcPath string, n int, sh Shell) (bool, string, error) {
	return appendUnique(rcPath, HistLimitLines(n, sh), HasHistLimits)
}

func HistLimitReassurance() string {
	return "this only affects the shell's own history depth (what arrow-up and the native Ctrl-R search through); urd's corpus lives in its own store and loses nothing to the trim"
}

// 144ms is a measured full rebuild of the full corpus (48790 records) - a cheap worst case.
func DaemonRestartNotice() string {
	return "daemon restarted with the current binary; the new process starts with a cold prefix cache and, worst case, a full reindex (measured 144ms on the full corpus)"
}

// There is deliberately no scan of the disk: walking cloud directories is unpredictable, and a
// .psql_history next door is not tellable by name.
func SyncDirHint() string {
	return fmt.Sprintf("put archived history files into %s - everything there is picked up automatically", config.ImportedDir())
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// A rebuild of a small corpus takes hundreds of microseconds, which rounds to a dishonest "0s".
func took(d time.Duration) string {
	if d >= time.Millisecond {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Microsecond).String()
}

// On the read path engine.Load leaves Multiline/Dropped/Filtered at zero by construction -
// printing them as a measurement would be a lie.
func Summary(info engine.Info) string {
	entries := count(info.Stats.Kept, "entry", "entries")
	from := count(info.Files, "source", "sources")
	if !info.Rebuilt {
		return fmt.Sprintf("%s from %s, index up to date, read in %s", entries, from, took(info.Elapsed))
	}
	return fmt.Sprintf("%s from %s, %d multiline joined, %d duplicates dropped, %d excluded by filters, indexed in %s",
		entries, from, info.Stats.Multiline, info.Stats.Dropped, info.Stats.Filtered, took(info.Elapsed))
}

// An expression that did not compile is silently not applied: without this line there is nowhere
// to learn the result differs from what was asked.
func Warnings(info engine.Info) []string {
	out := make([]string, 0, len(info.BadFilters)+len(info.BadSources))
	for _, p := range info.BadFilters {
		out = append(out, fmt.Sprintf("urd: filter pattern %q did not compile and was ignored, it filtered nothing", p))
	}
	// A source that went missing is the silent zero itself: without this line the summary looks healthy.
	for _, s := range info.BadSources {
		out = append(out, fmt.Sprintf("urd: source %s could not be read (%v), its commands are missing from the corpus", s.Path, s.Err))
	}
	return out
}
