//go:build integration

package resolver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// labelMentions replaces one strings.Contains scan of the rendered policy per declared label key
// in runPipeline. It must answer exactly as strings.Contains does, for every declared key (and its
// numbered-suffix prefix), on real renders, and on keys that end mid-word or hold other characters.
func TestLabelMentionsMatchesContains(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	declared, err := labels.ExtractDeclaredFromTree(filepath.Join(root, "autoshift", "values"), false)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"", "p", "project", "project-", "project-onboarding-prun", "x/y", "a b", "project.onboarding", "PROJECT"}
	for k := range declared {
		keys = append(keys, k, stripNumberedSuffix(k), k[:len(k)/2])
	}
	raws := []string{"", "autoshift.io/", "x autoshift.io/project-onboarding: 'true' autoshift.io/a.b_c-d/e autoshift.io/"}
	for _, dir := range []string{"stable/project-onboarding", "stable/egressip-nodes"} {
		raw, err := cachedKustomizeBuild(filepath.Join(root, "policies", dir), "enforce")
		if err != nil {
			t.Fatal(err)
		}
		raws = append(raws, raw)
	}
	for i, raw := range raws {
		m := newLabelMentions(raw)
		for _, k := range keys {
			if got, want := m.contains(k), strings.Contains(raw, "autoshift.io/"+k); got != want {
				t.Errorf("raw %d, key %q: labelMentions says %v, strings.Contains %v", i, k, got, want)
			}
		}
	}
}
