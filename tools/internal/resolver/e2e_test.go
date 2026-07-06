//go:build integration

package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// lookupRe extracts the arguments from a lookup call in an error message:
// lookup "apiVersion" "Kind" "namespace" "name"
var lookupRe = regexp.MustCompile(`lookup\s+"([^"]+)"\s+"([^"]+)"\s+"([^"]*)"\s+"([^"]*)"`)

// lookupHint parses a lookup call out of errMsg and returns a stub YAML snippet
// the developer can drop into tools/testdata/ to resolve the error.
func lookupHint(errMsg string) string {
	m := lookupRe.FindStringSubmatch(errMsg)
	if m == nil {
		return "hint: add a stub resource to tools/testdata/ — include the apiVersion, kind, name, and any fields the template reads"
	}
	apiVersion, kind, ns, name := m[1], m[2], m[3], m[4]
	stub := fmt.Sprintf("hint: add a stub to tools/testdata/ so the spoke lookup can resolve in CI.\n\t       Example stub:\n\t         ---\n\t         apiVersion: %s\n\t         kind: %s\n\t         metadata:", apiVersion, kind)
	if ns != "" {
		stub += fmt.Sprintf("\n\t           namespace: %s", ns)
	}
	if name != "" {
		stub += fmt.Sprintf("\n\t           name: %s", name)
	}
	stub += "\n\t         spec: {} # add whichever fields the template reads from this object"
	return stub
}

// repoRoot walks up from the package directory to find the repository root
// (identified by the presence of a `policies/` directory).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "policies")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("could not find repo root (no policies/ directory in any parent)")
		}
		dir = parent
	}
}

// TestPipeline_EndToEnd runs the full lint-labels pipeline against the real
// policies/ and autoshift/values/ directories. It mirrors what autoshift-ci
// does in CI.
//
// The test:
//   - fails if any chart fails helm template
//   - fails if any chart renders zero documents
//   - fails if any hub or spoke resolution error occurs
//   - fails if the label contract has Missing keys (consumed but not declared)
func TestPipeline_EndToEnd(t *testing.T) {
	root := repoRoot(t)

	policiesDir := filepath.Join(root, "policies")
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")
	allowlistPath := filepath.Join(root, ".github", "label-lint-allowlist.yaml")

	// Verify required directories exist.
	for _, dir := range []string{policiesDir, valuesDir} {
		if _, err := os.Stat(dir); err != nil {
			t.Skipf("required directory missing, skipping e2e: %s", dir)
		}
	}

	// 1. Extract declared labels from example files.
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	t.Logf("declared labels: %d", len(declared))

	// 2. Build synthetic labels and hub context.
	syntheticLabels := BuildSyntheticLabels(declared)
	ctx := HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: syntheticLabels,
	}

	// 3. Extract hub config + cluster-install config from example files.
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	t.Logf("hub config keys: %d, cluster-install config keys: %d, bare labels: %d",
		len(configs.HubConfig), len(configs.ClusterInstallConfig), len(configs.BareLabels))

	// Managed (spoke) cluster profiles: same rich label set as the hub, but
	// self-managed is 'false' and the node-provider labels are pinned to each
	// install platform. The pipeline resolves every chart against all of these in
	// addition to the primary hub context, so:
	//   - hub-only and managed-only policies are each exercised against a cluster
	//     of the matching clusterset type (self-managed true vs false), and
	//   - each install platform's cluster is run through the full policy set.
	//
	// One managed profile per install platform, driven entirely by
	// configs.ClusterInstallExtra (one entry per _example-cluster-install-*.yaml,
	// keyed by clusterInstall.platform) — so a new example file automatically
	// adds a new profile. Each profile's ManagedClusterName points at that
	// platform's rendered-config ("<primary>-<platform>.rendered-config", named
	// by GenerateSyntheticConfigMaps), so policies that read per-cluster config
	// via `fromConfigMap (print .ManagedClusterName ".rendered-config")` resolve
	// against that install's config.
	managedProfile := func(provider, clusterName string) HubContext {
		lbls := make(map[string]string, len(syntheticLabels)+4)
		for k, v := range syntheticLabels {
			lbls[k] = v
		}
		lbls["autoshift.io/self-managed"] = "false"
		lbls["autoshift.io/worker-nodes-provider"] = provider
		lbls["autoshift.io/infra-nodes-provider"] = provider
		lbls["autoshift.io/storage-nodes-provider"] = provider
		return HubContext{
			ManagedClusterName:   clusterName,
			ManagedClusterLabels: lbls,
		}
	}
	installPlatforms := make([]string, 0, len(configs.ClusterInstallExtra))
	for p := range configs.ClusterInstallExtra {
		installPlatforms = append(installPlatforms, p)
	}
	sort.Strings(installPlatforms)
	extraCtxs := make([]NamedContext, 0, len(installPlatforms))
	for _, p := range installPlatforms {
		extraCtxs = append(extraCtxs, NamedContext{
			Name: "managed-" + p,
			Ctx:  managedProfile(p, ctx.ManagedClusterName+"-"+p),
		})
	}

	// 4. Generate synthetic ConfigMaps and load testdata.
	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}

	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		t.Fatalf("could not load testdata: %v", err)
	}

	seedResources := append(syntheticCMs, testResources...)

	// 5. Create hub and spoke resolvers.
	r, err := NewResolver(seedResources)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seedResources)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}

	// 6. Run the full pipeline.
	consumed, results, err := RunPipeline(policiesDir, ctx, extraCtxs, r, spokeR, declared, configs, testdataDir)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	// 7. Evaluate per-chart results + run output assertions.
	//
	// Hub resolution errors (ResolveOK=false) are hard failures: they mean a
	// hub template called fail or crashed, so the policy would never deploy to
	// any cluster. A new policy that guards a config section with fail will
	// automatically fail CI here if _example.yaml is missing that section.
	//
	// Spoke resolution errors are hard failures: all spoke templates must
	// resolve cleanly against testdata stubs in the test environment.
	// resolutionHint maps a resolution error string to a developer action hint.
	// The first matching substring wins.
	resolutionHint := func(errMsg string) string {
		switch {
		case strings.Contains(errMsg, "fromSecret") || strings.Contains(errMsg, "Secret"):
			return "hint: add a stub Secret to tools/testdata/ (see tools/testdata/acs-secrets.yaml for an example)"
		case strings.Contains(errMsg, "fromConfigMap") || strings.Contains(errMsg, "ConfigMap"):
			// Synthetic ConfigMaps are generated from _example.yaml config sections.
			// Real ConfigMaps that exist on the hub cluster must be stubbed in testdata.
			return "hint: if this ConfigMap is read from the hub cluster, add a stub to tools/testdata/;\n\t       if it is generated by a cluster-config chart, ensure the config section is populated in the\n\t       hubClusterSets.*.config block of autoshift/values/clustersets/_example.yaml"
		case strings.Contains(errMsg, "lookup"):
			return lookupHint(errMsg)
		default:
			return "hint: check the template for undefined variables or missing _example.yaml values"
		}
	}

	// yamlHint points a developer at the template that produced an invalid
	// resolved document. The Kind/name in the error (e.g. Policy/policy-foo)
	// matches the template file policies/<policy>/templates/policy-foo.yaml.
	yamlHint := func(policy string) string {
		return fmt.Sprintf("hint: the Kind/name above identifies the source document — inspect policies/%s/templates/; a <no value> means a config key the template reads is missing from the relevant _example*.yaml, or a lookup returned nothing (add a tools/testdata/ stub)", policy)
	}

	resultsByPolicy := make(map[string]ChartResult, len(results))
	var helmFailures, emptyCharts, hubErrCharts, warnCharts, cleanCharts, managedErrCharts, yamlErrCharts int
	for _, res := range results {
		resultsByPolicy[res.Policy] = res
		if res.Err != nil {
			t.Errorf("FAIL  %s: helm template failed: %v", res.Policy, res.Err)
			helmFailures++
			continue
		}
		if !res.ResolveOK {
			for _, w := range res.ResolveWarns {
				t.Errorf("FAIL  %s: hub resolution error: %s\n\t       %s", res.Policy, w, resolutionHint(w))
			}
			hubErrCharts++
			continue
		}
		if len(res.SpokeWarns) > 0 {
			for _, w := range res.SpokeWarns {
				t.Errorf("FAIL  %s: spoke resolution error: %s\n\t       %s", res.Policy, w, resolutionHint(w))
			}
			warnCharts++
		} else {
			cleanCharts++
		}
		// YAML validity of the fully-resolved primary output — hard failure even
		// though hub/spoke resolution succeeded, so a template that resolves
		// cleanly but emits malformed YAML or a <no value> leak can't pass.
		if len(res.YAMLErrors) > 0 {
			for _, w := range res.YAMLErrors {
				t.Errorf("FAIL  %s: invalid resolved YAML: %s\n\t       %s", res.Policy, w, yamlHint(res.Policy))
			}
			yamlErrCharts++
		}
		// Managed-clusterset profiles (self-managed: 'false', one per install
		// platform) — same hard-fail treatment as the hub context, so a
		// hub-template branch that only runs on managed/spoke clusters (or only
		// for a given install platform) can't crash undetected.
		for _, ec := range extraCtxs {
			cr := res.ExtraResults[ec.Name]
			if !cr.ResolveOK {
				for _, w := range cr.ResolveWarns {
					t.Errorf("FAIL  %s [%s]: hub resolution error: %s\n\t       %s", res.Policy, ec.Name, w, resolutionHint(w))
				}
				managedErrCharts++
			} else if len(cr.SpokeWarns) > 0 {
				for _, w := range cr.SpokeWarns {
					t.Errorf("FAIL  %s [%s]: spoke resolution error: %s\n\t       %s", res.Policy, ec.Name, w, resolutionHint(w))
				}
				managedErrCharts++
			}
			if len(cr.YAMLErrors) > 0 {
				for _, w := range cr.YAMLErrors {
					t.Errorf("FAIL  %s [%s]: invalid resolved YAML: %s\n\t       %s", res.Policy, ec.Name, w, yamlHint(res.Policy))
				}
				yamlErrCharts++
			}
		}
	}

	// 7b. Output assertions — verify config-driven branches actually rendered.
	// These catch cases where a config section is missing from example files:
	// the hub template silently skips the block and produces no output, but
	// no error is raised because the guard is `if (gt (len ...) 0)`.
	assertContains := func(policy, needle, reason string) {
		t.Helper()
		res, ok := resultsByPolicy[policy]
		if !ok {
			t.Errorf("output assertion: chart %s not found in results", policy)
			return
		}
		if res.Err != nil {
			return // already reported as a failure above
		}
		if !strings.Contains(res.ResolvedYAML, needle) {
			t.Errorf("output assertion FAIL  %s: expected %q in rendered output\n  reason: %s\n  hint: add this config section to the relevant _example*.yaml",
				policy, needle, reason)
		}
	}

	// nmstate: every distinct NNCP type the policy can generate must appear.
	// If any are missing, the corresponding config section is absent from
	// _example.yaml and that code path has never been tested.
	assertContains("stable/nmstate", "type: bond",
		"networking.interfaces must include a bond interface")
	assertContains("stable/nmstate", "type: vlan",
		"networking.interfaces must include a vlan interface")
	assertContains("stable/nmstate", "type: ovs-bridge",
		"networking.ovsBridges must be populated in hub example config")
	assertContains("stable/nmstate", "bridge-mappings",
		"networking.ovnMappings must be populated in hub example config")
	assertContains("stable/nmstate", "nmstate-host-",
		"hosts section with per-host networking overrides must be in cluster-install example")
	assertContains("stable/nmstate", "nodeSelector",
		"networking.nodeSelector must be set in hub example config")

	// nmstate routes: destination field must not be empty or "<no value>".
	// Catches the dest/destination key name mismatch between example and policy.
	// Go template renders a missing map key as "<no value>"; an empty string key
	// produces "destination: " (trailing space, becomes "destination:" after trim).
	if res, ok := resultsByPolicy["stable/nmstate"]; ok && res.Err == nil {
		for i, line := range strings.Split(res.ResolvedYAML, "\n") {
			trimmed := strings.TrimSpace(line)
			// Strip the YAML list marker so "- destination: <no value>" matches too.
			bare := strings.TrimPrefix(trimmed, "- ")
			if bare == "destination:" || bare == "destination: \"\"" || bare == "destination: ''" ||
				bare == "destination: <no value>" {
				t.Errorf("output assertion FAIL  stable/nmstate: line %d has empty/missing route destination\n  reason: route key in example uses 'dest' but policy reads 'destination'", i+1)
			}
		}
	}

	// cluster-install: every platform's install policy body must resolve, not
	// just the merge-winning platform. GenerateSyntheticConfigMaps emits one
	// rendered-config ConfigMap per platform (baremetal + each _example-cluster-
	// install-<platform>.yaml), so all three bodies fire in this single run.
	// Each marker below is unique to one platform's output — if a platform's
	// example file or testdata is missing, its marker disappears and CI fails.
	assertContains("stable/cluster-install", "start_assisted_install",
		"baremetal path must render (BareMetalHost customDeploy) — _example-cluster-install-baremetal.yaml")
	assertContains("stable/cluster-install", "-aws-creds",
		"aws path must render (AWS credentials Secret) — _example-cluster-install-aws.yaml + aws-creds testdata")
	assertContains("stable/cluster-install", "-vsphere-creds",
		"vmware path must render (vSphere credentials Secret) — _example-cluster-install-vmware.yaml + vsphere-creds testdata")

	// disconnected-mirror: all four catalog source types must render when
	// config.disconnected is populated in the hub example.
	assertContains("stable/disconnected-mirror", "CatalogSource",
		"config.disconnected.catalogs must be populated in hub example config")
	assertContains("stable/disconnected-mirror", "ImageDigestMirrorSet",
		"config.disconnected.mirrorRegistry.mirrors must be populated in hub example config")

	// user-workload-monitoring: storage fields must be non-empty (UWM calls
	// fail if config.uwm is missing, but sub-fields degrade silently).
	assertContains("stable/user-workload-monitoring", "storage:",
		"config.uwm.prometheus.storage must be set in hub example config")

	// Multi-profile coverage: acm-mch-install emits `disableHubSelfManagement`
	// only when the cluster's self-managed label is 'false'. The hub context
	// (self-managed: 'true') must NOT emit it, and every managed profile
	// (self-managed: 'false') MUST — this proves each extra resolution pass runs
	// and genuinely flips clusterset-identity branches. If a managed pass stopped
	// running or reused hub labels, one of these fails.
	if res, ok := resultsByPolicy["stable/advanced-cluster-management"]; ok && res.Err == nil {
		if strings.Contains(res.ResolvedYAML, "disableHubSelfManagement") {
			t.Errorf("output assertion FAIL  stable/advanced-cluster-management: hub context (self-managed:'true') must NOT emit disableHubSelfManagement")
		}
		for _, ec := range extraCtxs {
			if !strings.Contains(res.ExtraResults[ec.Name].ResolvedYAML, "disableHubSelfManagement: true") {
				t.Errorf("output assertion FAIL  stable/advanced-cluster-management [%s]: managed profile (self-managed:'false') must emit disableHubSelfManagement: true\n  reason: this profile's resolution pass not exercising the self-managed:'false' branch", ec.Name)
			}
		}
	}

	t.Logf("\nACM resolution: %d clean, %d spoke errors, %d hub errors, %d errors across %d managed profiles, %d invalid-YAML, %d helm failures (%d charts × %d profiles)",
		cleanCharts, warnCharts, hubErrCharts, managedErrCharts, len(extraCtxs), yamlErrCharts, helmFailures, len(results), len(extraCtxs)+1)

	if helmFailures > 0 || emptyCharts > 0 || hubErrCharts > 0 || warnCharts > 0 || managedErrCharts > 0 || yamlErrCharts > 0 {
		t.Errorf("%d chart(s) failed helm template, %d rendered empty, %d hub resolution errors, %d spoke resolution errors, %d managed-clusterset resolution errors, %d invalid resolved YAML",
			helmFailures, emptyCharts, hubErrCharts, warnCharts, managedErrCharts, yamlErrCharts)
	}

	// 8. Check the label contract.
	allow, err := labels.LoadAllowlist(allowlistPath)
	if err != nil {
		t.Fatalf("could not load allowlist: %v", err)
	}

	report := labels.BuildReport(consumed, declared, allow)

	// Write markdown report if LABEL_REPORT_OUTPUT is set (used by CI to
	// produce the uploadable artifact without a separate binary invocation).
	if reportPath := os.Getenv("LABEL_REPORT_OUTPUT"); reportPath != "" {
		f, err := os.Create(reportPath)
		if err != nil {
			t.Errorf("could not create report file %s: %v", reportPath, err)
		} else {
			labels.WriteMarkdown(f, report)
			f.Close()
			t.Logf("label contract report written to %s", reportPath)
		}
	}

	if len(report.Missing) > 0 {
		msgs := make([]string, 0, len(report.Missing))
		for _, entry := range report.Missing {
			policies := ""
			if entry.Consumed != nil {
				policies = strings.Join(entry.Consumed.Policies(), ", ")
			}
			msgs = append(msgs, fmt.Sprintf("  autoshift.io/%s (consumed by: %s)", entry.Key, policies))
		}
		t.Errorf("label contract violations — %d key(s) consumed by policy templates but missing from all _example*.yaml files:\n%s\n\n"+
			"  fix: add each missing label under a `labels:` block in\n"+
			"       autoshift/values/clustersets/_example.yaml (for hub/cluster-set labels) or\n"+
			"       autoshift/values/clusters/_example.yaml (for per-cluster labels)\n"+
			"  once added, re-run: go test -tags integration -run TestPipeline_EndToEnd ./tools/internal/resolver/...",
			len(report.Missing), strings.Join(msgs, "\n"))
	}

	t.Logf("label contract: %d OK, %d missing, %d orphaned",
		len(report.OK), len(report.Missing), len(report.Orphaned))
}
