//go:build integration

package configkeys

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A component key inside a config: block, live or commented out.
var (
	reConfigOpen = regexp.MustCompile(`^(\s*)config:\s*$`)
	reLiveKey    = regexp.MustCompile(`^(\s*)([A-Za-z][\w-]*):`)
	reCommentKey = regexp.MustCompile(`^(\s*)#\s?([A-Za-z][\w-]*):\s*$`)
)

type keySite struct {
	line     int
	commented bool
}

// TestConfigBlocksDoNotCollide fails when a component appears more than once
// inside the same config: block, counting commented-out blocks as occurrences.
//
// A commented block is invisible to a YAML parser, so the usual duplicate-key
// checks cannot see it. Uncommenting one that shadows a live block produces a
// duplicate mapping key: YAML keeps the last, and the live configuration is
// silently discarded. Documenting a component's settings in exactly one block
// is what keeps that from happening.
func TestConfigBlocksDoNotCollide(t *testing.T) {
	root := repoRoot(t)

	var files []string
	for _, dir := range []string{"clustersets", "clusters"} {
		matches, err := filepath.Glob(filepath.Join(root, "autoshift", "values", dir, "*.yaml"))
		if err != nil {
			t.Fatalf("globbing %s: %v", dir, err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no values files found; the glob is broken")
	}

	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		rel, _ := filepath.Rel(root, file)

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			open := reConfigOpen.FindStringSubmatch(line)
			if open == nil {
				continue
			}
			checked++
			seen := map[string][]keySite{}
			keyIndent := len(open[1]) + 2

			for j := i + 1; j < len(lines); j++ {
				cur := lines[j]
				if strings.TrimSpace(cur) == "" {
					continue
				}
				// A live key at or above the config: indent ends the block.
				if m := reLiveKey.FindStringSubmatch(cur); m != nil && len(m[1]) <= len(open[1]) {
					break
				}
				if m := reLiveKey.FindStringSubmatch(cur); m != nil && len(m[1]) == keyIndent {
					seen[m[2]] = append(seen[m[2]], keySite{line: j + 1})
					continue
				}
				if m := reCommentKey.FindStringSubmatch(cur); m != nil && len(m[1]) == keyIndent {
					seen[m[2]] = append(seen[m[2]], keySite{line: j + 1, commented: true})
				}
			}

			names := make([]string, 0, len(seen))
			for name := range seen {
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				sites := seen[name]
				if len(sites) < 2 {
					continue
				}
				where := make([]string, 0, len(sites))
				for _, s := range sites {
					kind := "live"
					if s.commented {
						kind = "commented"
					}
					where = append(where, fmt.Sprintf("L%d (%s)", s.line, kind))
				}
				t.Errorf("%s: config key %q appears %d times in the config: block at L%d — %s.\n"+
					"  Uncommenting a shadowing block makes a duplicate YAML key and the live\n"+
					"  configuration is silently discarded. Document each component in ONE block.",
					rel, name, len(sites), i+1, strings.Join(where, ", "))
			}
		}
	}
	t.Logf("%d config: block(s) checked across %d values file(s)", checked, len(files))
}
