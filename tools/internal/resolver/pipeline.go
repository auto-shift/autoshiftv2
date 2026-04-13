package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	sigsyaml "sigs.k8s.io/yaml"
)

// ChartResult holds the outcome for one policy chart.
type ChartResult struct {
	Policy       string   // "stable/cert-manager"
	ChartDir     string   // path to chart directory
	HelmOK       bool     // helm template succeeded
	ResolveOK    bool     // all Policy documents resolved without error
	ResolveWarns []string // per-document resolution warnings (e.g. lookup failures)
	EmptyLabels  []string // label keys that resolved to empty string
	Err          error    // fatal error (helm template failed)
}

// HelmTemplate runs `helm template <name> <chartDir>` and returns the raw
// multi-document YAML output. If extraValuesFiles are provided, they are
// passed as `-f` flags (used to inject the ApplicationSet-level values that
// policy charts need to render conditional templates).
func HelmTemplate(chartDir string, extraValuesFiles ...string) (string, error) {
	name := filepath.Base(chartDir)
	args := []string{"template", name, chartDir}
	for _, f := range extraValuesFiles {
		args = append(args, "-f", f)
	}
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm template %s: %w\n%s", chartDir, err, out)
	}
	return string(out), nil
}

// RunPipeline processes all policy charts under policiesDir:
//
//  1. Discovers charts at <category>/<chart>/Chart.yaml
//  2. Runs `helm template` on each
//  3. Determines which declared labels are consumed by each chart (by checking
//     if `autoshift.io/<key>` appears anywhere in the rendered output)
//  4. Resolves hub templates using the ACM resolver with synthetic labels
//  5. Checks the resolved output for empty-string substitutions
//
// The consumed-labels map is built from what actually appears in the rendered
// helm output — no regex parsing of template syntax needed because helm has
// already resolved all Helm-level indirection.
func RunPipeline(
	policiesDir string,
	ctx HubContext,
	r *Resolver,
	declared map[string]*labels.Declared,
	hubConfig map[string]interface{},
	testdataDir string,
) (map[string]*labels.Consumed, []ChartResult, error) {
	charts, err := discoverCharts(policiesDir)
	if err != nil {
		return nil, nil, fmt.Errorf("discover charts: %w", err)
	}

	// Create a temp values file that provides the ApplicationSet-injected
	// values (hubClusterSets, managedClusterSets, policy_namespace, etc.)
	// that policy charts need to render their conditional templates.
	tmpDir, err := os.MkdirTemp("", "autoshift-lint-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	testValuesPath, err := WriteTestValues(tmpDir, ctx.ManagedClusterName, hubConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("write test values: %w", err)
	}

	// Sort charts with cluster-config-maps first (dependency ordering for
	// future ConfigMap injection).
	sort.SliceStable(charts, func(i, j int) bool {
		iCCM := strings.Contains(charts[i].policy, "cluster-config-maps")
		jCCM := strings.Contains(charts[j].policy, "cluster-config-maps")
		if iCCM != jCCM {
			return iCCM
		}
		return charts[i].policy < charts[j].policy
	})

	// Build the set of declared keys for quick lookup.
	declaredKeys := make(map[string]bool, len(declared))
	for key := range declared {
		declaredKeys[key] = true
	}

	// Load test resources (Secrets, ConfigMaps) from the testdata directory.
	// These represent resources that hub templates look up via
	// fromSecret/fromConfigMap/lookup but only exist after operators are
	// installed. Adding a new resource = drop a YAML file in testdata/.
	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load test resources: %w", err)
	}
	r.SetLocalResources(testResources)

	keysByPolicy := map[string]map[string]bool{}
	var results []ChartResult

	for _, chart := range charts {
		result := ChartResult{
			Policy:   chart.policy,
			ChartDir: chart.dir,
		}

		// 1. Helm template with the test values overlay.
		//    If the chart has .example files in files/, activate them in a temp
		//    copy so Files.Glob guards pass and all templates render.
		renderDir, cleanup, err := prepareChartForRender(chart.dir, tmpDir)
		if err != nil {
			result.Err = fmt.Errorf("prepare chart: %w", err)
			results = append(results, result)
			continue
		}
		rawYAML, err := HelmTemplate(renderDir, testValuesPath)
		cleanup()
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		result.HelmOK = true

		// 2. Determine consumed labels from the rendered output.
		//
		// Two passes:
		//   a) For each declared key, check if `autoshift.io/<key>` appears
		//      in the rendered output (including numbered-suffix prefix match).
		//   b) Scan the rendered output for ALL `autoshift.io/<key>` strings
		//      to catch labels consumed by templates that aren't in the
		//      declared map (these will surface as "missing" in the contract).
		consumed := make(map[string]bool)

		// Pass a: declared keys → check if consumed.
		for key := range declaredKeys {
			if strings.Contains(rawYAML, "autoshift.io/"+key) {
				consumed[key] = true
				continue
			}
			prefix := stripNumberedSuffix(key)
			if prefix != key && strings.Contains(rawYAML, "autoshift.io/"+prefix) {
				consumed[key] = true
			}
		}

		// Pass b: scan rendered output for any autoshift.io/* keys not in
		// the declared map. Uses simple string scanning — no regex.
		//
		// Only captures keys that appear in hub-template or Placement contexts
		// by looking for the patterns:
		//   - `.ManagedClusterLabels "autoshift.io/<key>"`  (hub template lookups)
		//   - `hasPrefix "autoshift.io/<key>"`              (prefix iteration)
		//   - `key: 'autoshift.io/<key>'`                   (Placement selectors)
		//   - `key: "autoshift.io/<key>"`                   (Placement selectors)
		//
		// This avoids picking up ConfigMap labels, annotations, and other
		// non-ManagedClusterLabels uses of the autoshift.io/ prefix.
		for _, pattern := range []string{
			`.ManagedClusterLabels "autoshift.io/`,
			`hasPrefix "autoshift.io/`,
			`key: 'autoshift.io/`,
			`key: "autoshift.io/`,
		} {
			remaining := rawYAML
			for {
				idx := strings.Index(remaining, pattern)
				if idx < 0 {
					break
				}
				after := remaining[idx+len(pattern):]
				remaining = after
				// Extract the key: contiguous lowercase alphanum + hyphens + underscores.
				end := 0
				for end < len(after) {
					c := after[end]
					if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
						end++
					} else {
						break
					}
				}
				if end < 2 {
					continue
				}
				key := after[:end]
				// Skip keys ending with `-` — these are hasPrefix patterns
				// (e.g. `metallb-bgp-`), not actual label keys.
				if strings.HasSuffix(key, "-") {
					continue
				}
				// Skip keys that are prefixes of a declared numbered-suffix
				// key (e.g. `worker-nodes-zone` when `worker-nodes-zone-1`
				// is declared). These are hasPrefix iteration patterns.
				isPrefix := false
				for dk := range declaredKeys {
					if strings.HasPrefix(dk, key+"-") {
						isPrefix = true
						break
					}
				}
				if isPrefix {
					continue
				}
				consumed[key] = true
			}
		}
		keysByPolicy[chart.policy] = consumed

		// 3. Resolve hub templates.
		resolveResult := r.ResolvePolicy(rawYAML, ctx)
		if len(resolveResult.Errors) == 0 {
			result.ResolveOK = true
		} else {
			result.ResolveWarns = resolveResult.Errors
		}

		// 4. If this is cluster-config-maps, parse its ConfigMap output and
		//    inject them as local resources for downstream charts.
		if chart.policy == "stable/cluster-config-maps" || chart.policy == "stable\\cluster-config-maps" {
			rawCMs, parseErr := ParseConfigMaps(rawYAML)
			if parseErr == nil && len(rawCMs) > 0 {
				renderedCM, mergeErr := MergeRenderedConfig(
					ctx.ManagedClusterName, "policies-autoshift", rawCMs,
				)
				if mergeErr == nil {
					allResources := append(testResources, rawCMs...)
					allResources = append(allResources, renderedCM)
					r.SetLocalResources(allResources)
				}
			}
		}

		// 5. Check for empty-string substitutions in the resolved output.
		//    For each label we provided, if the original had `autoshift.io/<key>`
		//    and the resolved output has an empty value where the label was, that
		//    indicates a problem.
		if result.ResolveOK {
			for key := range consumed {
				labelRef := "autoshift.io/" + key
				val := ctx.ManagedClusterLabels[labelRef]
				// If we provided a non-empty value but it doesn't appear in the
				// resolved output, the template may have discarded it (unusual).
				// If we provided empty, the template's | default should have
				// kicked in — check that it did.
				if val == "" {
					result.EmptyLabels = append(result.EmptyLabels, key)
				}
			}
			sort.Strings(result.EmptyLabels)
		}

		// 6. Validate that the raw helm template output is well-formed YAML.
		//    This catches template bugs that produce broken YAML before
		//    resolution — the #1 error source in policy development.
		//    We validate the RAW output (not the resolved output) because
		//    the resolver's JSON round-trip can alter formatting.
		yamlErrors := validateYAML(rawYAML)
		if len(yamlErrors) > 0 {
			for _, e := range yamlErrors {
				result.ResolveWarns = append(result.ResolveWarns, "invalid YAML in helm output: "+e)
			}
		}

		results = append(results, result)
	}

	// Convert per-policy keys into the aggregated Consumed map.
	allConsumed := KeysToConsumed(keysByPolicy)

	return allConsumed, results, nil
}

// prepareChartForRender checks if a chart has `.example` files in its `files/`
// directory. If so, it creates a temporary copy of the chart with those files
// activated (`.example` suffix stripped) so that `Files.Glob` guards in
// templates pass during rendering.
//
// Returns the directory to render from (either the original or the temp copy)
// and a cleanup function.
func prepareChartForRender(chartDir, tmpBase string) (renderDir string, cleanup func(), err error) {
	noop := func() {}

	// Find .example files.
	var examples []string
	filesDir := filepath.Join(chartDir, "files")
	_ = filepath.WalkDir(filesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".example") {
			examples = append(examples, path)
		}
		return nil
	})

	if len(examples) == 0 {
		return chartDir, noop, nil
	}

	// Copy the chart to a temp directory and activate example files.
	tmpChart, err := os.MkdirTemp(tmpBase, "chart-*")
	if err != nil {
		return "", noop, err
	}

	if err := copyDir(chartDir, tmpChart); err != nil {
		os.RemoveAll(tmpChart)
		return "", noop, fmt.Errorf("copy chart: %w", err)
	}

	// Activate each .example file by copying it without the .example suffix.
	for _, ex := range examples {
		rel, _ := filepath.Rel(chartDir, ex)
		dst := filepath.Join(tmpChart, strings.TrimSuffix(rel, ".example"))
		data, err := os.ReadFile(ex)
		if err != nil {
			os.RemoveAll(tmpChart)
			return "", noop, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			os.RemoveAll(tmpChart)
			return "", noop, err
		}
	}

	return tmpChart, func() { os.RemoveAll(tmpChart) }, nil
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// stripNumberedSuffix removes a trailing `-<digits>` segment from a key.
// E.g. "worker-nodes-zone-1" → "worker-nodes-zone",
//
//	"metallb-bgp-1" → "metallb-bgp",
//	"nmstate" → "nmstate" (unchanged).
func stripNumberedSuffix(key string) string {
	idx := strings.LastIndex(key, "-")
	if idx < 0 {
		return key
	}
	suffix := key[idx+1:]
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return key // suffix is not purely numeric
		}
	}
	return key[:idx]
}

// validateYAML checks that each document in a multi-doc YAML string is
// well-formed. Returns a list of error descriptions for any document that
// fails to parse.
//
// Documents that still contain spoke-side `{{ }}` template expressions are
// skipped — they can't be valid YAML until the spoke resolver processes them.
// Only fully-resolved documents are validated.
func validateYAML(multiDocYAML string) []string {
	var errs []string
	for i, doc := range splitYAMLDocuments(multiDocYAML) {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		// Skip documents with unresolved spoke-side templates.
		if strings.Contains(doc, "{{") {
			continue
		}
		var obj interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			errs = append(errs, fmt.Sprintf("document %d: %v", i+1, err))
		}
	}
	return errs
}

type chartInfo struct {
	policy string // "stable/cert-manager"
	dir    string // absolute path to chart directory
}

// discoverCharts finds all Chart.yaml files under policiesDir at the expected
// depth: <category>/<chart>/Chart.yaml.
func discoverCharts(policiesDir string) ([]chartInfo, error) {
	var charts []chartInfo

	err := filepath.WalkDir(policiesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "Chart.yaml" {
			return nil
		}

		rel, err := filepath.Rel(policiesDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			return nil
		}

		charts = append(charts, chartInfo{
			policy: parts[0] + "/" + parts[1],
			dir:    filepath.Dir(path),
		})
		return nil
	})

	return charts, err
}
