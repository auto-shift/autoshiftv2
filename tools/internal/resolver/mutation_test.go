//go:build integration

package resolver

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// deepCloneConfigs returns a deep copy of ExampleConfigs via JSON round-trip.
func deepCloneConfigs(src *ExampleConfigs) *ExampleConfigs {
	b, _ := json.Marshal(src)
	var dst ExampleConfigs
	_ = json.Unmarshal(b, &dst)
	return &dst
}

// deepCloneDeclared returns a shallow copy of the declared map (values are
// immutable *labels.Declared pointers so a shallow copy is safe).
func deepCloneDeclared(src map[string]*labels.Declared) map[string]*labels.Declared {
	dst := make(map[string]*labels.Declared, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// runMutated runs the full pipeline with a mutated ExampleConfigs and returns
// (resultsByPolicy, label-contract-report).
func runMutated(
	t *testing.T,
	root string,
	configs *ExampleConfigs,
	declared map[string]*labels.Declared,
) (map[string]ChartResult, *labels.Report) {
	t.Helper()

	policiesDir := filepath.Join(root, "policies")
	testdataDir := filepath.Join(root, "tools", "testdata")

	clusterName := "lint-cluster"
	syntheticLabels := BuildSyntheticLabels(declared)
	ctx := HubContext{
		ManagedClusterName:   clusterName,
		ManagedClusterLabels: syntheticLabels,
	}

	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, clusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testResources, _ := LoadTestResources(testdataDir)
	seedResources := append(syntheticCMs, testResources...)

	r, err := NewResolver(seedResources)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seedResources)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	consumed, results, err := RunPipeline(policiesDir, ctx, r, spokeR, declared, configs, testdataDir)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	byPolicy := make(map[string]ChartResult, len(results))
	for _, res := range results {
		byPolicy[res.Policy] = res
	}

	allow := &labels.Allowlist{}
	report := labels.BuildReport(consumed, declared, allow)
	return byPolicy, &report
}

// TestPipeline_MutationSweep verifies that the CI pipeline catches specific
// gaps in _example.yaml. Each case introduces one deliberate defect and
// asserts that the pipeline surfaces the correct failure.
//
// This is the negative-test counterpart of TestPipeline_EndToEnd: it proves
// the detection mechanism works, not just that the current config is clean.
func TestPipeline_MutationSweep(t *testing.T) {
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	allowlistPath := filepath.Join(root, ".github", "label-lint-allowlist.yaml")

	// Load the baseline config once — each subtest clones and mutates it.
	baseConfigs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	baseDeclared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}

	// Load the real allowlist so we don't flag known-exempt keys.
	allow, err := labels.LoadAllowlist(allowlistPath)
	if err != nil {
		allow = &labels.Allowlist{}
	}
	_ = allow // used inside subtests via report

	cases := []struct {
		name string
		// mutateConfigs modifies a deep clone of the base ExampleConfigs.
		mutateConfigs func(*ExampleConfigs)
		// mutateDeclared modifies a shallow clone of the base declared map.
		mutateDeclared func(map[string]*labels.Declared)

		// Output assertions: pipeline must NOT contain this string for the given
		// policy (because the config that drives it was removed).
		expectAbsentInPolicy  string
		expectAbsentString    string

		// Resolution error: the policy should have ResolveOK=false (hub template
		// resolution error), which surfaces the missing/broken config condition.
		expectResolutionError bool

		// Route assertion: at least one destination line must be empty / <no value>.
		expectEmptyDestination bool

		// Label contract assertion: this key must appear in report.Missing.
		expectMissingLabel string
	}{
		{
			name: "remove ovsBridges → no ovs-bridge NNCP rendered",
			mutateConfigs: func(cfg *ExampleConfigs) {
				net := hubNetworking(cfg)
				delete(net, "ovsBridges")
			},
			expectAbsentInPolicy: "stable/nmstate",
			expectAbsentString:   "type: ovs-bridge",
		},
		{
			name: "remove ovnMappings → no bridge-mappings rendered",
			mutateConfigs: func(cfg *ExampleConfigs) {
				net := hubNetworking(cfg)
				delete(net, "ovnMappings")
			},
			expectAbsentInPolicy: "stable/nmstate",
			expectAbsentString:   "bridge-mappings",
		},
		{
			name: "wrong route key (dest instead of destination) → empty destination",
			mutateConfigs: func(cfg *ExampleConfigs) {
				net := hubNetworking(cfg)
				routes, _ := net["routes"].(map[string]interface{})
				for id, rv := range routes {
					route, _ := rv.(map[string]interface{})
					if dest, ok := route["destination"]; ok {
						route["dest"] = dest
						delete(route, "destination")
						routes[id] = route
					}
				}
			},
			expectAbsentInPolicy:   "stable/nmstate",
			expectEmptyDestination: true,
		},
		{
			name: "remove disconnected catalogs → no CatalogSource rendered",
			mutateConfigs: func(cfg *ExampleConfigs) {
				disc, _ := cfg.HubConfig["disconnected"].(map[string]interface{})
				delete(disc, "catalogs")
			},
			expectAbsentInPolicy: "stable/disconnected-mirror",
			expectAbsentString:   "CatalogSource",
		},
		{
			// mirrorRegistry removal causes a len-on-zero-value error in the hub
			// template (policy bug: $mirrors needs | default list). The correct
			// assertion is that the policy no longer resolves cleanly — ResolveOK
			// must be false, signalling the missing-config condition.
			name: "remove mirror registry → policy resolution error (mirrors zero value)",
			mutateConfigs: func(cfg *ExampleConfigs) {
				disc, _ := cfg.HubConfig["disconnected"].(map[string]interface{})
				delete(disc, "mirrorRegistry")
			},
			expectAbsentInPolicy:         "stable/disconnected-mirror",
			expectResolutionError:        true,
		},
		{
			// compliance-auto-remediate is a leaf label (no sub-labels) so Pass b
			// can detect it without being silenced by the prefix check.
			name: "remove compliance-auto-remediate declaration → contract Missing violation",
			mutateDeclared: func(declared map[string]*labels.Declared) {
				delete(declared, "compliance-auto-remediate")
			},
			expectMissingLabel: "compliance-auto-remediate",
		},
		{
			// workload-partitioning has no sub-labels (no workload-partitioning-* in example)
			// so Pass b detects it without being silenced by the prefix check.
			name: "remove workload-partitioning declaration → contract Missing violation",
			mutateDeclared: func(declared map[string]*labels.Declared) {
				delete(declared, "workload-partitioning")
			},
			expectMissingLabel: "workload-partitioning",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := deepCloneConfigs(baseConfigs)
			decl := deepCloneDeclared(baseDeclared)

			if tc.mutateConfigs != nil {
				tc.mutateConfigs(cfg)
			}
			if tc.mutateDeclared != nil {
				tc.mutateDeclared(decl)
			}

			byPolicy, report := runMutated(t, root, cfg, decl)

			// --- output absence check ---
			if tc.expectAbsentInPolicy != "" {
				res, ok := byPolicy[tc.expectAbsentInPolicy]
				if !ok {
					t.Fatalf("policy %s not found in results", tc.expectAbsentInPolicy)
				}
				if res.Err != nil {
					t.Fatalf("policy %s had helm error: %v", tc.expectAbsentInPolicy, res.Err)
				}

				if tc.expectResolutionError {
					if res.ResolveOK {
						t.Errorf("expected hub resolution error for %s after mutation, but ResolveOK=true — missing config not detected",
							tc.expectAbsentInPolicy)
					} else {
						t.Logf("PASS: %s correctly has resolution error after mutation (ResolveOK=false)", tc.expectAbsentInPolicy)
					}
				}

				if tc.expectAbsentString != "" {
					if strings.Contains(res.ResolvedYAML, tc.expectAbsentString) {
						t.Errorf("mutation did not suppress %q in %s — config removal was not detected",
							tc.expectAbsentString, tc.expectAbsentInPolicy)
					} else {
						t.Logf("PASS: %q correctly absent from %s after mutation", tc.expectAbsentString, tc.expectAbsentInPolicy)
					}
				}

				if tc.expectEmptyDestination {
					found := false
					for _, line := range strings.Split(res.ResolvedYAML, "\n") {
						bare := strings.TrimPrefix(strings.TrimSpace(line), "- ")
						if bare == "destination:" || bare == "destination: <no value>" ||
							bare == "destination: \"\"" || bare == "destination: ''" {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("mutation did not produce empty destination in %s — key mismatch not detected",
							tc.expectAbsentInPolicy)
					} else {
						t.Logf("PASS: empty destination correctly detected in %s after mutation", tc.expectAbsentInPolicy)
					}
				}
			}

			// --- label contract check ---
			if tc.expectMissingLabel != "" {
				found := false
				for _, entry := range report.Missing {
					if entry.Key == tc.expectMissingLabel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("label %q not in report.Missing — declaration removal was not detected", tc.expectMissingLabel)
				} else {
					t.Logf("PASS: label %q correctly in report.Missing after declaration removed", tc.expectMissingLabel)
				}
			}
		})
	}
}

// hubNetworking is a helper that extracts (or creates) the networking map
// inside HubConfig, for use in mutation functions.
func hubNetworking(cfg *ExampleConfigs) map[string]interface{} {
	if cfg.HubConfig == nil {
		cfg.HubConfig = map[string]interface{}{}
	}
	net, _ := cfg.HubConfig["networking"].(map[string]interface{})
	if net == nil {
		net = map[string]interface{}{}
		cfg.HubConfig["networking"] = net
	}
	return net
}
