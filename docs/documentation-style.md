# Documentation style

`README.md` and everything under `docs/` are linted with Red Hat's own Vale style,
`vale-at-red-hat`. Continuous integration fails on error-level findings only. Everything below that
level is reported as a visible backlog and does not block a merge. Which rules are promoted to
error, and why, is recorded in `.vale.ini` next to each rule.

Run both checks before opening a pull request that touches documentation:

```bash
vale sync && vale --minAlertLevel=error README.md docs/
./scripts/build-docs.sh
```

> [!WARNING]
> The build does not validate a link to anything that is not a page. Zensical copies files that
> have an extension, so a link to a diagram or an example YAML file resolves, but a file with no
> extension is never copied and 404s on the published site while the build still reports success.
> `LICENSE` is the one case in the repository, and `scripts/build-docs.sh` copies it explicitly and
> fails if it is missing. Adding a link to another extensionless file needs the same treatment.

The second command matters as much as the first. It builds the site, removes the files that must
not be published, and checks the result. `mkdocs.yaml` sets `strict: true`, so a warning
fails the build, and a broken link is a warning. Nothing else catches a link that points at a page
which has moved. Zensical also validates heading anchors, so a link into a renamed heading fails
too.

Install the site generator with `pip install -r docs/requirements.txt`, which pins the same
version continuous integration builds and publishes with. Dependabot raises a pull request when a
new release appears. Zensical is before version 1.0, so read the release note before merging one.

## Getting Vale

Neither the linter nor the style rules are in the repository. A fresh clone contains exactly two
Vale files:

| Path | Tracked | What it is |
|---|---|---|
| `.vale.ini` | yes | Configuration: which rules gate the build, and why |
| `.vale/styles/config/vocabularies/AutoShift/accept.txt` | yes | The vocabulary, which is ours |
| `.vale/styles/RedHat/` | no | The Red Hat rule package, 46 files, fetched by `vale sync` |

`.gitignore` excludes `.vale/styles/*` and re-includes `.vale/styles/config/`, because the rule
package is a build dependency rather than source. Committing it would mean reviewing 46 vendored
files on every upstream release.

Install the linter, then fetch the rules:

```bash
brew install vale        # or download the release binary for your platform
vale sync                # reads Packages from .vale.ini and downloads the style
```

Continuous integration pins Vale itself to **3.17.1** and installs it by downloading the release
binary, so that a developer runs the same command that the build runs. Match that version locally
when a finding is difficult to reproduce.

Skipping `vale sync` does not produce a clean run. It produces this:

```
E100 [loadStyles] Runtime error

style 'RedHat' does not exist on StylesPath
```

> [!IMPORTANT]
> The rule package is not version-pinned. `.vale.ini` declares `Packages = RedHat`, which resolves
> through the Vale registry to the latest `vale-at-red-hat` release, and the build runs `vale sync`
> on every run. A new upstream release can therefore turn a clean repository red with no change on
> our side.

When that happens, the finding is usually genuine, so fix the prose. If a new rule is wrong for an
infrastructure repository, set it to `NO` in `.vale.ini` with the reason recorded next to it, the
way `RedHat.ConsciousLanguage` is handled. Do not answer a rule change by adding words to the
vocabulary file. To take the change deliberately rather than on the next build, replace the
`Packages` value with the URL of a specific release archive.

## Rules that fail the build

Eight rules are promoted to error in `.vale.ini`. Each was promoted only after the repository was
already clean against it, so an error means a regression that this change introduced.

| Rule | What it enforces |
|---|---|
| `RedHat.TermsErrors` | Banned terminology that has an unambiguous replacement |
| `RedHat.Contractions` | No contractions: write `cannot`, `does not`, `it is` |
| `RedHat.Hyphens` | Hyphenation, including `re-create` rather than `recreate` |
| `RedHat.Using` | `by using` rather than `using` when it follows a noun |
| `RedHat.Conjunctions` | No sentence-opening `So` or `Or`. Use `Therefore` or `Alternatively` |
| `RedHat.RepeatedWords` | A word accidentally repeated |
| `RedHat.ReleaseNotes` | Release-note phrasing |
| `RedHat.ProductCentricWriting` | Describe what the reader does, not what the product does |

The banned terms that come up most often in this repository, with the replacement Vale asks for:

| Do not write | Vale wants |
|---|---|
| `IPI` | `installer-provisioned infrastructure` |
| `UPI` | `user-provisioned infrastructure` |
| `hardcoded` | `hard-coded` |
| `typo` | `typing error` or `typographical error` |
| `vs` | `versus` or `compared to` |
| `a number of` | `several` |
| `trust store` | `truststore` |
| `would like` | `want` |
| `catalogue` | `catalog` |
| `labelled` | `labeled` |
| `sanity check` | `test`, `validate`, `verify` |
| a definite-article reference to an installer | `installation program` |
| `bare metal` before a noun | `bare-metal` |

The full list lives in `.vale/styles/RedHat/TermsErrors.yml` after `vale sync`. It is a substitution
rule, so the message always names the exact replacement.

## Rules that are reported but do not block

These are the current backlog, largest first. `.vale.ini` records the reason each is still open.
House style is to follow them anyway, so the backlog shrinks rather than grows.

- **Headings** use sentence-style capitalization. Renaming a heading moves its anchor and breaks
  inbound links, so clearing this rule needs a deliberate pass rather than a sweep.
- **No em dashes.** The `README.md` file is clean, and roughly 80 remain in `docs/` prose. Use a
  comma, a colon, parentheses, or split the sentence. A pair of em dashes is a parenthetical, so
  rewrite both halves together rather than one at a time.
- **Product names carry the `Red Hat` prefix.** Write `Red Hat Advanced Cluster Management for
  Kubernetes` on first use in a document and `Red Hat Advanced Cluster Management` after that. The
  same applies to `Red Hat OpenShift Data Foundation` and `Red Hat Advanced Cluster Security for
  Kubernetes`. `.vale.ini` deliberately does not add the short form to the vocabulary, because
  accepting it would silence 99 findings in one line and hide the rule rather than answer it.
- **Abbreviations** are defined on first use as `Full Name (ABBR)`, and only when the abbreviation is
  reused later. If it appears once, write the full name.
- **Kubernetes kinds belong in backticks**, which also improves the rendered page.
- **Passive voice** reads case by case, and is often correct when the actor is a controller.
- **No gerunds in titles**: `Install X` rather than `Installing X`. This also moves anchors.
- **`above` and `below`** for position in a document are flagged at suggestion level. Use `earlier`,
  `preceding`, `following`, or `that follows`. Keep them where the meaning is genuinely spatial or
  numeric, as in `at or above the target`.
- **`via`** is flagged at suggestion level. Use `through`, `by`, or `by using`.
- **American spelling** throughout.

`RedHat.ConsciousLanguage` is the one rule turned off. `master` is the name of an OpenShift node
role, `node-role.kubernetes.io/master`, and `blue/green` is the deployment strategy rather than a
description of color. Every finding the rule produced here was a false positive.

## Callouts and diagrams

Callouts use GitHub syntax, which the `callouts` plugin converts for the published site, so they
render both on GitHub and in the documentation site:

```markdown
> [!NOTE]
> Something worth knowing.

> [!IMPORTANT]
> Something that breaks if ignored.
```

Do not use the `!!! note` admonition syntax. It renders on the site and not on GitHub.

For diagrams, use draw.io for the canonical architecture pictures and mermaid for an inline
explanation inside a page. A mermaid `classDef` fill must also set an explicit `color:`, or the
label text is unreadable in dark mode. The house style for the draw.io diagrams is documented in
[docs/diagrams/README.md](diagrams/README.md).

## The vocabulary file

`.vale/styles/config/vocabularies/AutoShift/accept.txt` exempts terms that Vale does not recognize:
product names it does not know, Kubernetes kinds, tool names, and the callout keywords that the
`> [!TIP]` syntax otherwise trips. Add a term there only when it is a genuine name. Never add one to
silence a rule you do not want to follow. Every existing entry was verified to suppress a real
finding, and entries that Vale already knew have been removed.

## Editing prose in bulk

Scripted find and replace over prose has broken this documentation set repeatedly. A pattern that
looks safe matches inside a code block, a URL, or a longer word, and the damage is not visible in a
rendered preview. When a term has to change across many files, match whole terms only, exclude code
blocks, and read the resulting diff in full before committing.
