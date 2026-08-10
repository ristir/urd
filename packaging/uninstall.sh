#!/bin/sh
# Removes urd and everything it ever wrote, including leftovers of older installs.
# A script, not a subcommand: a binary that will not start cannot uninstall itself.
#
#   sh uninstall.sh            plan, then ask
#   sh uninstall.sh --dry-run  plan only
#   sh uninstall.sh --yes      no questions
set -eu

DRY=0
YES=0
for arg in "$@"; do
	case $arg in
	--dry-run) DRY=1 ;;
	--yes | -y) YES=1 ;;
	-h | --help)
		sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "uninstall.sh: unknown argument $arg" >&2
		exit 2
		;;
	esac
done

: "${HOME:?HOME is not set}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/urd"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/urd"
RUNTIME_DIR="${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}"
SOCK="$RUNTIME_DIR/urd-$(id -u).sock"

# Collected before anything happens, so a wrong guess shows up in the plan.
binaries=""
for dir in "$HOME/.local/bin" /usr/local/bin /opt/homebrew/bin "${GOBIN:-${GOPATH:-$HOME/go}/bin}"; do
	if [ -f "$dir/urd" ]; then
		binaries="$binaries $dir/urd"
	fi
done

rcfiles=""
for rc in "${ZDOTDIR:-$HOME}/.zshrc" "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
	[ -f "$rc" ] || continue
	case " $rcfiles " in *" $rc "*) continue ;; esac
	if grep -q 'urd hook' "$rc" 2>/dev/null; then
		rcfiles="$rcfiles $rc"
	fi
done

dirs=""
for d in "$CONFIG_DIR" "$DATA_DIR"; do
	[ -d "$d" ] && dirs="$dirs $d"
done

# Globs are expanded here, so an unmatched pattern never reaches rm as a literal path.
runtime=""
for f in "$SOCK" "$SOCK.pid"; do
	[ -e "$f" ] && runtime="$runtime $f"
done

backups=""
for f in "$HOME"/.zshrc.urd-bak-* "$HOME"/.bashrc.urd-bak-* "$CONFIG_DIR"/config.toml.urd-bak-*; do
	[ -e "$f" ] && backups="$backups $f"
done

dumps=""
for f in "$HOME"/urd_history_*; do
	[ -e "$f" ] && dumps="$dumps $f"
done

show() {
	label=$1
	shift
	[ $# -eq 0 ] && return 0
	echo "$label"
	for item in "$@"; do
		echo "    $item"
	done
}

echo "urd uninstall plan"
echo
# shellcheck disable=SC2086
show "  stop the daemon and remove:" $runtime
# shellcheck disable=SC2086
show "  delete binaries:" $binaries
# shellcheck disable=SC2086
show "  delete directories (index, imported history, config):" $dirs
# shellcheck disable=SC2086
show "  drop the 'urd hook' line from:" $rcfiles
# shellcheck disable=SC2086
show "  delete backups urd left behind:" $backups

if [ -n "$dumps" ]; then
	echo
	echo "  KEPT - your own history dumps, delete them yourself if you want to:"
	# shellcheck disable=SC2086
	for item in $dumps; do echo "    $item"; done
fi

if [ -z "$binaries$dirs$rcfiles$runtime$backups" ]; then
	echo "  nothing found - urd is not installed here"
	exit 0
fi

if [ "$DRY" -eq 1 ]; then
	echo
	echo "dry run, nothing was touched"
	exit 0
fi

if [ "$YES" -eq 0 ]; then
	echo
	printf 'proceed? [y/N] '
	read -r reply </dev/tty
	case $reply in
	y | Y | yes | YES) ;;
	*)
		echo "cancelled"
		exit 1
		;;
	esac
fi

echo

# Signalled only if line 2 of the pid file still names a urd binary: a PID gets reused,
# and killing a stranger is worse than leaving a daemon to its idle timeout.
if [ -f "$SOCK.pid" ]; then
	pid=$(sed -n 1p "$SOCK.pid" 2>/dev/null || true)
	exe=$(sed -n 2p "$SOCK.pid" 2>/dev/null || true)
	case $exe in
	*/urd)
		if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
			echo "stopping daemon $pid ($exe)"
			kill "$pid" 2>/dev/null || true
			# Long enough for the kernel to close the socket, short enough not to hang.
			i=0
			while [ "$i" -lt 20 ] && kill -0 "$pid" 2>/dev/null; do
				sleep 0.1
				i=$((i + 1))
			done
		fi
		;;
	*)
		[ -n "$exe" ] && echo "pid file names $exe, not a urd binary - not signalling"
		;;
	esac
fi

# Anything still serving from a binary about to be deleted: matched by full path,
# never by the bare name - pkill -f also matches environment variables.
for bin in $binaries; do
	if command -v pgrep >/dev/null 2>&1; then
		for pid in $(pgrep -f "^$bin serve" 2>/dev/null || true); do
			echo "stopping daemon $pid ($bin serve)"
			kill "$pid" 2>/dev/null || true
		done
	fi
done

# Existence is rechecked: a config backup goes away with the directory above it, and
# "removed" about a file that was already gone is output nobody can audit.
drop() {
	for f in "$@"; do
		[ -e "$f" ] || continue
		rm -rf "$f" && echo "removed $f"
	done
}

# shellcheck disable=SC2086
drop $runtime
# shellcheck disable=SC2086
drop $binaries
# shellcheck disable=SC2086
drop $dirs

for rc in $rcfiles; do
	# A copy first: the grep below is the only thing deciding which line goes.
	cp "$rc" "$rc.urd-removed-$(date +%Y%m%d-%H%M%S)"
	tmp=$(mktemp)
	grep -v 'urd hook' "$rc" >"$tmp" || true
	cat "$tmp" >"$rc"
	rm -f "$tmp"
	echo "cleaned $rc (copy kept as $rc.urd-removed-*)"
done

# shellcheck disable=SC2086
drop $backups

echo
echo "done. Open a new shell: the widget stays live in this one until you do."
