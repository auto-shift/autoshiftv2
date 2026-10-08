//go:build integration

package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// TestPolicySourceMutations breaks a real policy in a staged copy and asserts the suite reports it
// with a message a developer can act on.
//
// The mutation sweep in mutation_test.go perturbs the example values. This perturbs the policy
// source instead, which is where most defects are actually written. Each case is one way a policy
// goes wrong; `want` holds the substrings the failure has to contain, so a check that starts
// reporting something vague fails here rather than silently becoming useless.
//
// Real policies are copied rather than hand-written fixtures: they carry real hub templates, real
// placements and real dependencies, so a mutation exercises the same code path a developer hits.

type policyMutation struct {
	name   string // subtest name
	policy string // "stable/file-integrity"
	note   string // what the developer did wrong, printed when the assertion fails
	// mutate edits the staged copy. policyDir is the staged policy, testdataDir the staged stubs.
	mutate func(t *testing.T, policyDir, testdataDir string)
	// want lists substrings that must all appear in the collected diagnostics.
	want []string
	// wantClean asserts the opposite: the mutation must NOT be reported, documenting a known gap.
	wantClean bool
}

func TestPolicySourceMutations(t *testing.T) {
	root := repoRoot(t)

	for _, mc := range policyMutationCases() {
		mc := mc
		t.Run(mc.name, func(t *testing.T) {
			t.Parallel()
			diag := runPolicyMutation(t, root, mc)
			// POLMUT_DUMP=1 prints what each mutation actually produced, which is how you
			// tighten `want` when adding a case.
			if os.Getenv("POLMUT_DUMP") != "" {
				for _, line := range strings.Split(strings.TrimRight(diag, "\n"), "\n") {
					t.Logf("DIAG %s | %s", mc.name, line)
				}
			}

			if mc.wantClean {
				if diag != "" {
					t.Errorf("mutation %q was expected to go unreported, but produced:\n%s", mc.name, diag)
				}
				return
			}

			var missing []string
			for _, want := range mc.want {
				if !strings.Contains(diag, want) {
					missing = append(missing, want)
				}
			}
			if len(missing) > 0 {
				t.Errorf(`the suite did not report this defect clearly.

	       What was broken: %s
	       Policy:          %s
	       Expected the diagnostics to mention: %q

	       Actual diagnostics:
%s`, mc.note, mc.policy, missing, indentBlock(diag))
			}
		})
	}
}

// runPolicyMutation stages a copy of one real policy plus the testdata stubs, applies the
// mutation, runs the pipeline against the copy, and returns every diagnostic as one string.
func runPolicyMutation(t *testing.T, root string, mc policyMutation) string {
	t.Helper()

	// Stage inside the repo (.tmp is gitignored) so findComponentsRoot still reaches
	// components/, which the shared operator-install chart needs.
	tmpBase := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tmpBase, 0o755); err != nil {
		t.Fatalf("create .tmp: %v", err)
	}
	stageRoot, err := os.MkdirTemp(tmpBase, "polmut-")
	if err != nil {
		t.Fatalf("stage dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stageRoot) })

	policiesDir := filepath.Join(stageRoot, "policies")
	stagedPolicy := filepath.Join(policiesDir, mc.policy)
	if err := copyTree(filepath.Join(root, "policies", mc.policy), stagedPolicy); err != nil {
		t.Fatalf("copy policy %s: %v", mc.policy, err)
	}
	// The pipeline stages a policy at its path relative to whichever ancestor holds components/,
	// so a nested kustomization pulling the shared chart resolves only when that relative depth
	// matches. Copying components/ into the stage root makes it a self-contained repo root and
	// keeps policies/<tier>/<name> at the same depth as the real tree.
	if err := copyTree(filepath.Join(root, "components"), filepath.Join(stageRoot, "components")); err != nil {
		t.Fatalf("copy components: %v", err)
	}
	stagedTestdata := filepath.Join(stageRoot, "testdata")
	if err := copyTree(filepath.Join(root, "tools", "testdata"), stagedTestdata); err != nil {
		t.Fatalf("copy testdata: %v", err)
	}

	mc.mutate(t, stagedPolicy, stagedTestdata)

	valuesDir := filepath.Join(root, "autoshift", "values")
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: BuildSyntheticLabels(declared),
	}
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	cms, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	stubs, err := LoadTestResources(stagedTestdata)
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append(cms, stubs...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	consumed, results, pipeErr := RunPipeline(policiesDir, ctx, nil, r, spokeR, declared, configs, stagedTestdata)

	var b strings.Builder
	if pipeErr != nil {
		fmt.Fprintf(&b, "pipeline error: %v\n", pipeErr)
	}
	for _, res := range results {
		if res.Err != nil {
			fmt.Fprintf(&b, "%s: render failed: %v\n", res.Policy, res.Err)
		}
		for _, w := range res.ResolveWarns {
			fmt.Fprintf(&b, "%s: hub resolution: %s\n", res.Policy, w)
		}
		for _, w := range res.SpokeWarns {
			fmt.Fprintf(&b, "%s: spoke resolution: %s\n", res.Policy, w)
		}
		for _, e := range res.YAMLErrors {
			fmt.Fprintf(&b, "%s: resolved output invalid: %s\n", res.Policy, e)
		}
		for _, f := range scanForEmptyPolicies(res.Policy, "hub", res.ResolvedYAML) {
			if m := pgManifests(policiesDir, res.Policy)[f.Policy]; m != "" {
				fmt.Fprintf(&b, "%s: empty policy %s (from %s): %s\n", res.Policy, f.Policy, m, f.Reason)
			} else {
				fmt.Fprintf(&b, "%s: empty policy %s: %s\n", res.Policy, f.Policy, f.Reason)
			}
		}
	}
	noReadme, intervals := checkPolicyStructure(policiesDir)
	for _, id := range noReadme {
		fmt.Fprintf(&b, "policy structure: %s has no README.md\n", id)
	}
	for _, m := range intervals {
		fmt.Fprintf(&b, "policy structure: %s\n", m)
	}
	for _, m := range findLongPolicyNames(t, stageRoot, policiesDir, false) {
		fmt.Fprintf(&b, "policy name: %s\n", m)
	}
	for _, e := range labels.BuildReport(consumed, declared, nil).Missing {
		fmt.Fprintf(&b, "label contract: %s consumed but not declared in any _example file\n", e.Key)
	}
	return b.String()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode())
	})
}

func indentBlock(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("\t         " + line + "\n")
	}
	return b.String()
}

// editFile applies a literal replacement inside the staged copy, failing the test if the text to
// replace is not present, so a mutation cannot silently become a no-op when a policy is rewritten.
func editFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), old) {
		t.Fatalf("mutation is stale: %s no longer contains %q", filepath.Base(path), old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), old, replacement, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
