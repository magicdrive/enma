#!/bin/sh
# Checks the archives a GoReleaser run wrote (e.g. `goreleaser release
# --snapshot --clean --skip=publish`), without publishing anything:
#   - the checksum file lists every archive, and each one matches it;
#   - each platform the release builds has exactly one archive holding the
#     binary, LICENSE, README.md and the completion scripts;
#   - each binary is built for the platform its archive names (when `file`
#     exists), and the tar.gz binaries are executable;
#   - the binary for this machine runs and prints the expected version.
#
# usage: scripts/verify-dist.sh [dist-dir] [expected-version-substring]
set -eu

dist=${1:-dist}
want_version=${2:-}
fail() { echo "verify-dist: $*" >&2; exit 1; }

[ -d "$dist" ] || fail "no directory $dist"
cd "$dist"

sums=enma_checksums.txt
[ -f "$sums" ] || fail "no $sums"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum -c "$sums" >/dev/null || fail "checksum mismatch"
else
	shasum -a 256 -c "$sums" >/dev/null || fail "checksum mismatch"
fi
for a in enma_*.tar.gz enma_*.zip; do
	[ -f "$a" ] || continue
	awk -v f="$a" '$2 == f { found = 1 } END { exit !found }' "$sums" || fail "$a is not in $sums"
done
echo "ok   checksums ($(wc -l <"$sums" | tr -d ' ') files)"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

host_os=$(go env GOOS 2>/dev/null || uname -s | tr 'A-Z' 'a-z')
host_arch=$(go env GOARCH 2>/dev/null || uname -m)
case "$host_arch" in x86_64) host_arch=amd64 ;; aarch64) host_arch=arm64 ;; esac
host=${host_os}_$host_arch
ran=no

# The first five are the supported platforms (README "Platforms" says which of
# them CI tests); the 386 and windows/arm64 builds are published as well
# (GoReleaser's default architectures) but not tested.
for target in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 \
	linux_386 windows_386 windows_arm64; do
	os=${target%_*}
	arch=${target#*_}
	if [ "$os" = windows ]; then ext=zip bin=enma.exe; else ext=tar.gz bin=enma; fi
	set -- enma_*_"${target}"*."$ext"
	[ -f "$1" ] || fail "$target: no .$ext archive"
	[ $# -eq 1 ] || fail "$target: $# archives"
	archive=$1

	out="$work/$target"
	mkdir -p "$out"
	if [ "$ext" = zip ]; then
		if command -v unzip >/dev/null 2>&1; then
			unzip -q "$archive" -d "$out"
		elif command -v python3 >/dev/null 2>&1; then
			python3 -I -m zipfile -e "$archive" "$out"
		else
			fail "$archive: need unzip or python3 to read a zip archive"
		fi
	else
		tar -xzf "$archive" -C "$out"
	fi
	for f in "$bin" LICENSE README.md completions/enma-completion.sh \
		completions/zsh/_enma completions/bash/enma-completion.bash; do
		[ -f "$out/$f" ] || fail "$archive: missing $f"
	done
	if [ "$ext" = tar.gz ] && [ ! -x "$out/$bin" ]; then
		fail "$archive: $bin is not executable"
	fi

	if command -v file >/dev/null 2>&1; then
		kind=$(file -b "$out/$bin")
		case "$target" in
		linux_amd64) pat='ELF 64-bit*x86-64*' ;;
		linux_arm64) pat='ELF 64-bit*aarch64*' ;;
		darwin_amd64) pat='Mach-O 64-bit*x86_64*' ;;
		darwin_arm64) pat='Mach-O 64-bit*arm64*' ;;
		windows_amd64) pat='PE32+*x86-64*' ;;
		linux_386) pat='ELF 32-bit*Intel 80386*' ;;
		windows_386) pat='PE32 *Intel 80386*' ;;
		windows_arm64) pat='PE32+*Aarch64*' ;;
		esac
		# shellcheck disable=SC2254
		case "$kind" in $pat) ;; *) fail "$archive: $bin is \"$kind\"" ;; esac
	fi

	if [ "$target" = "$host" ]; then
		v=$("$out/$bin" --version) || fail "$archive: $bin --version failed"
		case "$v" in
		"enma version "*"$want_version"*) ;;
		*) fail "$archive: unexpected version output \"$v\"" ;;
		esac
		echo "ok   $target runs: $v"
		ran=yes
	fi
	echo "ok   $archive"
done

[ "$ran" = yes ] || echo "note: no archive for this machine ($host); the binary was not run"
