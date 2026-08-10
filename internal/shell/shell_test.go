package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ristir/urd/internal/config"
)

func TestZshSubstitutesConfig(t *testing.T) {
	cfg := config.Default()
	cfg.UI.Trigger = "hish"
	cfg.UI.Indicator = "below"
	got, err := Zsh(cfg, "/usr/local/bin/urd")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"URD_TRIGGER='hish'",
		"URD_INDICATOR='below'",
		"URD_BIN='/usr/local/bin/urd'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in generated glue", want)
		}
	}
	if strings.Contains(got, "@@") {
		t.Error("unsubstituted placeholder left in glue")
	}
}

func TestZshCarriesEngineMode(t *testing.T) {
	cfg := config.Default()
	if got, err := Zsh(cfg, "urd"); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(got, "URD_MODE=daemon") {
		t.Error("default mode did not reach the glue")
	}

	cfg.Engine.Mode = "oneshot"
	got, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "URD_MODE=oneshot") {
		t.Error("oneshot mode did not reach the glue")
	}
	if !strings.Contains(got, "_urd_ask_socket() {\n  [[ $URD_MODE == daemon ]] || return 1\n") {
		t.Error("the socket path is not gated on the mode")
	}

	cfg.Engine.Mode = "one shot"
	if got, err := Zsh(cfg, "urd"); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(got, "URD_MODE=oneshot") {
		t.Errorf("unrecognised mode was not normalised: %q", modeLine(got))
	}
}

func modeLine(glue string) string {
	for _, l := range strings.Split(glue, "\n") {
		if strings.Contains(l, "URD_MODE=") {
			return l
		}
	}
	return ""
}

func TestZshBindsNoHotkeyByDefault(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "bindkey '^R' _urd_hotkey") {
		t.Error("Ctrl-R stolen without being asked")
	}
	if strings.Contains(got, "bindkey ''") {
		t.Error("empty hotkey produced an empty bindkey")
	}
}

func TestZshBindsCtrlRWhenStolen(t *testing.T) {
	cfg := config.Default()
	cfg.UI.StealCtrlR = true
	got, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "bindkey '^R' _urd_hotkey") {
		t.Error("steal_ctrl_r did not bind Ctrl-R")
	}
}

func TestZshBindsCustomHotkey(t *testing.T) {
	cfg := config.Default()
	cfg.UI.Hotkey = "^Xu"
	got, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "bindkey '^Xu' _urd_hotkey") {
		t.Error("custom hotkey not bound")
	}
}

func TestZshQuotesHostileConfigValues(t *testing.T) {
	cfg := config.Default()
	cfg.UI.Trigger = "u rd's"
	cfg.UI.Indicator = "su ffix'x"
	cfg.UI.Hotkey = "^X'; echo URD-PWNED; '"
	got, err := Zsh(cfg, "/usr/local/bin/urd")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "@@") {
		t.Fatal("unsubstituted placeholder left in glue")
	}
	for _, want := range []string{
		`URD_TRIGGER='u rd'\''s'`,
		`URD_INDICATOR='su ffix'\''x'`,
		`bindkey '^X'\''; echo URD-PWNED; '\''' _urd_hotkey`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in generated glue", want)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "glue.zsh")
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("zsh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("the glue does not parse: %v\n%s", err, out)
	}

	script := "bindkey() { : }\nzle() { : }\nzmodload() { : }\ntypeset() { builtin typeset \"$@\" }\nsource " + path + "\n"
	out, err := exec.Command("zsh", "-f", "-c", script).CombinedOutput()
	if strings.Contains(string(out), "URD-PWNED") {
		t.Fatalf("sourcing the glue executed a command from the config: %s", out)
	}
	if len(out) != 0 {
		t.Fatalf("sourcing the glue printed junk (err %v): %q", err, out)
	}
}

func TestBashRefusesAHostileHotkey(t *testing.T) {
	for _, bad := range []string{`\C-x'; echo URD-PWNED; '`, `\C-x"`, "\\C-x`id`", `\C-x$(id)`, "\\C-x\n"} {
		cfg := config.Default()
		cfg.UI.Hotkey = bad
		got, err := Bash(cfg, "urd")
		if err == nil {
			t.Errorf("hotkey %q accepted, glue:\n%s", bad, got)
			continue
		}
		if got != "" {
			t.Errorf("hotkey %q refused but glue still produced: %s", bad, got)
		}
		if !strings.Contains(err.Error(), "ui.hotkey") {
			t.Errorf("error does not name the offending setting: %v", err)
		}
	}
}

func TestBashAcceptsOrdinaryKeySequences(t *testing.T) {
	for _, ok := range []string{`\C-r`, `\C-x\C-u`, `\e[A`, "^Xu"} {
		cfg := config.Default()
		cfg.UI.Hotkey = ok
		got, err := Bash(cfg, "urd")
		if err != nil {
			t.Errorf("hotkey %q rejected: %v", ok, err)
			continue
		}
		if !strings.Contains(got, `bind -x '"`+ok+`": _urd_pick'`) {
			t.Errorf("hotkey %q not bound in the glue", ok)
		}
	}
}

func TestZshIsIdempotentGuarded(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "_urd_loaded") {
		t.Error("glue lacks a double-source guard")
	}
}

func sourceGlueTwice(t *testing.T, cfg config.Config, middle, tail string) string {
	t.Helper()
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh not available")
	}
	glue, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "glue.zsh")
	if err := os.WriteFile(path, []byte(glue), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "source " + path + "\n" + middle + "\nsource " + path + "\n" + tail + "\n"
	out, err := exec.Command("zsh", "-f", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing the glue twice failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestZshReloadReappliesConfigValues(t *testing.T) {
	names := make([]string, 0, len(Commands))
	for _, n := range Commands {
		names = append(names, n.Name)
	}
	out := sourceGlueTwice(t, config.Default(),
		`URD_COLOR_MARK=stale; URD_TRIGGER=stale; root=; _urd_tree[$root]=stale`,
		`root=; print -r -- "$URD_COLOR_MARK|$URD_TRIGGER|${_urd_tree[$root]}"`)
	want := "fg=white,bold|urd|" + strings.Join(names, " ")
	if !strings.Contains(out, want) {
		t.Errorf("the second source did not re-apply the config: got %q, want a line %q", out, want)
	}
}

func TestZshReloadDoesNotRebuildTheKeymap(t *testing.T) {
	out := sourceGlueTwice(t, config.Default(),
		`bindkey -M urd '^X' _urd_older`,
		`bindkey -M urd -L "^X"`)
	if !strings.Contains(out, "_urd_older") {
		t.Errorf("the second source rebuilt the urd keymap and dropped a user binding: %q", out)
	}
}

func TestZshReloadRebindsItsOwnKeys(t *testing.T) {
	out := sourceGlueTwice(t, config.Default(),
		`bindkey -M urd -r '^I'`,
		`bindkey -M urd -L "^I"`)
	if !strings.Contains(out, "_urd_complete") {
		t.Errorf("a re-eval did not restore the widget's own binding: %q", out)
	}
}

func TestZshReloadKeepsLiveWidgetState(t *testing.T) {
	out := sourceGlueTwice(t, config.Default(),
		`_urd_fd=7; _urd_query=live`,
		`print -r -- "$_urd_fd|$_urd_query"`)
	if !strings.Contains(out, "7|live") {
		t.Errorf("the second source reset live widget state: %q", out)
	}
}

func walkCommands(nodes []Command, path string, visit func(full string, n Command)) {
	for _, n := range nodes {
		full := n.Name
		if path != "" {
			full = path + " " + n.Name
		}
		visit(full, n)
		walkCommands(n.Children, full, visit)
	}
}

func TestZshCarriesTheCommandTree(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if len(Commands) == 0 {
		t.Fatal("command tree is empty")
	}

	names := make([]string, 0, len(Commands))
	for _, n := range Commands {
		names = append(names, n.Name)
	}
	if want := "'' '" + strings.Join(names, " ") + "'"; !strings.Contains(got, want) {
		t.Errorf("root of the tree is not in the glue as %q", want)
	}

	walkCommands(Commands, "", func(full string, n Command) {
		if len(n.Children) == 0 {
			return
		}
		kids := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			kids = append(kids, c.Name)
		}
		want := "'" + full + "' '" + strings.Join(kids, " ") + "'"
		if !strings.Contains(got, want) {
			t.Errorf("children of %q are not in the glue as %q", full, want)
		}
	})
}

func TestZshCarriesExportRedirectHints(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`'--dmp export zsh' '> ~/.zsh_history'`,
		`'--dmp export bash' '> ~/.bash_history'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in generated glue", want)
		}
	}
	if !strings.Contains(got, "_urd_hints=(") {
		t.Error("glue lacks the _urd_hints table")
	}
}

// Every kind needs a case branch in the glue, or the argument is silently not substituted.
func TestZshImplementsEverySuggestionKind(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	body := zshFunctionBody(t, got, "_urd_suggest_for")
	kinds := 0
	walkCommands(Commands, "", func(full string, n Command) {
		if n.Default == "" {
			return
		}
		kinds++
		if !strings.Contains(body, "\n    "+n.Default+")") {
			t.Errorf("suggestion kind %q (used by %q) has no case branch in _urd_suggest_for", n.Default, full)
		}
		if !strings.Contains(got, "'"+full+"' '"+n.Default+"'") {
			t.Errorf("default of %q did not reach _urd_defaults", full)
		}
	})
	if kinds == 0 {
		t.Fatal("no command in the tree suggests an argument")
	}
}

func TestZshComputesSuggestionsWithoutForking(t *testing.T) {
	got, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "zmodload zsh/datetime") {
		t.Error("glue does not load zsh/datetime, so strftime is unavailable")
	}
	for _, fn := range []string{"_urd_suggest_for", "_urd_command_view"} {
		body := zshFunctionBody(t, got, fn)
		for _, bad := range []string{"$(", "`", "date ", "ls "} {
			if strings.Contains(body, bad) {
				t.Errorf("%s spawns a process: found %q in\n%s", fn, bad, body)
			}
		}
	}
}

func TestBashGlueUsesBindX(t *testing.T) {
	got, err := Bash(config.Default(), "/usr/local/bin/urd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bind -x", "READLINE_LINE", "/usr/local/bin/urd"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in bash glue", want)
		}
	}
	if strings.Contains(got, "@@") {
		t.Error("unsubstituted placeholder left in bash glue")
	}
}

func TestBashGlueHasDoubleSourceGuard(t *testing.T) {
	got, err := Bash(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "_urd_loaded") {
		t.Error("bash glue lacks a double-source guard")
	}
}

func TestZshSubstitutesColors(t *testing.T) {
	cfg := config.Default()
	cfg.Colors.Mark = "fg=15,bold"
	glue, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fg=15,bold", "fg=green", "fg=cyan,bold", "fg=8", "underline"} {
		if !strings.Contains(glue, want) {
			t.Errorf("glue lacks %q", want)
		}
	}
}

func TestZshAcceptsAllColorsEmptyAtOnce(t *testing.T) {
	cfg := config.Default()
	cfg.Colors = config.Colors{}
	got, err := Zsh(cfg, "urd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"URD_COLOR_PROMPT=''", "URD_COLOR_MARK=''", "URD_COLOR_BUILTIN=''", "URD_COLOR_HINT=''",
		"URD_COLOR_QUERY=''",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in glue", want)
		}
	}
	if strings.Contains(got, "@@") {
		t.Error("unsubstituted placeholder left in glue")
	}
}

func zshFunctionBody(t *testing.T, glue, name string) string {
	t.Helper()
	start := strings.Index(glue, name+"() {")
	if start < 0 {
		t.Fatalf("function %s not found in glue", name)
	}
	rest := glue[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("function %s has no closing brace on its own line", name)
	}
	return rest[:end]
}

func TestZshSearchBranchPaintsOnlyMarks(t *testing.T) {
	glue, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	marks := zshFunctionBody(t, glue, "_urd_mark_result")
	if !strings.Contains(marks, "URD_COLOR_MARK") {
		t.Fatalf("_urd_mark_result lost the mark highlight entirely:\n%s", marks)
	}
	for _, other := range []string{"URD_COLOR_BUILTIN", "URD_COLOR_QUERY", "URD_COLOR_PROMPT", "URD_COLOR_HINT"} {
		if strings.Contains(marks, other) {
			t.Fatalf("_urd_mark_result paints the result with %s:\n%s", other, marks)
		}
	}
	body := zshFunctionBody(t, glue, "_urd_render")
	if !strings.Contains(body, `region_highlight+=("0 ${#BUFFER} $URD_COLOR_QUERY")`) {
		t.Fatalf("the query is no longer painted as a whole with colors.query:\n%s", body)
	}
	for _, wrong := range []string{
		`region_highlight+=("0 ${#BUFFER} $URD_COLOR_MARK")`,
		`region_highlight+=("P 0 ${#PREDISPLAY}`,
	} {
		if strings.Contains(body, wrong) {
			t.Fatalf("_urd_render paints the result wholesale: %s", wrong)
		}
	}
}

func TestZshLiteralBranchPaintsOnlyTheRecognisedWords(t *testing.T) {
	glue, err := Zsh(config.Default(), "urd")
	if err != nil {
		t.Fatal(err)
	}
	body := zshFunctionBody(t, glue, "_urd_render")
	start := strings.Index(body, "if (( _urd_literal ))")
	if start < 0 {
		t.Fatal("literal branch not found in _urd_render")
	}
	end := strings.Index(body[start:], "\n  elif")
	if end < 0 {
		t.Fatal("end of literal branch not found in _urd_render")
	}
	branch := body[start : start+end]
	if !strings.Contains(branch, `region_highlight+=("0 $_urd_matched_len $URD_COLOR_BUILTIN")`) {
		t.Fatalf("literal branch does not paint the recognised words with builtin colour:\n%s", branch)
	}
	if strings.Contains(branch, `region_highlight+=("0 ${#BUFFER}`) {
		t.Fatalf("literal branch still paints the whole buffer:\n%s", branch)
	}
}

func TestZshRejectsHostileColor(t *testing.T) {
	cfg := config.Default()
	cfg.Colors.Builtin = "fg=green'; echo INJECTED; :'"
	if _, err := Zsh(cfg, "urd"); err == nil {
		t.Fatal("hostile colour accepted")
	} else if !strings.Contains(err.Error(), "colors.builtin") {
		t.Fatalf("error does not name the setting: %v", err)
	}
}
