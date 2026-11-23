//go:build integration

package resolver

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The ACM Governance standard on every policy comes from autoshift.policyStandard (managed-cluster
// policies) or autoshift.policyStandardHub (hub-only policies). These tests keep that true as policies
// are added, because nothing else notices: a hard-coded "NIST SP 800-53" renders fine, and so does a
// ${POLICY_STANDARD} that one renderer forgets to substitute.

const (
	standardToken    = "${POLICY_STANDARD}"
	standardHubToken = "${POLICY_STANDARD_HUB}"
)

type pgStandardsConfig struct {
	PolicyDefaults struct {
		Standards []string `yaml:"standards"`
		Placement struct {
			PlacementPath string `yaml:"placementPath"`
		} `yaml:"placement"`
	} `yaml:"policyDefaults"`
	Policies []struct {
		Name      string   `yaml:"name"`
		Standards []string `yaml:"standards"`
		Placement struct {
			PlacementPath string `yaml:"placementPath"`
		} `yaml:"placement"`
	} `yaml:"policies"`
}

func pgConfigPaths(t *testing.T, root string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "policies", "*", "*", "policy-generator-config.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("found no policy-generator-config.yaml files")
	}
	return paths
}

func readPGStandards(t *testing.T, path string) pgStandardsConfig {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg pgStandardsConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cfg
}

func isStandardPlaceholder(s string) bool {
	return s == standardToken || s == standardHubToken
}

// Every PolicyGenerator config declares its standard as exactly one quoted placeholder, in
// policyDefaults and in any per-policy override. Quoting keeps a custom name with YAML syntax in it
// (ISO/IEC 27001:2022) a single string after substitution.
func TestPolicyStandard_PGConfigsUsePlaceholder(t *testing.T) {
	root := repoRoot(t)
	quoted := regexp.MustCompile(`^\s*- "\$\{POLICY_STANDARD(_HUB)?\}"\s*$`)
	anyEntry := regexp.MustCompile(`^\s*- .*(POLICY_STANDARD|NIST SP 800-53)`)
	files := append(pgConfigPaths(t, root), filepath.Join(root, "scripts", "templates", "pg-config.yaml.template"))
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if anyEntry.MatchString(line) && !quoted.MatchString(line) {
				t.Errorf("%s:%d: standards entry %q must be exactly \"${POLICY_STANDARD}\" or \"${POLICY_STANDARD_HUB}\", quoted", rel, i+1, strings.TrimSpace(line))
			}
		}
		if strings.HasSuffix(path, ".template") {
			continue
		}
		cfg := readPGStandards(t, path)
		if len(cfg.PolicyDefaults.Standards) != 1 || !isStandardPlaceholder(cfg.PolicyDefaults.Standards[0]) {
			t.Errorf("%s: policyDefaults.standards must be one placeholder, got %v", rel, cfg.PolicyDefaults.Standards)
		}
		for _, p := range cfg.Policies {
			if p.Standards == nil {
				continue
			}
			if len(p.Standards) != 1 || !isStandardPlaceholder(p.Standards[0]) {
				t.Errorf("%s: %s: standards override must be one placeholder, got %v", rel, p.Name, p.Standards)
			}
		}
	}
}

type placementSelector struct {
	Spec struct {
		Predicates []struct {
			RequiredClusterSelector struct {
				LabelSelector struct {
					MatchExpressions []struct {
						Key      string   `yaml:"key"`
						Operator string   `yaml:"operator"`
						Values   []string `yaml:"values"`
					} `yaml:"matchExpressions"`
				} `yaml:"labelSelector"`
			} `yaml:"requiredClusterSelector"`
		} `yaml:"predicates"`
	} `yaml:"spec"`
}

// requiresHub reports whether a Placement only ever selects hubs. Predicates are ORed, so every
// predicate must carry a hub restriction: autoshift.io/cluster-type In [hub], or
// autoshift.io/self-managed In [true] (a cluster that manages itself is a hub). Placements that reach
// the hub some other way (a label only the hub carries, for example) are not detected, so this is
// one-directional: a hub-only placement must use the hub standard, but the hub standard is not
// forbidden elsewhere.
func requiresHub(t *testing.T, path string) bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var pl placementSelector
	if err := yaml.Unmarshal(body, &pl); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(pl.Spec.Predicates) == 0 {
		return false
	}
	for _, pred := range pl.Spec.Predicates {
		hub := false
		for _, e := range pred.RequiredClusterSelector.LabelSelector.MatchExpressions {
			only := e.Operator == "In" && len(e.Values) == 1
			if only && ((e.Key == "autoshift.io/cluster-type" && e.Values[0] == "hub") ||
				(e.Key == "autoshift.io/self-managed" && e.Values[0] == "true")) {
				hub = true
			}
		}
		if !hub {
			return false
		}
	}
	return true
}

// A policy whose placement requires cluster-type In [hub] reports the hub standard.
func TestPolicyStandard_HubOnlyPoliciesUseHubStandard(t *testing.T) {
	root := repoRoot(t)
	for _, path := range pgConfigPaths(t, root) {
		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(path)
		cfg := readPGStandards(t, path)
		for _, p := range cfg.Policies {
			placement := p.Placement.PlacementPath
			if placement == "" {
				placement = cfg.PolicyDefaults.Placement.PlacementPath
			}
			if placement == "" {
				continue
			}
			standards := p.Standards
			if standards == nil {
				standards = cfg.PolicyDefaults.Standards
			}
			if requiresHub(t, filepath.Join(dir, placement)) && (len(standards) != 1 || standards[0] != standardHubToken) {
				t.Errorf("%s: %s is placed only on hubs (every predicate in %s requires cluster-type In [hub] or self-managed In [true]) but reports %v; use %s",
					rel, p.Name, placement, standards, standardHubToken)
			}
		}
	}
}

// A Helm-templated Placement is hub-only when its clusterSets are the hub clustersets and not also the
// managed ones (policy-gitops-operator-install spans both and correctly keeps the managed standard).
var (
	hubClusterSetPlacement     = regexp.MustCompile(`range \$[A-Za-z]+, \$[A-Za-z]+ := \$?\.Values\.hubClusterSets`)
	managedClusterSetPlacement = regexp.MustCompile(`range \$[A-Za-z]+, \$[A-Za-z]+ := \$?\.Values\.managedClusterSets`)
)

// Every Helm-templated Policy reads its standard from autoshift values and quotes it, and one placed
// on the hub clustersets reads policyStandardHub.
func TestPolicyStandard_HelmTemplatesUseValues(t *testing.T) {
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "policies", "*", "*", "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range paths {
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, "policy.open-cluster-management.io/standards:") {
				continue
			}
			if !strings.Contains(line, ".Values.autoshift).policyStandard") || !strings.Contains(line, "| quote") {
				t.Errorf("%s:%d: standards annotation must come from autoshift.policyStandard or policyStandardHub and be quoted: %s",
					rel, i+1, strings.TrimSpace(line))
			}
			if hubClusterSetPlacement.Match(body) && !managedClusterSetPlacement.Match(body) && !strings.Contains(line, ".Values.autoshift).policyStandardHub") {
				t.Errorf("%s:%d: this policy is placed on the hub clustersets, so its standards annotation must read policyStandardHub: %s",
					rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}

var sedPlaceholder = regexp.MustCompile(`s\|\\(\$\{[A-Z_]+\})\|`)

func sedPlaceholders(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	set := map[string]bool{}
	for _, m := range sedPlaceholder.FindAllStringSubmatch(string(body), -1) {
		set[m[1]] = true
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Both CMP copies (the bootstrap chart's and the one the openshift-gitops policy keeps on every hub),
// scripts/pg-render.sh and the validation suite substitute the same placeholders, and every
// placeholder a PolicyGenerator config or placement uses is one of them.
func TestPlaceholders_AllRenderersAgree(t *testing.T) {
	root := repoRoot(t)
	suite := map[string]bool{}
	for token := range PlaceholderTestValues {
		suite[token] = true
	}
	want := sortedKeys(suite)
	renderers := []string{
		"openshift-gitops/templates/policy-generator-cmp.yaml",
		"policies/stable/openshift-gitops/templates/policy-gitops-cmp-plugin.yaml",
		"scripts/pg-render.sh",
	}
	for _, r := range renderers {
		got := sedPlaceholders(t, filepath.Join(root, r))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s substitutes %v; the suite (PlaceholderTestValues) substitutes %v. Every renderer must substitute the same set", r, got, want)
		}
	}

	used := regexp.MustCompile(`\$\{[A-Z_]+\}`)
	var files []string
	for _, pattern := range []string{"policy-generator-config.yaml", "placement*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(root, "policies", "*", "*", pattern))
		files = append(files, m...)
	}
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, token := range used.FindAllString(string(body), -1) {
			if !suite[token] {
				t.Errorf("%s uses %s, which no renderer substitutes", rel, token)
			}
		}
	}
}

// OCI mode renders each PolicyGenerator directory into a stock Helm chart at release time
// (scripts/render-policygen-charts.sh), with the placeholders still in place, and substitutes them at
// install time with a literal replace. PolicyGenerator writes the standards annotation as a plain
// scalar, so the substitution has to add quoting: package two real charts and render them with names
// that are YAML-significant when unquoted.
func TestOCIChart_QuotesCustomStandard(t *testing.T) {
	root := repoRoot(t)
	bin, pluginHome := resolveKustomizeTools(root)
	if bin == "" || pluginHome == "" {
		t.Skip("kustomize/PolicyGenerator not found; run make install-policy-generator")
	}
	for _, tool := range []string{"bash", "helm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	charts := t.TempDir()
	cmd := exec.Command("bash", "scripts/render-policygen-charts.sh",
		"policies/stable/htpasswd", "policies/stable/advanced-cluster-security")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "KUSTOMIZE="+bin, "KUSTOMIZE_PLUGIN_HOME="+pluginHome,
		"CHARTS_DIR="+charts, "VERSION=0.0.0-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render-policygen-charts.sh: %v\n%s", err, out)
	}
	values := filepath.Join(charts, "values.yaml")
	if err := os.WriteFile(values, []byte("autoshift:\n  policyStandard: \"Platform: Security\"\n  policyStandardHub: \"2022\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Platform: Security": true, "2022": true}
	for _, chart := range []string{"htpasswd", "advanced-cluster-security"} {
		out, err := exec.Command("helm", "template", "asv",
			filepath.Join(charts, "policies", chart+"-0.0.0-test.tgz"), "-f", values).CombinedOutput()
		if err != nil {
			t.Errorf("%s: helm template with a colon-space standard failed: %v\n%s", chart, err, out)
			continue
		}
		if strings.Contains(string(out), "${POLICY_STANDARD") {
			t.Errorf("%s: an unsubstituted standard placeholder is left in the output", chart)
		}
		// Annotations decode into yaml.Node, not string: yaml.v3 would turn an unquoted 2022 into "2022"
		// and hide exactly the regression this test exists to catch. The tag must be !!str.
		dec := yaml.NewDecoder(strings.NewReader(string(out)))
		policies := 0
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name        string               `yaml:"name"`
					Annotations map[string]yaml.Node `yaml:"annotations"`
				} `yaml:"metadata"`
			}
			if err := dec.Decode(&doc); err != nil {
				if err != io.EOF {
					t.Errorf("%s: rendered output is not valid YAML: %v", chart, err)
				}
				break
			}
			if doc.Kind != "Policy" {
				continue
			}
			policies++
			node, ok := doc.Metadata.Annotations["policy.open-cluster-management.io/standards"]
			switch {
			case !ok:
				t.Errorf("%s: %s has no standards annotation", chart, doc.Metadata.Name)
			case node.ShortTag() != "!!str":
				t.Errorf("%s: %s standards annotation %q is a %s, not a string; the value must be quoted",
					chart, doc.Metadata.Name, node.Value, node.ShortTag())
			case !want[node.Value]:
				t.Errorf("%s: %s has standard %q, want one of the configured names", chart, doc.Metadata.Name, node.Value)
			}
		}
		if policies == 0 {
			t.Errorf("%s: rendered no Policy documents", chart)
		}
	}
}
