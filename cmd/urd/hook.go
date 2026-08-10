package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ristir/urd/internal/config"
	"github.com/ristir/urd/internal/shell"
)

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "urd"
	}
	return p
}

func runHook(out, errOut io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "urd hook: expected zsh or bash")
		return 2
	}
	if isDashFlag(args[0]) {
		return rejectFlag(errOut, "hook", args[0])
	}
	cfg, _ := config.Load()
	switch args[0] {
	case "zsh":
		// On an error the glue is not printed at all: eval of half a line in .zshrc is worse than no hook.
		glue, err := shell.Zsh(cfg, selfPath())
		if err != nil {
			fmt.Fprintf(errOut, "urd hook: %v\n", err)
			return 1
		}
		fmt.Fprint(out, glue)
		return 0
	case "bash":
		glue, err := shell.Bash(cfg, selfPath())
		if err != nil {
			fmt.Fprintf(errOut, "urd hook: %v\n", err)
			return 1
		}
		fmt.Fprint(out, glue)
		return 0
	default:
		fmt.Fprintf(errOut, "urd hook: unsupported shell %q\n", args[0])
		return 2
	}
}
