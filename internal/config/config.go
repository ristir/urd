package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Engine struct {
	Mode        string `toml:"mode"`
	IdleTimeout string `toml:"idle_timeout"`
}

type Sources struct {
	Auto  bool     `toml:"auto"`
	Extra []string `toml:"extra"`
}

type UI struct {
	Indicator  string `toml:"indicator"`
	Trigger    string `toml:"trigger"`
	Hotkey     string `toml:"hotkey"`
	StealCtrlR bool   `toml:"steal_ctrl_r"`
}

// Filters is where exclude used to live: still read, because a filter that quietly
// stops applying puts the commands it hid back in the results.
type Filters struct {
	Exclude []string `toml:"exclude"`
}

// Delimiters are the characters a query word is cut into literals on, "ans-pl" turning
// into *ans*-*pl*; an empty string means whole substrings.
type Search struct {
	Exclude    []string `toml:"exclude"`
	Delimiters string   `toml:"delimiters"`
}

// Colors are zle_highlight specs and reach region_highlight literally; zsh only.
type Colors struct {
	Prompt  string `toml:"prompt"`
	Mark    string `toml:"mark"`
	Builtin string `toml:"builtin"`
	Hint    string `toml:"hint"`
	Query   string `toml:"query"`
}

type Config struct {
	Engine  Engine  `toml:"engine"`
	Sources Sources `toml:"sources"`
	UI      UI      `toml:"ui"`
	Filters Filters `toml:"filters"`
	Search  Search  `toml:"search"`
	Colors  Colors  `toml:"colors"`
}

func Default() Config {
	return Config{
		Engine:  Engine{Mode: "daemon", IdleTimeout: "1h"},
		Sources: Sources{Auto: true},
		UI:      UI{Indicator: "suffix", Trigger: "urd"},
		// Anchored "^history", or the filter also eats "cat ~/.zsh_history". A space is
		// not in the delimiter set and never can be: it separates words one level above.
		Search: Search{Exclude: []string{"^history", "^urd"}, Delimiters: "-_/.,;:="},
		// The query is underlined, not coloured: a colour inside the brackets fights the
		// mark, while an underline plus the cursor after it reads as an input field.
		Colors: Colors{Prompt: "fg=cyan,bold", Mark: "fg=white,bold", Builtin: "fg=green", Hint: "fg=8", Query: "underline"},
	}
}

func (c Config) IdleDuration() time.Duration {
	d, err := time.ParseDuration(c.Engine.IdleTimeout)
	if err != nil || d <= 0 {
		return time.Hour
	}
	return d
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "urd")
	}
	return filepath.Join(home(), ".config", "urd")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

func DataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "urd")
	}
	return filepath.Join(home(), ".local", "share", "urd")
}

func ImportedDir() string { return filepath.Join(DataDir(), "imported") }
func IndexPath() string   { return filepath.Join(DataDir(), "index.bin") }

// SocketPath stays in TMPDIR rather than DataDir: a socket is runtime state and
// must not be dragged into cloud sync along with the data.
func SocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "urd-"+itoa(os.Getuid())+".sock")
}

func PidPath() string { return SocketPath() + ".pid" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func Load() (Config, error) {
	c, _, err := LoadMeta()
	return c, err
}

// LoadMeta names the keys of the file that landed in no field of Config -
// toml.Decode drops them silently. On a syntax error unknown is empty.
func LoadMeta() (Config, []string, error) {
	c := Default()
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return c, nil, nil
	}
	if err != nil {
		return c, nil, err
	}
	meta, err := toml.Decode(string(data), &c)
	if err != nil {
		return Default(), nil, err
	}
	adoptLegacy(&c, meta)
	keys := meta.Undecoded()
	if len(keys) == 0 {
		return c, nil, nil
	}
	unknown := make([]string, len(keys))
	for i, k := range keys {
		unknown[i] = k.String()
	}
	return c, unknown, nil
}

// Decoding leaves the default in the new field, so without this a file that only has
// [filters] would run with the default filter list instead of the one it asks for.
func adoptLegacy(c *Config, meta toml.MetaData) {
	if !meta.IsDefined("search", "exclude") && meta.IsDefined("filters", "exclude") {
		c.Search.Exclude = c.Filters.Exclude
	}
}

func tomlValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String())
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = tomlValue(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprint(v.Interface())
	}
}

// Save writes through Text: toml.NewEncoder cannot emit the hint on every key.
func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(), []byte(Text(c)), 0o644)
}

// SaveWithBackup keeps a copy first, since Save rewrites the file whole and loses
// comments and key order. A failed backup leaves the file untouched.
func SaveWithBackup(c Config) (string, error) {
	data, err := os.ReadFile(Path())
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	backup := ""
	if len(data) > 0 {
		backup = fmt.Sprintf("%s.urd-bak-%s", Path(), time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return "", err
		}
	}
	return backup, Save(c)
}
