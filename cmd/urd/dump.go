package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/corpus"
	"github.com/ristir/urd/internal/dump"
	"github.com/ristir/urd/internal/engine"
)

const secretWarning = "urd: a dump contains your raw history, including secrets in command lines; it is written with mode 0600, keep it that way"

func writeDump(path string, render func(io.Writer) error) error {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func runDmp(out, errOut io.Writer, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "load":
			return runDmpLoad(out, errOut, args[1:])
		case "export":
			return runDmpExport(out, errOut, args[1:])
		}
	}
	return runDmpWrite(out, errOut, args)
}

// runDmpWrite: engine.Unfiltered, not engine.Load - an archive must not depend on
// which filters happened to be set at dump time.
func runDmpWrite(out, errOut io.Writer, args []string) int {
	target := ""
	for _, a := range args {
		if isDashFlag(a) {
			return rejectFlag(errOut, "dmp", a)
		}
		target = a
	}

	cfg, _ := loadConfigOrWarn()
	c, _ := engine.Unfiltered(cfg)

	// A warning always goes to stderr: "urd --dmp - | ssh host urd --dmp load -" must
	// not receive anything but the records themselves.
	fmt.Fprintln(errOut, secretWarning)

	if target == "" {
		path, err := dump.DefaultPath(time.Now())
		if err != nil {
			fmt.Fprintf(errOut, "urd dmp: %v\n", err)
			return 1
		}
		target = path
	}
	if target == "-" {
		if err := dump.Write(out, c); err != nil {
			fmt.Fprintf(errOut, "urd dmp: %v\n", err)
			return 1
		}
		return 0
	}
	if err := writeDump(target, func(w io.Writer) error { return dump.Write(w, c) }); err != nil {
		fmt.Fprintf(errOut, "urd dmp: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "saved %d entries to %s\n", len(c.Items), target)
	return 0
}

func runDmpLoad(out, errOut io.Writer, args []string) int {
	src := ""
	if len(args) > 0 {
		if isDashFlag(args[0]) {
			return rejectFlag(errOut, "dmp load", args[0])
		}
		src = args[0]
	}
	if src == "" {
		found, err := dump.NewestDefault()
		if err != nil {
			fmt.Fprintf(errOut, "urd dmp load: %v\n", err)
			return 1
		}
		src = found
	}

	var data []byte
	var err error
	if src == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(src)
	}
	if err != nil {
		fmt.Fprintf(errOut, "urd dmp load: %v\n", err)
		return 1
	}

	entries := dump.Read(data, src)
	if len(entries) == 0 {
		fmt.Fprintln(errOut, "urd dmp load: nothing recognised in the input")
		return 1
	}

	// JSONL, not the positional format: only JSONL carries Source in the record, or an
	// import from another host turns into commands with no source at all.
	c, _ := corpus.Build(entries)
	f, target, err := dump.Create(config.ImportedDir(), src, time.Now())
	if err != nil {
		fmt.Fprintf(errOut, "urd dmp load: %v\n", err)
		return 1
	}
	werr := dump.Write(f, c)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		// An unfinished import is deleted: otherwise it stays a source and silently
		// serves truncated history.
		os.Remove(target)
		fmt.Fprintf(errOut, "urd dmp load: %v\n", werr)
		return 1
	}
	fmt.Fprintf(out, "imported %d entries into %s\n", len(c.Items), target)
	return 0
}

func runDmpExport(out, errOut io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "urd dmp export: expected zsh or bash")
		return 2
	}
	shell := args[0]
	if isDashFlag(shell) {
		return rejectFlag(errOut, "dmp export", shell)
	}

	var writeFn func(io.Writer, *corpus.Corpus) error
	switch shell {
	case "zsh":
		writeFn = dump.WriteZsh
	case "bash":
		writeFn = dump.WriteBash
	default:
		fmt.Fprintf(errOut, "urd dmp export: unsupported shell %q\n", shell)
		return 2
	}

	target := "-"
	for _, a := range args[1:] {
		if isDashFlag(a) {
			return rejectFlag(errOut, "dmp export", a)
		}
		target = a
	}

	cfg, _ := loadConfigOrWarn()
	c, _ := engine.Unfiltered(cfg)
	render := func(w io.Writer) error { return writeFn(w, c) }

	fmt.Fprintln(errOut, secretWarning)
	if target == "-" {
		if err := render(out); err != nil {
			fmt.Fprintf(errOut, "urd dmp export: %v\n", err)
			return 1
		}
		return 0
	}
	if err := writeDump(target, render); err != nil {
		fmt.Fprintf(errOut, "urd dmp export: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "exported %d entries to %s\n", len(c.Items), target)
	return 0
}
