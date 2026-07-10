//go:build integration

package resolver

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	sigsyaml "sigs.k8s.io/yaml"
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

	// ---- Generic install-config invariants (platform-agnostic) -------------
	// The cluster-install policy renders one base64-encoded install-config
	// Secret per example file (keyed lint-cluster-<variant>). Decode them all
	// and assert invariants that must hold for EVERY platform, so adding a new
	// _example-cluster-install-<platform>.yaml is exercised here for free —
	// coverage and content checks are driven entirely by the example files.
	if res, ok := resultsByPolicy["stable/cluster-install"]; ok && res.Err == nil {
		installConfigs := decodeInstallConfigs(t, res.ResolvedYAML)
		if len(installConfigs) == 0 {
			t.Errorf("output assertion FAIL  stable/cluster-install: no install-config.yaml Secret rendered")
		}

		// Not every platform emits an install-config: IPI platforms (aws,
		// vmware) render a Hive install-config Secret, while agent-based
		// baremetal provisions via BareMetalHost customDeploy with none. So the
		// per-variant existence check applies only to examples that declare a
		// static-host list under a platform section (the IPI static-IP pattern):
		// those MUST produce an install-config, and that host block MUST render —
		// guarding against the whole hosts branch silently dropping out. The
		// null/empty invariants below still run over every install-config that
		// exists, whatever platform produced it.
		variants, err := clusterInstallExampleVariants(root)
		if err != nil {
			t.Fatalf("read cluster-install examples: %v", err)
		}
		for _, v := range variants {
			if !v.hasHosts {
				continue
			}
			cluster := "lint-cluster-" + v.name
			ic, ok := installConfigs[cluster]
			if !ok {
				t.Errorf("output assertion FAIL  stable/cluster-install: _example-cluster-install-%s.yaml declares static hosts but produced no install-config (expected cluster %q)\n  reason: the example's platform body did not resolve through the pipeline", v.name, cluster)
				continue
			}
			if !strings.Contains(ic, "hosts:") {
				t.Errorf("output assertion FAIL  stable/cluster-install: _example-cluster-install-%s.yaml declares static hosts but its install-config has no hosts block\n  reason: the static-IP host branch did not render", v.name)
			}
		}

		// Invariant for every platform: a missing optional field must be OMITTED,
		// never emitted as `key: null` (how the vmware static-IP gateway /
		// nameservers bug manifested) or as an empty identifier string. Build the
		// object with only the keys that are set. `pullSecret: ""` is the sole
		// legitimately-empty field, so we scan for null scalars generically and
		// keep a small, extensible denylist for empty identifiers.
		bannedEmpty := []string{"failureDomain: \"\""}
		for cluster, ic := range installConfigs {
			for i, line := range strings.Split(ic, "\n") {
				if strings.HasSuffix(strings.TrimRight(line, " \t"), ": null") {
					t.Errorf("output assertion FAIL  stable/cluster-install: install-config for %s line %d emits a null scalar:\n    %s\n  reason: build the object with only the keys that are set, never emit `key: null`", cluster, i+1, strings.TrimSpace(line))
				}
			}
			for _, bad := range bannedEmpty {
				if strings.Contains(ic, bad) {
					t.Errorf("output assertion FAIL  stable/cluster-install: install-config for %s contains %q\n  reason: omit optional identifiers when unset — an empty string matches nothing", cluster, bad)
				}
			}
		}
	}

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

// TestAutoshiftChart_ClusterInstallExamples renders the top-level autoshift/
// chart against every autoshift/values/clusters/_example-cluster-install-*.yaml
// profile. Unlike TestPipeline_EndToEnd — which renders individual policies/*
// charts — this exercises templates that live ONLY in the autoshift/ chart,
// most importantly autoshift/templates/_validate-cluster-install.tpl (invoked
// at autoshift-app-set.yaml:1). That validator gates cluster provisioning by
// platform, and a platform (e.g. vmware) missing from its $validPlatforms list
// makes `helm template ./autoshift` fail at render time — a failure the policy-
// only pipeline test cannot see. Every _example-cluster-install-*.yaml must
// render clean here.
func TestAutoshiftChart_ClusterInstallExamples(t *testing.T) {
	root := repoRoot(t)
	chartDir := filepath.Join(root, "autoshift")
	globalValues := filepath.Join(root, "autoshift", "values", "global.yaml")
	hubValues := filepath.Join(root, "autoshift", "values", "clustersets", "hub.yaml")

	examples, err := filepath.Glob(filepath.Join(root, "autoshift", "values", "clusters", "_example-cluster-install-*.yaml"))
	if err != nil {
		t.Fatalf("glob cluster-install examples: %v", err)
	}
	if len(examples) == 0 {
		t.Fatal("no _example-cluster-install-*.yaml files found — expected at least one per supported platform")
	}

	for _, example := range examples {
		example := example
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(example), "_example-cluster-install-"), ".yaml")
		t.Run(name, func(t *testing.T) {
			if _, err := HelmTemplate(chartDir, globalValues, hubValues, example); err != nil {
				t.Errorf("autoshift chart failed to render with %s.\n"+
					"  This example passes the policy-level pipeline test but fails the autoshift/ chart —\n"+
					"  most likely autoshift/templates/_validate-cluster-install.tpl rejects this platform's\n"+
					"  config (e.g. platform missing from $validPlatforms, or an unhandled required field).\n"+
					"  error: %v", filepath.Base(example), err)
			}
		})
	}
}

// decodeInstallConfigs extracts every base64-encoded install-config.yaml Secret
// value from the resolved cluster-install output, decodes it, and returns a map
// of cluster name (install-config metadata.name) → decoded install-config YAML.
// The install-config is stored base64-encoded inside a Secret, so its contents
// are invisible to plain string assertions on the resolved policy YAML.
func decodeInstallConfigs(t *testing.T, resolvedYAML string) map[string]string {
	t.Helper()
	re := regexp.MustCompile(`install-config\.yaml: '([A-Za-z0-9+/=]+)'`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(resolvedYAML, -1) {
		decoded, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			t.Errorf("install-config.yaml is not valid base64: %v", err)
			continue
		}
		var meta struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if err := sigsyaml.Unmarshal(decoded, &meta); err != nil {
			t.Errorf("decoded install-config is not valid YAML: %v", err)
			continue
		}
		name := meta.Metadata.Name
		if name == "" {
			name = fmt.Sprintf("unnamed-%d", len(out))
		}
		out[name] = string(decoded)
	}
	return out
}

// clusterInstallVariant describes one _example-cluster-install-*.yaml file.
type clusterInstallVariant struct {
	name     string // the part after "_example-cluster-install-" (matches installVariant)
	hasHosts bool   // declares a non-empty static-host list under config.<platform>.hosts
}

// clusterInstallExampleVariants enumerates the _example-cluster-install-*.yaml
// files and, for each, derives its variant key and whether it declares static
// hosts. Driven entirely by the files so a new platform example is picked up
// automatically — no per-platform code here.
func clusterInstallExampleVariants(root string) ([]clusterInstallVariant, error) {
	dir := filepath.Join(root, "autoshift", "values", "clusters")
	matches, err := filepath.Glob(filepath.Join(dir, "_example-cluster-install-*.yaml"))
	if err != nil {
		return nil, err
	}
	var out []clusterInstallVariant
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var parsed map[string]interface{}
		if err := sigsyaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		out = append(out, clusterInstallVariant{
			name:     installVariant(filepath.Base(path)),
			hasHosts: exampleDeclaresPlatformHosts(parsed),
		})
	}
	return out, nil
}

// exampleDeclaresPlatformHosts reports whether any cluster in the parsed example
// declares a non-empty static-host list under config.<platform>.hosts (the
// IPI static-IP pattern, e.g. config.vsphere.hosts). It intentionally does NOT
// match baremetal's config.hosts (a hostname-keyed map, not a platform-section
// list), which does not map into an install-config hosts block.
func exampleDeclaresPlatformHosts(parsed map[string]interface{}) bool {
	clusters, _ := parsed["clusters"].(map[string]interface{})
	for _, cv := range clusters {
		cluster, _ := cv.(map[string]interface{})
		cfg, _ := cluster["config"].(map[string]interface{})
		for _, sectionVal := range cfg {
			section, ok := sectionVal.(map[string]interface{})
			if !ok {
				continue
			}
			if hosts, ok := section["hosts"].([]interface{}); ok && len(hosts) > 0 {
				return true
			}
		}
	}
	return false
}
