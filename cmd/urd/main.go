package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/ristir/urd/internal/shell"
)

func canonical(name string) string {
	if full, ok := shell.Aliases()[name]; ok {
		return full
	}
	return name
}

// Empty unless "make dist" stamps them: Go's own build info then carries the module
// version for "go install pkg@version" and the revision for a build in a git tree.
var (
	version = ""
	commit  = ""
	date    = ""
)

// "git describe --always" falls back to the bare hash, so the revision is not printed
// twice. Go embeds no build time of its own, deliberately, so an unstamped build has none.
func composeVersion(ver, rev, built string) string {
	if strings.Contains(ver, rev) {
		rev = ""
	}
	var extra []string
	if rev != "" {
		extra = append(extra, rev)
	}
	if built != "" {
		extra = append(extra, built)
	}
	switch {
	case ver == "" && len(extra) == 0:
		return "dev"
	case ver == "":
		return strings.Join(extra, ", ")
	case len(extra) == 0:
		return ver
	}
	return ver + " (" + strings.Join(extra, ", ") + ")"
}

func buildVersion() string {
	ver, rev := version, commit
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return composeVersion(ver, rev, date)
	}
	if ver == "" && info.Main.Version != "" {
		// "(devel)" is what Go writes for a build that is not a module release.
		if ver = info.Main.Version; ver == "(devel)" {
			ver = "dev"
		}
	}
	if rev == "" {
		var dirty bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) > 7 {
					rev = s.Value[:7]
				} else {
					rev = s.Value
				}
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" && dirty {
			rev += "-dirty"
		}
	}
	return composeVersion(ver, rev, date)
}

var commands = map[string]func(out, errOut io.Writer, args []string) int{
	"--cfg":   func(out, errOut io.Writer, args []string) int { return runCfg(out, errOut, args) },
	"--dmp":   func(out, errOut io.Writer, args []string) int { return runDmp(out, errOut, args) },
	"--bench": func(out, errOut io.Writer, args []string) int { return runBench(out, errOut, args) },
	"hook":    func(out, errOut io.Writer, args []string) int { return runHook(out, errOut, args) },
	"query":   func(out, errOut io.Writer, args []string) int { return runQuery(out, args) },
	"serve":   func(out, errOut io.Writer, args []string) int { return runServe() },
	"pick":    func(out, errOut io.Writer, args []string) int { return runPick(out, os.Stdin, errOut, args) },
}

// A lone "-" is a legal value for stdin/stdout, not a flag that could go unrecognised.
func isDashFlag(a string) bool {
	return a != "-" && strings.HasPrefix(a, "-")
}

func isQuery(arg string) bool {
	return !isDashFlag(arg) && commands[arg] == nil
}

func rejectFlag(errOut io.Writer, cmd, flag string) int {
	fmt.Fprintf(errOut, "urd %s: unrecognised flag %q\n", cmd, flag)
	return 2
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// Called by the generated glue, not by a human: "hook" from .zshrc, the rest from the widget.
var hiddenCommands = map[string]bool{"hook": true, "query": true, "serve": true, "pick": true}

type helpLine struct{ name, args, about string }

// The order is fixed, not sorted: "--setup" has to come before "--bench".
var helpTable = []helpLine{
	{"", "", "reindex if needed, start the daemon, print a summary"},
	{"", "<words>", "print the freshest command matching every word (bash has no live mode)"},
	{"--cfg", "[edit|fill]", "print the config, open it in $EDITOR, or write the missing keys into it"},
	{"--dmp", "[file]", "write a portable dump of the whole history"},
	{"--dmp", "load [file]", "read a dump or any raw history file back"},
	{"--dmp", "export <zsh|bash> [file|-]", "write history in a shell's own format"},
	{"--setup", "", "run the first-run questions again"},
	{"--bench", "[--query <q>] [--runs <n>]", "measure per-keystroke search latency"},
	{"--help", "", "print this help"},
	{"--version", "", "print version"},
}

func spellings(name string) string {
	for _, c := range shell.Commands {
		if c.Name != name {
			continue
		}
		if c.Short != "" {
			return c.Short + "|" + c.Name
		}
	}
	return name
}

func (l helpLine) usage() string {
	out := "urd"
	if l.name != "" {
		out += " " + spellings(l.name)
	}
	if l.args != "" {
		out += " " + l.args
	}
	return out
}

func helpFor(name string) []helpLine {
	var out []helpLine
	for _, l := range helpTable {
		if l.name == name {
			out = append(out, l)
		}
	}
	return out
}

// --help is intercepted here and only here, so no runX has to know about that flag.
func dispatch(out, errOut io.Writer, name string, args []string) int {
	name = canonical(name)
	run, ok := commands[name]
	if !ok {
		fmt.Fprintf(errOut, "urd: unknown command %q\n\n%s", name, usageText())
		return 2
	}
	if hasHelpFlag(args) {
		if lines := helpFor(name); len(lines) > 0 {
			fmt.Fprintln(out, "Usage:")
			for _, l := range lines {
				fmt.Fprintf(out, "  %s\n    %s\n", l.usage(), l.about)
			}
			return 0
		}
	}
	return run(out, errOut, args)
}

func usageText() string {
	width := 0
	for _, l := range helpTable {
		if n := len(l.usage()); n > width {
			width = n
		}
	}

	var b strings.Builder
	b.WriteString("urd - history search for zsh that rewrites your prompt\n\nUsage:\n")
	for _, l := range helpTable {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, l.usage(), l.about)
	}
	return b.String()
}

// In a Dockerfile or CI stdout is not a terminal, and .zshrc must not be touched there.
func isTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(runRoot(os.Stdout, os.Stderr, os.Stdin, isTTY(), false, startDaemon))
	}
	// Resolved here because the three commands below never reach dispatch.
	switch canonical(args[0]) {
	case "--help":
		fmt.Fprint(os.Stdout, usageText())
	case "--version":
		fmt.Fprintln(os.Stdout, buildVersion())
	case "--setup":
		os.Exit(runRoot(os.Stdout, os.Stderr, os.Stdin, isTTY(), true, startDaemon))
	default:
		if isQuery(args[0]) {
			os.Exit(runSearch(os.Stdout, os.Stderr, args))
		}
		os.Exit(dispatch(os.Stdout, os.Stderr, args[0], args[1:]))
	}
}
