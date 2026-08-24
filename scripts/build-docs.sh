#!/usr/bin/env bash
#
# Builds the documentation site, prunes it to what should be published, and verifies the result.
# Run by continuous integration and by hand, so both do the same thing.
#
# Usage: scripts/build-docs.sh
set -euo pipefail

cd "$(dirname "$0")/.."
SITE_DIR=site

fail() { echo "::error::$1"; exit 1; }

# By precedence, so a fork publishes its own canonical URLs rather than pointing search engines
# at upstream. Explicit SITE_URL wins, then GitLab, then GitHub, then the mkdocs.yaml fallback.
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

# --clean is required: an incremental build is incoherent with the output directory this script
# prunes, and reports phantom missing pages. strict: true fails the build on a bad link or anchor.
zensical build --clean -f mkdocs.yaml

# The site is the home page plus docs/. Prune after the build because Zensical copies the whole
# working tree and ignores exclude_docs. Allow-list, so anything new at the root stays unpublished.
python3 - "$SITE_DIR" <<'PRUNE_TO_DOCS'
import json
import pathlib
import sys

site = pathlib.Path(sys.argv[1])

# Generator output, with no source file behind it.
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

# search.json is generated before the prune and keeps an entry, with body text, for every page
# removed. Filter it or search returns 404s and republishes unpublished content.
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

# Fails closed if Zensical starts emitting something new at the root.
unexpected=$(cd "$SITE_DIR" && find . -type f | sed 's|^\./||' \
  | grep -vE '^(docs|assets)/' \
  | grep -vE '^(index|404)\.html$|^(search\.json|sitemap\.xml|sitemap\.xml\.gz|objects\.inv|\.nojekyll)$' \
  || true)
[ -z "$unexpected" ] || fail "unexpected files in the published tree: ${unexpected}"

# The index is generated upstream of the prune, so verify it rather than trust the filter above.
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

# A cheap second layer under gitleaks, for a credential committed into docs/.
leaked=$(find "$SITE_DIR/docs" -type f \
  \( -name '*secret*' -o -name '*.key' -o -name '*.pem' -o -name 'kubeconfig*' \) \
  ! -name '*.html' -print)
[ -z "$leaked" ] || fail "a credential-shaped file reached the output: ${leaked}"

# A theme or extension setting that stops applying still builds clean and ships a wrong site.
# One check per setting.

# The style guide documents the syntax literally, so it holds the marker legitimately.
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
