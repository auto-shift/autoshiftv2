//go:build integration

package resolver

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// Stress harness for project-onboarding (P1b). It drives kustomize render, hub resolution and spoke
// resolution directly (like TestProjectOnboarding_NoSweepWithoutRenderedConfig), so a test can see
// the hub-resolved Policy (for size measurements), time each stage, and choose its own stubs.

var (
	stressRawCache   sync.Map // remediation -> kustomize output
	stressBaseOnce   sync.Once
	stressBaseCfg    []byte // JSON of ExampleConfigs
	stressDeclared   map[string]*labels.Declared
	stressBaseErr    error
	stressHubCfgOnce sync.Once
	stressHubCfg     map[string]interface{}
)

func stressRaw(t *testing.T, remediation string) string {
	t.Helper()
	if v, ok := stressRawCache.Load(remediation); ok {
		return v.(string)
	}
	out, err := KustomizeBuildWithRemediation(filepath.Join(repoRoot(t), "policies", "stable", "project-onboarding"), remediation)
	if err != nil {
		t.Fatalf("render with REMEDIATION=%s: %v", remediation, err)
	}
	stressRawCache.Store(remediation, out)
	return out
}

// stressConfigs returns a fresh deep copy of the example configs and the declared label set.
func stressConfigs(t *testing.T) (*ExampleConfigs, map[string]*labels.Declared) {
	t.Helper()
	stressBaseOnce.Do(func() {
		valuesDir := filepath.Join(repoRoot(t), "autoshift", "values")
		cfgs, err := ExtractExampleConfigs(valuesDir)
		if err != nil {
			stressBaseErr = err
			return
		}
		stressBaseCfg, stressBaseErr = json.Marshal(cfgs)
		if stressBaseErr != nil {
			return
		}
		stressDeclared, stressBaseErr = labels.ExtractDeclaredFromTree(valuesDir, false)
	})
	if stressBaseErr != nil {
		t.Fatalf("stress base: %v", stressBaseErr)
	}
	var c ExampleConfigs
	if err := json.Unmarshal(stressBaseCfg, &c); err != nil {
		t.Fatal(err)
	}
	return &c, stressDeclared
}

// stressDeploymentConfigPath is a deployment-shaped config: existing namespaces adopted and
// enforced, with group RoleBindings only. It stands in for a real clusterset's projects.
const stressDeploymentConfigPath = "tools/testdata/project-onboarding-migration/projects.yaml"

// stressDeploymentConfig returns a deep copy of the config in stressDeploymentConfigPath.
func stressDeploymentConfig(t *testing.T) map[string]interface{} {
	t.Helper()
	stressHubCfgOnce.Do(func() {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), stressDeploymentConfigPath))
		if err != nil {
			t.Fatal(err)
		}
		if err := sigsyaml.Unmarshal(b, &stressHubCfg); err != nil {
			t.Fatal(err)
		}
	})
	if stressHubCfg == nil {
		t.Fatalf("%s holds no config", stressDeploymentConfigPath)
	}
	raw, _ := json.Marshal(stressHubCfg)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

type stressOpts struct {
	remediation string            // REMEDIATION substituted at render, default enforce
	labels      map[string]string // cluster labels over the synthetic ones
	stubs       []string          // files under tools/testdata/stress to seed
	extra       string            // inline stub documents
	drop        []string          // top-level tools/testdata files to leave out
	noTestdata  bool              // seed no top-level testdata at all
	mutate      func(cfg map[string]interface{})
	allowHubErr bool // a hub resolution error is the expected outcome
	reverseSeed bool // seed the live objects in reverse order
	allShards   bool // resolve all eight shard Policies, not only those onboardingShardsFor keeps
	filtered    bool // resolve only the kept shards even with prune on (the filter's own test)

	// rawEdit edits the rendered Policies before hub resolution.
	rawEdit func(raw string) string
}

type stressResult struct {
	raw       string
	hub       string
	spoke     string
	hubErrs   []string
	spokeErrs []string
	hubDur    time.Duration
	spokeDur  time.Duration
	objs      []onboardingObject
}

func stressParseStubs(t *testing.T, data string) []unstructured.Unstructured {
	t.Helper()
	var out []unstructured.Unstructured
	for _, doc := range splitYAMLDocuments(data) {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("stub does not parse: %v\n%s", err, doc)
		}
		if len(obj) == 0 {
			continue
		}
		out = append(out, unstructured.Unstructured{Object: obj})
	}
	return out
}

// stressSeed builds the resources the hub and spoke resolvers see.
func stressSeed(t *testing.T, o stressOpts, cfgs *ExampleConfigs) []unstructured.Unstructured {
	t.Helper()
	root := repoRoot(t)
	cms, err := GenerateSyntheticConfigMaps(cfgs, "lint-cluster", "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	seed := append([]unstructured.Unstructured{}, cms...)
	if !o.noTestdata {
		src := filepath.Join(root, "tools", "testdata")
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		skip := map[string]bool{}
		for _, d := range o.drop {
			skip[d] = true
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || skip[e.Name()] {
				continue
			}
			objs, err := cachedStubFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			seed = append(seed, objs...)
		}
	}
	for _, s := range o.stubs {
		b, err := os.ReadFile(filepath.Join(root, "tools", "testdata", "stress", s))
		if err != nil {
			t.Fatal(err)
		}
		seed = append(seed, stressParseStubs(t, string(b))...)
	}
	if o.extra != "" {
		seed = append(seed, stressParseStubs(t, o.extra)...)
	}
	return stressDedupeSeed(seed)
}

// stressDedupeSeed keeps the last object of each kind, namespace and name. A stress stub can
// declare an object, such as the kube-system Namespace, that tools/testdata also holds, and the
// fake client refuses a second object of the same name. The stub, seeded later, wins.
func stressDedupeSeed(seed []unstructured.Unstructured) []unstructured.Unstructured {
	key := func(u unstructured.Unstructured) string {
		return u.GetAPIVersion() + "|" + u.GetKind() + "|" + u.GetNamespace() + "|" + u.GetName()
	}
	last := map[string]int{}
	for i, u := range seed {
		last[key(u)] = i
	}
	out := make([]unstructured.Unstructured, 0, len(last))
	for i, u := range seed {
		if last[key(u)] == i {
			out = append(out, u)
		}
	}
	return out
}

// stressParseStubsLenient mirrors LoadTestResources: malformed documents are skipped.
func stressParseStubsLenient(data string) []unstructured.Unstructured {
	var out []unstructured.Unstructured
	for _, doc := range splitYAMLDocuments(data) {
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(strings.TrimSpace(doc)), &obj); err != nil || len(obj) == 0 {
			continue
		}
		out = append(out, unstructured.Unstructured{Object: obj})
	}
	return out
}

// stressRun renders, hub-resolves and spoke-resolves project-onboarding for one configuration.
func stressRun(t *testing.T, o stressOpts) stressResult {
	t.Helper()
	if o.remediation == "" {
		o.remediation = "enforce"
	}
	cfgs, declared := stressConfigs(t)
	if o.mutate != nil {
		o.mutate(cfgs.HubConfig)
	}
	seed := stressSeed(t, o, cfgs)
	if o.reverseSeed {
		for i, j := 0, len(seed)-1; i < j; i, j = i+1, j-1 {
			seed[i], seed[j] = seed[j], seed[i]
		}
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	ctx.ManagedClusterLabels["autoshift.io/project-onboarding"] = "true"
	for k, v := range o.labels {
		if v == "\x00absent" {
			delete(ctx.ManagedClusterLabels, k)
			continue
		}
		ctx.ManagedClusterLabels[k] = v
	}
	res := stressResult{raw: stressRaw(t, o.remediation)}
	// As in runOnboardingShards, prune on resolves every shard.
	all := o.allShards || (ctx.ManagedClusterLabels[onboardingPruneLabel] == "true" && !o.filtered)
	keep := onboardingShardsFor(cfgs, seed, all)
	raw, err := onboardingKeepShards(res.raw, keep)
	if err != nil {
		t.Fatal(err)
	}
	if o.rawEdit != nil {
		raw = o.rawEdit(raw)
	}
	hubR, err := NewResolver(seed)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	hub := hubR.ResolvePolicy(raw, ctx)
	res.hubDur = time.Since(start)
	res.hub, res.hubErrs = hub.Resolved, hub.Errors
	if len(hub.Errors) > 0 {
		if !o.allowHubErr {
			t.Fatalf("hub resolve: %v", hub.Errors)
		}
		return res
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	spoke := spokeR.ResolveSpokeTemplates(hub.Resolved, ctx)
	res.spokeDur = time.Since(start)
	res.spoke, res.spokeErrs = spoke.Resolved, spoke.Errors
	if len(spoke.Errors) > 0 {
		t.Fatalf("spoke resolve: %v", spoke.Errors)
	}
	checkOnboardingShardsResolved(t, spoke.Resolved, keep)
	res.objs = onboardingObjects(t, ChartResult{ResolvedYAML: spoke.Resolved})
	return res
}

// stressProjects returns the projects map of cfg (created when missing).
func stressProjects(cfg map[string]interface{}) map[string]interface{} {
	p, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		p = map[string]interface{}{}
		cfg["projects"] = p
	}
	return p
}

// stressAddAB adds the stress-a (fully featured) and stress-b (minimal) projects that the
// stress-onboarding-prune.yaml stubs belong to.
func stressAddAB(cfg map[string]interface{}) {
	projects := stressProjects(cfg)
	projects["stress-a"] = map[string]interface{}{
		"team": "a", "environment": "PROD",
		"groups":         map[string]interface{}{"admin": "A_ADMINS", "write": "A_WRITERS", "read": "A_READERS"},
		"size":           "small",
		"networkProfile": "strict",
		"network": map[string]interface{}{"rules": map[string]interface{}{
			"from-web": map[string]interface{}{
				"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"example.com/team": "web"}}},
			},
			"deny-db": map[string]interface{}{
				"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"stress-db"}}},
			},
			"dns": map[string]interface{}{"enabled": false},
		}},
		"egressIP": []interface{}{"198.51.100.10"},
	}
	projects["stress-b"] = map[string]interface{}{
		"groups": map[string]interface{}{"admin": "B_ADMINS"},
	}
}

func stressProject(cfg map[string]interface{}, name string) map[string]interface{} {
	return stressProjects(cfg)[name].(map[string]interface{})
}

// stressKey identifies an object: kind|namespace|name.
func stressKey(o onboardingObject) string {
	meta, _ := o.obj["metadata"].(map[string]interface{})
	ns, _ := meta["namespace"].(string)
	name, _ := meta["name"].(string)
	kind, _ := o.obj["kind"].(string)
	return kind + "|" + ns + "|" + name
}

// stressPruned maps every mustnothave entry (status ConfigMap excluded) to the remediationAction
// of the ConfigurationPolicy that carries it; a key carried by both reads "enforce+inform".
func stressPruned(objs []onboardingObject) map[string]string {
	out := map[string]string{}
	for _, o := range objs {
		if o.complianceType != "mustnothave" || isStatus(o) {
			continue
		}
		k := stressKey(o)
		if prev, ok := out[k]; ok && prev != o.remediationAction {
			out[k] = "enforce+inform"
			continue
		}
		out[k] = o.remediationAction
	}
	return out
}

// stressRendered maps every rendered (not mustnothave) object to its ConfigurationPolicy
// remediationAction, status ConfigMap excluded.
func stressRendered(objs []onboardingObject) map[string]string {
	out := map[string]string{}
	for _, o := range objs {
		if o.complianceType == "mustnothave" || isStatus(o) {
			continue
		}
		k := stressKey(o)
		if prev, ok := out[k]; ok && prev != o.remediationAction {
			out[k] = "enforce+inform"
			continue
		}
		out[k] = o.remediationAction
	}
	return out
}

// stressStatus returns the status ConfigMap data (empty when cleared).
func stressStatus(t *testing.T, objs []onboardingObject) map[string]string {
	t.Helper()
	return onboardingStatus(t, objs)
}

// stressPolicySize is the measured size of one hub-resolved Policy.
type stressPolicySize struct {
	name string
	real int            // compact JSON size of the Policy
	est  int            // the hub's size estimate from its comment, -1 when absent
	raw  map[string]int // object-templates-raw size per ConfigurationPolicy
}

// stressPolicySizes measures every Policy in yamlDocs (every shard for project onboarding): the
// compact JSON size (HTML escaping off, as a lower bound of what the API server stores), the size
// estimate the hub published in its comment, and the size of each ConfigurationPolicy's
// object-templates-raw, keyed by name.
func stressPolicySizes(t *testing.T, yamlDocs string) []stressPolicySize {
	t.Helper()
	var out []stressPolicySize
	for _, obj := range onboardingPolicies(yamlDocs) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(obj)
		b := bytes.TrimSpace(buf.Bytes())
		name, _ := nestedString(obj, "metadata", "name")
		ps := stressPolicySize{name: name, real: len(b), est: -1, raw: map[string]int{}}
		if m := sizeEstimateRe.FindSubmatch(b); m != nil {
			ps.est, _ = strconv.Atoi(string(m[1]))
		}
		spec, _ := obj["spec"].(map[string]interface{})
		for _, tpl := range spec["policy-templates"].([]interface{}) {
			def := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			cp := def["metadata"].(map[string]interface{})["name"].(string)
			raw, _ := def["spec"].(map[string]interface{})["object-templates-raw"].(string)
			ps.raw[cp] = len(raw)
		}
		out = append(out, ps)
	}
	if len(out) == 0 {
		t.Fatal("no Policy document")
	}
	return out
}

// stressPolicyJSONSize returns the compact JSON size of the largest Policy document in yamlDocs
// (the largest shard), and the size of each ConfigurationPolicy's object-templates-raw over all of
// them, keyed by name.
func stressPolicyJSONSize(t *testing.T, yamlDocs string) (int, map[string]int) {
	t.Helper()
	largest := 0
	per := map[string]int{}
	for _, ps := range stressPolicySizes(t, yamlDocs) {
		if ps.real > largest {
			largest = ps.real
		}
		for k, v := range ps.raw {
			per[k] = v
		}
	}
	return largest, per
}
