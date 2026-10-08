#!/bin/sh
# Installs lxcpeek from the latest release:
#
#   curl -fsSL https://raw.githubusercontent.com/instantnodeeu/lxcpeek/main/install.sh | sh
#
# As root it goes to /usr/local/bin, otherwise to ~/.local/bin. BINDIR picks
# another directory, VERSION=v0.1.0 a specific release. Run it again to update.
set -eu

REPO=instantnodeeu/lxcpeek
NAME=lxcpeek

die() { printf 'install: %s\n' "$*" >&2; exit 1; }

# fetch <url> <file>
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "needs curl or wget"
	fi
}

[ "$(uname -s)" = Linux ] || die "release builds are for linux only, use go install on $(uname -s)"
case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "no release build for $(uname -m), use go install" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

version=${VERSION:-}
if [ -z "$version" ]; then
	fetch "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest"
	version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest")
	[ -n "$version" ] || die "could not find the latest release, set VERSION=v0.1.0"
fi

asset=$NAME-linux-$arch
url=https://github.com/$REPO/releases/download/$version
fetch "$url/$asset" "$tmp/$asset" || die "download failed: $url/$asset"
fetch "$url/sha256sums.txt" "$tmp/sums" || die "download failed: $url/sha256sums.txt"
want=$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/sums")
if command -v sha256sum >/dev/null 2>&1; then sum=sha256sum
elif command -v shasum >/dev/null 2>&1; then sum="shasum -a 256"
else die "needs sha256sum or shasum to check the download"; fi
got=$($sum "$tmp/$asset" | awk '{ print $1 }')
[ -n "$want" ] && [ "$got" = "$want" ] || die "checksum mismatch for $asset, not installing it"

if [ -z "${BINDIR:-}" ]; then
	if [ "$(id -u)" = 0 ] || [ -w /usr/local/bin ]; then BINDIR=/usr/local/bin; else BINDIR=$HOME/.local/bin; fi
fi
mkdir -p "$BINDIR"
# rename over the old binary so a running one isn't touched
cp "$tmp/$asset" "$BINDIR/.$NAME.new"
chmod 755 "$BINDIR/.$NAME.new"
mv -f "$BINDIR/.$NAME.new" "$BINDIR/$NAME"

echo "installed $NAME $version to $BINDIR/$NAME"
case :$PATH: in
	*:"$BINDIR":*) ;;
	*) echo "$BINDIR is not in your PATH, add it or run $BINDIR/$NAME" ;;
esac
echo "run this again to update, rm $BINDIR/$NAME to remove"
