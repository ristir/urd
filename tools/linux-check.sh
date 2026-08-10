#!/usr/bin/env bash
# Exercises the Linux-only paths - socket location, /proc for the parent's name, pgrep,
# bash as the shell being set up - against an installed urd in a sandbox HOME.
set -u

B=${1:-$HOME/.local/bin/urd}
SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

pass=0
fail=0
# The output comes in as an argument, never on stdin: from a pipe this runs in a subshell
# and the counters are thrown away - the summary said "0 ok, 0 failed" under nine passes.
check() { # check <name> <expected substring> <actual>
	local name=$1 want=$2 got=$3
	if [[ $got == *"$want"* ]]; then
		printf 'ok    %s\n' "$name"
		pass=$((pass + 1))
	else
		printf 'FAIL  %s\n      want %q in:\n%s\n' "$name" "$want" "$got"
		fail=$((fail + 1))
	fi
}

echo "=== the machine ==="
uname -srm
. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME"
bash --version | head -1
command -v zsh >/dev/null && zsh --version || echo "zsh: not installed"
echo "XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR-<unset>}"
echo

export HOME="$SANDBOX/home"
mkdir -p "$HOME"
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share"
# The runtime dir too, or the daemon under test shares the socket with the real one and
# the uninstaller at the end deletes it - which is what happened on the first run.
export XDG_RUNTIME_DIR="$SANDBOX/run"
mkdir -p "$XDG_RUNTIME_DIR"
printf '%s\n' \
	'ansible-playbook playbooks/users/admins.yml -l cache-01.lab -bD' \
	'kubectl get pods -n demo' \
	'docker compose up -d rotator' > "$HOME/.bash_history"

echo "=== 1. the binary runs, statically ==="
"$B" --version
file "$B" 2>/dev/null | sed 's/^/      /'
echo

echo "=== 2. bare urd: index, daemon, summary ==="
"$B" 2>&1 | head -4
echo

echo "=== 3. the socket landed where Linux puts it ==="
ls -la "${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}"/urd-"$(id -u)".sock* 2>&1 | sed 's/^/      /'
echo

echo "=== 4. the daemon answers, and search works ==="
check "urd query through the daemon" "ansible-playbook" "$("$B" query 0 ans-pl 2>&1 | head -2)"
check "urd <words> prints the match" "ansible-playbook" "$("$B" ans-pl 2>/dev/null)"
check "another query" "kubectl get pods" "$("$B" kube 2>/dev/null)"
echo

echo "=== 5. the shell is detected from /proc, not from \$SHELL ==="
# SHELL deliberately lies: the parent is bash, and the parent is what must win.
from_bash=$(SHELL=/bin/zsh bash -c "'$B' | cat" 2>&1 | grep -A1 'add this line')
check "bash parent -> .bashrc" ".bashrc" "$from_bash"
check "bash parent -> hook bash" "hook bash" "$from_bash"
if command -v zsh >/dev/null; then
	from_zsh=$(SHELL=/bin/bash zsh -f -c "'$B' | cat" 2>&1 | grep -A1 'add this line')
	check "zsh parent -> .zshrc" ".zshrc" "$from_zsh"
	check "zsh parent -> hook zsh" "hook zsh" "$from_zsh"
fi
echo

echo "=== 6. the bash glue loads and binds ==="
"$B" hook bash > "$SANDBOX/glue.bash"
bash -n "$SANDBOX/glue.bash" && echo "      parses"
check "Ctrl-X Ctrl-U is bound" "_urd_pick" \
	"$(bash -i -c "source $SANDBOX/glue.bash; bind -X" 2>/dev/null | grep -i urd)"
check "the pick dialog answers" "ansible-playbook" "$(printf 'ans-pl\r\r' | "$B" pick 2>/dev/null)"
echo

echo "=== 7. the zsh glue parses (the widget itself needs a pty) ==="
if command -v zsh >/dev/null; then
	"$B" hook zsh > "$SANDBOX/glue.zsh"
	zsh -n "$SANDBOX/glue.zsh" && echo "      parses"
	check "Tab is bound in the urd keymap" "_urd_complete" \
		"$(zsh -f -c "source $SANDBOX/glue.zsh; bindkey -M urd -L '^I'" 2>&1)"
	check "zstat -H works here (the config reload depends on it)" "ok=" \
		"$(zsh -f -c "zmodload zsh/stat; typeset -A st; zstat -H st -- $SANDBOX/glue.zsh && print ok=\$st[size]" 2>&1)"
else
	echo "      skipped: no zsh"
fi
echo

echo "=== 8. pgrep matches by full path, which the uninstaller relies on ==="
pgrep -f "^$B serve" >/dev/null && echo "      the daemon is found by its own path" ||
	echo "      no daemon running right now"
echo

echo "=== 9. config: fill, and the reload stamp ==="
"$B" --cfg fill 2>&1 | head -3 | sed 's/^/      /'
head -3 "$XDG_CONFIG_HOME/urd/config.toml" | sed 's/^/      /'
"$B" hook bash | grep -c . >/dev/null && echo "      hook still prints after fill"
echo

echo "=== 10. the uninstaller, on this sandbox only ==="
sh "${2:-$HOME/uninstall.sh}" --yes 2>&1 | tail -6 | sed 's/^/      /'
echo

printf '=== %d ok, %d failed ===\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
