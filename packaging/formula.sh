#!/bin/sh
# Renders packaging/urd.rb, which is a template, into a real formula on stdout.
#
#   sh packaging/formula.sh v0.1.0 dist > dist/urd.rb
set -eu

tag=${1:?usage: formula.sh <tag, e.g. v0.1.0> [dist dir]}
dist=${2:-dist}
sums="$dist/checksums.txt"
[ -f "$sums" ] || {
	echo "formula.sh: $sums not found - run 'make dist' first" >&2
	exit 1
}

repo=${GITHUB_REPOSITORY:-ristir/urd}
base="https://github.com/$repo/releases/download/$tag"
template=$(dirname "$0")/urd.rb

# Read in the main shell, not inside the sed arguments: an exit from a command
# substitution ends only that subshell, and the first version of this emitted a formula
# with four empty sha256 lines and a zero status.
for platform in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
	file="urd_${tag}_${platform}.tar.gz"
	# Both sha256sum and shasum put the name last, sometimes with a "*" for binary mode.
	hash=$(awk -v f="$file" '$NF == f || $NF == "*" f {print $1}' "$sums")
	if [ -z "$hash" ]; then
		echo "formula.sh: no checksum for $file in $sums" >&2
		exit 1
	fi
	eval "sha_$platform=\$hash"
done

out=$(sed \
	-e "s|@@VERSION@@|${tag#v}|g" \
	-e "s|@@URL_DARWIN_ARM64@@|$base/urd_${tag}_darwin_arm64.tar.gz|g" \
	-e "s|@@SHA_DARWIN_ARM64@@|$sha_darwin_arm64|g" \
	-e "s|@@URL_DARWIN_AMD64@@|$base/urd_${tag}_darwin_amd64.tar.gz|g" \
	-e "s|@@SHA_DARWIN_AMD64@@|$sha_darwin_amd64|g" \
	-e "s|@@URL_LINUX_ARM64@@|$base/urd_${tag}_linux_arm64.tar.gz|g" \
	-e "s|@@SHA_LINUX_ARM64@@|$sha_linux_arm64|g" \
	-e "s|@@URL_LINUX_AMD64@@|$base/urd_${tag}_linux_amd64.tar.gz|g" \
	-e "s|@@SHA_LINUX_AMD64@@|$sha_linux_amd64|g" \
	"$template")

# A placeholder that survived would install the wrong thing quietly.
case $out in
*'@''@'*)
	echo "formula.sh: a placeholder was left unsubstituted" >&2
	exit 1
	;;
esac

printf '%s\n' "$out"
