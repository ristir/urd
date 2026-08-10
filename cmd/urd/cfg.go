package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/ristir/urd/internal/config"
)

// Rendered through the same writer as Save, so this is exactly what the first write
// will produce, hints included.
func writeDefaults(out, errOut io.Writer) int {
	fmt.Fprint(out, config.Text(config.Default()))
	return 0
}

// loadConfigOrWarn parses the config for commands addressed to a human; on the hot
// path (runQuery) the same error is swallowed on purpose, to keep the prompt alive.
func loadConfigOrWarn() (config.Config, error) {
	cfg, unknown, err := config.LoadMeta()
	if err != nil {
		fmt.Fprintf(os.Stderr, "urd: %s is invalid (%v), using defaults\n", config.Path(), err)
		return cfg, err
	}
	for _, key := range unknown {
		fmt.Fprintf(os.Stderr, "urd: %s: unknown config key %q, ignored\n", config.Path(), key)
	}
	return cfg, err
}

func runCfg(out, errOut io.Writer, args []string) int {
	path := config.Path()
	loadConfigOrWarn()

	if len(args) > 0 {
		switch {
		case isDashFlag(args[0]):
			return rejectFlag(errOut, "cfg", args[0])
		case args[0] == "fill":
			added, moved, backup, err := config.Fill()
			if err != nil {
				fmt.Fprintf(errOut, "urd cfg: %v\n", err)
				return 1
			}
			if len(added) == 0 {
				fmt.Fprintf(out, "%s: every key is already there\n", path)
				return 0
			}
			if backup != "" {
				fmt.Fprintf(out, "previous file kept as %s\n", backup)
			}
			for _, m := range moved {
				fmt.Fprintf(out, "%s: moved %s\n", path, m)
			}
			fmt.Fprintf(out, "%s: wrote %d key(s)\n", path, len(added))
			for _, k := range added {
				fmt.Fprintln(out, " ", k)
			}
			return 0
		case args[0] == "edit":
			if _, err := os.Stat(path); err != nil {
				if !os.IsNotExist(err) {
					fmt.Fprintf(errOut, "urd cfg: %v\n", err)
					return 1
				}
				cfg, _ := config.Load()
				if err := config.Save(cfg); err != nil {
					fmt.Fprintf(errOut, "urd cfg: %v\n", err)
					return 1
				}
			}
			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				editor = "vi"
			}
			cmd := exec.Command(editor, path)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(errOut, "urd cfg: %v\n", err)
				return 1
			}
			return 0
		}
	}

	fmt.Fprintln(out, path)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintln(out, "(file does not exist yet, showing defaults)")
		return writeDefaults(out, errOut)
	}
	if err != nil {
		fmt.Fprintf(errOut, "urd cfg: %v\n", err)
		return 1
	}
	fmt.Fprint(out, string(data))
	// The keys the file lacks: a section from a new version is invisible otherwise,
	// and it is the one that runs. Commented, so the block can be copied into the file.
	if absent := config.Absent(); len(absent) > 0 {
		fmt.Fprintln(out, "\n# in effect but not in the file:")
		for _, line := range absent {
			fmt.Fprintln(out, "#", line)
		}
	}
	return 0
}
