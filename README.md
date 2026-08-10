# urd

History search for zsh: type three words, get the whole command back in your prompt.

![urd rewriting the input line as the query is typed](demo/urd.gif)

Urðr is the norn of the past, "that which has become". Nothing here predicts: it shows
what already happened, ranked by recency.

**zsh is the shell this is for. bash support is real but reduced, and fish has none.**
The input line rewritten as you type needs a shell that lets a program intercept every
keystroke and colour part of the line: zsh's ZLE does, readline does not at any version.
So bash gets a hotkey and a dialog instead — see
[what bash gets instead](#what-bash-gets-instead).

## Install

```sh
brew install ristir/tap/urd
```

Or `go install github.com/ristir/urd/cmd/urd@latest`, or take
`urd_<version>_<os>_<arch>.tar.gz` from the
[releases page](https://github.com/ristir/urd/releases) and put `urd` in `$PATH`.

macOS and Linux, one static binary, no cgo.

`urd --version` prints the tag, the revision and when it was built:

```
v0.1.0 (ffec288, 2026-08-10T11:33:22Z)
```

Off a tag it is `git describe` output; a build made without the Makefile falls back to
Go's build info, which records no build time.

## The one manual step

```sh
urd --setup
```

It shows the line and asks before appending it to your `~/.zshrc` — or `~/.bashrc`, since
the shell comes from the parent process and not from `$SHELL`. With stdout not a terminal
(Dockerfile, CI) it asks nothing and edits nothing. The first bare `urd` offers the same.

By hand it is one line, at the very END of the rc file:

```sh
eval "$(/full/path/to/urd hook zsh)"
```

The end matters: oh-my-zsh, `zsh-syntax-highlighting` and friends bind keys when sourced,
and this widget has to bind after them. So does the full path, which is what `--setup`
writes: on Ubuntu a bare name binds nothing and prints `command not found` per shell
start, because `~/.local/bin` enters `$PATH` from `~/.profile`, which only login shells
read, while `.bashrc` is read by every interactive one.

## Using it

This is what it replaces:

```
history | grep ans | grep rate | grep -i ver
```

A screenful to squint at, and the line you wanted still has to be retyped. `urd ans rate
ver` puts it in the prompt instead, cursor at the end, ready to edit.

Type the trigger word and a space on an empty line. Every keystroke after that goes into
a query instead of the buffer, and the buffer is rewritten to the freshest command
containing all the query words. Matching is a strict substring, like `grep`, never fuzzy:
`ans ratel` and `ans rate-li` find different commands.

Any of `-_/.,;:=` cuts a query word into pieces that must occur in that order, with the
delimiter itself occurring literally in between — `ans-pl` means `*ans*-*pl*` and finds
`ansible-playbook`, while `anspl` finds nothing and `ans-pl` never reaches
`ansible playbook`. Marks land on the pieces rather than the whole word, so `pl/us/`
marks `playbooks` and `users` and leaves `admins.yml` alone. Wrap a word in quotes to
turn all of that off: `'cache-01'` is one literal, dash included.

| Typed | What you see |
| --- | --- |
| `urd ` | the trigger word lights up; nothing in the buffer yet |
| `urd ans rate` | `urd ansible-playbook …/rate-limit.yml -l cache-02… [ans rate - 1/3]` |
| Up, Up | same query, older candidates: `[ans rate - 2/3]`, `[ans rate - 3/3]` |
| `urd --d` | `urd --d [--dmp]` — a leading dash means a urd command, not a query |
| `urd --dmp ex`, Tab | `urd --dmp export [zsh\|bash]` — Tab finishes a urd command |
| `urd -c`, Tab | `urd --cfg [edit\|fill]` — a short spelling grows into the canonical one |

The underlined tail inside the brackets is the query, with the blinking cursor at its
end; everything around it is the tool talking, and neither the trigger word nor the dim
`[query - n/total]` ever enters the buffer.

Enter on a search result only accepts it into the line and a second Enter runs it — a
tool wrote that line, and a version or a host usually wants fixing first. Enter on a urd
command runs it at once. Only the *first* word can become a command, and only by starting
with a dash; elsewhere a dash is an ordinary search character, so `urd ans -e` finds
`site.yml -e env=prod`.

## What bash gets instead

Everything above describes zsh. readline has no per-keystroke hook and cannot colour part
of an input line — not a version issue, not something a plugin fixes — so bash gets this:

```sh
# Ctrl-X Ctrl-U        a dialog that puts the command you pick into the line
urd ans rate            # prints the freshest match, so $(urd ans rate) is that command
```

| | zsh | bash |
| --- | --- | --- |
| the line rewritten as you type | yes | no |
| how you enter | `urd ` and a space | `Ctrl-X Ctrl-U`, or your own `ui.hotkey` |
| what you get | the command in your line, editable, cursor at the end | a dialog on `/dev/tty`, then the pick in your line |
| marks on what matched | yes | no |
| Tab finishing a urd command | yes | no |
| `urd <words>` printing a match | yes | yes |
| a config edit applying without a new shell | yes | no |
| the corpus, ranking, delimiters, dumps | same | same |

The dialog reaches the input line only because readline lets a `bind -x` callback write
`READLINE_LINE`; nothing outside that callback can, so a separately run `urd ans rate`
prints instead — stdout alone, match count on stderr. Both shells read both histories, so
the corpus is the same wherever you are.

## Commands

```
urd - history search for zsh that rewrites your prompt

Usage:
  urd                                        reindex if needed, start the daemon, print a summary
  urd <words>                                print the freshest command matching every word (bash has no live mode)
  urd -c|--cfg [edit|fill]                   print the config, open it in $EDITOR, or write the missing keys into it
  urd -d|--dmp [file]                        write a portable dump of the whole history
  urd -d|--dmp load [file]                   read a dump or any raw history file back
  urd -d|--dmp export <zsh|bash> [file|-]    write history in a shell's own format
  urd -s|--setup                             run the first-run questions again
  urd -b|--bench [--query <q>] [--runs <n>]  measure per-keystroke search latency
  urd -h|--help                              print this help
  urd -v|--version                           print version
```

Each command answers to three spellings — `--cfg`, `-c` and the single-dash `-cfg` — and
Tab in the widget grows any of them into the canonical one. Each answers `--help` with
its own usage; any other flag is refused by name, `urd --dmp --nonsense` exiting 2 with
`urd dmp: unrecognised flag "--nonsense"`.

`hook`, `query`, `serve` and `pick` work but stay out of the help: the generated glue
types them, not a human. They are also the only bare words that are not a query — any
other first word without a dash is searched for, so `urd cfg` finds a command containing
"cfg" rather than complaining, and `urd hook zsh` typed by hand becomes a search unless
you write `command urd hook zsh`.

## Config

`~/.config/urd/config.toml`, honouring `XDG_CONFIG_HOME`; a missing file is normal.
`urd --cfg` prints the path and what is in effect — with no file yet, this, which is also
what the first write produces:

```toml
[engine]
  mode = "daemon"  # daemon | oneshot
  idle_timeout = "1h"  # "30m", "1h", "24h"

[sources]
  auto = true  # true | false
  extra = []  # e.g. ["~/backups/**/*history*"]

[ui]
  indicator = "suffix"  # suffix | below | off
  trigger = "urd"  # any word: "urd", "hist", "h"
  hotkey = ""  # "^R", "^Xu"; "" = unbound
  steal_ctrl_r = false  # true | false; native search stays on ^Xr

[search]
  exclude = ["^history", "^urd"]  # RE2, anchor it: ["^history", "^sudo rm"]
  delimiters = "-_/.,;:="  # any characters; "" = whole words only

[colors]
  prompt = "fg=cyan,bold"  # fg=cyan,bold | fg=8 | fg=#8fbf7f | "" = none
  mark = "fg=white,bold"  # fg=white,bold | standout | underline
  builtin = "fg=green"  # fg=green | bold | "" = none
  hint = "fg=8"  # fg=8 | fg=blue | "" = none
  query = "underline"  # underline | standout | fg=white
```

Where the values are not self-evident: `engine.mode = "oneshot"` pays a fork, an exec and
an index read on every keystroke, so it is a fallback rather than a choice.
`sources.auto` covers `$HISTFILE`, `~/.zsh_history`, `~/.bash_history` and `imported/`;
`extra` takes paths and globs, with a leading `~` and `**` at any depth. `ui.trigger` is
the word a space turns into search mode, so `hist` or `h` is as valid as `urd`.
`[colors]` is zsh only, and `colors.hint` needs `indicator = "suffix"`: `below` prints
through `zle -M`, where `region_highlight` does not reach.

`urd --cfg` prints the file as it stands, then a commented block naming the keys in
effect but absent from it — the only way to notice a section a new version added.
`urd --cfg fill` writes those keys in, keeping the previous file as
`config.toml.urd-bak-*`. It edits lines rather than re-encoding, so your comments, key
order and indentation survive.

`exclude` used to live in its own `[filters]`, and a file that still says so keeps
filtering: a filter that quietly stopped applying would put back exactly the commands it
was hiding. `urd --cfg fill` moves it into `[search]` and drops the empty header, but
leaves a `[filters]` block holding anything this version does not recognise.

When an edit starts to matter:

| Changed | Applies |
| --- | --- |
| `[ui]`, `[colors]` | on the next `urd ` — entering the mode compares the file with the copy the glue was printed from and reprints it when they differ. `ui.trigger` needs one entry more: the word is compared before the reload happens |
| `ui.hotkey`, `ui.steal_ctrl_r` | a hotkey newly added applies the same way; a *changed* one needs a new tab, because the previous binding cannot be revoked without knowing what it was |
| `[search]`, `[sources]` | by themselves within 5 seconds: the live daemon re-reads the config on every source check |
| `[engine] idle_timeout` | at the next daemon start |

The reload compares mtime and size, which `zstat` reports without a fork; its blind spot
is an edit inside the same second the shell started that also keeps the file the same
length. A config urd refuses to parse prints no glue, and the widget then keeps the values
it had rather than losing its functions to an empty `eval`.

## Moving history between machines

```sh
urd --dmp                                          # a portable JSONL dump in ~
urd --dmp - | ssh host urd --dmp load -            # or straight onto another machine
urd --dmp load ~/Downloads/old-host.bash_history   # import a dump or any raw histfile
zsh -f -c 'urd --dmp export zsh > ~/.zsh_history'  # write it back in zsh's own format
```

Imports land in `~/.local/share/urd/imported/` and everything there becomes a source
automatically — symlink it into a cloud folder for sync. Dumps are `0600` and hold secrets
in the clear; the warning goes to stderr, never stdout, so the pipe above carries data
only.

The export needs its own `zsh -f` and a raised `SAVEHIST` (the first run offers): zsh
trims the history file once it passes `$SAVEHIST` by 20%, so the oldest of what you just
wrote would vanish on the next append.

## Uninstall

```sh
sh packaging/uninstall.sh --dry-run   # what it would remove
sh packaging/uninstall.sh             # the same list, then asks
```

It prints every path first, then stops the daemon, deletes the binary from the usual
places, removes `~/.config/urd` and `~/.local/share/urd`, drops the `urd hook` line from
your rc file (keeping a copy) and clears the backups urd left behind. Your own
`~/urd_history_*` dumps are listed and kept; `~/.zsh_history` urd never wrote to at all.
A script rather than a subcommand, because a binary that will not start cannot uninstall
itself.

## Tested on

Everything below was run on the machine this was written on, and only there:

| | |
| --- | --- |
| macOS | 15.7.3 (24G419), Apple Silicon (arm64) |
| zsh | 5.9 — the live widget, every pty test |
| bash | 5.3.3 on macOS, 5.2.21 on Ubuntu |
| Ubuntu | 24.04.1 LTS, kernel 6.8.0, x86_64 — the release archive, unpacked and run |
| Go | 1.26.4; the module targets 1.22 |
| history | ~48k commands, 3 sources, 341 archived `.bash_history` files |
| terminals | iTerm2 and the macOS Terminal, `TERM=xterm-256color` |

On Ubuntu the whole suite runs green, pty tests and all, under Go 1.22.2 — the version
apt ships. `tools/linux-check.sh` exercises the Linux-only paths against an installed
binary; three defects surfaced there and nowhere else, all fixed.

bash 3.2.57, which Apple still ships as `/bin/bash`, loads the glue and defines the
dialog; whether the hotkey binds there is unverified, since `bind -X` — the only way to
list what `bind -x` registered — does not exist before bash 4.

## What it does not do

- **fish** — `fish_history` is YAML-ish and has no parser here.
- **ssh mode** — no pulling history off a remote host; use the pipe above.
- **`.gz`, `.tar.gz`** — unpack them yourself.
- **secret filtering in dumps** — deliberate: such a filter catches
  `AWS_SECRET_ACCESS_KEY=`, misses `-e P=hunter2`, and buys false confidence.
- **a menu-bar icon** — Cocoa plus cgo on macOS, no single API on Linux; `urd` in
  tmux `status-right` scratches the same itch.
- **exit codes** — in a zsh histfile entry (`: 1786106991:0;ping unit-01`) the field
  after the timestamp is the command's *duration*, not its status, so "successful commands
  only" would need a metadata database fed by a per-command hook.

Everything it does say goes to stderr under a `urd:` prefix: sources and entry counts,
what was joined, deduplicated or filtered away, whether the index was read or rebuilt,
and every pattern or config key it did not understand. A missing command otherwise gets
believed and retyped.

## License

MIT — see [LICENSE](LICENSE). The two dependencies, `BurntSushi/toml` and
`creack/pty`, are MIT as well.
