#!/usr/bin/env bash
# Homebrew end-to-end test of the formula a release would publish.
#
# Takes a dist directory written by
#   hephaestus run --stages build,archive,checksum,formula --tag vX.Y.Z --fresh
# (`make homebrew-e2e` runs it, CI does it in ci.yml), points the generated
# formula dist/homebrew/enma.rb at the local archives (file:// URLs instead of
# the GitHub release that does not exist yet; the sha256 lines are kept), and
# runs the real Homebrew in a throw-away tap: brew style, brew audit --strict,
# brew install, the installed binary and its bash, zsh and fish completions,
# brew test, brew uninstall.
#
# It installs into the machine's Homebrew prefix and removes what it created
# on exit. It refuses to run when a formula named enma is already installed,
# so a workstation's own installation is never touched; intended for CI
# runners and containers.
#
# usage: scripts/homebrew-e2e.sh [dist-dir]
set -euo pipefail
export LC_ALL=C

dist="$(cd "${1:-dist}" && pwd)"
formula_src="$dist/homebrew/enma.rb"
tap="enma-e2e/t$RANDOM$RANDOM"

export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_DEVELOPER=1 HOMEBREW_NO_ASK=1 \
  HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK=1 \
  HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1

step() { printf '\n==> %s\n' "$*"; }
fail() { echo "homebrew-e2e: $*" >&2; exit 1; }

[[ -f "$formula_src" ]] || fail "no $formula_src (run the formula stage first)"
command -v brew >/dev/null || fail "brew is not installed"
if brew list --formula enma >/dev/null 2>&1; then
  fail "a formula named enma is already installed; run this test on a disposable machine"
fi

# Homebrew 7.0.11 and later load a formula of a tap that is not official only
# once it is trusted (brew trust): brew audit refuses the formula of an
# untrusted tap. Only the throw-away tap is trusted, while it exists (when
# this Homebrew has brew trust); brew untrust also forgets the formula brew
# install trusts.
trusts=false
brew trust --help >/dev/null 2>&1 && trusts=true

cleanup() {
  set +e
  brew uninstall --force "$tap/enma" >/dev/null 2>&1
  if [[ "$trusts" == true ]]; then brew untrust --tap "$tap" >/dev/null 2>&1; fi
  brew untap --force "$tap" >/dev/null 2>&1
}
trap cleanup EXIT

# The release the formula was generated for: its URLs name the tag.
tag="$(sed -n 's|.*/releases/download/\([^/]*\)/enma_.*|\1|p' "$formula_src" | sort -u)"
[[ -n "$tag" && "$(wc -l <<<"$tag")" -eq 1 ]] || fail "cannot find the one release tag in $formula_src"
version="${tag#v}"

step "Formula for $tag, pointed at $dist"
brew tap-new --no-git "$tap" >/dev/null
if [[ "$trusts" == true ]]; then brew trust --tap "$tap"; fi
formula="$(brew --repository "$tap")/Formula/enma.rb"
# The download URLs become local files; everything else is the generated
# formula as it would be published. Homebrew reads the version from the
# GitHub release URL, so it is stated once the URLs are local.
sed -e "s|https://github.com/magicdrive/enma/releases/download/$tag/|file://$dist/|" \
  -e "s|^  homepage \(.*\)$|  homepage \1\\
  version \"$version\"|" \
  "$formula_src" >"$formula"
grep -q 'releases/download' "$formula" && fail "a URL was not replaced"
cat "$formula"

step "brew style"
brew style --formula "$tap/enma"

step "brew audit --strict"
brew audit --strict --formula "$tap/enma"

step "brew install"
brew install --formula "$tap/enma"

step "Installed binary and completions"
prefix="$(brew --prefix)"
out="$("$prefix/bin/enma" --version)"
[[ "$out" == "enma version $tag" ]] || fail "enma --version printed '$out', want 'enma version $tag'"
echo "$out"
keg="$(brew --prefix "$tap/enma")"
for f in etc/bash_completion.d/enma share/zsh/site-functions/_enma share/fish/vendor_completions.d/enma.fish; do
  [[ -f "$keg/$f" ]] || fail "missing $f in the keg"
  [[ -e "$prefix/$f" ]] || fail "$f is not linked into $prefix"
  echo "ok   $f"
done
# the files are the scripts of the repository, unchanged
repo="$(cd "$(dirname "$0")/.." && pwd)"
cmp "$keg/etc/bash_completion.d/enma" "$repo/misc/completions/bash/enma-completion.bash"
cmp "$keg/share/zsh/site-functions/_enma" "$repo/misc/completions/zsh/_enma"
cmp "$keg/share/fish/vendor_completions.d/enma.fish" "$repo/misc/completions/fish/enma.fish"

step "brew test"
brew test "$tap/enma"

step "brew uninstall"
brew uninstall "$tap/enma"
[[ ! -e "$prefix/bin/enma" ]] || fail "$prefix/bin/enma is still there"

echo
echo "Homebrew E2E passed for enma $tag on $(uname -s)/$(uname -m)"
