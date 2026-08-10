package shell

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/ristir/urd/internal/config"
)

//go:embed zsh.zsh
var zshGlue string

//go:embed bash.bash
var bashGlue string

// Each value names a case branch in _urd_suggest_for: only the glue can compute the
// argument, since a fork per keystroke is banned.
const (
	suggestNewDump    = "newdump"
	suggestNewestDump = "newestdump"
)

// Command is a node of the command tree. Default names the kind of argument the widget
// puts in the line; Hint shows in the brackets at once and never enters the buffer.
type Command struct {
	Name     string
	Short    string
	Default  string
	Hint     string
	Children []Command
}

// Commands is the only declaration of what commands exist. The order shows in the hint
// on the first dash, hence it matches usageText.
var Commands = []Command{
	{Name: "--cfg", Short: "-c", Children: []Command{{Name: "edit"}, {Name: "fill"}}},
	{Name: "--dmp", Short: "-d", Default: suggestNewDump, Children: []Command{
		{Name: "load", Default: suggestNewestDump},
		{Name: "export", Children: []Command{
			{Name: "zsh", Hint: "> ~/.zsh_history"},
			{Name: "bash", Hint: "> ~/.bash_history"},
		}},
	}},
	{Name: "--setup", Short: "-s"},
	{Name: "--bench", Short: "-b"},
	{Name: "--help", Short: "-h"},
	{Name: "--version", Short: "-v"},
}

// AliasesOf lists every spelling of a command other than its canonical name.
func AliasesOf(c Command) []string {
	var out []string
	if c.Short != "" {
		out = append(out, c.Short)
	}
	if single := "-" + strings.TrimPrefix(c.Name, "--"); single != c.Name {
		out = append(out, single)
	}
	return out
}

// Aliases maps every alias to the canonical name.
func Aliases() map[string]string {
	out := map[string]string{}
	for _, c := range Commands {
		for _, a := range AliasesOf(c) {
			out[a] = c.Name
		}
	}
	return out
}

// Laid out twice: alias -> canonical resolves a short word, canonical -> aliases keeps
// the order Commands declares, which an associative array does not have.
func aliasTables() (alias, short []string) {
	for _, c := range Commands {
		names := AliasesOf(c)
		if len(names) == 0 {
			continue
		}
		for _, a := range names {
			alias = append(alias, quote(a), quote(c.Name))
		}
		short = append(short, quote(c.Name), quote(strings.Join(names, " ")))
	}
	return alias, short
}

// Three tables keyed by path for the glue's associative arrays; the root key is "".
func commandTree() (children, defaults, hints []string) {
	var walk func(path string, nodes []Command)
	walk = func(path string, nodes []Command) {
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, n.Name)
		}
		children = append(children, quote(path), quote(strings.Join(names, " ")))
		for _, n := range nodes {
			full := n.Name
			if path != "" {
				full = path + " " + n.Name
			}
			if n.Default != "" {
				defaults = append(defaults, quote(full), quote(n.Default))
			}
			if n.Hint != "" {
				hints = append(hints, quote(full), quote(n.Hint))
			}
			if len(n.Children) > 0 {
				walk(full, n.Children)
			}
		}
	}
	walk("", Commands)
	return children, defaults, hints
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// validColor allows the alphabet of zle_highlight specs (letters, digits, = , # _ -):
// the value reaches the glue under eval, where a hostile setting once ran a command.
func validColor(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '=' || r == ',' || r == '#' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func colorPlaceholder(name, value string) (string, error) {
	if !validColor(value) {
		return "", fmt.Errorf("colors.%s %q is not a valid zle_highlight spec: only letters, digits and = , # _ - are allowed", name, value)
	}
	return quote(value), nil
}

// Zsh returns the glue with config values substituted.
func Zsh(cfg config.Config, bin string) (string, error) {
	hotkey := cfg.UI.Hotkey
	if cfg.UI.StealCtrlR {
		hotkey = "^R"
	}
	binding := ""
	if hotkey != "" {
		binding = "bindkey " + quote(hotkey) + " _urd_hotkey"
	}

	// A decision, not the raw string: a config value with a space would break typeset.
	mode := "oneshot"
	if cfg.Engine.Mode == "daemon" {
		mode = "daemon"
	}

	promptColor, err := colorPlaceholder("prompt", cfg.Colors.Prompt)
	if err != nil {
		return "", err
	}
	markColor, err := colorPlaceholder("mark", cfg.Colors.Mark)
	if err != nil {
		return "", err
	}
	builtinColor, err := colorPlaceholder("builtin", cfg.Colors.Builtin)
	if err != nil {
		return "", err
	}
	hintColor, err := colorPlaceholder("hint", cfg.Colors.Hint)
	if err != nil {
		return "", err
	}
	queryColor, err := colorPlaceholder("query", cfg.Colors.Query)
	if err != nil {
		return "", err
	}

	tree, defaults, hints := commandTree()
	alias, short := aliasTables()

	// The widget compares this with the file on entering the mode, which is how an edit
	// applies without a new shell: everything else here is baked in.
	cfgStamp := "0 0"
	if st, err := os.Stat(config.Path()); err == nil {
		cfgStamp = fmt.Sprintf("%d %d", st.ModTime().Unix(), st.Size())
	}

	// Quoted on every substitution, not only paths: an apostrophe in hotkey once gave a
	// line that ran an arbitrary command on every shell start.
	r := strings.NewReplacer(
		"@@BIN@@", quote(bin),
		"@@SOCK@@", quote(config.SocketPath()),
		"@@TRIGGER@@", quote(cfg.UI.Trigger),
		"@@INDICATOR@@", quote(cfg.UI.Indicator),
		"@@MODE@@", mode,
		"@@TREE@@", strings.Join(tree, " "),
		"@@DEFAULTS@@", strings.Join(defaults, " "),
		"@@HINTS@@", strings.Join(hints, " "),
		"@@ALIAS@@", strings.Join(alias, " "),
		"@@SHORT@@", strings.Join(short, " "),
		"@@CFG@@", quote(config.Path()),
		"@@CFG_STAMP@@", quote(cfgStamp),
		"@@HOTKEY_BINDING@@", binding,
		"@@COLOR_PROMPT@@", promptColor,
		"@@COLOR_MARK@@", markColor,
		"@@COLOR_BUILTIN@@", builtinColor,
		"@@COLOR_HINT@@", hintColor,
		"@@COLOR_QUERY@@", queryColor,
	)
	return r.Replace(zshGlue), nil
}

// The value goes inside the double quotes of bind -x, where a quote closes the spec and
// $ and backtick expand, and there is nothing to escape with - so a check is what is left.
func validKeyseq(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
		if r == '\'' || r == '"' || r == '`' || r == '$' {
			return false
		}
	}
	return true
}

// Bash returns the glue for the reduced mode. A hotkey is mandatory: bash has no entry
// prefix, so there is nothing else to reach the mode with.
func Bash(cfg config.Config, bin string) (string, error) {
	hotkey := cfg.UI.Hotkey
	if cfg.UI.StealCtrlR {
		hotkey = `\C-r`
	}
	if hotkey == "" {
		hotkey = `\C-x\C-u`
	}
	if !validKeyseq(hotkey) {
		return "", fmt.Errorf("ui.hotkey %q is not a usable readline key sequence: quotes, backticks, $ and non-printable characters are not allowed in the bash binding", hotkey)
	}
	r := strings.NewReplacer(
		"@@BIN@@", quote(bin),
		"@@HOTKEY@@", hotkey,
	)
	return r.Replace(bashGlue), nil
}
