//go:build integration

package resolver

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
)

// TestScenarios runs every directory under tools/testdata/scenarios/ as a deployment of its own.
//
// TestPipeline_EndToEnd resolves every policy against the values the example files declare, so a
// label has exactly one tested value: quay-db-mode is always bundled there, and the managed and
// external branches never render. A scenario is the missing second value. It is a values file in
// the same shape a user writes, layered on autoshift/values/clustersets/_example.yaml the way a
// per-cluster file is, with the objects that deployment must render beside it.
//
// Each scenario is input, a fake cluster, and expected output:
//
//	values.yaml      a clusterset or per-cluster override, as under autoshift/values/. Its leading
//	                 comment says what deployment this is and what it guards, and is required.
//	cluster/*.yaml   optional cluster objects the policies look up; replaces any object of the same
//	                 kind, namespace and name in tools/testdata/. Secrets may use stringData.
//	expected/*.yaml  partial objects the policies must render, compared the way ACM musthave
//	                 compares. Under expected/<policy-name>/ they must come from that Policy.
//	absent/*.yaml    optional objects no policy may render, selected by kind, name and namespace,
//	                 with the same <policy-name>/ directories.
//
// The run mirrors deployment: the top-level chart renders global.yaml, _example.yaml and the
// scenario together, so its validation templates run, then the scenario's labels and config are
// applied over the example's and the policies resolve against them.
//
// Adding coverage for a mode is a new directory. No Go changes. See tools/testdata/scenarios/README.md.
//
// Every policy chart resolves in every scenario, so a scenario also catches its settings breaking a
// policy it was not written for. A full run costs a few seconds per scenario.
func TestScenarios(t *testing.T) {
	root := repoRoot(t)
	scenariosDir := filepath.Join(root, "tools", "testdata", "scenarios")
	specs, err := filepath.Glob(filepath.Join(scenariosDir, "*", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) == 0 {
		t.Fatalf("no scenarios under %s; the suite would silently cover nothing", scenariosDir)
	}

	declared, err := labels.ExtractDeclaredFromTree(filepath.Join(root, "autoshift", "values"), false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}

	for _, spec := range specs {
		dir := filepath.Dir(spec)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			runScenario(t, root, dir, declared)
		})
	}
}

func runScenario(t *testing.T, root, dir string, declared map[string]*labels.Declared) {
	valuesDir := filepath.Join(root, "autoshift", "values")
	policiesDir := filepath.Join(root, "policies")
	valuesFile := filepath.Join(dir, "values.yaml")

	requireDescription(t, valuesFile)
	expected := loadExpected(t, dir)
	absent := loadAbsent(t, dir)

	// 1. The top-level chart, exactly as an Application renders a per-cluster override.
	renderChart(t, filepath.Join(root, "autoshift"),
		filepath.Join(valuesDir, "global.yaml"),
		filepath.Join(valuesDir, "clustersets", "_example.yaml"),
		valuesFile)

	// 2. Labels and config: the example's, with the scenario's applied over them.
	ov := loadScenarioValues(t, valuesFile)
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	lbls := BuildSyntheticLabels(declared)
	var undeclared []string
	for k, v := range ov.labels {
		if _, ok := declared[k]; !ok {
			undeclared = append(undeclared, k)
		}
		configs.BareLabels[k] = v
		lbls["autoshift.io/"+k] = v
	}
	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		t.Fatalf("values.yaml sets labels no _example*.yaml declares, so no policy reads them as written: %s", strings.Join(undeclared, ", "))
	}
	if configs.HubConfig == nil {
		configs.HubConfig = map[string]any{}
	}
	deepMerge(configs.HubConfig, ov.clusterSetConfig)
	if configs.ClusterInstallConfig == nil {
		configs.ClusterInstallConfig = map[string]any{}
	}
	deepMerge(configs.ClusterInstallConfig, ov.clusterConfig)
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: lbls}

	// 3. The fake cluster: tools/testdata/, with the scenario's cluster/ objects laid over it.
	base, err := LoadTestResources(filepath.Join(root, "tools", "testdata"))
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	stubs, err := LoadTestResources(filepath.Join(dir, "cluster"))
	if err != nil {
		t.Fatalf("load cluster/: %v", err)
	}
	for i := range stubs {
		if err := stringDataToData(&stubs[i]); err != nil {
			t.Fatalf("cluster/ %s/%s: %v", stubs[i].GetKind(), stubs[i].GetName(), err)
		}
	}
	resources := deduplicateResources(base, stubs)

	// 4. Resolve.
	syntheticCMs, err := GenerateSyntheticConfigMaps(configs, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	seed := append(syntheticCMs, resources...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	_, results, err := RunPipelineWithOptions(policiesDir, ctx, nil, r, spokeR, declared, configs, "",
		PipelineOptions{TestResources: resources})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	t.Logf("resolved %d policy charts", len(results))

	// 5. The same hard failures TestPipeline_EndToEnd reports.
	var objs []renderedObject
	var unresolved []string
	empty := map[string]map[string]string{} // chart -> policy -> finding
	for _, res := range results {
		switch {
		case res.Err != nil:
			t.Errorf("%s: %v", res.Policy, res.Err)
			continue
		case !res.ResolveOK:
			for _, w := range res.ResolveWarns {
				t.Errorf("%s: hub resolution error: %s\n\t       %s", res.Policy, w, scenarioHint(w))
			}
		}
		for _, w := range res.SpokeWarns {
			t.Errorf("%s: spoke resolution error: %s\n\t       %s", res.Policy, w, scenarioHint(w))
		}
		for _, w := range res.YAMLErrors {
			t.Errorf("%s: invalid resolved YAML: %s", res.Policy, w)
		}
		o, u, errs := collectRenderedObjects(res.Policy, res.ResolvedYAML)
		objs, unresolved = append(objs, o...), append(unresolved, u...)
		for _, e := range errs {
			t.Errorf("%s", e)
		}
		for _, f := range scanForEmptyPolicies(res.Policy, "scenario", res.ResolvedYAML) {
			m := pgManifests(policiesDir, f.Chart)[f.Policy]
			if !assertionOnlyGate(policiesDir, f.Chart, m) {
				if empty[res.Policy] == nil {
					empty[res.Policy] = map[string]string{}
				}
				empty[res.Policy][f.Policy] = f.String()
			}
		}
	}

	// 6. Placement: only the policies whose Placement selects this cluster reach it.
	unplaced, err := unplacedPolicies(objs, lbls)
	if err != nil {
		t.Fatalf("placement: %v", err)
	}
	placed := objs[:0:0]
	for _, o := range objs {
		if !unplaced[o.Policy] {
			placed = append(placed, o)
		}
	}

	// 7. The expected output, and what must not be rendered.
	subject := evaluateExpected(t, expected, placed, unresolved)
	evaluateAbsent(t, absent, placed)

	// 8. Empty ConfigurationPolicies report Compliant while enforcing nothing. As in the
	// end-to-end check there is no allowlist: in a deployment that selects the policy, applying
	// nothing is the defect, or the policy repeats its placement gate as a conditional. Only the
	// charts an expected object came from are judged. The rest were not written for this
	// deployment, and the end-to-end suite judges them across every profile.
	for _, chart := range sortedKeys(toAnyMap(subject)) {
		for policy := range unplaced {
			delete(empty[chart], policy)
		}
		for _, f := range sortedValues(empty[chart]) {
			t.Errorf("FAIL  %s\n\t       In this deployment the policy applies no objects, so it reports Compliant while\n\t       enforcing nothing.", f)
		}
	}
}

// requireDescription fails a scenario whose values.yaml does not open with a comment saying what
// deployment it is and what it guards: a scenario nobody can explain is one nobody can safely change.
func requireDescription(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("%s: open with a comment saying what deployment this is and what it guards", path)
		}
		return
	}
	t.Fatalf("%s: empty", path)
}

func toAnyMap(m map[string]bool) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// scenarioValues is what a values.yaml contributes: labels, config applied at clusterset level,
// and config applied at cluster level, which a cluster's rendered-config merges over the clusterset's.
type scenarioValues struct {
	labels           map[string]string
	clusterSetConfig map[string]any
	clusterConfig    map[string]any
}

// loadScenarioValues reads the parts of a values file the pipeline models. Anything else would be
// rendered by the top-level chart but ignored by the policy pass, so it is rejected rather than
// appearing to be tested.
func loadScenarioValues(t *testing.T, path string) scenarioValues {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	ov := scenarioValues{labels: map[string]string{}, clusterSetConfig: map[string]any{}, clusterConfig: map[string]any{}}
	read := func(where string, entry any, cfg map[string]any) {
		m, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("%s: %s is not a map", path, where)
		}
		for k, v := range m {
			switch k {
			case "labels":
				for lk, lv := range asMap(v) {
					if _, isStr := lv.(string); !isStr {
						t.Fatalf("%s: %s.labels.%s is %T; quote it, as labels are strings", path, where, lk, lv)
					}
					ov.labels[lk] = lv.(string)
				}
			case "config":
				deepMerge(cfg, asMap(v))
			default:
				t.Fatalf("%s: %s.%s is not modelled by the policy pass; scenarios support labels and config", path, where, k)
			}
		}
	}
	clusters := 0
	// Clusters are read last so a cluster label overrides the same clusterset label, as it does in
	// the cluster-labels policy.
	tops := sortedKeys(raw)
	sort.SliceStable(tops, func(i, j int) bool { return tops[i] != "clusters" && tops[j] == "clusters" })
	for _, top := range tops {
		switch top {
		case "hubClusterSets", "managedClusterSets":
			for _, name := range sortedKeys(asMap(raw[top])) {
				read(top+"."+name, asMap(raw[top])[name], ov.clusterSetConfig)
			}
		case "clusters":
			for _, name := range sortedKeys(asMap(raw[top])) {
				clusters++
				read("clusters."+name, asMap(raw[top])[name], ov.clusterConfig)
			}
		default:
			t.Fatalf("%s: top-level %q is not modelled by the policy pass; scenarios support hubClusterSets, managedClusterSets and clusters", path, top)
		}
	}
	if clusters > 1 {
		t.Fatalf("%s: declares %d clusters; a scenario models one", path, clusters)
	}
	if len(ov.labels) == 0 && len(ov.clusterSetConfig) == 0 && len(ov.clusterConfig) == 0 {
		t.Fatalf("%s: sets nothing, so the scenario is the example profile TestPipeline_EndToEnd already runs", path)
	}
	return ov
}

// stringDataToData does what the API server does on write, so stubs can be read and edited as
// plain text: the policies read data, base64 encoded.
func stringDataToData(u *unstructured.Unstructured) error {
	if u.GetKind() != "Secret" {
		return nil
	}
	sd, ok := u.Object["stringData"]
	if !ok {
		return nil
	}
	m, ok := sd.(map[string]any)
	if !ok {
		return fmt.Errorf("stringData is not a map")
	}
	data, _ := u.Object["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("stringData.%s is %T, not a string", k, v)
		}
		data[k] = base64.StdEncoding.EncodeToString([]byte(s))
	}
	u.Object["data"] = data
	delete(u.Object, "stringData")
	return nil
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// scenarioHint points a resolution failure at where a scenario supplies what was missing.
func scenarioHint(errMsg string) string {
	switch {
	case strings.Contains(errMsg, "Secret") || strings.Contains(errMsg, "ConfigMap"):
		return "hint: add the object to this scenario's cluster/, or to tools/testdata/ if every profile needs it"
	case strings.Contains(errMsg, "lookup"):
		return lookupHint(errMsg)
	default:
		return "hint: check values.yaml against what the template reads, and the template for undefined variables"
	}
}
