//go:build integration

package resolver

import (
	"os"
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

// requiresHub reports whether a Placement only ever selects hubs: it requires
// autoshift.io/cluster-type In [hub]. Placements that reach the hub some other way (a label only the
// hub carries, for example) are not detected, so this is one-directional: a hub-only placement must
// use the hub standard, but the hub standard is not forbidden elsewhere.
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
	for _, pred := range pl.Spec.Predicates {
		for _, e := range pred.RequiredClusterSelector.LabelSelector.MatchExpressions {
			if e.Key == "autoshift.io/cluster-type" && e.Operator == "In" && len(e.Values) == 1 && e.Values[0] == "hub" {
				return true
			}
		}
	}
	return false
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
				t.Errorf("%s: %s is placed only on hubs (%s requires cluster-type In [hub]) but reports %v; use %s",
					rel, p.Name, placement, standards, standardHubToken)
			}
		}
	}
}

// Every Helm-templated Policy reads its standard from autoshift values and quotes it.
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
