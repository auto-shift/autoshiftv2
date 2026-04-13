package labels

import (
	"fmt"
	"io"
	"strings"
)

// WriteText writes a human-readable summary of the report to w.
// `verbose` includes the per-bucket key list. `showPolicies` adds the list of
// policies that reference each key (only shown in verbose mode).
func WriteText(w io.Writer, r Report, verbose, showPolicies bool) {
	total := len(r.Entries)
	fmt.Fprintf(w, "Label contract summary (%d unique keys)\n", total)
	fmt.Fprintf(w, "  OK:       %4d\n", len(r.OK))
	fmt.Fprintf(w, "  missing:  %4d\n", len(r.Missing))
	fmt.Fprintf(w, "  orphaned: %4d\n", len(r.Orphaned))
	fmt.Fprintln(w)

	writeBucket := func(title string, bucket []Entry, explain string) {
		if len(bucket) == 0 {
			return
		}
		fmt.Fprintf(w, "== %s (%d) ==\n", title, len(bucket))
		if explain != "" {
			fmt.Fprintf(w, "%s\n", explain)
		}
		for _, e := range bucket {
			fmt.Fprintf(w, "  %s\n", e.Key)
			if verbose {
				if showPolicies && e.Consumed != nil {
					pols := e.Consumed.Policies()
					if len(pols) > 0 {
						fmt.Fprintf(w, "      consumed by: %s\n", strings.Join(pols, ", "))
					}
				}
				if e.Declared != nil {
					files := e.Declared.Files()
					if len(files) > 0 {
						fmt.Fprintf(w, "      declared in: %s\n", strings.Join(files, ", "))
					}
				}
			}
		}
		fmt.Fprintln(w)
	}

	writeBucket("missing",
		r.Missing,
		"These labels are referenced by a policy template but are not declared\nin any `_example*.yaml` catalog file. Every label a chart references\nmust be documented in the example catalog.")
	writeBucket("orphaned",
		r.Orphaned,
		"These labels are declared in example files but no policy template\nconsumes them. They may be dead documentation — remove them, or allowlist\nthem if intentional.")
}

// WriteMarkdown writes a GitHub-flavored Markdown report, suitable for uploading
// as a PR-visible artifact.
func WriteMarkdown(w io.Writer, r Report) {
	fmt.Fprintf(w, "# AutoShift label contract report\n\n")
	fmt.Fprintf(w, "| Bucket | Count |\n|---|---|\n")
	fmt.Fprintf(w, "| OK | %d |\n", len(r.OK))
	fmt.Fprintf(w, "| missing | %d |\n", len(r.Missing))
	fmt.Fprintf(w, "| orphaned | %d |\n\n", len(r.Orphaned))

	writeBucket := func(title string, bucket []Entry, withPolicies bool) {
		if len(bucket) == 0 {
			return
		}
		fmt.Fprintf(w, "## %s (%d)\n\n", title, len(bucket))
		if withPolicies {
			fmt.Fprintf(w, "| Label | Consumed by |\n|---|---|\n")
			for _, e := range bucket {
				pols := ""
				if e.Consumed != nil {
					pols = strings.Join(e.Consumed.Policies(), ", ")
				}
				fmt.Fprintf(w, "| `autoshift.io/%s` | %s |\n", e.Key, pols)
			}
		} else {
			fmt.Fprintf(w, "| Label | Declared in |\n|---|---|\n")
			for _, e := range bucket {
				files := ""
				if e.Declared != nil {
					files = strings.Join(e.Declared.Files(), ", ")
				}
				fmt.Fprintf(w, "| `autoshift.io/%s` | %s |\n", e.Key, files)
			}
		}
		fmt.Fprintln(w)
	}

	writeBucket("missing", r.Missing, true)
	writeBucket("orphaned", r.Orphaned, false)
}
