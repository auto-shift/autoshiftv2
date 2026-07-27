package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// NamedContext pairs a cluster resolution context with a short profile name
// (e.g. "managed-vmware") used in diagnostics and per-profile assertions.
type NamedContext struct {
	Name string
	Ctx  HubContext
}

// ContextResult holds the resolution outcome for one chart against one extra
// cluster profile.
type ContextResult struct {
	ResolveOK    bool
	ResolveWarns []string
	SpokeWarns   []string
	YAMLErrors   []string // malformed YAML / <no value> in the fully-resolved output
	ResolvedYAML string
}

// ChartResult holds the outcome for one policy chart.
type ChartResult struct {
	Policy       string   // "stable/cert-manager"
	ChartDir     string   // path to chart directory
	HelmOK       bool     // helm template succeeded
	ResolveOK    bool     // all Policy documents resolved without error
	ResolveWarns []string // per-document resolution warnings (e.g. lookup failures)
	SpokeWarns   []string // warnings from the spoke-side second pass
	YAMLErrors   []string // malformed YAML / <no value> in the fully-resolved primary output
	EmptyLabels  []string // label keys that resolved to empty string
	Err          error    // fatal error (helm template failed or zero docs rendered)
	ResolvedYAML string   // final multi-doc YAML after hub+spoke resolution (for output assertions)

	// ExtraResults holds resolution outcomes for each additional cluster profile
	// passed to RunPipeline (e.g. managed-baremetal, managed-aws, managed-vmware),
	// keyed by profile name. Same rendered YAML and seed resources as the primary
	// pass — only .ManagedClusterLabels differ. Empty when no extra contexts were
	// supplied.
	ExtraResults map[string]ContextResult
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

// KustomizeBuild renders a PolicyGenerator policy directory the same way the
// repo-server CMP does: substitute the per-deployment ${...} placeholders, then
// run `kustomize build` with the same flags as the CMP. Test values mirror a
// non-dryRun deployment (REMEDIATION=enforce) so object-templates render in
// enforce mode.
//
// The kustomize binary and PolicyGenerator plugin come from (in order):
// $KUSTOMIZE_BIN / $KUSTOMIZE_PLUGIN_HOME, then the repo-local .tools/ that
// `make install-policy-generator` stages, then `kustomize` on PATH.
func KustomizeBuild(policyDir string) (string, error) {
	repl := strings.NewReplacer(
		"${POLICY_NAMESPACE}", "policies-autoshift",
		"${REMEDIATION}", "enforce",
		"${EVAL_COMPLIANT}", "10m",
		"${EVAL_NONCOMPLIANT}", "30s",
		"${CLUSTER_SET_SUFFIX}", "",
	)
	work, err := os.MkdirTemp("", "autoshift-kustomize-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)

	// Stage the policy dir, substituting placeholders in every file. The exact
	// ${...} tokens never appear in manifests (whose hub templates use $var), so
	// substituting broadly is safe — mirrors an envsubst restricted to these vars.
	//
	// A policy may render a shared Helm chart via a nested kustomization whose
	// helmGlobals.chartHome reaches up to the repo-level components/ dir. To keep
	// that relative path resolvable inside the isolated work tree, stage the policy
	// at its repo-relative path and copy components/ alongside. Falls back to flat
	// staging when no components/ root is found (policies that don't use it are
	// unaffected — they just render from a deeper path).
	absPolicy, _ := filepath.Abs(policyDir)
	buildTarget := work
	if root := findComponentsRoot(absPolicy); root != "" {
		rel, _ := filepath.Rel(root, absPolicy)
		buildTarget = filepath.Join(work, rel)
		if err := copyDirSubst(absPolicy, buildTarget, repl); err != nil {
			return "", fmt.Errorf("stage kustomize dir: %w", err)
		}
		if err := copyDirSubst(filepath.Join(root, "components"), filepath.Join(work, "components"), repl); err != nil {
			return "", fmt.Errorf("stage components dir: %w", err)
		}
	} else if err := copyDirSubst(policyDir, work, repl); err != nil {
		return "", fmt.Errorf("stage kustomize dir: %w", err)
	}

	bin, pluginHome := resolveKustomizeTools(policyDir)
	// Match the flags the repo-server CMP uses so the tested render == the deployed one.
	cmd := exec.Command(bin, "build", "--enable-alpha-plugins", "--enable-helm",
		"--load-restrictor", "LoadRestrictionsNone", buildTarget)
	cmd.Env = os.Environ()
	// PolicyGenerator renders a manifest path that is itself a kustomization (for the
	// shared-chart pattern) by spawning a nested `kustomize build`; that nested build is
	// configured by these env vars, NOT the outer flags above.
	cmd.Env = append(cmd.Env,
		"POLICY_GEN_ENABLE_HELM=true",
		"POLICY_GEN_DISABLE_LOAD_RESTRICTORS=true",
	)
	if pluginHome != "" {
		cmd.Env = append(cmd.Env, "KUSTOMIZE_PLUGIN_HOME="+pluginHome)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return "", fmt.Errorf("kustomize not found for %s — run `make install-policy-generator` "+
				"(stages kustomize + the PolicyGenerator plugin into .tools/): %w", policyDir, err)
		}
		return "", fmt.Errorf("kustomize build %s: %w\n%s", policyDir, err, out)
	}
	return string(out), nil
}

// resolveKustomizeTools picks the kustomize binary and plugin home, preferring
// explicit env vars, then the repo-local .tools/ from `make install-policy-generator`
// (found by walking up from policyDir), then `kustomize` on PATH.
func resolveKustomizeTools(policyDir string) (bin, pluginHome string) {
	bin = os.Getenv("KUSTOMIZE_BIN")
	pluginHome = os.Getenv("KUSTOMIZE_PLUGIN_HOME")
	if bin != "" && pluginHome != "" {
		return bin, pluginHome
	}
	for dir := policyDir; ; {
		if bin == "" {
			if cand := filepath.Join(dir, ".tools", "kustomize"); isFile(cand) {
				bin = cand
			}
		}
		if pluginHome == "" {
			if cand := filepath.Join(dir, ".tools", "kustomize-plugin"); isDir(cand) {
				pluginHome = cand
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir || (bin != "" && pluginHome != "") {
			break
		}
		dir = parent
	}
	if bin == "" {
		bin = "kustomize"
	}
	return bin, pluginHome
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// copyDirSubst copies src to dst, applying repl to the contents of every file.
// findComponentsRoot walks up from dir to the nearest ancestor containing a
// components/ directory (the repo-level home for shared Helm charts). Returns ""
// if none is found before the filesystem root.
func findComponentsRoot(dir string) string {
	for {
		if st, err := os.Stat(filepath.Join(dir, "components")); err == nil && st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func copyDirSubst(src, dst string, repl *strings.Replacer) error {
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
		return os.WriteFile(target, []byte(repl.Replace(string(data))), 0o644)
	})
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
	extraCtxs []NamedContext,
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

	// resolvePasses runs the two-stage resolution for one chart's rendered YAML
	// against a given cluster context: pass 1 resolves hub templates
	// ({{hub ... hub}}), pass 2 resolves spoke templates ({{ ... }}). Returns
	// (hubResolveOK, hubErrors, spokeErrors, resolvedYAML). Called once for the
	// primary context and, when managedCtx is set, once for the managed context.
	resolvePasses := func(rawYAML string, c HubContext) (bool, []string, []string, string) {
		var resolveWarns, spokeWarns []string
		resolveOK := false

		hubResult := r.ResolvePolicy(rawYAML, c)
		if len(hubResult.Errors) == 0 {
			resolveOK = true
		} else {
			resolveWarns = hubResult.Errors
		}

		// Strip string defaults first so any config key the template consumes but
		// the example file doesn't declare produces "<no value>" in the output
		// rather than silently falling back to a hardcoded string.
		spokeInput := stripStringDefaults(hubResult.Resolved)
		if spokeR != nil && strings.Contains(spokeInput, "{{") {
			spokeResult := spokeR.ResolveSpokeTemplates(spokeInput, c)
			if len(spokeResult.Errors) > 0 {
				spokeWarns = spokeResult.Errors
			}
			if spokeResult.Resolved != "" {
				spokeInput = spokeResult.Resolved
			}
		}
		return resolveOK, resolveWarns, spokeWarns, spokeInput
	}

	for _, chart := range charts {
		result := ChartResult{
			Policy:   chart.policy,
			ChartDir: chart.dir,
		}

		// 1. Render the policy — kustomize+PolicyGenerator or Helm, per marker file.
		var rawYAML string
		if chart.kind == "kustomize" {
			rawYAML, err = KustomizeBuild(chart.dir)
			if err != nil {
				result.Err = err
				results = append(results, result)
				continue
			}
		} else {
			// Prepare chart for rendering (activate .example files if present).
			renderDir, cleanup, perr := prepareChartForRender(chart.dir, tmpDir)
			if perr != nil {
				result.Err = fmt.Errorf("prepare chart: %w", perr)
				results = append(results, result)
				continue
			}
			rawYAML, err = HelmTemplate(renderDir, testValuesPath)
			cleanup()
			if err != nil {
				result.Err = err
				results = append(results, result)
				continue
			}
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
			`key: autoshift.io/`, // unquoted: kustomize/PolicyGenerator placement predicates
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

		// 4-5. Resolve hub + spoke templates against the primary (hub,
		// self-managed) context.
		var spokeInput string
		result.ResolveOK, result.ResolveWarns, result.SpokeWarns, spokeInput = resolvePasses(rawYAML, ctx)

		// 5b. Resolve against each additional cluster profile (managed spokes,
		// one per install platform). Same rendered YAML and seed resources — only
		// .ManagedClusterLabels differ — so hub templates that branch on
		// clusterset identity / provider get every profile's branch exercised.
		if len(extraCtxs) > 0 {
			result.ExtraResults = make(map[string]ContextResult, len(extraCtxs))
			for _, ec := range extraCtxs {
				ok, rw, sw, out := resolvePasses(rawYAML, ec.Ctx)
				result.ExtraResults[ec.Name] = ContextResult{
					ResolveOK:    ok,
					ResolveWarns: rw,
					SpokeWarns:   sw,
					YAMLErrors:   validateYAML(out),
					ResolvedYAML: out,
				}
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

		// 7. Validate YAML on fully-resolved documents (primary context; extra
		// contexts are validated inline in step 5b). These are surfaced as their
		// own hard failures (result.YAMLErrors), independent of hub ResolveOK, so
		// malformed YAML / <no value> on an otherwise-clean chart still fails CI.
		result.YAMLErrors = validateYAML(spokeInput)

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
		// id names the offending document by its Kind/name so a developer can go
		// straight to the source: a resolved Policy "policy-<x>" maps to the
		// template policies/<chart>/templates/policy-<x>.yaml.
		id := docIdentity(doc, i)
		// "<no value>" in output means a template consumed a config key that
		// was absent from the example file (its | default "..." was stripped).
		if strings.Contains(doc, "<no value>") {
			for j, line := range strings.Split(doc, "\n") {
				if strings.Contains(line, "<no value>") {
					errs = append(errs, fmt.Sprintf(
						"%s line %d: <no value> — a config key the template reads is missing from the relevant _example*.yaml, or a lookup returned nothing (add a tools/testdata/ stub): %s",
						id, j+1, strings.TrimSpace(line)))
				}
			}
		}
		var obj interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			errs = append(errs, fmt.Sprintf("%s: malformed YAML: %v", id, err))
		}
	}
	return errs
}

// docIdentity returns a short "Kind/name (document N)" label for a resolved
// YAML document, scanned leniently so it still works when the document is
// malformed deeper down. Falls back to "document N" when kind/name aren't found.
func docIdentity(doc string, idx int) string {
	var kind, name string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if kind == "" && strings.HasPrefix(trimmed, "kind:") {
			kind = strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:"))
		}
		// Top-level metadata.name sits at two-space indent; deeper `name:` fields
		// (inside object-templates-raw, refs, etc.) are more indented.
		if name == "" && strings.HasPrefix(line, "  name:") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		}
		if kind != "" && name != "" {
			break
		}
	}
	switch {
	case kind != "" && name != "":
		return fmt.Sprintf("%s/%s (document %d)", kind, name, idx+1)
	case kind != "":
		return fmt.Sprintf("%s (document %d)", kind, idx+1)
	default:
		return fmt.Sprintf("document %d", idx+1)
	}
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
	kind   string // "helm" (Chart.yaml) or "kustomize" (policy-generator-config.yaml)
}

// discoverCharts finds all policy directories under policiesDir at the expected
// depth <category>/<chart>, discriminated by MARKER FILE (matching the hybrid
// ApplicationSet):
//   - policy-generator-config.yaml -> PolicyGenerator/kustomize policy
//   - Chart.yaml                   -> Helm policy
//
// A migrated policy drops Chart.yaml, so each dir yields exactly one entry.
func discoverCharts(policiesDir string) ([]chartInfo, error) {
	var charts []chartInfo

	err := filepath.WalkDir(policiesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		var kind string
		switch d.Name() {
		case "policy-generator-config.yaml":
			kind = "kustomize"
		case "Chart.yaml":
			kind = "helm"
		default:
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
			kind:   kind,
		})
		return nil
	})

	return charts, err
}
