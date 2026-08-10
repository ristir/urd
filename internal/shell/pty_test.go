package shell

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ristir/urd/internal/config"
)

const prompt = "RDY>"

type session struct {
	t          *testing.T
	f          *os.File
	bin        string
	home       string
	cmd        *exec.Cmd
	cols, rows int
	mu         sync.Mutex
	buf        strings.Builder
}

// A minimal Ubuntu carries only C.utf8, and forcing en_US.UTF-8 there leaves zsh in the
// C locale with MULTIBYTE off. Empty means none, and a test needing one skips.
func utf8Locale() string {
	out, err := exec.Command("locale", "-a").Output()
	if err != nil {
		return ""
	}
	have := map[string]string{}
	for _, l := range strings.Fields(string(out)) {
		have[strings.ToLower(l)] = l
	}
	for _, want := range []string{"en_us.utf-8", "en_us.utf8", "c.utf-8", "c.utf8"} {
		if name, ok := have[want]; ok {
			return name
		}
	}
	return ""
}

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "urd")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/urd")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

func killOrphanDaemons(t *testing.T, bin string) {
	t.Helper()
	pattern := regexp.QuoteMeta(bin) + " serve"
	var pids []string
	for i := 0; i < 20; i++ {
		out, _ := exec.Command("pgrep", "-f", pattern).Output()
		pids = strings.Fields(string(out))
		if len(pids) == 0 {
			return
		}
		for _, s := range pids {
			if pid, err := strconv.Atoi(s); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					p.Kill()
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("orphaned daemon(s) survived cleanup: pids %v, bin %s", pids, bin)
}

func newSession(t *testing.T, hist string) *session {
	return newSessionSized(t, hist, 200, 40)
}

func newSessionSized(t *testing.T, hist string, cols, rows int) *session {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}
	home := prepareHome(t, hist)
	return startSession(t, home, home, cols, rows)
}

func newSessionWithDeadSocket(t *testing.T, hist string) (*session, *int64) {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}
	home := prepareHome(t, hist)
	// sun_path is limited to 104 bytes on macOS, and a t.TempDir() path with a test name does not fit.
	runtimeDir, err := os.MkdirTemp("", "urd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	accepts := startDeadSocket(t, config.SocketPath())
	return startSession(t, home, runtimeDir, 200, 40), accepts
}

func newSessionWithLiarSocket(t *testing.T, hist, answer string) *session {
	return newSessionWithHeadedSocket(t, hist, "urd1 1 0", answer)
}

func newSessionWithHeadedSocket(t *testing.T, hist, head, answer string) *session {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}
	home := prepareHome(t, hist)
	runtimeDir, err := os.MkdirTemp("", "urd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	startLiarSocket(t, config.SocketPath(), head, answer)
	return startSession(t, home, runtimeDir, 200, 40)
}

func startLiarSocket(t *testing.T, path, head, answer string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("liar socket listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					if _, err := fmt.Fprintf(c, "%s\n%s\n0:4\n", head, answer); err != nil {
						return
					}
				}
			}(c)
		}
	}()
}

func startDeadSocket(t *testing.T, path string) *int64 {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("dead socket listen: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var accepted int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&accepted, 1)
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return &accepted
}

func prepareHome(t *testing.T, hist string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(hist), 0o600); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(home, ".config", "urd")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[engine]\nidle_timeout = \"2s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func startSession(t *testing.T, home, runtimeDir string, cols, rows int) *session {
	t.Helper()
	bin := buildBinary(t)
	cmd := exec.Command("zsh", "-f", "-i")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"HISTFILE=",
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_RUNTIME_DIR="+runtimeDir,
		"PATH="+filepath.Dir(bin)+":"+os.Getenv("PATH"),
		"TERM=xterm-256color",
		// Set explicitly: without it zsh falls back to C with MULTIBYTE off, which is not how anyone runs.
		"LANG="+utf8Locale(),
		"LC_ALL="+utf8Locale(),
		"PS1="+prompt+" ",
		"COLUMNS="+strconv.Itoa(cols),
		"LINES="+strconv.Itoa(rows),
	)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, f: f, bin: bin, home: home, cmd: cmd, cols: cols, rows: rows}
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := f.Read(chunk)
			if n > 0 {
				s.mu.Lock()
				s.buf.Write(chunk[:n])
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		f.Close()
		cmd.Process.Kill()
		cmd.Wait()
		killOrphanDaemons(t, bin)
	})

	s.expect(prompt)
	s.send("bindkey -e; unsetopt zle_bracketed_paste 2>/dev/null; PROMPT='" + prompt + " '\n")
	s.send("eval \"$(" + bin + " hook zsh)\"\n")
	s.expect(prompt)
	s.send(bin + " query 0 echo >/dev/null\n")
	s.waitPrompts(4)
	return s
}

func (s *session) send(str string) {
	s.t.Helper()
	if _, err := s.f.WriteString(str); err != nil {
		s.t.Fatalf("write %q: %v", str, err)
	}
	time.Sleep(60 * time.Millisecond)
}

func (s *session) raw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *session) snapshot() *term {
	t := newTerm(s.cols, s.rows)
	t.write([]byte(s.raw()))
	return t
}

func (s *session) text() string { return s.snapshot().text() }

func (s *session) line() string {
	t := s.snapshot()
	rows := t.rows()
	last := -1
	for i, r := range rows {
		if strings.HasPrefix(r, prompt) {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	var b strings.Builder
	for y := last; y < t.h; y++ {
		x := 0
		if y == last {
			x = len(prompt) + 1
		}
		for ; x < t.w; x++ {
			b.WriteRune(t.grid[y][x].ch)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func (s *session) prompts() int {
	n := 0
	for _, r := range s.snapshot().rows() {
		if strings.HasPrefix(r, prompt) {
			n++
		}
	}
	return n
}

func (s *session) wait(what string, ok func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	s.t.Fatalf("timeout waiting for %s; screen:\n%s", what, s.text())
}

func (s *session) expect(sub string) {
	s.t.Helper()
	s.wait(strconv.Quote(sub)+" on screen", func() bool { return strings.Contains(s.text(), sub) })
}

func (s *session) refute(sub string) {
	s.t.Helper()
	if strings.Contains(s.text(), sub) {
		s.t.Fatalf("did not expect %q; screen:\n%s", sub, s.text())
	}
}

func (s *session) expectLine(want string) {
	s.t.Helper()
	s.wait("input line "+strconv.Quote(want), func() bool { return s.line() == want })
}

func (s *session) expectLineMatch(pattern string) {
	s.t.Helper()
	re := regexp.MustCompile(pattern)
	s.wait("input line matching "+strconv.Quote(pattern), func() bool { return re.MatchString(s.line()) })
}

func (s *session) refuteLine(sub string) {
	s.t.Helper()
	if strings.Contains(s.line(), sub) {
		s.t.Fatalf("did not expect %q in the input line %q", sub, s.line())
	}
}

func (s *session) expectRow(want string) {
	s.t.Helper()
	s.wait("row "+strconv.Quote(want), func() bool {
		for _, r := range s.snapshot().rows() {
			if r == want {
				return true
			}
		}
		return false
	})
}

func (s *session) refuteRow(want string) {
	s.t.Helper()
	for _, r := range s.snapshot().rows() {
		if r == want {
			s.t.Fatalf("did not expect row %q; screen:\n%s", want, s.text())
		}
	}
}

func (s *session) waitPrompts(n int) {
	s.t.Helper()
	s.wait(strconv.Itoa(n)+" prompts", func() bool { return s.prompts() >= n })
}

func (s *session) expectStyle(sub, fg string, bold bool) {
	s.t.Helper()
	gotFg, gotBold, found := s.snapshot().styleOf(sub)
	if !found {
		s.t.Fatalf("%q not on screen:\n%s", sub, s.text())
	}
	if gotFg != fg || gotBold != bold {
		s.t.Errorf("style of %q: fg=%q bold=%v, want fg=%q bold=%v", sub, gotFg, gotBold, fg, bold)
	}
}

func (s *session) expectUnderlinePattern(sub, mask string) {
	s.t.Helper()
	if len(mask) != len([]rune(sub)) {
		s.t.Fatalf("mask %q is %d long, substring %q is %d", mask, len(mask), sub, len([]rune(sub)))
	}
	cells, _, _, found := s.snapshot().cellsOf(sub)
	if !found {
		s.t.Fatalf("%q not on screen:\n%s", sub, s.text())
	}
	for i, c := range cells {
		want := mask[i] == '_'
		if c.underline != want {
			s.t.Errorf("underline of %q at %d (%q) = %v, want %v", sub, i, string(c.ch), c.underline, want)
			return
		}
	}
}

// region_highlight in zsh 5.9 knows only fg, bg, bold, standout and underline; only the real cursor blinks.
func (s *session) expectCursorAfter(sub string) {
	s.t.Helper()
	t := s.snapshot()
	_, row, col, found := t.cellsOf(sub)
	if !found {
		s.t.Fatalf("%q not on screen:\n%s", sub, s.text())
	}
	if t.cy != row || t.cx != col {
		s.t.Errorf("cursor at row %d col %d, want row %d col %d (right after %q)\n%s", t.cy, t.cx, row, col, sub, s.text())
	}
}

func TestGlueSurvivesDoubleLoad(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}
	bin := buildBinary(t)
	glue := filepath.Join(t.TempDir(), "glue.zsh")
	scripts := map[string]string{
		"eval":   `eval "$(BIN hook zsh)"; eval "$(BIN hook zsh)"; print ALIVE`,
		"source": `BIN hook zsh > GLUE; source GLUE; source GLUE; print ALIVE`,
	}
	for name, script := range scripts {
		script = strings.NewReplacer("BIN", bin, "GLUE", glue).Replace(script)
		out, err := exec.Command("zsh", "-f", "-c", script).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ALIVE") {
			t.Errorf("%s: err=%v out=%q", name, err, out)
		}
	}
}

func TestWidgetReevalAppliesEditedColors(t *testing.T) {
	s := newSession(t, fixture)
	cfg := "[engine]\nidle_timeout = \"2s\"\n\n[colors]\nmark = \"fg=red\"\n"
	if err := os.WriteFile(filepath.Join(s.home, ".config", "urd", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s.send("eval \"$(" + s.bin + " hook zsh)\"\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectStyle("echo", "31", false)
}

func TestWidgetPicksUpAnEditedConfigWithoutANewShell(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectStyle("echo", "37", true)
	s.send("\x07")

	path := filepath.Join(s.home, ".config", "urd", "config.toml")
	cfg := "[engine]\nidle_timeout = \"2s\"\n\n[colors]\nmark = \"fg=red\"\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectStyle("echo", "31", false)
}

func TestWidgetSurvivesAnUnacceptableConfigEdit(t *testing.T) {
	s := newSession(t, fixture)
	path := filepath.Join(s.home, ".config", "urd", "config.toml")
	// A colour spec with a quote in it: refused by validColor, which is what stops a config running a command.
	if err := os.WriteFile(path, []byte("[colors]\nmark = \"fg=red'; echo PWNED; '\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.refute("PWNED")
	s.expectStyle("echo", "37", true)
}

const fixture = ": 100:0;echo alpha-oldest rate-limit\n" +
	": 200:0;echo beta-middle rate-limit\n" +
	": 300:0;echo gamma-newest ratelimit\n"

func TestWidgetRewritesBufferAndHighlights(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectStyle("echo", "37", true)
	s.expectStyle("[ech - 1/3]", "90", false)
	s.expectStyle("gamma-newest", "", false)
}

func TestWidgetEmptyMarkColorSkipsHighlightEntirely(t *testing.T) {
	s := newSession(t, fixture)
	s.send("URD_COLOR_MARK=\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectStyle("echo", "", false)
	s.expectStyle("gamma-newest", "", false)
}

func TestWidgetSearchResultIsNotColoured(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.expectStyle("ratelimit", "37", true)
	s.expectStyle("gamma-newest", "", false)
}

func TestWidgetQueryInBracketsIsUnderlined(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectUnderlinePattern("[ech - 1/3]", ".___.......")
	s.expectUnderlinePattern("echo gamma-newest", ".................")
}

func TestWidgetCursorSitsRightAfterTheQuery(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectCursorAfter("[ech")
	s.send("o")
	s.expectLine("urd echo gamma-newest ratelimit [echo - 1/3]")
	s.expectCursorAfter("[echo")
}

func TestWidgetCursorAfterTriggerOnEmptyQuery(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.expectLine("urd")
	s.expectCursorAfter("urd ")
}

func TestWidgetTabCompletesSubcommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp ex")
	s.expectLine("urd --dmp ex [export]")
	s.send("\t")
	s.expectLine("urd --dmp export [zsh|bash]")
}

func TestWidgetTabCompletionOpensTheNextLevel(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--c")
	s.send("\t")
	s.expectLine("urd --cfg [edit|fill]")
	s.send("e")
	s.send("\t")
	s.expectLine("urd --cfg edit")
}

func TestWidgetTabWithSeveralCandidatesKeepsTheQuery(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--")
	s.expectLine("urd -- [--cfg|--dmp|--setup|--bench|--help|--version]")
	s.send("\t")
	s.expectLine("urd -- [--cfg|--dmp|--setup|--bench|--help|--version]")
}

func TestWidgetTabCompletesShortAlias(t *testing.T) {
	for _, typed := range []string{"-c", "-cf", "-cfg"} {
		s := newSession(t, fixture)
		s.send("urd ")
		s.send(typed)
		s.send("\t")
		s.expectLine("urd --cfg [edit|fill]")
	}
}

func TestWidgetTabOnASingleDashAddsTheSecond(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("-")
	s.send("\t")
	s.expectLine("urd -- [--cfg|--dmp|--setup|--bench|--help|--version]")
}

func TestWidgetShortAliasIsRecognisedWithoutTab(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("-d")
	s.expectLine("urd -d [load|export]")
	s.expectStyle("-d", "32", false)
}

func TestWidgetPartialAliasHintsItsCommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("-se")
	s.expectLine("urd -se [--setup]")
}

func TestWidgetTabInSearchModeChangesNothing(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.send("\t")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
}

func TestWidgetBuiltinCommandIsGreen(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--cfg")
	s.expectLine("urd --cfg [edit|fill]")
	s.expectStyle("--cfg", "32", false)
}

func TestWidgetKeepsTriggerVisibleAsPrompt(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.expectLine("urd")
	s.expectStyle("urd", "36", true)
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
}

func TestWidgetEmptyQueryShowsPrefixOnly(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.expectLine("urd")
	s.expectStyle("urd", "36", true)
	s.refuteLine("[")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\x15")
	s.expectLine("urd")
	s.refuteLine("[")
	s.refuteLine(":")
}

func TestWidgetTriggerColourIsTheOnlyModeSignal(t *testing.T) {
	s := newSession(t, fixture)

	s.send("urd ")
	s.expectLine("urd")
	s.expectStyle("urd", "36", true)
	s.refuteLine("urd:")

	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.expectStyle("urd", "36", true)
	s.refuteLine("urd:")

	s.send("\x15")
	s.send("--cfg")
	s.expectLine("urd --cfg [edit|fill]")
	s.expectStyle("urd", "36", true)
	s.expectStyle("--cfg", "32", false)
	s.refuteLine("urd:")
}

func TestWidgetAcceptedCommandHasNoPrefix(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	before := s.prompts()
	s.send("\r")
	s.expectLine("echo gamma-newest ratelimit")
	s.refuteLine("urd ")
	s.send("\r")
	s.expectRow("gamma-newest ratelimit")
	s.waitPrompts(before + 1)
}

func TestWidgetTriggerPrefixIsHighlighted(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.expectStyle("urd", "36", true)
}

func TestWidgetBuiltinRunsOnFirstEnter(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--cfg")
	s.expectLine("urd --cfg [edit|fill]")
	before := s.prompts()
	s.send("\r")
	s.expect("[engine]")
	s.waitPrompts(before + 1)
}

func TestWidgetSearchStillNeedsTwoEnters(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\r")
	time.Sleep(300 * time.Millisecond)
	s.refuteRow("gamma-newest ratelimit")
	s.send("\r")
	s.expectRow("gamma-newest ratelimit")
}

func TestWidgetEnterAcceptsWithoutRunning(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	before := s.prompts()
	s.send("\r")
	s.expectLine("echo gamma-newest ratelimit")
	time.Sleep(300 * time.Millisecond)
	s.refuteRow("gamma-newest ratelimit")
	if s.prompts() != before {
		t.Fatalf("first Enter submitted the line: prompts %d -> %d", before, s.prompts())
	}
	s.send("\r")
	s.expectRow("gamma-newest ratelimit")
	s.waitPrompts(before + 1)
}

func TestWidgetArrowStepsIntoPast(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("rate-li")
	s.expectLine("urd echo beta-middle rate-limit [rate-li - 1/2]")
	s.send("\x1b[A")
	s.expectLine("urd echo alpha-oldest rate-limit [rate-li - 2/2]")
	s.send("\x1b[A")
	s.expectLine("urd echo alpha-oldest rate-limit [rate-li - 2/2]")
	s.send("\x1b[B")
	s.expectLine("urd echo beta-middle rate-limit [rate-li - 1/2]")
}

func TestWidgetZeroMatchKeepsBufferAndSaysSo(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("zzz")
	s.expectLine("urd echo gamma-newest ratelimit [ratelzzz - no match]")
}

func TestWidgetBackspaceRestoresPreviousCandidate(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratelx")
	s.expectLine("urd echo gamma-newest ratelimit [ratelx - no match]")
	s.send("\x7f")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
}

func TestWidgetBackspaceOnEmptyQueryLeavesTrigger(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("r")
	s.expect("[r - 1/3]")
	s.send("\x7f")
	s.expectLine("urd")
	s.send(" ")
	s.send("r")
	s.expect("[r - 1/3]")
}

func TestWidgetEscapeRestoresEmptyBuffer(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\x1b")
	s.expectLine("")
	before := s.prompts()
	s.send("\r")
	s.waitPrompts(before + 1)
	s.refuteRow("gamma-newest ratelimit")
}

func TestWidgetSpaceOutsideModeStillInsertsSpace(t *testing.T) {
	s := newSession(t, fixture)
	s.send("echo one two\r")
	s.expectRow("one two")
}

func TestWidgetCtrlUClearsQueryAndStaysInMode(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\x15")
	s.expectLine("urd")
	s.send("beta")
	s.expectLine("urd echo beta-middle rate-limit [beta - 1/1]")
}

func TestWidgetUnboundKeyLeavesModeAndReplays(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\x14")
	s.expectLine("")
	s.send("urd ")
	s.send("beta")
	s.expectLine("urd echo beta-middle rate-limit [beta - 1/1]")
}

func TestWidgetSubcommandInFirstPositionBecomesCommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--cfg")
	s.expectLine("urd --cfg [edit|fill]")
	s.expectStyle("--cfg", "32", false)
	before := s.prompts()
	s.send("\r")
	s.expect("[engine]")
	s.waitPrompts(before + 1)
}

func TestWidgetBareCommandNameWithoutDashStaysSearch(t *testing.T) {
	extra := ": 500:0;echo iptables-save gamma-rules\n"
	s := newSession(t, fixture+extra)
	s.send("urd ")
	s.send("cfg")
	s.expect("[cfg - no match]")
	s.refuteLine("[edit|fill]")
	s.send("\x07")
	s.send("urd ")
	s.send("echo save")
	s.expectLine("urd echo iptables-save gamma-rules [echo save - 1/1]")
	s.expectStyle("gamma-rules", "", false)
}

func TestWidgetQueryCaseInsensitive(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("RATEL")
	s.expectLine("urd echo gamma-newest ratelimit [RATEL - 1/1]")
}

func TestWidgetCyrillicQueryHighlightsByRunes(t *testing.T) {
	// Without a UTF-8 locale zsh has MULTIBYTE off and this would measure the C locale.
	if utf8Locale() == "" {
		t.Skip("no UTF-8 locale on this machine")
	}
	s := newSession(t, fixture+": 600:0;echo привет-мир tail\n")
	s.send("urd ")
	s.send("привет")
	s.expectLine("urd echo привет-мир tail [привет - 1/1]")
	s.expectStyle("привет-мир", "37", true)
	s.expectStyle("tail", "", false)
}

func TestWidgetSpawnsDaemonAtMostOncePerEntry(t *testing.T) {
	s := newSession(t, fixture)
	dir := t.TempDir()
	log := filepath.Join(dir, "spawns")
	wrapper := filepath.Join(dir, "urd-spy")
	spy := "#!/bin/sh\nif [ \"$1\" = serve ]; then echo spawn >> " + log + "; exit 1; fi\nexec " + s.bin + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(spy), 0o700); err != nil {
		t.Fatal(err)
	}
	s.send("URD_BIN=" + wrapper + "\n")
	s.waitPrompts(5)

	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	time.Sleep(400 * time.Millisecond)
	if n := countLines(t, log); n != 1 {
		t.Fatalf("five keystrokes spawned the daemon %d times, want exactly 1", n)
	}

	s.send("\x07")
	s.send("urd ")
	s.send("beta")
	s.expectLine("urd echo beta-middle rate-limit [beta - 1/1]")
	time.Sleep(400 * time.Millisecond)
	if n := countLines(t, log); n != 2 {
		t.Fatalf("second mode entry spawned %d times in total, want 2", n)
	}
}

func TestWidgetOneshotModeNeverTouchesSocketOrSpawnsDaemon(t *testing.T) {
	s := newSessionWithLiarSocket(t, fixture, "echo IMPOSTOR")

	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo IMPOSTOR [ratel - 1/1]")
	s.send("\x07")

	dir := t.TempDir()
	log := filepath.Join(dir, "spawns")
	wrapper := filepath.Join(dir, "urd-spy")
	spy := "#!/bin/sh\nif [ \"$1\" = serve ]; then echo spawn >> " + log + "; exit 1; fi\nexec " + s.bin + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(spy), 0o700); err != nil {
		t.Fatal(err)
	}
	s.send("URD_MODE=oneshot; URD_BIN=" + wrapper + "; URD_SOCK=" + filepath.Join(dir, "nope.sock") + "\n")
	s.waitPrompts(5)

	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.refuteLine("IMPOSTOR")
	time.Sleep(400 * time.Millisecond)
	if n := countLines(t, log); n != 0 {
		t.Fatalf("oneshot mode spawned the daemon %d times, want 0", n)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func TestWidgetEngineFailureDegradesGracefully(t *testing.T) {
	s := newSession(t, fixture)
	s.send("URD_BIN=/nonexistent/urd\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("rate")
	s.expectLine("urd [rate - engine unavailable]")
	s.send("\x07")
	s.expectLine("")
	s.send("echo alive\r")
	s.expectRow("alive")
}

func TestWidgetHotkeyTakesBufferAsQuery(t *testing.T) {
	s := newSession(t, fixture)
	s.send("bindkey '^Xu' _urd_hotkey\n")
	s.waitPrompts(5)
	s.send("ratel")
	s.expectLine("ratel")
	s.send("\x18u")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	s.send("\x07")
	s.expectLine("ratel")
}

func TestWidgetSigintLeavesShellUsable(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	before := s.prompts()
	s.send("\x03")
	s.waitPrompts(before + 1)
	s.expectLine("")
	s.send("echo alive\r")
	s.expectRow("alive")
}

func TestWidgetIndicatorBelowMovesCounterOffTheLine(t *testing.T) {
	s := newSession(t, fixture)
	s.send("URD_INDICATOR=below\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("ratel")
	s.expectRow(prompt + " urd echo gamma-newest ratelimit")
	s.expect("[ratel - 1/1]")
}

func TestWidgetIndicatorBelowClearsCounterOnEmptyQuery(t *testing.T) {
	s := newSession(t, fixture)
	s.send("URD_INDICATOR=below\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("ratel")
	s.expect("[ratel - 1/1]")
	s.send("\x15")
	s.expectLine("urd")
	s.refute("[ratel - 1/1]")
}

func TestWidgetIndicatorOffShowsNoCounter(t *testing.T) {
	s := newSession(t, fixture)
	s.send("URD_INDICATOR=off\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("ratel")
	s.expectRow(prompt + " urd echo gamma-newest ratelimit")
	s.refute("1/1")
}

func TestWidgetLongCommandInNarrowWindow(t *testing.T) {
	long := ": 400:0;echo delta-long " + strings.Repeat("padpadpad ", 12) + "tail-marker\n"
	s := newSessionSized(t, fixture+long, 40, 40)
	s.send("urd ")
	s.send("delta")
	s.expectLine("urd echo delta-long " + strings.Repeat("padpadpad ", 12) + "tail-marker [delta - 1/1]")
	before := s.prompts()
	s.send("\r")
	s.send("\r")
	s.waitPrompts(before + 1)
	s.expect("tail-marker")
}

func TestWidgetStopsWaitingOnSocketAfterOneTimeout(t *testing.T) {
	s, accepts := newSessionWithDeadSocket(t, fixture)
	start := time.Now()
	s.send("urd ")
	s.send("ratel")
	s.expectLine("urd echo gamma-newest ratelimit [ratel - 1/1]")
	elapsed := time.Since(start)
	if got := atomic.LoadInt64(accepts); got != 1 {
		t.Fatalf("socket accepted %d connections for 5 keystrokes, want exactly 1 (degraded flag should stop retrying the socket after the first timeout)", got)
	}
	// Five keystrokes at 60 ms would be 300 ms of socket waits: a crude fuse, not an exact bound.
	if elapsed > 2*time.Second {
		t.Fatalf("mode took %v, socket wait was not abandoned after the first timeout", elapsed)
	}
}

func TestWidgetFlagInFirstPositionBecomesCommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--help")
	s.expectLine("urd --help")
	before := s.prompts()
	s.send("\r")
	s.expect("Usage:")
	s.waitPrompts(before + 1)
}

func TestWidgetDashLaterInQueryStaysSearch(t *testing.T) {
	extra := ": 500:0;echo alpha -e beta\n"
	s := newSession(t, fixture+extra)
	s.send("urd ")
	s.send("alpha -e")
	s.expectLine("urd echo alpha -e beta [alpha -e - 1/1]")
	s.expectStyle("beta", "", false)
}

func TestWidgetBareDashAloneBecomesCommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("-")
	s.expectLine("urd - [--cfg|--dmp|--setup|--bench|--help|--version]")
	s.expectStyle("urd", "36", true)
	s.refuteLine(":")
}

func TestWidgetTimeoutIsSixHundredthsOfASecond(t *testing.T) {
	glue, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(glue, "URD_TIMEOUT=6") {
		t.Fatalf("timeout is not 60ms in the generated glue")
	}
}

func TestWidgetProtocolMismatchLeavesTheMode(t *testing.T) {
	s := newSessionWithHeadedSocket(t, fixture, "urd9 1 0", "echo FROM-THE-FUTURE")

	s.send("urd ")
	s.send("b")
	s.expect("urd: protocol mismatch, restart your shell")
	s.refute("FROM-THE-FUTURE")

	s.send("echo mode$((3+4))gone\n")
	s.expect("mode7gone")
}

const dmpLine = `^urd --dmp ~/urd_history_\d{8}-\d{4} \[load\|export\]$`

func writeDumpFile(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(home, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWidgetEmptyQueryHasNeitherColonNorHint(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.expectLine("urd")
	s.expectStyle("urd", "36", true)
	s.refuteLine(":")
	s.refuteLine("[")
}

func TestWidgetFirstDashHintsEveryCommand(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("-")
	s.expectLine("urd - [--cfg|--dmp|--setup|--bench|--help|--version]")
	s.expectStyle("[--cfg|--dmp|--setup|--bench|--help|--version]", "90", false)
}

func TestWidgetDoubleDashHintsTheSameList(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--")
	s.expectLine("urd -- [--cfg|--dmp|--setup|--bench|--help|--version]")
}

func TestWidgetPartialCommandNarrowsTheHint(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--d")
	s.expectLine("urd --d [--dmp]")
}

func TestWidgetCompleteCommandWithoutChildrenHasNoHint(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--help")
	s.expectLine("urd --help")
	s.refuteLine("[")
	s.expectStyle("--help", "32", false)
}

func TestWidgetCfgHintsEdit(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--cfg")
	s.expectLine("urd --cfg [edit|fill]")
	s.expectStyle("--cfg", "32", false)
	s.expectStyle("[edit|fill]", "90", false)
}

func TestWidgetDmpSuggestsANewDumpName(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp")
	s.expectLine("urd --dmp [load|export]")
	s.refuteLine("urd_history_")
	s.expectStyle("--dmp", "32", false)
	s.send(" ")
	s.expectLineMatch(dmpLine)
	s.expectStyle("~/urd_history_", "", false)
	s.expectStyle("[load|export]", "90", false)
}

func TestWidgetDmpExportHintsTheShells(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp export")
	s.expectLine("urd --dmp export [zsh|bash]")
	s.expectStyle("--dmp export", "32", false)
	s.expectStyle("[zsh|bash]", "90", false)
}

func TestWidgetDmpLoadSuggestsTheNewestDump(t *testing.T) {
	s := newSession(t, fixture)
	writeDumpFile(t, s.home, "urd_history_20260807-2210")
	s.send("urd ")
	s.send("--dmp load")
	s.expectLine("urd --dmp load")
	s.refuteLine("urd_history_20260807-2210")
	s.send(" ")
	s.expectLine("urd --dmp load ~/urd_history_20260807-2210")
	s.refuteLine("[")
	s.expectStyle("--dmp load", "32", false)
	s.expectStyle("~/urd_history_20260807-2210", "", false)
}

func TestWidgetDmpLoadPicksTheNewestByMtime(t *testing.T) {
	s := newSession(t, fixture)
	older := writeDumpFile(t, s.home, "urd_history_20260807-2210")
	newer := writeDumpFile(t, s.home, "urd_history_20260101-0000")
	now := time.Now()
	if err := os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatal(err)
	}
	s.send("urd ")
	s.send("--dmp load ")
	s.expectLine("urd --dmp load ~/urd_history_20260101-0000")
}

func TestWidgetDmpLoadWithoutAnyDumpSuggestsNothing(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp load")
	s.expectLine("urd --dmp load")
	s.refuteLine("urd_history_")
}

func TestWidgetSearchRowKeepsPromptColouredTrigger(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("ech")
	s.expectLine("urd echo gamma-newest ratelimit [ech - 1/3]")
	s.expectStyle("urd", "36", true)
	s.expectStyle("gamma-newest", "", false)
}

func TestWidgetSuggestedArgumentYieldsToTypedOne(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp")
	s.expectLine("urd --dmp [load|export]")
	s.send(" ~/my")
	s.expectLine("urd --dmp ~/my")
	s.refuteLine("urd_history_")
}

func TestWidgetEnterRunsWhatIsShown(t *testing.T) {
	s := newSession(t, fixture)
	s.send("urd ")
	s.send("--dmp ")
	s.expectLineMatch(dmpLine)
	name := regexp.MustCompile(`urd_history_\d{8}-\d{4}`).FindString(s.line())
	if name == "" {
		t.Fatalf("no suggested dump name in the line %q", s.line())
	}
	before := s.prompts()
	s.send("\r")
	s.expect("saved ")
	s.waitPrompts(before + 1)
	path := filepath.Join(s.home, name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Enter did not create the dump shown in the line: %v", err)
	}
}

func TestWidgetSuggestionNeedsNoExternalCommand(t *testing.T) {
	s := newSession(t, fixture)
	writeDumpFile(t, s.home, "urd_history_20260807-2210")
	s.send("PATH=/nonexistent\n")
	s.waitPrompts(5)
	s.send("urd ")
	s.send("--dmp load ")
	s.expectLine("urd --dmp load ~/urd_history_20260807-2210")
	s.send("\x15")
	s.send("--dmp ")
	s.expectLineMatch(dmpLine)
}

func TestWidgetDmpExportZshHintsRedirectTarget(t *testing.T) {
	s := newSession(t, fixture)
	histPath := filepath.Join(s.home, ".zsh_history")
	before, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(histPath)
	if err != nil {
		t.Fatal(err)
	}

	s.send("urd ")
	s.send("--dmp export zsh")
	s.expectLine("urd --dmp export zsh [> ~/.zsh_history]")
	s.expectStyle("--dmp export zsh", "32", false)

	s.send(" ")
	s.expectLine("urd --dmp export zsh [> ~/.zsh_history]")
	s.send("\x7f")
	s.expectLine("urd --dmp export zsh [> ~/.zsh_history]")

	beforePrompts := s.prompts()
	s.send("\r")
	s.expect("alpha-oldest")
	s.waitPrompts(beforePrompts + 1)

	after, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("~/.zsh_history changed: the redirection hint leaked into the buffer")
	}
	afterInfo, err := os.Stat(histPath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("~/.zsh_history mtime changed: something wrote to it")
	}
}

func TestWidgetDmpExportBashHintsRedirectTarget(t *testing.T) {
	s := newSession(t, fixture)
	bashHist := filepath.Join(s.home, ".bash_history")
	if _, err := os.Stat(bashHist); !os.IsNotExist(err) {
		t.Fatalf("test fixture already has ~/.bash_history: %v", err)
	}

	s.send("urd ")
	s.send("--dmp export bash")
	s.expectLine("urd --dmp export bash [> ~/.bash_history]")
	s.expectStyle("--dmp export bash", "32", false)

	before := s.prompts()
	s.send("\r")
	s.expect("alpha-oldest")
	s.waitPrompts(before + 1)

	if _, err := os.Stat(bashHist); !os.IsNotExist(err) {
		t.Fatalf("~/.bash_history was created: the redirection hint leaked into the buffer (err=%v)", err)
	}
}
