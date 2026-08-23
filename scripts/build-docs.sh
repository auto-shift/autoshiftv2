#!/usr/bin/env bash
#
# Builds the documentation site, removes what must not be published, and verifies the result.
#
# One script so continuous integration and a developer run the same steps. Two properties of this
# site make the prune necessary rather than optional: docs_dir is the repository root, so every
# file in the working tree is copied into the output, and Zensical accepts mkdocs.yaml's
# exclude_docs key while silently ignoring it. Pruning afterwards is the only control there is.
#
# Usage: scripts/build-docs.sh
set -euo pipefail

cd "$(dirname "$0")/.."
SITE_DIR=site

fail() { echo "::error::$1"; exit 1; }

# Derived so a fork publishes its own canonical URLs and sitemap without editing mkdocs.yaml.
# Outside continuous integration the fallback in mkdocs.yaml applies, which is this site's real
# address, so a local build matches production.
if [ -n "${GITHUB_REPOSITORY:-}" ]; then
  owner=$(echo "${GITHUB_REPOSITORY%%/*}" | tr '[:upper:]' '[:lower:]')
  SITE_URL="https://${owner}.github.io/${GITHUB_REPOSITORY#*/}/"
  export SITE_URL
  echo "Building with site_url=${SITE_URL}"
fi

# Always clean. Zensical's incremental build is not coherent with an output directory that
# changed underneath it, and this script prunes that directory on every run: an incremental build
# afterwards reported eighteen pages as missing that were present, then none on a second run.
# With strict: true that surfaces as a failing build for no reason. A clean build costs a second.
#
# mkdocs.yaml sets strict: true, so a broken link or a broken heading anchor fails here.
zensical build --clean -f mkdocs.yaml

# Two different problems, handled two different ways.
#
# 1. Output with no source in the repository. A working tree holds what a fresh checkout does not:
#    a pull secret, a personal instructions file, downloaded tool binaries, caches. Git already
#    knows which files those are, so this is derived rather than listed. Deriving it matters: a
#    list only excludes what someone remembered to add.
#
#    The set is "tracked, plus new files, minus ignored files", so work in progress still previews
#    before it is committed. In continuous integration the two are the same thing, because a fresh
#    checkout has nothing untracked.
python3 - "$SITE_DIR" <<'PRUNE_UNTRACKED'
import pathlib, subprocess, sys

site = pathlib.Path(sys.argv[1])
known = set(subprocess.run(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard"],
    capture_output=True, text=True, check=True).stdout.split("\n"))

# Produced by the generator, so there is no source file to match against.
generated = {"search.json", "sitemap.xml", "sitemap.xml.gz", "404.html", "objects.inv", ".nojekyll"}

removed = 0
for path in sorted(site.rglob("*")):
    if not path.is_file():
        continue
    rel = path.relative_to(site).as_posix()
    if rel in generated or rel.startswith("assets/"):
        continue
    if path.name == "index.html":
        # A rendered page. Keep it only if the Markdown it came from is in the repository,
        # otherwise a gitignored file such as CLAUDE.md publishes as a page.
        parent = path.parent.relative_to(site).as_posix()
        sources = ["README.md"] if parent == "." else [f"{parent}.md", f"{parent}/README.md"]
        if any(source in known for source in sources):
            continue
    elif rel in known:
        continue
    path.unlink()
    removed += 1

print(f"Removed {removed} files with no source in the repository")
PRUNE_UNTRACKED

# 2. Files that are in the repository but are plumbing rather than documentation. These have to be
#    named, because nothing distinguishes them automatically. They change rarely.
PRUNE=(
  .git .github .devcontainer
  .gitattributes .gitignore .gitlab-ci.yml .gitleaks.toml .shellcheckrc .vale.ini
  Makefile renovate.json mkdocs.yaml
  # Instructions for coding agents, not documentation for readers. Nothing links to them, so
  # removing the rendered pages leaves no broken link behind.
  AGENTS CLAUDE
)
for path in "${PRUNE[@]}"; do
  rm -rf "${SITE_DIR:?}/${path}"
done
find "$SITE_DIR" -type d -empty -delete

for path in "${PRUNE[@]}"; do
  [ ! -e "${SITE_DIR}/${path}" ] || fail "prune did not remove ${path} from the output"
done

# Zensical copies files that have an extension and skips ones that do not, so LICENSE is never
# published and the README badge that links to it 404s. The strict build does not catch this: its
# link validation only covers pages. Copy it after the prune so nothing can remove it again.
cp LICENSE "$SITE_DIR/LICENSE"
[ -f "$SITE_DIR/LICENSE" ] || fail "LICENSE was not published, so the README badge will 404"

# Guards the case the sweep cannot: a credential that someone committed, so git considers it a
# legitimate tracked file. gitleaks is the real defence there; this is one cheap layer more.
leaked=$(find "$SITE_DIR" -maxdepth 1 -type f \
  \( -name '*secret*' -o -name '*.key' -o -name '*.pem' -o -name 'kubeconfig*' \) \
  ! -name '*.html' -print)
[ -z "$leaked" ] || fail "a credential-shaped file reached the output: ${leaked}"

# The build only fails on links and anchors, so a Markdown extension or theme setting that stops
# applying still produces a clean build and a wrong site. Each check stands for one such setting,
# and each was confirmed to fail when that setting is removed.

# The style guide documents the syntax literally, so it legitimately holds the marker.
raw=$(grep -rlE '\[!(NOTE|TIP|WARNING|IMPORTANT|CAUTION)\]' "$SITE_DIR" --include='*.html' \
  | grep -v 'docs/documentation-style/' || true)
[ -z "$raw" ] || fail "callout syntax reached the output (${raw}): pymdownx.quotes is not applying"
grep -rq 'class="admonition' "$SITE_DIR" --include='*.html' \
  || fail "no admonitions rendered: pymdownx.quotes is not applying"

grep -q '<loc>' "$SITE_DIR/sitemap.xml" || fail "sitemap.xml has no URLs: site_url is not taking effect"
grep -q 'rel="canonical"' "$SITE_DIR/index.html" || fail "no canonical URL: site_url is not taking effect"

grep -rq 'class="mermaid"' "$SITE_DIR" --include='*.html' \
  || fail "no mermaid blocks rendered: superfences custom_fences is not applying"

grep -q 'red-hat.css' "$SITE_DIR/index.html" || fail "brand stylesheet is not linked"
test -f "$SITE_DIR/docs/assets/red-hat.css" || fail "brand stylesheet was not published"

echo "Site built, pruned and verified in ${SITE_DIR}/"
