// Command democast plays the README demo in a real pty and writes demo/urd.cast
// (asciicast v2). Turning that into a GIF is agg's job, from the Makefile.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Any faster and the line rewrite flickers past before a human can read it.
const keyDelay = 150 * time.Millisecond

const beatPause = 1200 * time.Millisecond

const (
	termCols = 100
	termRows = 18
)

// Invented: a recording that goes to a public repository must hold not one real host
// or command of the developer's.
const fixtureHistory = `: 1700000001:0;ansible-playbook site.yml -l node-a1.lab -e answer_role=cache
: 1700000002:0;kubectl get pods -n demo -l app=rate-limit
: 1700000003:0;docker ps --filter name=ratelimit
: 1700000004:0;ansible-playbook restart.yml -l ratelimit-01.lab
: 1700000005:0;kubectl logs deploy/answer-svc -n demo
: 1700000006:0;docker logs -f ratelimit-worker
: 1700000007:0;ansible-playbook deploy.yml -l cache-01.lab -e app=ratelimit
: 1700000008:0;kubectl rollout restart deploy/rate-limit -n demo
: 1700000009:0;ansible-playbook status.yml -l web-03.lab
: 1700000010:0;ansible-playbook deploy.yml -l cache-02.lab -e app=ratelimit:7c1a9f2
`

// oneshot never touches the socket and never forks "urd serve", so no daemon outlives
// the recording.
const fixtureConfig = "[engine]\nmode = \"oneshot\"\n"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "democast:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("zsh"); err != nil {
		return fmt.Errorf("zsh not found on PATH: %w", err)
	}

	work, err := os.MkdirTemp("", "urd-democast")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	bin := filepath.Join(work, "urd")
	build := exec.Command("go", "build", "-o", bin, "./cmd/urd")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build urd: %w\n%s", err, out)
	}

	home := filepath.Join(work, "home")
	if err := writeFixtureHome(home, bin); err != nil {
		return err
	}

	rec, err := newRecorder(home)
	if err != nil {
		return err
	}
	defer rec.close()

	checks := playScenario(rec)

	outDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	outPath := filepath.Join(outDir, "urd.cast")
	if err := rec.writeCast(outPath); err != nil {
		return err
	}
	fmt.Println("wrote", outPath)

	failed := 0
	for _, c := range checks {
		status := "found"
		if !c.found {
			status = "NOT FOUND"
			failed++
		}
		fmt.Printf("%-55s %-30q %s\n", c.label, c.want, status)
	}
	if failed > 0 {
		return fmt.Errorf("%d scene check(s) did not find what they were supposed to render", failed)
	}
	return nil
}

// Searched upwards, because this runs both from the repo root and from its own directory.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// The .zshrc warms the index before the first frame, so no rebuild shows in the recording.
func writeFixtureHome(home, bin string) error {
	dirs := []string{
		home,
		filepath.Join(home, ".config", "urd"),
		filepath.Join(home, ".local", "share"),
		filepath.Join(home, ".run"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(fixtureHistory), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "urd", "config.toml"), []byte(fixtureConfig), 0o600); err != nil {
		return err
	}
	zshrc := fmt.Sprintf(
		"bindkey -e\nunsetopt zle_bracketed_paste 2>/dev/null\nPROMPT=%s\neval \"$(%s hook zsh)\"\n%s query 0 echo >/dev/null 2>&1\n",
		shQuote("%B%F{green}$%f%b "), shQuote(bin), shQuote(bin),
	)
	return os.WriteFile(filepath.Join(home, ".zshrc"), []byte(zshrc), 0o600)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type sceneCheck struct {
	label string
	want  string
	found bool
}

// Checks read the rebuilt canvas, not the raw stream: ZLE tears a word apart with
// per-character SGR resets during a differential redraw, which once fooled a check here.
func playScenario(r *recorder) []sceneCheck {
	var checks []sceneCheck
	check := func(label, want string) {
		checks = append(checks, sceneCheck{label: label, want: want, found: strings.Contains(r.snapshot(), want)})
	}

	r.pause(beatPause) // scene 1: an empty prompt

	r.typeText("urd ") // scene 2: the trigger word lights up
	r.pause(beatPause)
	check("scene 2: trigger word on screen", "urd")

	r.typeText("ans") // scene 3: the line is rewritten as the query is typed
	r.typeText(" rate")
	r.pause(beatPause)
	check(`scene 3: query narrows to "ans rate", 3 matches`, "ans rate - 1/3")

	r.key("\x1b[A") // scene 4: arrow up twice - the candidate counter changes
	r.pause(beatPause)
	check("scene 4: first up arrow, 2/3", "ans rate - 2/3")
	r.key("\x1b[A")
	r.pause(beatPause)
	check("scene 4: second up arrow, 3/3", "ans rate - 3/3")

	r.key("\r") // scene 5: Enter accepts the found command into the line without running it
	r.pause(beatPause)
	check("scene 5: accepted candidate stays in the line, unexecuted", "restart.yml -l ratelimit-01.lab")

	r.key("\x03") // scene 6: Ctrl-C brings back an empty line
	r.pause(beatPause)

	r.typeText("urd --dmp") // scene 7: the hint [load|export]
	r.pause(beatPause)
	check("scene 7a: --dmp hint", "[load|export]")

	r.typeText(" ") // a space puts the new dump's name next to the hint
	r.pause(beatPause)
	check("scene 7b: suggested dump name next to the hint", "urd_history_")

	r.typeText("export") // the typed word displaces the suggested name, the list narrows to a leaf
	r.pause(beatPause)
	check("scene 7c: --dmp export hint", "[zsh|bash]")

	r.typeText(" zsh") // the export/zsh leaf - a static hint for the redirect target
	r.pause(beatPause)
	check("scene 7d: --dmp export zsh redirect hint", "> ~/.zsh_history")

	r.key("\x03") // scene 8: Ctrl-C, the end of the recording
	r.pause(beatPause)

	return checks
}

type capEvent struct {
	t    float64
	data string
}

type recorder struct {
	f     *os.File
	cmd   *exec.Cmd
	start time.Time

	mu     sync.Mutex
	events []capEvent
	canvas *term
}

func newRecorder(home string) (*recorder, error) {
	cmd := exec.Command("zsh", "-i")
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"ZDOTDIR="+home,
		"HISTFILE=",
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_RUNTIME_DIR="+filepath.Join(home, ".run"),
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"COLUMNS="+strconv.Itoa(termCols),
		"LINES="+strconv.Itoa(termRows),
	)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(termRows), Cols: uint16(termCols)})
	if err != nil {
		return nil, err
	}
	r := &recorder{f: f, cmd: cmd, start: time.Now(), canvas: newTerm(termCols, termRows)}
	go r.drain()
	return r, nil
}

// Draining starts with the process, not with the scenario, so the .cast holds the shell
// coming up too. Each chunk also goes into the canvas the scene checks read.
func (r *recorder) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := r.f.Read(buf)
		if n > 0 {
			t := time.Since(r.start).Seconds()
			data := string(buf[:n])
			r.mu.Lock()
			r.events = append(r.events, capEvent{t, data})
			r.canvas.write(buf[:n])
			r.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (r *recorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canvas.text()
}

func (r *recorder) close() {
	r.f.Close()
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	r.cmd.Wait()
}

func (r *recorder) typeText(s string) {
	for _, ch := range s {
		r.f.WriteString(string(ch))
		time.Sleep(keyDelay)
	}
}

func (r *recorder) key(s string) {
	r.f.WriteString(s)
	time.Sleep(keyDelay)
}

func (r *recorder) pause(d time.Duration) { time.Sleep(d) }

type castHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Env       map[string]string `json:"env"`
}

func (r *recorder) writeCast(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	header := castHeader{
		Version:   2,
		Width:     termCols,
		Height:    termRows,
		Timestamp: r.start.Unix(),
		Env:       map[string]string{"TERM": "xterm-256color", "SHELL": "/bin/zsh"},
	}
	headerLine, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(headerLine, '\n')); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		line, err := json.Marshal([]interface{}{e.t, "o", e.data})
		if err != nil {
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}
