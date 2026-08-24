#!/usr/bin/env bash
#
# Builds the documentation site, removes what must not be published, and verifies the result.
#
# One script so continuous integration and a developer run the same steps. The prune is not
# optional: docs_dir is the repository root, so Zensical copies the whole working tree into the
# output, and it accepts mkdocs.yaml's exclude_docs key while silently ignoring it. docs_dir has
# to stay the root, because that is what makes README.md the home page and lets its links resolve
# identically on GitHub and on the site.
#
# Usage: scripts/build-docs.sh
set -euo pipefail

cd "$(dirname "$0")/.."
SITE_DIR=site

fail() { echo "::error::$1"; exit 1; }

# Resolved by precedence rather than by platform, so one script is correct upstream, in a GitHub
# fork, and in a GitLab copy including a self-managed or disconnected one. A fork that publishes
# with the upstream address would tell search engines its own documentation is a duplicate.
#
#   SITE_URL           an explicit override: a custom domain, or an internal host
#   CI_PAGES_URL       GitLab, predefined per job on gitlab.com and self-managed alike
#   GITHUB_REPOSITORY  GitHub Pages, derived from the owner and repository
#   mkdocs.yaml        the fallback, used outside continuous integration where nothing publishes
if [ -n "${SITE_URL:-}" ]; then
  echo "Building with site_url=${SITE_URL} (explicit)"
elif [ -n "${CI_PAGES_URL:-}" ]; then
  SITE_URL="${CI_PAGES_URL%/}/"
  export SITE_URL
  echo "Building with site_url=${SITE_URL} (GitLab Pages)"
elif [ -n "${GITHUB_REPOSITORY:-}" ]; then
  # Pages hostnames are lowercase; the owner segment is not guaranteed to be.
  owner=$(printf '%s' "${GITHUB_REPOSITORY%%/*}" | tr '[:upper:]' '[:lower:]')
  SITE_URL="https://${owner}.github.io/${GITHUB_REPOSITORY#*/}/"
  export SITE_URL
  echo "Building with site_url=${SITE_URL} (GitHub Pages)"
else
  echo "Building with site_url from mkdocs.yaml"
fi

# Always clean. Zensical's incremental build is not coherent with an output directory that
# changed underneath it, and this script prunes that directory on every run: an incremental build
# afterwards reported eighteen pages as missing that were present, then none on a second run.
# With strict: true that surfaces as a failing build for no reason. A clean build costs a second.
#
# mkdocs.yaml sets strict: true, so a broken link or a broken heading anchor fails here.
zensical build --clean -f mkdocs.yaml

# The site is the README, rendered as the home page, plus docs/. Nothing else.
#
# An allow-list rather than a deny-list, and the direction is the point. docs_dir is the
# repository root, so Zensical copies every file in the working tree into the output, and it
# accepts mkdocs.yaml's exclude_docs key while silently ignoring it. Default-deny means a new file
# at the root, a credential or a tool cache included, is never published because nobody
# remembered to exclude it.
python3 - "$SITE_DIR" <<'PRUNE_TO_DOCS'
import json
import pathlib
import sys

site = pathlib.Path(sys.argv[1])

# Emitted by the generator, so there is no source file behind them.
KEEP_FILES = {
    "index.html", "404.html", "search.json",
    "sitemap.xml", "sitemap.xml.gz", "objects.inv", ".nojekyll",
}
KEEP_TREES = ("docs/", "assets/")

removed = 0
for path in sorted(site.rglob("*")):
    if not path.is_file():
        continue
    rel = path.relative_to(site).as_posix()
    if rel in KEEP_FILES or rel.startswith(KEEP_TREES):
        continue
    path.unlink()
    removed += 1

# The search index is generated before this script prunes anything, so it still carries an entry,
# with the page's full body text, for every page just removed. Left alone it both serves results
# that 404 and republishes the content of pages deliberately taken off the site.
index = site / "search.json"
if index.exists():
    data = json.loads(index.read_text())
    items = data.get("items", [])

    def published(location: str) -> bool:
        path = location.split("#")[0]
        return (site / (path + "index.html" if path.endswith("/") or not path else path)).exists()

    kept = [i for i in items if published(i.get("location", ""))]
    if len(kept) != len(items):
        data["items"] = kept
        index.write_text(json.dumps(data))
        print(f"Dropped {len(items) - len(kept)} search entries for pages that are not published")

print(f"Removed {removed} files that are neither the home page nor docs/")
PRUNE_TO_DOCS

find "$SITE_DIR" -type d -empty -delete

# Fails closed. Recomputed from the output rather than assumed, so a change in what Zensical emits
# shows up here as an error instead of as a surprise on the published site.
unexpected=$(cd "$SITE_DIR" && find . -type f | sed 's|^\./||' \
  | grep -vE '^(docs|assets)/' \
  | grep -vE '^(index|404)\.html$|^(search\.json|sitemap\.xml|sitemap\.xml\.gz|objects\.inv|\.nojekyll)$' \
  || true)
[ -z "$unexpected" ] || fail "unexpected files in the published tree: ${unexpected}"

# The crawl that found this: search results pointed at pages the prune had removed, and carried
# their body text with them. Asserted rather than trusted, because the index is generated upstream
# of the prune and will drift again if Zensical changes what it emits.
python3 - "$SITE_DIR" <<'CHECK_SEARCH_INDEX'
import json
import pathlib
import sys

site = pathlib.Path(sys.argv[1])
index = site / "search.json"

missing = []
if index.exists():
    for item in json.loads(index.read_text()).get("items", []):
        location = item.get("location", "").split("#")[0]
        page = location + "index.html" if location.endswith("/") or not location else location
        if not (site / page).exists():
            missing.append(location)

if missing:
    print(f"::error::search index references unpublished pages: {sorted(set(missing))[:5]}")
    sys.exit(1)
CHECK_SEARCH_INDEX

# Everything published now comes from docs/, so this only guards a credential committed there.
# gitleaks is the real defence; this is one cheap layer more.
leaked=$(find "$SITE_DIR/docs" -type f \
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
