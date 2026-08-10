# democast

Generates the demo recording for the project README.

## Why not just record it by hand

Hand-drawing the frames is not an option: the whole point of the demo is the input
line being rewritten as the query is typed, and that is cursor control sequences the
widget genuinely emits. Frames written by hand would show something plausible rather
than what `urd` actually prints.

So `democast` builds the `urd` binary from the current code, brings up a real `zsh` in
a real pty (`github.com/creack/pty`), plays the scenario from `task-36-brief.md` into
it character by character, and puts the captured byte stream into `demo/urd.cast`, a
file in asciicast v2 format.

## Running it

```
make demo
```

or directly:

```
go run ./tools/democast
```

## The history the demo plays with

It is not the developer's history: `democast` composes one itself out of a dozen
invented commands (`ansible-playbook`, `kubectl`, `docker`) with made-up host names in
the `.lab` domain. The recording goes to a public repository and must hold not one
real host, password or path.

The engine mode in the demo session's config is `oneshot`: the recording needs no
daemon, and none must outlive the `democast` process.

## The pause between keystrokes

A recording with no pause between keystrokes is unreadable: the per-character redraw
of the line flickers past faster than anyone can follow. The timing is two constants
in `main.go`:

- `keyDelay = 150ms` - the pause between individual characters while the scenario
  "types" a word or a command.
- `beatPause = 1200ms` - the pause between the scenario's actions (arrows, Enter,
  Ctrl-C) and between scenes: time to read a frame before the next action.

To record the demo slower or faster, change both values - those only, no other code.

The scenario carries `--dmp` on to `--dmp export zsh`: the brief stopped at a bare
`--dmp`, but the command tree reads better whole - it shows the suggested name giving
way to a typed one, the `[zsh|bash]` hint, and the leaf's static hint for the redirect
target. The total stays under ~20 seconds; past that nobody watches a README loop to
the end.

## Checking the recording

After recording, `democast` looks for the lines each scene has to produce on the
"canvas" (the stream with ANSI codes applied, not the raw byte stream) and prints
found/not found for each. A bare grep over the raw stream is no good for this: ZLE
redraws the line differentially and wraps every changed character in its own colour
reset, so a word ends up torn apart by byte codes even when it is whole on screen. Not
skipping this check matters: one revision of this tool already passed review on exactly
that mistake.

## Then the GIF

GitHub does not embed the asciinema player in a README, so a `.cast` is useless there
by itself: a GIF is needed. `agg` (https://github.com/asciinema/agg) turns `.cast` into
GIF:

```
agg demo/urd.cast demo/urd.gif
```

If `agg` is not installed, `make demo` does not try to install it - that is a human's
decision, not a tool's. To get it: `brew install agg` (or `cargo install --locked agg`
if a version without Homebrew is wanted).

`demo/*.cast` is in `.gitignore`: it is a line-by-line dump of a pty session, tens of
kilobytes, rebuilt in seconds and worth nothing in git. The resulting `demo/urd.gif`,
on the contrary, is committed: it is a finished asset for the README.

## The GIF is a release prerequisite, not decoration

`demo/urd.gif` is the first element of the root README, ahead of any text. Without it a
reader on GitHub sees a broken image in the first screenful, so the GIF has to be in
the repository before publication:

```
make demo
git add -f demo/urd.gif && git commit -m "chore: refresh the README demo recording"
```

`-f` is not required by the current `.gitignore` (only `demo/*.cast` is ignored) but is
kept as insurance: that rule is easy to widen to `demo/` and lose the asset silently.
The GIF needs rebuilding after every change to the widget that shows on screen - hints,
colours, the indicator's format.
