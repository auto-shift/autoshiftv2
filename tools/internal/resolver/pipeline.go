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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ChartResult holds the outcome for one policy chart.
type ChartResult struct {
	Policy       string   // "stable/cert-manager"
	ChartDir     string   // path to chart directory
	HelmOK       bool     // helm template succeeded
	ResolveOK    bool     // all Policy documents resolved without error
	ResolveWarns []string // per-document resolution warnings (e.g. lookup failures)
	SpokeWarns   []string // warnings from the spoke-side second pass
	EmptyLabels  []string // label keys that resolved to empty string
	Err          error    // fatal error (helm template failed or zero docs rendered)
	ResolvedYAML string   // final multi-doc YAML after hub+spoke resolution (for output assertions)
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
//  1. Generates synthetic ConfigMaps from example file configs and pre-seeds
//     the hub resolver so all downstream lookup calls get realistic data.
//  2. Discovers charts at <category>/<chart>/Chart.yaml
//  3. Runs `helm template` on each with fully-populated test values
//  4. Verifies each chart renders at least one non-empty document
//  5. Determines which declared labels are consumed by each chart
//  6. Resolves hub templates ({{hub ... hub}}) using the ACM resolver
//  7. Runs a second spoke-side pass ({{ ... }}) for maximum coverage
//  8. Validates YAML on all fully-resolved documents
func RunPipeline(
	policiesDir string,
	ctx HubContext,
	r *Resolver,
	spokeR *Resolver,
	declared map[string]*labels.Declared,
	configs *ExampleConfigs,
	testdataDir string,
) (map[string]*labels.Consumed, []ChartResult, error) {
	charts, err := discoverCharts(policiesDir)
	if err != nil {
		return nil, nil, fmt.Errorf("discover charts: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "autoshift-lint-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	testValuesPath, err := WriteTestValues(tmpDir, ctx.ManagedClusterName, configs)
	if err != nil {
		return nil, nil, fmt.Errorf("write test values: %w", err)
	}

	// Sort charts with cluster-config-maps first so its ConfigMap output can be
	// injected before downstream charts run.
	sort.SliceStable(charts, func(i, j int) bool {
		iCCM := strings.Contains(charts[i].policy, "cluster-config-maps")
		jCCM := strings.Contains(charts[j].policy, "cluster-config-maps")
		if iCCM != jCCM {
			return iCCM
		}
		return charts[i].policy < charts[j].policy
	})

	// Pre-seed resolvers with synthetic ConfigMaps + testdata resources.
	// These provide realistic hub template lookup results from the very first
	// chart. The cluster-config-maps helm output (processed later in the loop)
	// supplements these with the actual rendered values.
	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load test resources: %w", err)
	}

	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		return nil, nil, fmt.Errorf("generate synthetic configmaps: %w", err)
	}

	seedResources := append(syntheticCMs, testResources...)
	r.SetLocalResources(seedResources)
	if spokeR != nil {
		spokeR.SetLocalResources(seedResources)
	}

	// Build the set of declared keys for quick lookup.
	declaredKeys := make(map[string]bool, len(declared))
	for key := range declared {
		declaredKeys[key] = true
	}

	keysByPolicy := map[string]map[string]bool{}
	var results []ChartResult

	for _, chart := range charts {
		result := ChartResult{
			Policy:   chart.policy,
			ChartDir: chart.dir,
		}

		// 1. Prepare chart for rendering (activate .example files if present).
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

		// 2. Non-empty document check — a chart that renders nothing under
		// full-coverage test values almost certainly has a template bug.
		nonEmpty := 0
		for _, doc := range splitYAMLDocuments(rawYAML) {
			if strings.TrimSpace(doc) != "" {
				nonEmpty++
			}
		}
		if nonEmpty == 0 {
			result.Err = fmt.Errorf("chart rendered no documents with full test values — check conditional guards")
			results = append(results, result)
			continue
		}

		// 3. Determine consumed labels from the rendered output (two passes).
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

		// Pass b: scan rendered output for any autoshift.io/* keys not yet in
		// the declared map (these will surface as "missing" in the contract).
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
				if strings.HasSuffix(key, "-") {
					continue
				}
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

		// 4. Hub template resolution (pass 1).
		hubResult := r.ResolvePolicy(rawYAML, ctx)
		if len(hubResult.Errors) == 0 {
			result.ResolveOK = true
		} else {
			result.ResolveWarns = hubResult.Errors
		}

		// 5. Spoke-side resolution (pass 2).
		// Strip string defaults first so any config key the template consumes
		// but the example file doesn't declare produces "<no value>" in the
		// output rather than silently falling back to a hardcoded string.
		spokeInput := stripStringDefaults(hubResult.Resolved)
		if spokeR != nil && strings.Contains(spokeInput, "{{") {
			spokeResult := spokeR.ResolveSpokeTemplates(spokeInput, ctx)
			if len(spokeResult.Errors) > 0 {
				result.SpokeWarns = spokeResult.Errors
			}
			// Use the spoke-resolved output for YAML validation where possible.
			if spokeResult.Resolved != "" {
				spokeInput = spokeResult.Resolved
			}
		}

		// 6. If this is cluster-config-maps, parse its raw ConfigMap output and
		// inject them as local resources for downstream charts. This supplements
		// the synthetic CMs with values actually rendered by helm.
		if strings.HasSuffix(chart.policy, "/cluster-config-maps") || strings.HasSuffix(chart.policy, "\\cluster-config-maps") {
			rawCMs, parseErr := ParseConfigMaps(rawYAML)
			if parseErr == nil && len(rawCMs) > 0 {
				renderedCM, mergeErr := MergeRenderedConfig(
					ctx.ManagedClusterName, "policies-autoshift", rawCMs,
				)
				if mergeErr == nil {
					helmResources := append(testResources, rawCMs...)
					helmResources = append(helmResources, renderedCM)
					// Merge with synthetic CMs: helm output takes precedence.
					// Deduplicate so helm-rendered CMs replace synthetic ones
					// with the same identity (e.g. rendered-config).
					allResources := deduplicateResources(syntheticCMs, helmResources)
					r.SetLocalResources(allResources)
					if spokeR != nil {
						spokeR.SetLocalResources(allResources)
					}
				}
			}
		}

		// 7. Validate YAML on fully-resolved documents.
		yamlErrors := validateYAML(spokeInput)
		if len(yamlErrors) > 0 {
			for _, e := range yamlErrors {
				result.ResolveWarns = append(result.ResolveWarns, "invalid YAML in rendered output: "+e)
			}
		}

		// 8. Track empty-string label substitutions for diagnostics.
		if result.ResolveOK {
			for key := range consumed {
				labelRef := "autoshift.io/" + key
				val := ctx.ManagedClusterLabels[labelRef]
				if val == "" {
					result.EmptyLabels = append(result.EmptyLabels, key)
				}
			}
			sort.Strings(result.EmptyLabels)
		}

		// 9. Preserve final resolved YAML for output assertions in tests.
		result.ResolvedYAML = spokeInput

		results = append(results, result)
	}

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

	tmpChart, err := os.MkdirTemp(tmpBase, "chart-*")
	if err != nil {
		return "", noop, err
	}

	if err := copyDir(chartDir, tmpChart); err != nil {
		os.RemoveAll(tmpChart)
		return "", noop, fmt.Errorf("copy chart: %w", err)
	}

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
			return key
		}
	}
	return key[:idx]
}

// deduplicateResources merges base and override slices, keeping the last
// occurrence of any resource with the same (kind, namespace, name) key.
// Resources in override take precedence over resources in base.
func deduplicateResources(base, override []unstructured.Unstructured) []unstructured.Unstructured {
	type resKey struct{ kind, ns, name string }
	seen := make(map[resKey]int)
	merged := make([]unstructured.Unstructured, 0, len(base)+len(override))

	add := func(r unstructured.Unstructured) {
		k := resKey{
			kind: r.GetKind(),
			ns:   r.GetNamespace(),
			name: r.GetName(),
		}
		if idx, exists := seen[k]; exists {
			merged[idx] = r
		} else {
			seen[k] = len(merged)
			merged = append(merged, r)
		}
	}

	for _, r := range base {
		add(r)
	}
	for _, r := range override {
		add(r)
	}
	return merged
}

// validateYAML checks that each document in a multi-doc YAML string is
// well-formed and free of un-substituted template placeholders.
//
// Documents that still contain spoke-side `{{ }}` template expressions are
// skipped — they can't be valid YAML until the spoke resolver processes them.
func validateYAML(multiDocYAML string) []string {
	var errs []string
	for i, doc := range splitYAMLDocuments(multiDocYAML) {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		if strings.Contains(doc, "{{") {
			continue
		}
		// "<no value>" in output means a template consumed a config key that
		// was absent from the example file (its | default "..." was stripped).
		if strings.Contains(doc, "<no value>") {
			for j, line := range strings.Split(doc, "\n") {
				if strings.Contains(line, "<no value>") {
					errs = append(errs, fmt.Sprintf(
						"document %d line %d: <no value> — config key consumed by template is missing from example file: %s",
						i+1, j+1, strings.TrimSpace(line)))
				}
			}
		}
		var obj interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			errs = append(errs, fmt.Sprintf("document %d: %v", i+1, err))
		}
	}
	return errs
}

// stripStringDefaults removes | default "..." and | default '...' from
// spoke template text while preserving | default dict and | default list.
//
// This is applied before spoke resolution in the test pipeline so that any
// config key the template consumes but the example file doesn't declare will
// produce "<no value>" in the output rather than silently falling back to a
// hardcoded string. Structural defaults (dict, list) are kept because they
// prevent nil panics on optional map/slice lookups.
func stripStringDefaults(s string) string {
	const needle = "| default "
	var out strings.Builder
	for {
		idx := strings.Index(s, needle)
		if idx < 0 {
			out.WriteString(s)
			break
		}
		rest := s[idx+len(needle):]

		// Keep structural defaults (dict, list) — write prefix unchanged.
		if strings.HasPrefix(rest, "dict") || strings.HasPrefix(rest, "list") {
			out.WriteString(s[:idx])
			out.WriteString(needle)
			s = rest
			continue
		}

		// Non-empty quoted string default — strip it along with the
		// preceding whitespace (the space before the pipe).
		if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
			quote := rest[0]
			if end := strings.IndexByte(rest[1:], quote); end >= 0 {
				if quoted := rest[1 : end+1]; quoted != "" {
					out.WriteString(strings.TrimRight(s[:idx], " \t"))
					s = rest[end+2:]
					continue
				}
			}
		}

		// Unknown form or empty-string default — keep as-is.
		out.WriteString(s[:idx])
		out.WriteString(needle)
		s = rest
	}
	return out.String()
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
