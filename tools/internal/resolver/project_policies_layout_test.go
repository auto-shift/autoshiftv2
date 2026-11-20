//go:build integration

package resolver

// The developer-guide layout of the project policies (project-onboarding, namespace-rbac,
// custom-sccs, project-networks, egressip-nodes): enforcing content in manifests/ as policy-<name>,
// inform-only content in test/ as its own policy-<name>-test with remediationAction inform, never
// bundled into the enforcing Policy (developer guide, "Gating on cluster state").

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

// splitPolicyDirs maps each directory to its enforcing Policies and the name of the
// ConfigurationPolicy each one carries. The ConfigurationPolicy of a -test Policy is named after it.
// The onboarding shards keep their -enforce ConfigurationPolicy names, so the live ones are updated
// in place (README, "Migration").
var splitPolicyDirs = map[string]map[string]string{
	"stable/namespace-rbac":   {"policy-namespace-rbac": "policy-namespace-rbac"},
	"stable/custom-sccs":      {"policy-custom-sccs": "policy-custom-sccs"},
	"stable/project-networks": {"policy-project-networks": "policy-project-networks"},
	"stable/egressip-nodes":   {"policy-egressip-nodes": "policy-egressip-nodes"},
	projectOnboardingPolicy:   onboardingEnforcePolicies(),
}

func onboardingEnforcePolicies() map[string]string {
	out := map[string]string{}
	for shard := 0; shard < onboardingShards; shard++ {
		name := fmt.Sprintf("%s%d", onboardingShardPrefix, shard)
		out[name] = name + "-enforce"
	}
	return out
}

// renderedPolicy is one root Policy of a rendered directory.
type renderedPolicy struct {
	name    string
	root    string // spec.remediationAction, "" when unset
	cps     map[string]string
	deps    []string
	evalOK  map[string]bool
	pruneBy []string // ConfigurationPolicies that set pruneObjectBehavior
}

func parseRenderedPolicies(t *testing.T, rendered string) map[string]renderedPolicy {
	t.Helper()
	out := map[string]renderedPolicy{}
	for _, doc := range splitYAMLDocuments(rendered) {
		var obj map[string]interface{}
		if sigsyaml.Unmarshal([]byte(doc), &obj) != nil || obj["kind"] != "Policy" {
			continue
		}
		name, _ := nestedString(obj, "metadata", "name")
		spec, _ := obj["spec"].(map[string]interface{})
		p := renderedPolicy{name: name, cps: map[string]string{}, evalOK: map[string]bool{}}
		p.root, _ = spec["remediationAction"].(string)
		for _, d := range asSlice(spec["dependencies"]) {
			dn, _ := d.(map[string]interface{})["name"].(string)
			p.deps = append(p.deps, dn)
		}
		for _, tpl := range asSlice(spec["policy-templates"]) {
			def, _ := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			cpName, _ := nestedString(def, "metadata", "name")
			cpSpec, _ := def["spec"].(map[string]interface{})
			p.cps[cpName], _ = cpSpec["remediationAction"].(string)
			ei, _ := cpSpec["evaluationInterval"].(map[string]interface{})
			p.evalOK[cpName] = ei["compliant"] != nil && ei["noncompliant"] != nil
			if _, ok := cpSpec["pruneObjectBehavior"]; ok {
				p.pruneBy = append(p.pruneBy, cpName)
			}
		}
		if _, dup := out[name]; dup {
			t.Fatalf("Policy %s rendered twice", name)
		}
		out[name] = p
	}
	return out
}

func asSlice(v interface{}) []interface{} {
	s, _ := v.([]interface{})
	return s
}

// assertEnforceTestSplit fails unless rendered (one directory, REMEDIATION=remediation) holds
// exactly the enforcing Policies of enforce, each with its one ConfigurationPolicy following
// remediation at the root and in the child, and for each a <name>-test Policy whose root and one
// ConfigurationPolicy are inform. Neither depends on the other, no ConfigurationPolicy sets
// pruneObjectBehavior, and every one carries evaluationInterval with compliant and noncompliant.
func assertEnforceTestSplit(t *testing.T, rendered, remediation string, enforce map[string]string) {
	t.Helper()
	got := parseRenderedPolicies(t, rendered)
	want := map[string]string{} // Policy -> its only ConfigurationPolicy
	for p, cp := range enforce {
		want[p] = cp
		want[p+"-test"] = p + "-test"
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(got) != len(want) {
		t.Fatalf("REMEDIATION=%s: rendered Policies %v, want %d (each enforcing Policy and its -test)", remediation, names, len(want))
	}
	for name, cp := range want {
		p, ok := got[name]
		if !ok {
			t.Errorf("REMEDIATION=%s: no Policy %s in %v", remediation, name, names)
			continue
		}
		mode := remediation
		if strings.HasSuffix(name, "-test") {
			mode = "inform"
		}
		if p.root != mode {
			t.Errorf("REMEDIATION=%s: %s root remediationAction = %q, want %q", remediation, name, p.root, mode)
		}
		if len(p.cps) != 1 || p.cps[cp] != mode {
			t.Errorf("REMEDIATION=%s: %s ConfigurationPolicies = %v, want only %s (%s)", remediation, name, p.cps, cp, mode)
		}
		if len(p.deps) != 0 {
			t.Errorf("%s depends on %v; the README explains why neither Policy of the pair depends on the other", name, p.deps)
		}
		if len(p.pruneBy) != 0 {
			t.Errorf("%s: %v set pruneObjectBehavior; nothing may be deleted when a ConfigurationPolicy goes away", name, p.pruneBy)
		}
		for c, ok := range p.evalOK {
			if !ok {
				t.Errorf("%s: ConfigurationPolicy %s needs evaluationInterval compliant and noncompliant", name, c)
			}
		}
		if len(name) > 40 {
			t.Errorf("Policy name %s is %d characters, over 40 (TestPolicyNameLengths upstream)", name, len(name))
		}
	}
}

// TestProjectPolicies_EnforceTestSplit renders every directory with REMEDIATION enforce and inform
// (dry run) and checks the split.
func TestProjectPolicies_EnforceTestSplit(t *testing.T) {
	t.Parallel()
	for dir, enforce := range splitPolicyDirs {
		for _, remediation := range []string{"enforce", "inform"} {
			rendered, err := cachedKustomizeBuild(filepath.Join(repoRoot(t), "policies", dir), remediation)
			if err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			assertEnforceTestSplit(t, rendered, remediation, enforce)
		}
	}
}

// TestProjectPolicies_Layout: manifests/ and test/ exist, the earlier manifests-enforce/ and
// manifests-inform/ are gone, and every directory has a README.
func TestProjectPolicies_Layout(t *testing.T) {
	t.Parallel()
	for dir := range splitPolicyDirs {
		base := filepath.Join(repoRoot(t), "policies", dir)
		for _, sub := range []string{"manifests", "test", "README.md", "placement.yaml", "kustomization.yaml", "policy-generator-config.yaml"} {
			if _, err := os.Stat(filepath.Join(base, sub)); err != nil {
				t.Errorf("%s: %v", dir, err)
			}
		}
		for _, sub := range []string{"manifests-enforce", "manifests-inform"} {
			if _, err := os.Stat(filepath.Join(base, sub)); err == nil {
				t.Errorf("%s/%s still exists; enforcing content goes in manifests/, inform-only content in test/", dir, sub)
			}
		}
	}
}

// assertTemplatesInSync fails unless manifests/<file> and test/<file> of dir are the same template
// apart from the one $want line.
func assertTemplatesInSync(t *testing.T, dir, file string) {
	t.Helper()
	base := filepath.Join(repoRoot(t), "policies", dir)
	read := func(sub string) []string {
		b, err := os.ReadFile(filepath.Join(base, sub, file))
		if err != nil {
			t.Fatalf("read %s: %v", sub, err)
		}
		return strings.Split(string(b), "\n")
	}
	enforce, inform := read("manifests"), read("test")
	if len(enforce) != len(inform) {
		t.Fatalf("line counts differ: manifests/ %d, test/ %d", len(enforce), len(inform))
	}
	var diffs int
	for i := range enforce {
		if enforce[i] == inform[i] {
			continue
		}
		diffs++
		if enforce[i] != `  {{hub- $want := "enforce" hub}}` || inform[i] != `  {{hub- $want := "inform" hub}}` {
			t.Errorf("line %d differs beyond $want:\n  manifests/: %s\n  test/:      %s", i+1, enforce[i], inform[i])
		}
	}
	if diffs != 1 {
		t.Errorf("want exactly one differing line ($want), got %d", diffs)
	}
}
