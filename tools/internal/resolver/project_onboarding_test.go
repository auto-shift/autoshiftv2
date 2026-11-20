//go:build integration

package resolver

import (
	"encoding/json"
	"fmt"
	"hash/adler32"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

const projectOnboardingPolicy = "stable/project-onboarding"

// onboardingShards is the fixed number of shard Policies, policy-project-onboarding-0 to -7. Each
// has an inform-only policy-project-onboarding-<shard>-test.
const onboardingShards = 8

// onboardingShardPrefix is the name of a shard Policy without its shard number.
const onboardingShardPrefix = "policy-project-onboarding-"

// onboardingTestSuffix ends the name of the inform-only Policy of a shard, and of its one
// ConfigurationPolicy; the shard Policy's ConfigurationPolicy ends in -enforce.
const onboardingTestSuffix = "-test"

// onboardingShardOf is the shard a project name belongs to: adler32 of the name, mod 8, the same
// mapping the template computes with sprig adler32sum.
func onboardingShardOf(name string) int {
	return int(adler32.Checksum([]byte(name)) % onboardingShards)
}

// onboardingObject is one object rendered into a ConfigurationPolicy, tagged with the
// ConfigurationPolicy that carries it and its root Policy. For project onboarding, policy is the
// shard, policy-project-onboarding-<shard>, for both the shard Policy and its -test Policy, so
// every assertion on shard routing reads the same; rootPolicy is the Policy itself and
// configPolicy tells the two apart (-enforce or -test).
type onboardingObject struct {
	policy            string
	rootPolicy        string
	configPolicy      string
	remediationAction string
	complianceType    string
	item              map[string]interface{}
	obj               map[string]interface{}
}

// runOnboarding runs the full pipeline with the example config optionally mutated, and returns the
// resolved Policy for stable/project-onboarding. Same flow as runMutated (mutation_test.go), which
// sits behind the mutation build tag and so cannot be shared.
func runOnboarding(t *testing.T, mutate func(projects map[string]interface{})) ChartResult {
	t.Helper()
	if mutate == nil {
		return runOnboardingConfig(t, nil)
	}
	return runOnboardingConfig(t, func(cfg map[string]interface{}) {
		projects, ok := cfg["projects"].(map[string]interface{})
		if !ok {
			t.Fatal("_example.yaml config.projects is missing")
		}
		mutate(projects)
	})
}

// runOnboardingConfig is runOnboarding with access to the whole example config, for mutations of
// config.projectDefaults.
func runOnboardingConfig(t *testing.T, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingLabels(t, nil, mutate)
}

// onboardingPruneLabel is the cluster label that opts a cluster into deletion with 'true'.
const onboardingPruneLabel = "autoshift.io/project-onboarding-prune"

// pruneOn is the cluster label that opts a cluster into deletion.
var pruneOn = map[string]string{onboardingPruneLabel: "true"}

// runPrune is runOnboarding on a cluster labelled project-onboarding-prune: 'true'.
func runPrune(t *testing.T, mutate func(projects map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingLabels(t, pruneOn, func(cfg map[string]interface{}) {
		if mutate != nil {
			mutate(cfg["projects"].(map[string]interface{}))
		}
	})
}

// runOnboardingLabels is runOnboardingConfig with cluster labels overridden on top of the
// synthetic ones (declared example values).
func runOnboardingLabels(t *testing.T, clusterLabels map[string]string, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingIn(t, filepath.Join(repoRoot(t), "tools", "testdata"), clusterLabels, mutate)
}

// runOnboardingIn is runOnboardingLabels against the stubs in testdataDir, for cases that need the
// managed cluster to look different from the shared testdata (see testdataWithout).
func runOnboardingIn(t *testing.T, testdataDir string, clusterLabels map[string]string, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingAll(t, testdataDir, clusterLabels, func(c *ExampleConfigs) {
		if mutate != nil {
			mutate(c.HubConfig)
		}
	})
}

// runOnboardingAll is runOnboardingIn with access to every example config, including the
// cluster-level ClusterInstallConfig that the lint cluster's rendered config merges over the hub
// clusterset config.
func runOnboardingAll(t *testing.T, testdataDir string, clusterLabels map[string]string, mutate func(c *ExampleConfigs)) ChartResult {
	t.Helper()
	return runOnboardingShards(t, testdataDir, clusterLabels, mutate, false)
}

// runOnboardingAllShards is runOnboardingLabels resolving all eight shard Policies, for tests that
// assert on shards holding none of the projects.
func runOnboardingAllShards(t *testing.T, clusterLabels map[string]string, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingShards(t, filepath.Join(repoRoot(t), "tools", "testdata"), clusterLabels, func(c *ExampleConfigs) {
		if mutate != nil {
			mutate(c.HubConfig)
		}
	}, true)
}

// runPruneAllShards is runPrune resolving all eight shard Policies, for assertions that something is
// absent or pruned exactly once: a shard holding none of the projects must not add an entry either.
func runPruneAllShards(t *testing.T, mutate func(projects map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingAllShards(t, pruneOn, onProjects(mutate))
}

// runOnboardingProjectsAllShards is runOnboarding resolving all eight shard Policies.
func runOnboardingProjectsAllShards(t *testing.T, mutate func(projects map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingAllShards(t, nil, onProjects(mutate))
}

// runOnboardingInAllShards is runOnboardingIn resolving all eight shard Policies.
func runOnboardingInAllShards(t *testing.T, testdataDir string, clusterLabels map[string]string, mutate func(cfg map[string]interface{})) ChartResult {
	t.Helper()
	return runOnboardingShards(t, testdataDir, clusterLabels, func(c *ExampleConfigs) {
		if mutate != nil {
			mutate(c.HubConfig)
		}
	}, true)
}

// onProjects adapts a mutation of config.projects to one of the whole config.
func onProjects(mutate func(projects map[string]interface{})) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		if mutate != nil {
			mutate(cfg["projects"].(map[string]interface{}))
		}
	}
}

// runOnboardingShards is runOnboardingAll; with allShards false and prune off it resolves only the
// shard Policies that can render anything for this config and these stubs (onboardingShardsFor).
// Prune on always resolves all eight: those tests assert that objects are pruned exactly once or
// not at all, which a shard outside the filter's model of the routing could break.
func runOnboardingShards(t *testing.T, testdataDir string, clusterLabels map[string]string, mutate func(c *ExampleConfigs), allShards bool) ChartResult {
	t.Helper()
	allShards = allShards || clusterLabels[onboardingPruneLabel] == "true"
	root := repoRoot(t)
	// A fresh deep copy of the example configs, so a mutation never leaks into another run, and the
	// declared labels, both read from the values files once per test binary (stressConfigs).
	cloned, declared := stressConfigs(t)
	if mutate != nil {
		mutate(cloned)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	for k, v := range clusterLabels {
		ctx.ManagedClusterLabels[k] = v
	}
	syntheticCMs, err := GenerateSyntheticConfigMaps(cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testResources, _ := cachedLoadTestResources(testdataDir)
	seed := append(syntheticCMs, testResources...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	// Render only this policy: the full tree is ~100 charts and these tests make many runs.
	// The synthetic ConfigMaps it reads are seeded by runPipeline itself, so it does not
	// depend on cluster-config-maps rendering first.
	onlyOnboarding := func(policy string) bool { return policy == projectOnboardingPolicy }
	keep := onboardingShardsFor(cloned, testResources, allShards)
	_, results, err := runPipelineTransform(filepath.Join(root, "policies"), onlyOnboarding, cachedRender, onboardingShardFilter(t, keep), ctx, nil, r, spokeR, declared, cloned, testdataDir)
	if err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	for _, res := range results {
		if res.Policy == projectOnboardingPolicy {
			if res.Err != nil || !res.ResolveOK {
				t.Fatalf("%s did not resolve: err=%v warns=%v", res.Policy, res.Err, res.ResolveWarns)
			}
			checkOnboardingShardsResolved(t, res.ResolvedYAML, keep)
			return res
		}
	}
	t.Fatalf("%s not found in pipeline results", projectOnboardingPolicy)
	return ChartResult{}
}

// onboardingShardsFor returns the shards whose Policy can render anything besides its own status
// for this config and these live objects, every shard with allShards: shard 0 (the admin tier and
// objects naming no project), the shard of every configured project name (its objects, and its
// namespace's objects in the prune sweep), of every project a live object names in its
// autoshift.io/project label, and of the project the name of a live EgressIP autoshift-<p> or
// AdminNetworkPolicy autoshift-project-<p> names. This mirrors the routing in projects.yaml
// ($projShard and the prune sweep). Every other shard holds no project and no object, so it only
// clears its status, or reports what every shard reports (the rendered config, the total count).
// Tests that assert on such shards render all of them.
func onboardingShardsFor(cfgs *ExampleConfigs, live []unstructured.Unstructured, allShards bool) map[int]bool {
	keep := map[int]bool{0: true}
	if allShards {
		for shard := 1; shard < onboardingShards; shard++ {
			keep[shard] = true
		}
		return keep
	}
	configs := []map[string]interface{}{cfgs.HubConfig, cfgs.ClusterInstallConfig}
	for _, extra := range cfgs.ClusterInstallExtra {
		configs = append(configs, extra)
	}
	for _, cfg := range configs {
		projects, _ := cfg["projects"].(map[string]interface{})
		for name := range projects {
			keep[onboardingShardOf(name)] = true
		}
	}
	for _, u := range live {
		// A Namespace onboarding created is deleted, reported or kept by the shard of the project its
		// label names once that project is removed.
		if u.GetKind() == "Namespace" && u.GetLabels()["autoshift.io/onboarding-created"] == "true" {
			if p := u.GetLabels()["autoshift.io/project"]; p != "" {
				keep[onboardingShardOf(p)] = true
			}
			continue
		}
		// The sweep reads only these kinds, and only objects labeled managed-by project-onboarding.
		// Any other labeled Namespace counts through its declaring project, which is configured.
		if !onboardingSweptKinds[u.GetKind()] || u.GetLabels()["autoshift.io/managed-by"] != "project-onboarding" {
			continue
		}
		if p := u.GetLabels()["autoshift.io/project"]; p != "" {
			keep[onboardingShardOf(p)] = true
		}
		name := u.GetName()
		switch {
		case u.GetKind() == "EgressIP" && strings.HasPrefix(name, "autoshift-"):
			keep[onboardingShardOf(strings.TrimPrefix(name, "autoshift-"))] = true
		case u.GetKind() == "AdminNetworkPolicy" && strings.HasPrefix(name, "autoshift-project-"):
			keep[onboardingShardOf(strings.TrimPrefix(name, "autoshift-project-"))] = true
		}
	}
	return keep
}

// onboardingSweptKinds are the kinds the prune sweep of projects.yaml looks up.
var onboardingSweptKinds = map[string]bool{"RoleBinding": true, "ResourceQuota": true, "LimitRange": true,
	"NetworkPolicy": true, "EgressFirewall": true, "AdminNetworkPolicy": true, "EgressIP": true}

// onboardingPolicyNameRe finds the name of a shard Policy, or of its -test Policy, in its rendered
// document.
var onboardingPolicyNameRe = regexp.MustCompile(`(?m)^  name: (policy-project-onboarding-[0-9]+(?:-test)?)$`)

// onboardingShardOfPolicy returns the shard of a shard Policy name or of its -test Policy name, or
// -1 for any other name.
func onboardingShardOfPolicy(name string) int {
	if !strings.HasPrefix(name, onboardingShardPrefix) {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, onboardingShardPrefix), onboardingTestSuffix))
	if err != nil {
		return -1
	}
	return n
}

// onboardingShardPolicy returns the shard Policy name for a shard Policy or its -test Policy, and
// any other name unchanged.
func onboardingShardPolicy(name string) string {
	if shard := onboardingShardOfPolicy(name); shard >= 0 {
		return fmt.Sprintf("%s%d", onboardingShardPrefix, shard)
	}
	return name
}

// onboardingKeepShards drops from rendered project-onboarding output the shard Policies not in
// keep; every other document is kept as rendered.
func onboardingKeepShards(raw string, keep map[int]bool) (string, error) {
	if len(keep) == onboardingShards {
		return raw, nil
	}
	var out []string
	for _, doc := range splitYAMLDocuments(raw) {
		// PolicyGenerator writes kind and metadata.name at the top of each document, before any
		// object-templates-raw text, so the first matches are the Policy's own.
		if strings.HasPrefix(doc, "apiVersion: policy.open-cluster-management.io/v1\nkind: Policy\n") {
			m := onboardingPolicyNameRe.FindStringSubmatch(doc)
			if m == nil {
				return "", fmt.Errorf("rendered Policy has no shard name:\n%.300s", doc)
			}
			if !keep[onboardingShardOfPolicy(m[1])] {
				continue
			}
		}
		out = append(out, doc)
	}
	return strings.Join(out, "---\n"), nil
}

// onboardingShardFilter is the runPipelineTransform transform that keeps only the shards in keep.
func onboardingShardFilter(t *testing.T, keep map[int]bool) func(policy, raw string) string {
	return func(policy, raw string) string {
		if policy != projectOnboardingPolicy {
			return raw
		}
		out, err := onboardingKeepShards(raw, keep)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
}

// checkOnboardingShardsResolved fails unless the resolved output holds exactly the shard Policies
// in keep, each with its -test Policy.
func checkOnboardingShardsResolved(t *testing.T, resolved string, keep map[int]bool) {
	t.Helper()
	got := map[string]bool{}
	for _, p := range onboardingPolicies(resolved) {
		name, _ := nestedString(p, "metadata", "name")
		if onboardingShardOfPolicy(name) >= 0 {
			if got[name] {
				t.Fatalf("shard Policy %s resolved twice", name)
			}
			got[name] = true
		}
	}
	if len(got) != 2*len(keep) {
		t.Fatalf("resolved shard Policies %v, want shards %v, each with its -test Policy", got, keep)
	}
	for shard := range keep {
		name := fmt.Sprintf("%s%d", onboardingShardPrefix, shard)
		if !got[name] || !got[name+onboardingTestSuffix] {
			t.Fatalf("resolved shard Policies %v, want shards %v, each with its -test Policy", got, keep)
		}
	}
}

var (
	onboardingBaselineOnce sync.Once
	onboardingBaseline     ChartResult
	onboardingPruneOnce    sync.Once
	onboardingPruneResult  ChartResult
)

// onboardingPruneBase is onboardingBase on a cluster with deletion opted in.
func onboardingPruneBase(t *testing.T) ChartResult {
	t.Helper()
	onboardingPruneOnce.Do(func() { onboardingPruneResult = runOnboardingAllShards(t, pruneOn, nil) })
	return onboardingPruneResult
}

func onboardingBase(t *testing.T) ChartResult {
	t.Helper()
	onboardingBaselineOnce.Do(func() { onboardingBaseline = runOnboardingAllShards(t, nil, nil) })
	return onboardingBaseline
}

// onboardingPolicies returns the root Policy documents of a resolved chart: every shard Policy
// (policy-project-onboarding-<i>) when there are any, else every Policy, for the other charts that
// share these helpers (an enforcing Policy and its -test Policy, see project_policies_layout_test.go).
func onboardingPolicies(yamlDocs string) []map[string]interface{} {
	var shards, all []map[string]interface{}
	for _, doc := range splitYAMLDocuments(yamlDocs) {
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err != nil || obj["kind"] != "Policy" {
			continue
		}
		all = append(all, obj)
		if name, _ := nestedString(obj, "metadata", "name"); strings.HasPrefix(name, onboardingShardPrefix) {
			shards = append(shards, obj)
		}
	}
	if len(shards) > 0 {
		return shards
	}
	return all
}

// onboardingPolicy returns the root Policy document of a resolved chart, the first shard for
// project onboarding.
func onboardingPolicy(t *testing.T, res ChartResult) map[string]interface{} {
	t.Helper()
	if p := onboardingPolicies(res.ResolvedYAML); len(p) > 0 {
		return p[0]
	}
	t.Fatal("no Policy document in resolved output")
	return nil
}

// parseOnboardingObjects parses every object-templates-raw block of the Policies into objects.
func parseOnboardingObjects(yamlDocs string) ([]onboardingObject, error) {
	policies := onboardingPolicies(yamlDocs)
	if len(policies) == 0 {
		return nil, fmt.Errorf("no Policy document in the output")
	}
	var out []onboardingObject
	for _, policy := range policies {
		rootName, _ := nestedString(policy, "metadata", "name")
		policyName := onboardingShardPolicy(rootName)
		before := len(out)
		spec, _ := policy["spec"].(map[string]interface{})
		templates, _ := spec["policy-templates"].([]interface{})
		for _, tpl := range templates {
			def, _ := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			name, _ := def["metadata"].(map[string]interface{})["name"].(string)
			cpSpec, _ := def["spec"].(map[string]interface{})
			remediation, _ := cpSpec["remediationAction"].(string)
			rawBlock, _ := cpSpec["object-templates-raw"].(string)
			var items []map[string]interface{}
			if err := sigsyaml.Unmarshal([]byte(rawBlock), &items); err != nil {
				return nil, fmt.Errorf("object-templates-raw of %s is not a YAML list: %v\n%s", name, err, rawBlock)
			}
			for _, item := range items {
				obj, _ := item["objectDefinition"].(map[string]interface{})
				ct, _ := item["complianceType"].(string)
				out = append(out, onboardingObject{policy: policyName, rootPolicy: rootName, configPolicy: name, remediationAction: remediation, complianceType: ct, item: item, obj: obj})
			}
		}
		// A shard Policy always writes or clears its status ConfigMap, and its -test Policy always
		// asserts it absent, so one without any object lost its output; onboardingShardStatus relies
		// on that to tell resolved shards apart.
		if onboardingShardOfPolicy(rootName) >= 0 && len(out) == before {
			return nil, fmt.Errorf("shard Policy %s renders no object", rootName)
		}
	}
	return out, nil
}

// onboardingObjects parses every object-templates-raw block of the Policies into objects.
func onboardingObjects(t *testing.T, res ChartResult) []onboardingObject {
	t.Helper()
	out, err := parseOnboardingObjects(res.ResolvedYAML)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// namespaceNamed returns the Namespace object for name, or nil.
func namespaceNamed(objs []onboardingObject, name string) map[string]interface{} {
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "Namespace" && meta["name"] == name && o.complianceType != "mustnothave" {
			return meta
		}
	}
	return nil
}

func stringMap(m map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	inner, _ := m[key].(map[string]interface{})
	for k, v := range inner {
		s, _ := v.(string)
		out[k] = s
	}
	return out
}

// onboardingStatusName is the status ConfigMap of the single policy the shards replaced; shard 0
// deletes it. Each shard writes its own, onboardingStatusName-<shard>.
const onboardingStatusName = "project-onboarding-status"

func onboardingStatusShardName(shard int) string {
	return fmt.Sprintf("%s-%d", onboardingStatusName, shard)
}

// isStatus reports whether o is a status ConfigMap (a shard's, written, cleared or gated, or the
// single policy's, deleted), which is not a project object and never a prune entry.
func isStatus(o onboardingObject) bool {
	meta, _ := o.obj["metadata"].(map[string]interface{})
	name, _ := meta["name"].(string)
	return o.obj["kind"] == "ConfigMap" && (name == onboardingStatusName || strings.HasPrefix(name, onboardingStatusName+"-"))
}

// isLegacyStatus reports whether o is the deletion of the single policy's status ConfigMap.
func isLegacyStatus(o onboardingObject) bool {
	meta, _ := o.obj["metadata"].(map[string]interface{})
	return isStatus(o) && meta["name"] == onboardingStatusName
}

// statusEntries returns the shard status ConfigMap entries with the given complianceType and
// remediationAction, the single policy's excluded.
func statusEntries(objs []onboardingObject, complianceType, remediation string) []onboardingObject {
	var out []onboardingObject
	for _, o := range objs {
		if isStatus(o) && !isLegacyStatus(o) && o.complianceType == complianceType && o.remediationAction == remediation {
			out = append(out, o)
		}
	}
	return out
}

// onboardingShardStatus returns, per shard, the data of the status ConfigMap its enforce
// ConfigurationPolicy writes, or an empty map when it clears it. Each shard must write or clear
// its own ConfigMap exactly once, and shard 0 must delete the single policy's. A run that resolved
// only some shards (onboardingShardsFor) has nil for the others; shard 0 is always resolved, and
// parseOnboardingObjects fails on a resolved shard Policy that renders nothing.
func onboardingShardStatus(t *testing.T, objs []onboardingObject) []map[string]string {
	t.Helper()
	out := make([]map[string]string, onboardingShards)
	resolved := map[int]bool{}
	for _, o := range objs {
		if shard := onboardingShardOfPolicy(o.policy); shard >= 0 {
			resolved[shard] = true
		}
	}
	if !resolved[0] {
		t.Fatalf("shard 0 is not in the resolved output")
	}
	for shard := 0; shard < onboardingShards; shard++ {
		if !resolved[shard] {
			continue
		}
		want := onboardingStatusShardName(shard)
		policy := fmt.Sprintf("%s%d", onboardingShardPrefix, shard)
		var written, cleared []onboardingObject
		for _, o := range objs {
			meta, _ := o.obj["metadata"].(map[string]interface{})
			if !isStatus(o) || o.remediationAction != "enforce" || meta["name"] != want {
				continue
			}
			if o.policy != policy {
				t.Errorf("%s is rendered by %s, want %s", want, o.policy, policy)
			}
			switch o.complianceType {
			case "mustonlyhave":
				written = append(written, o)
			case "mustnothave":
				cleared = append(cleared, o)
			}
		}
		if len(written)+len(cleared) != 1 {
			t.Fatalf("want shard %d's enforce ConfigurationPolicy to write or clear %s exactly once, got %d writes and %d clears",
				shard, want, len(written), len(cleared))
		}
		if len(cleared) == 1 {
			out[shard] = map[string]string{}
			continue
		}
		meta, _ := written[0].obj["metadata"].(map[string]interface{})
		if meta["namespace"] != "open-cluster-management-agent-addon" {
			t.Errorf("status ConfigMap namespace = %v, want open-cluster-management-agent-addon", meta["namespace"])
		}
		if stringMap(meta, "labels")["autoshift.io/project-onboarding-status"] != "true" {
			t.Errorf("status ConfigMap labels = %v, want autoshift.io/project-onboarding-status: \"true\"", meta["labels"])
		}
		out[shard] = stringMap(written[0].obj, "data")
	}
	var legacy int
	for _, o := range objs {
		if isLegacyStatus(o) {
			legacy++
			if o.complianceType != "mustnothave" || o.policy != onboardingShardPrefix+"0" || o.remediationAction != "enforce" {
				t.Errorf("%s: want only shard 0's enforce ConfigurationPolicy to delete it, got %s in %s (%s)",
					onboardingStatusName, o.complianceType, o.configPolicy, o.remediationAction)
			}
		}
	}
	if legacy != 1 {
		t.Errorf("want shard 0 to delete %s once, got %d entries", onboardingStatusName, legacy)
	}
	return out
}

// onboardingStatus returns the status data of all shards merged: a key reported by more than one
// shard (rendered-config, project-onboarding.size) holds their messages joined by "; " when they
// differ, in shard order.
func onboardingStatus(t *testing.T, objs []onboardingObject) map[string]string {
	t.Helper()
	merged := map[string]string{}
	for _, data := range onboardingShardStatus(t, objs) {
		for k, v := range data {
			if prev, ok := merged[k]; ok && prev != v {
				v = prev + "; " + v
			}
			merged[k] = v
		}
	}
	return merged
}

// Each shard is two Policies after resolution too: policy-project-onboarding-<i> carries only its
// enforce ConfigurationPolicy, policy-project-onboarding-<i>-test only its inform one, each root
// matching its child (an inform root can never turn a report into a delete).
func TestProjectOnboarding_Structure(t *testing.T) {
	t.Parallel()
	policies := onboardingPolicies(onboardingBase(t).ResolvedYAML)
	if len(policies) != 2*onboardingShards {
		t.Fatalf("want %d shard Policies, got %d", 2*onboardingShards, len(policies))
	}
	for i, policy := range policies {
		name, _ := nestedString(policy, "metadata", "name")
		want, mode, cp := fmt.Sprintf("%s%d", onboardingShardPrefix, i/2), "enforce", "-enforce"
		if i%2 == 1 {
			want, mode, cp = want+onboardingTestSuffix, "inform", ""
		}
		if name != want {
			t.Errorf("Policy %d is %s, want %s", i, name, want)
		}
		spec := policy["spec"].(map[string]interface{})
		if ra, _ := spec["remediationAction"].(string); ra != mode {
			t.Errorf("%s: root remediationAction = %q, want %q", name, ra, mode)
		}
		got := map[string]string{}
		for _, tpl := range spec["policy-templates"].([]interface{}) {
			def := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			cpSpec := def["spec"].(map[string]interface{})
			got[def["metadata"].(map[string]interface{})["name"].(string)], _ = cpSpec["remediationAction"].(string)
		}
		if len(got) != 1 || got[name+cp] != mode {
			t.Errorf("%s: want only ConfigurationPolicy %s%s (%s), got %v", name, name, cp, mode, got)
		}
	}
}

func TestProjectOnboarding_NamespaceAndLabels(t *testing.T) {
	t.Parallel()
	// Prune on: project-labels is then exactly this run's keys (off keeps the stub's earlier keys).
	objs := onboardingObjects(t, onboardingPruneBase(t))
	meta := namespaceNamed(objs, "payments-api")
	if meta == nil {
		t.Fatalf("no Namespace named payments-api in %d objects", len(objs))
	}
	lbls := stringMap(meta, "labels")
	wantLabels := map[string]string{
		"autoshift.io/project":        "payments-api",
		"autoshift.io/managed-by":     "project-onboarding",
		"example.com/team":            "payments",
		"example.com/environment":     "NONPROD",
		"example.com/cost-center":     "cc-4711",
		"network.example.com/vlan-70": "true",
	}
	for k, v := range wantLabels {
		if lbls[k] != v {
			t.Errorf("label %s = %q, want %q (labels: %v)", k, lbls[k], v, lbls)
		}
	}
	ann := stringMap(meta, "annotations")
	if ann["openshift.io/display-name"] != "Payments API" {
		t.Errorf("display-name = %q, want Payments API", ann["openshift.io/display-name"])
	}
	wantApplied := "example.com/cost-center,example.com/environment,example.com/team,network.example.com/vlan-70"
	if ann["autoshift.io/project-labels"] != wantApplied {
		t.Errorf("project-labels = %q, want %q", ann["autoshift.io/project-labels"], wantApplied)
	}
	// payments-api sets no mode and no adopt, so it is enforced.
	if got := remediationOf(objs, "payments-api"); got != "enforce" {
		t.Errorf("payments-api Namespace rendered with remediationAction %q, want enforce", got)
	}
}

func TestProjectOnboarding_SkipUnresolvedLabel(t *testing.T) {
	t.Parallel()
	meta := namespaceNamed(onboardingObjects(t, onboardingBase(t)), "reporting")
	if meta == nil {
		t.Fatal("no Namespace named reporting")
	}
	lbls := stringMap(meta, "labels")
	if _, ok := lbls["example.com/cost-center"]; ok {
		t.Errorf("reporting has no costCenter; cost-center label must be skipped, got %v", lbls)
	}
	if lbls["example.com/team"] != "reporting" {
		t.Errorf("team label = %q, want reporting", lbls["example.com/team"])
	}
	for k := range lbls {
		if strings.Contains(k, "vlan") {
			t.Errorf("reporting has no vlan; vlan label must be skipped, got %s", k)
		}
	}
	if strings.Contains(stringMap(meta, "annotations")["autoshift.io/project-labels"], "cost-center") {
		t.Errorf("skipped label recorded in project-labels annotation")
	}
}

func TestProjectOnboarding_EmptyFieldSkipped(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["reporting"].(map[string]interface{})["costCenter"] = ""
	})
	meta := namespaceNamed(onboardingObjects(t, res), "reporting")
	if meta == nil {
		t.Fatal("no Namespace named reporting")
	}
	if v, ok := stringMap(meta, "labels")["example.com/cost-center"]; ok {
		t.Errorf("empty costCenter must skip the label, got %q", v)
	}
	// Skipped, not rejected: had the empty value reached validation, cc- would be reported.
	if got, ok := onboardingStatus(t, onboardingObjects(t, res))["reporting"]; ok {
		t.Errorf("an empty field skips its label silently, want no status entry for reporting, got %q", got)
	}
}

func TestProjectOnboarding_DisabledCatalogEntry(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		p := projects["payments-api"].(map[string]interface{})
		p["labels"] = map[string]interface{}{"cost-center": map[string]interface{}{"enabled": false}}
	})
	objs := onboardingObjects(t, res)
	meta := namespaceNamed(objs, "payments-api")
	if meta == nil {
		t.Fatal("no Namespace named payments-api")
	}
	lbls := stringMap(meta, "labels")
	if _, ok := lbls["example.com/cost-center"]; ok {
		t.Errorf("disabled catalog entry still applied: %v", lbls)
	}
	if lbls["example.com/team"] != "payments" {
		t.Errorf("other catalog entries must survive, team = %q", lbls["example.com/team"])
	}
	// The override must not leak into another project through shared defaults.
	if other := namespaceNamed(objs, "reporting"); other == nil || stringMap(other, "labels")["example.com/team"] != "reporting" {
		t.Errorf("reporting lost its team label after payments-api overrode a catalog entry")
	}
}

// roleBindingNamed returns the RoleBinding called name in namespace, or nil. Prune entries
// (mustnothave) are not counted; see pruned.
func roleBindingNamed(objs []onboardingObject, namespace, name string) map[string]interface{} {
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "RoleBinding" && meta["namespace"] == namespace && meta["name"] == name && o.complianceType != "mustnothave" {
			return o.obj
		}
	}
	return nil
}

func TestProjectOnboarding_RBAC(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	want := []struct{ name, role, group string }{
		{"autoshift-admin", "admin", "K8S_payments_ADMINS_NONPROD"},
		{"autoshift-write", "edit", "K8S_payments_DEVELOPERS_NONPROD"},
		{"autoshift-read", "view", "K8S_SHARED_AUDITORS"},
	}
	for _, w := range want {
		rb := roleBindingNamed(objs, "payments-api", w.name)
		if rb == nil {
			t.Errorf("no RoleBinding %s in payments-api", w.name)
			continue
		}
		roleRef, _ := rb["roleRef"].(map[string]interface{})
		if roleRef["kind"] != "ClusterRole" || roleRef["name"] != w.role || roleRef["apiGroup"] != "rbac.authorization.k8s.io" {
			t.Errorf("%s roleRef = %v, want ClusterRole %s", w.name, roleRef, w.role)
		}
		subjects, _ := rb["subjects"].([]interface{})
		if len(subjects) != 1 {
			t.Errorf("%s subjects = %v, want exactly one", w.name, subjects)
			continue
		}
		subj, _ := subjects[0].(map[string]interface{})
		if subj["kind"] != "Group" || subj["name"] != w.group || subj["apiGroup"] != "rbac.authorization.k8s.io" {
			t.Errorf("%s subject = %v, want Group %s", w.name, subj, w.group)
		}
		lbls := stringMap(rb["metadata"].(map[string]interface{}), "labels")
		if lbls["autoshift.io/project"] != "payments-api" || lbls["autoshift.io/managed-by"] != "project-onboarding" {
			t.Errorf("%s ownership labels = %v", w.name, lbls)
		}
	}
	if rb := roleBindingNamed(objs, "reporting", "autoshift-read"); rb == nil {
		t.Error("reporting has no autoshift-read RoleBinding")
	}
}

// roleRef is immutable, so a changed role mapping needs the RoleBinding recreated.
func TestProjectOnboarding_RoleBindingRecreateOption(t *testing.T) {
	t.Parallel()
	res := onboardingBase(t)
	var seen int
	var templates []interface{}
	for _, policy := range onboardingPolicies(res.ResolvedYAML) {
		templates = append(templates, policy["spec"].(map[string]interface{})["policy-templates"].([]interface{})...)
	}
	for _, tpl := range templates {
		cp := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})
		var items []map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(cp["object-templates-raw"].(string)), &items); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			def, _ := item["objectDefinition"].(map[string]interface{})
			if def["kind"] != "RoleBinding" || item["complianceType"] == "mustnothave" {
				continue
			}
			seen++
			if item["recreateOption"] != "IfRequired" {
				t.Errorf("RoleBinding template recreateOption = %v, want IfRequired", item["recreateOption"])
			}
		}
	}
	if seen == 0 {
		t.Error("no RoleBinding templates found")
	}
}

func TestProjectOnboarding_InvalidLabelKey(t *testing.T) {
	t.Parallel()
	for _, vlan := range []string{"70/x", "7_0/x", "7 0", strings.Repeat("a", 70)} {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["vlan"] = vlan
		})
		meta := namespaceNamed(onboardingObjects(t, res), "payments-api")
		if meta == nil {
			t.Fatalf("vlan %q: no Namespace named payments-api", vlan)
		}
		for k := range stringMap(meta, "labels") {
			if strings.HasPrefix(k, "network.example.com/") {
				t.Errorf("vlan %q: invalid label key %q must be skipped", vlan, k)
			}
		}
		if lbls := stringMap(meta, "labels"); lbls["example.com/team"] != "payments" {
			t.Errorf("vlan %q: other labels must survive, team = %q", vlan, lbls["example.com/team"])
		}
		if strings.Contains(stringMap(meta, "annotations")["autoshift.io/project-labels"], "vlan") {
			t.Errorf("vlan %q: skipped label listed in project-labels", vlan)
		}
	}
}

// A project field can also break the prefix: it is a DNS subdomain, so '/' in it makes two slashes.
func TestProjectOnboarding_InvalidLabelKeyPrefix(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["labels"].(map[string]interface{})["team"] = map[string]interface{}{
			"enabled": true, "key": "{team}.example.com/team", "value": "x",
		}
		cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["team"] = "Bad_Prefix"
	})
	objs := onboardingObjects(t, res)
	meta := namespaceNamed(objs, "payments-api")
	if meta == nil {
		t.Fatal("no Namespace named payments-api")
	}
	want := "label Bad_Prefix.example.com/team=x is not a valid Kubernetes label"
	if got := onboardingStatus(t, objs)["payments-api"]; got != want {
		t.Errorf("status[payments-api] = %q, want %q", got, want)
	}
	// The live example.com/team (applied before, per the stub) is kept while the project is in
	// error; only the invalid key must never be applied.
	for k := range stringMap(meta, "labels") {
		if strings.Contains(k, "Bad_Prefix") {
			t.Errorf("label with invalid prefix %q must be skipped", k)
		}
	}
	if stringMap(meta, "labels")["example.com/environment"] != "NONPROD" {
		t.Error("other labels must survive")
	}
}

func TestProjectOnboarding_MissingAdminGroup(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["groups"].(map[string]interface{})["admin"] = "K8S_{missing}_ADMINS"
		for _, p := range cfg["projects"].(map[string]interface{}) {
			if groups, ok := p.(map[string]interface{})["groups"].(map[string]interface{}); ok {
				delete(groups, "admin")
			}
		}
	})
	objs := onboardingObjects(t, res)
	// A project with no resolvable admin group is skipped whole: no admin binding, no other
	// binding, and no Namespace, so nothing is half-onboarded.
	for _, project := range []string{"payments-api", "reporting"} {
		if rb := roleBindingNamed(objs, project, "autoshift-admin"); rb != nil {
			t.Errorf("%s has no resolvable admin group, want no autoshift-admin RoleBinding, got %v", project, rb)
		}
		if rb := roleBindingNamed(objs, project, "autoshift-write"); rb != nil {
			t.Errorf("%s is skipped, want no autoshift-write RoleBinding, got %v", project, rb)
		}
		if namespaceNamed(objs, project) != nil {
			t.Errorf("%s is skipped, want no Namespace", project)
		}
	}
	t.Run("status", func(t *testing.T) {
		status := onboardingStatus(t, objs)
		for _, project := range []string{"payments-api", "reporting"} {
			if status[project] != "no resolvable admin group" {
				t.Errorf("status[%s] = %q, want %q (status %v)", project, status[project], "no resolvable admin group", status)
			}
		}
	})
}

// Skipping is contained per project: reporting is made valid with its own admin override and
// still renders while payments-api is skipped.
func TestProjectOnboarding_MissingAdminGroupIsPerProject(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["groups"].(map[string]interface{})["admin"] = "K8S_{missing}_ADMINS"
		projects := cfg["projects"].(map[string]interface{})
		delete(projects["payments-api"].(map[string]interface{})["groups"].(map[string]interface{}), "admin")
		projects["reporting"].(map[string]interface{})["groups"] = map[string]interface{}{"admin": "K8S_REPORTING_ADMINS"}
	})
	objs := onboardingObjects(t, res)
	if rb := roleBindingNamed(objs, "payments-api", "autoshift-admin"); rb != nil {
		t.Errorf("payments-api must have no autoshift-admin, got %v", rb)
	}
	if namespaceNamed(objs, "reporting") == nil {
		t.Error("reporting must still render its Namespace")
	}
	rb := roleBindingNamed(objs, "reporting", "autoshift-admin")
	if rb == nil {
		t.Fatal("reporting must still render autoshift-admin")
	}
	subj := rb["subjects"].([]interface{})[0].(map[string]interface{})
	if subj["name"] != "K8S_REPORTING_ADMINS" {
		t.Errorf("reporting admin group = %v, want K8S_REPORTING_ADMINS", subj["name"])
	}
}

func TestProjectOnboarding_InvalidLabelValue(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["team"] = strings.Repeat("a", 70)
	})
	objs := onboardingObjects(t, res)
	meta := namespaceNamed(objs, "payments-api")
	if meta == nil {
		t.Fatal("no Namespace named payments-api")
	}
	// The live value (applied before, per the stub) is kept while the project is in error; the
	// invalid value must never be applied.
	lbls := stringMap(meta, "labels")
	if v := lbls["example.com/team"]; v == strings.Repeat("a", 70) {
		t.Errorf("70-character team label must be skipped, got %q", v)
	}
	if lbls["example.com/environment"] != "NONPROD" {
		t.Errorf("other labels must survive, environment = %q", lbls["example.com/environment"])
	}
	t.Run("status", func(t *testing.T) {
		want := "label example.com/team=" + strings.Repeat("a", 70) + " is not a valid Kubernetes label"
		status := onboardingStatus(t, objs)
		if status["payments-api"] != want {
			t.Errorf("status[payments-api] = %q, want %q", status["payments-api"], want)
		}
		if _, ok := status["reporting"]; ok {
			t.Errorf("reporting is valid, want no status entry, got %q", status["reporting"])
		}
	})
}

func TestProjectOnboarding_OwnershipLabelsNotOverridable(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["labels"].(map[string]interface{})["hijack"] = map[string]interface{}{
			"enabled": true, "key": "autoshift.io/managed-by", "value": "someone-else",
		}
	})
	lbls := stringMap(namespaceNamed(onboardingObjects(t, res), "payments-api"), "labels")
	if lbls["autoshift.io/managed-by"] != "project-onboarding" {
		t.Errorf("managed-by = %q, a catalog entry must not overwrite ownership labels", lbls["autoshift.io/managed-by"])
	}
}

// Two projects declaring one namespace. The first in name order keeps it; the later
// one is skipped whole and reported, so the two never fight over its labels or bindings.
func TestProjectOnboarding_DuplicateNamespace(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["reporting"].(map[string]interface{})["namespace"] = "payments-api"
	})
	objs := onboardingObjects(t, res)
	status := onboardingStatus(t, objs)
	if want := "namespace payments-api already declared by payments-api"; status["reporting"] != want {
		t.Errorf("status[reporting] = %q, want %q (status %v)", status["reporting"], want, status)
	}
	if _, ok := status["payments-api"]; ok {
		t.Errorf("payments-api declared the namespace first, want no status entry, got %q", status["payments-api"])
	}
	if n := len(namespaceIn(objs, "payments-api", "enforce")); n != 1 {
		t.Errorf("want one enforced payments-api Namespace, got %d", n)
	}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if meta["name"] == "payments-api" && o.obj["kind"] == "Namespace" && stringMap(meta, "labels")["autoshift.io/project"] != "payments-api" {
			t.Errorf("payments-api Namespace rendered for another project: %v", stringMap(meta, "labels"))
		}
		if meta["namespace"] == "payments-api" && o.obj["kind"] == "RoleBinding" && stringMap(meta, "labels")["autoshift.io/project"] != "payments-api" {
			t.Errorf("RoleBinding %v in payments-api rendered for another project", meta["name"])
		}
	}
	if roleBindingNamed(objs, "payments-api", "autoshift-admin") == nil {
		t.Error("payments-api must still render autoshift-admin")
	}
}

// The ESO precedent: the enforce ConfigurationPolicy writes the status ConfigMap while errors exist
// and clears it otherwise; the inform one always asserts it is absent, so the Policy is NonCompliant
// exactly while it exists. Stale objects reported with pruning off are not errors.
func TestProjectOnboarding_StatusClearedWhenClean(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("baseline has no invalid or blocked project, want the status cleared, got %v", status)
	}
	if len(pruned(objs, "RoleBinding", "removed-project", "autoshift-admin")) != 1 {
		t.Fatal("baseline must report the stale removed-project binding (prune off)")
	}
	if n := len(statusEntries(objs, "mustnothave", "inform")); n != onboardingShards {
		t.Errorf("want one inform gate per shard (mustnothave %s-<shard>), got %d", onboardingStatusName, n)
	}
	for _, o := range objs {
		if isStatus(o) && o.remediationAction == "inform" && o.complianceType != "mustnothave" {
			t.Errorf("the inform ConfigurationPolicy must only gate the status, got %s", o.complianceType)
		}
	}
}

func TestProjectOnboarding_StatusGateWithErrors(t *testing.T) {
	t.Parallel()
	res := runOnboardingAllShards(t, nil, func(cfg map[string]interface{}) {
		cfg["projects"].(map[string]interface{})["legacy-app"].(map[string]interface{})["adopt"] = false
	})
	objs := onboardingObjects(t, res)
	if len(onboardingStatus(t, objs)) != 1 {
		t.Errorf("want one status entry, got %v", onboardingStatus(t, objs))
	}
	if n := len(statusEntries(objs, "mustnothave", "inform")); n != onboardingShards {
		t.Errorf("want one inform gate per shard (mustnothave %s-<shard>), got %d", onboardingStatusName, n)
	}
}

// A project's hub errors and the managed cluster's ownership block share its one status entry.
func TestProjectOnboarding_StatusJoinsHubAndSpokeErrors(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		addProject(projects, "foreign", "onb-owned-other")
		projects["foreign"].(map[string]interface{})["costCenter"] = "bad value"
	})
	want := "label example.com/cost-center=cc-bad value is not a valid Kubernetes label; namespace onb-owned-other is owned by project someone-else"
	if got := onboardingStatus(t, onboardingObjects(t, res))["foreign"]; got != want {
		t.Errorf("status[foreign] = %q, want %q", got, want)
	}
}

// One invalid project never costs the others anything.
func TestProjectOnboarding_BadProjectIsolated(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		// No team or environment: no group template resolves, so no admin group.
		projects["broken"] = map[string]interface{}{"displayName": "Broken"}
	})
	objs := onboardingObjects(t, res)
	status := onboardingStatus(t, objs)
	if status["broken"] != "no resolvable admin group" || len(status) != 1 {
		t.Errorf("want only broken reported, got %v", status)
	}
	if namespaceNamed(objs, "broken") != nil {
		t.Error("broken is invalid, want no Namespace")
	}
	for project, levels := range map[string][]string{
		"payments-api": {"autoshift-admin", "autoshift-write", "autoshift-read"},
		"reporting":    {"autoshift-admin", "autoshift-write", "autoshift-read"},
		"legacy-app":   {"autoshift-admin", "autoshift-write", "autoshift-read"},
	} {
		if namespaceNamed(objs, project) == nil {
			t.Errorf("%s must still render its Namespace", project)
		}
		for _, rb := range levels {
			if roleBindingNamed(objs, project, rb) == nil {
				t.Errorf("%s must still render RoleBinding %s", project, rb)
			}
		}
	}
}

// The enforce and inform templates are the same template; only $want differs. Drift between them
// would make one mode render different objects than the other.
func TestProjectOnboarding_TemplatesInSync(t *testing.T) {
	t.Parallel()
	assertTemplatesInSync(t, projectOnboardingPolicy, "projects.yaml")
}

// remediationOf returns the remediationAction of the ConfigurationPolicy carrying the Namespace
// (and all RoleBindings) of project, "" when nothing renders, "mixed" when they are split.
//
// Prune entries (mustnothave) are not the project's objects and are ignored, as is the inform
// drop-report Namespace of a project whose Namespace is in the enforce ConfigurationPolicy.
func remediationOf(objs []onboardingObject, project string) string {
	got := ""
	enforcedNs := namespaceIn(objs, project, "enforce") != nil
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if meta["name"] != project && meta["namespace"] != project {
			continue
		}
		if o.complianceType == "mustnothave" || (o.obj["kind"] == "Namespace" && enforcedNs && o.remediationAction == "inform") {
			continue
		}
		if got != "" && got != o.remediationAction {
			return "mixed"
		}
		got = o.remediationAction
	}
	return got
}

func TestProjectOnboarding_ModeSplit(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	if got := remediationOf(objs, "legacy-app"); got != "inform" {
		t.Errorf("legacy-app (adopt: true, no mode) rendered with %q, want only the inform ConfigurationPolicy", got)
	}
	if roleBindingNamed(objs, "legacy-app", "autoshift-admin") == nil {
		t.Error("legacy-app must render its RoleBindings in the inform ConfigurationPolicy")
	}
	if got := remediationOf(objs, "payments-api"); got != "enforce" {
		t.Errorf("payments-api rendered with %q, want only the enforce ConfigurationPolicy", got)
	}
}

func TestProjectOnboarding_AdoptExplicitEnforce(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["legacy-app"].(map[string]interface{})["mode"] = "enforce"
	})
	if got := remediationOf(onboardingObjects(t, res), "legacy-app"); got != "enforce" {
		t.Errorf("legacy-app with mode enforce rendered with %q, want enforce", got)
	}
}

func TestProjectOnboarding_ExplicitInform(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["mode"] = "inform"
	})
	if got := remediationOf(onboardingObjects(t, res), "payments-api"); got != "inform" {
		t.Errorf("payments-api with mode inform rendered with %q, want inform", got)
	}
}

// The stub Namespace legacy-app exists without ownership labels. Without adopt it is somebody
// else's namespace, so nothing is rendered for it, in either mode.
func TestProjectOnboarding_NoAdoptExistingNamespace(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["legacy-app"].(map[string]interface{})["adopt"] = false
	})
	objs := onboardingObjects(t, res)
	if got := remediationOf(objs, "legacy-app"); got != "" {
		t.Errorf("legacy-app exists unowned and adopt is false, want no objects, rendered with %q", got)
	}
	if namespaceNamed(objs, "payments-api") == nil {
		t.Error("other projects must still render")
	}
	t.Run("status", func(t *testing.T) {
		want := "namespace legacy-app exists and is not owned by AutoShift; set adopt: true to take it over"
		if got := onboardingStatus(t, objs)["legacy-app"]; got != want {
			t.Errorf("status[legacy-app] = %q, want %q", got, want)
		}
	})
}

// A namespace that does not exist yet needs no adoption.
func TestProjectOnboarding_NewNamespaceNeedsNoAdopt(t *testing.T) {
	t.Parallel()
	if namespaceNamed(onboardingObjects(t, onboardingBase(t)), "reporting") == nil {
		t.Error("reporting does not exist on the cluster, want it rendered without adopt")
	}
}

// renderedRemediation renders the policy dir through the CMP path with the given REMEDIATION and
// returns, over every shard Policy and -test Policy, the root remediationAction of each Policy and
// the remediationAction of each ConfigurationPolicy, keyed by name.
func renderedRemediation(t *testing.T, remediation string) (map[string]string, map[string]string) {
	t.Helper()
	out, err := cachedKustomizeBuild(filepath.Join(repoRoot(t), "policies", "stable", "project-onboarding"), remediation)
	if err != nil {
		t.Fatalf("render with REMEDIATION=%s: %v", remediation, err)
	}
	roots := map[string]string{}
	got := map[string]string{}
	for _, obj := range onboardingPolicies(out) {
		spec := obj["spec"].(map[string]interface{})
		name, _ := nestedString(obj, "metadata", "name")
		roots[name], _ = spec["remediationAction"].(string)
		for _, tpl := range spec["policy-templates"].([]interface{}) {
			def := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			ra, _ := def["spec"].(map[string]interface{})["remediationAction"].(string)
			got[def["metadata"].(map[string]interface{})["name"].(string)] = ra
		}
	}
	if len(got) == 0 {
		t.Fatal("no Policy in render")
	}
	return roots, got
}

// Dry-run is the deployment-level REMEDIATION=inform: each shard's enforce ConfigurationPolicy and
// its root follow it, the -test Policy and its ConfigurationPolicy are always inform.
func TestProjectOnboarding_DryRun(t *testing.T) {
	t.Parallel()
	roots, got := renderedRemediation(t, "enforce")
	droots, dry := renderedRemediation(t, "inform")
	if len(got) != 2*onboardingShards || len(dry) != 2*onboardingShards {
		t.Fatalf("want %d ConfigurationPolicies, got %v and %v", 2*onboardingShards, got, dry)
	}
	for i := 0; i < onboardingShards; i++ {
		p := fmt.Sprintf("%s%d", onboardingShardPrefix, i)
		tp := p + onboardingTestSuffix
		if got[p+"-enforce"] != "enforce" || got[tp] != "inform" || roots[p] != "enforce" || roots[tp] != "inform" {
			t.Errorf("enforce render: %s ConfigurationPolicies = %v, %v, roots %v, %v, want enforce + inform", p, got[p+"-enforce"], got[tp], roots[p], roots[tp])
		}
		if dry[p+"-enforce"] != "inform" || dry[tp] != "inform" || droots[p] != "inform" || droots[tp] != "inform" {
			t.Errorf("dry-run render: %s ConfigurationPolicies = %v, %v, roots %v, %v, want all inform", p, dry[p+"-enforce"], dry[tp], droots[p], droots[tp])
		}
	}
	for _, r := range []string{"enforce", "inform"} {
		rendered, err := cachedKustomizeBuild(filepath.Join(repoRoot(t), "policies", projectOnboardingPolicy), r)
		if err != nil {
			t.Fatal(err)
		}
		assertEnforceTestSplit(t, rendered, r, splitPolicyDirs[projectOnboardingPolicy])
	}
}

func addProject(projects map[string]interface{}, name, namespace string) {
	projects[name] = map[string]interface{}{"team": "x", "environment": "PROD", "namespace": namespace}
}

// onb-owned-other is owned by another project. Without adopt, taking it over would rewrite the
// owner label and replace that project's RoleBindings, so nothing is rendered.
func TestProjectOnboarding_NamespaceOwnedByOtherProject(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) { addProject(projects, "foreign", "onb-owned-other") })
	objs := onboardingObjects(t, res)
	if got := remediationOf(objs, "onb-owned-other"); got != "" {
		t.Errorf("namespace owned by another project, adopt not set: want no objects, rendered with %q", got)
	}
	t.Run("status", func(t *testing.T) {
		want := "namespace onb-owned-other is owned by project someone-else"
		if got := onboardingStatus(t, objs)["foreign"]; got != want {
			t.Errorf("status[foreign] = %q, want %q", got, want)
		}
	})
}

// onb-owned-self already carries this project's own ownership label, so it renders normally.
func TestProjectOnboarding_NamespaceOwnedByThisProject(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) { addProject(projects, "selfowned", "onb-owned-self") })
	objs := onboardingObjects(t, res)
	if got := remediationOf(objs, "onb-owned-self"); got != "enforce" {
		t.Errorf("namespace owned by this project: want rendered with enforce, got %q", got)
	}
	if roleBindingNamed(objs, "onb-owned-self", "autoshift-admin") == nil {
		t.Error("namespace owned by this project must render its RoleBindings")
	}
}

// adopt only covers a namespace no AutoShift project owns; another project's namespace is never taken.
func TestProjectOnboarding_AdoptNeverTakesOtherProjectsNamespace(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		addProject(projects, "foreign", "onb-owned-other")
		projects["foreign"].(map[string]interface{})["adopt"] = true
	})
	objs := onboardingObjects(t, res)
	if got := remediationOf(objs, "onb-owned-other"); got != "" {
		t.Errorf("adopt: true on a namespace owned by another project: want no objects, rendered with %q", got)
	}
	// foreign is an inform project (adopt: true, no mode); its error is reported all the same.
	t.Run("status", func(t *testing.T) {
		want := "namespace onb-owned-other is owned by project someone-else"
		if got := onboardingStatus(t, objs)["foreign"]; got != want {
			t.Errorf("status[foreign] = %q, want %q", got, want)
		}
	})
}

// pruned returns the mustnothave entries for kind in namespace (any name when name is "").
func pruned(objs []onboardingObject, kind, namespace, name string) []onboardingObject {
	var out []onboardingObject
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.complianceType != "mustnothave" || o.obj["kind"] != kind || meta["namespace"] != namespace {
			continue
		}
		if name == "" || meta["name"] == name {
			out = append(out, o)
		}
	}
	return out
}

// namespaceItem returns the object-template item (complianceType, metadataComplianceType, ...) of
// the Namespace name, or nil.
func namespaceItem(objs []onboardingObject, name string) map[string]interface{} {
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "Namespace" && meta["name"] == name && o.complianceType != "mustnothave" {
			return o.item
		}
	}
	return nil
}

// namespaceIn returns the Namespace entries named name in the ConfigurationPolicy with the given
// remediationAction (prune entries excluded), in render order.
func namespaceIn(objs []onboardingObject, name, remediation string) []onboardingObject {
	var out []onboardingObject
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "Namespace" && meta["name"] == name && o.complianceType != "mustnothave" && o.remediationAction == remediation {
			out = append(out, o)
		}
	}
	return out
}

func listsKey(list, key string) bool {
	return strings.Contains(","+list+",", ","+key+",")
}

// Prune on: the stub payments-api namespace records example.com/old as applied before; it is no
// longer in the catalog, so it is dropped. Labels and annotations other tools own are carried from
// the live namespace, because metadataComplianceType mustonlyhave makes the rendered set exact.
func TestProjectOnboarding_RemovedCatalogLabel(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingPruneBase(t))
	meta := namespaceNamed(objs, "payments-api")
	if meta == nil {
		t.Fatal("no Namespace named payments-api")
	}
	lbls := stringMap(meta, "labels")
	if v, ok := lbls["example.com/old"]; ok {
		t.Errorf("example.com/old was applied before and is no longer declared, want it dropped, got %q", v)
	}
	if lbls["other.tool/keep"] != "y" {
		t.Errorf("label owned by another tool must be kept, other.tool/keep = %q (labels %v)", lbls["other.tool/keep"], lbls)
	}
	if lbls["example.com/team"] != "payments" || lbls["autoshift.io/project"] != "payments-api" {
		t.Errorf("declared labels must be rendered, got %v", lbls)
	}
	ann := stringMap(meta, "annotations")
	if ann["openshift.io/sa.scc.mcs"] != "s0:c1,c0" {
		t.Errorf("annotation owned by another tool must be carried, got %v", ann)
	}
	if listsKey(ann["autoshift.io/project-labels"], "example.com/old") {
		t.Errorf("project-labels must list only this run's keys, got %q", ann["autoshift.io/project-labels"])
	}
	item := namespaceItem(objs, "payments-api")
	if item["complianceType"] != "musthave" || item["metadataComplianceType"] != "mustonlyhave" {
		t.Errorf("existing namespace: want complianceType musthave + metadataComplianceType mustonlyhave, got %v / %v",
			item["complianceType"], item["metadataComplianceType"])
	}
	if n := len(namespaceIn(objs, "payments-api", "inform")); n != 0 {
		t.Errorf("prune on: no drop report in the inform ConfigurationPolicy, got %d Namespace entries", n)
	}
}

// Prune off (the default): the dropped label stays on the enforced Namespace, and the inform
// ConfigurationPolicy reports the drop with an exact Namespace check that has it removed.
func TestProjectOnboarding_PruneOffKeepsLabels(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	enforced := namespaceIn(objs, "payments-api", "enforce")
	if len(enforced) != 1 {
		t.Fatalf("want one enforced payments-api Namespace, got %d", len(enforced))
	}
	meta := enforced[0].obj["metadata"].(map[string]interface{})
	lbls := stringMap(meta, "labels")
	if lbls["example.com/old"] != "x" || lbls["other.tool/keep"] != "y" || lbls["example.com/environment"] != "NONPROD" {
		t.Errorf("prune off: enforced Namespace must keep example.com/old and apply this run's labels, got %v", lbls)
	}
	applied := stringMap(meta, "annotations")["autoshift.io/project-labels"]
	for _, k := range []string{"example.com/old", "example.com/team", "example.com/environment"} {
		if !listsKey(applied, k) {
			t.Errorf("prune off: project-labels %q must be the union of earlier and this run's keys, missing %s", applied, k)
		}
	}
	reports := namespaceIn(objs, "payments-api", "inform")
	if len(reports) != 1 {
		t.Fatalf("prune off: want one drop report for payments-api in the inform ConfigurationPolicy, got %d", len(reports))
	}
	if reports[0].item["metadataComplianceType"] != "mustonlyhave" || reports[0].complianceType != "musthave" {
		t.Errorf("drop report must be musthave + metadataComplianceType mustonlyhave, got %v", reports[0].item)
	}
	rmeta := reports[0].obj["metadata"].(map[string]interface{})
	rl := stringMap(rmeta, "labels")
	if _, ok := rl["example.com/old"]; ok || rl["other.tool/keep"] != "y" {
		t.Errorf("drop report must show the namespace without example.com/old and with other tools' labels, got %v", rl)
	}
	if listsKey(stringMap(rmeta, "annotations")["autoshift.io/project-labels"], "example.com/old") {
		t.Error("drop report project-labels must list only this run's keys")
	}
	if n := len(namespaceIn(objs, "reporting", "inform")); n != 0 {
		t.Errorf("nothing dropped from reporting, want no drop report, got %d", n)
	}
}

// Only the exact value "true" turns deletion on.
func TestProjectOnboarding_PruneLabelExactTrue(t *testing.T) {
	t.Parallel()
	res := runOnboardingLabels(t, map[string]string{"autoshift.io/project-onboarding-prune": "True"}, nil)
	objs := onboardingObjects(t, res)
	for _, o := range objs {
		if o.complianceType == "mustnothave" && o.remediationAction != "inform" && !isStatus(o) {
			t.Errorf("prune label 'True' must not enable deletion, got enforce mustnothave %v", o.obj["metadata"])
		}
	}
	enforced := namespaceIn(objs, "payments-api", "enforce")
	if len(enforced) != 1 {
		t.Fatalf("want one enforced payments-api Namespace, got %d", len(enforced))
	}
	if lbls := stringMap(enforced[0].obj["metadata"].(map[string]interface{}), "labels"); lbls["example.com/old"] != "x" {
		t.Errorf("prune label 'True' must not strip labels, got %v", lbls)
	}
}

// A namespace that does not exist yet has no live metadata to carry, so it is created with this
// run's metadata only and no metadataComplianceType.
func TestProjectOnboarding_NewNamespaceMetadataMerge(t *testing.T) {
	t.Parallel()
	item := namespaceItem(onboardingObjects(t, onboardingBase(t)), "reporting")
	if item == nil {
		t.Fatal("no Namespace named reporting")
	}
	if v, ok := item["metadataComplianceType"]; ok {
		t.Errorf("new namespace must not set metadataComplianceType, got %v", v)
	}
}

func TestProjectOnboarding_PruneStaleBinding(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingPruneBase(t))
	got := pruned(objs, "RoleBinding", "removed-project", "autoshift-admin")
	if len(got) != 1 {
		t.Fatalf("want one mustnothave for RoleBinding removed-project/autoshift-admin, got %d", len(got))
	}
	if got[0].remediationAction != "enforce" {
		t.Errorf("undeclared project's binding pruned by the %q ConfigurationPolicy, want enforce", got[0].remediationAction)
	}
	if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
		t.Errorf("payments-api bindings are declared, want no mustnothave, got %d", len(p))
	}
}

// Prune off (the default): stale owned objects are only reported, by the inform
// ConfigurationPolicy, whatever the owning project's mode. Nothing is in the enforce one.
func TestProjectOnboarding_PruneOffReportsOnly(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, objs []onboardingObject, want map[string]string) {
		t.Helper()
		for _, o := range objs {
			if o.complianceType == "mustnothave" && o.remediationAction != "inform" && !isStatus(o) {
				t.Errorf("prune off: mustnothave %v in the %q ConfigurationPolicy, want inform only", o.obj["metadata"], o.remediationAction)
			}
		}
		for ns, name := range want {
			if len(pruned(objs, "RoleBinding", ns, name)) != 1 {
				t.Errorf("prune off: want %s/%s reported once in the inform ConfigurationPolicy", ns, name)
			}
		}
	}
	t.Run("default", func(t *testing.T) {
		check(t, onboardingObjects(t, onboardingBase(t)), map[string]string{
			"removed-project": "autoshift-admin", "legacy-app": "autoshift-stale",
		})
	})
	t.Run("enforce project", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["groups"].(map[string]interface{})["write"] = "K8S_{missing}"
		})
		check(t, onboardingObjects(t, res), map[string]string{"payments-api": "autoshift-write"})
	})
	t.Run("inform project", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			p := projects["payments-api"].(map[string]interface{})
			p["mode"] = "inform"
			p["groups"].(map[string]interface{})["write"] = "K8S_{missing}"
		})
		objs := onboardingObjects(t, res)
		check(t, objs, map[string]string{"payments-api": "autoshift-write"})
		if n := len(namespaceIn(objs, "payments-api", "enforce")); n != 0 {
			t.Errorf("inform project: want no Namespace in the enforce ConfigurationPolicy, got %d", n)
		}
	})
}

// Prune on: a level that no longer resolves is no longer declared, so its owned binding is pruned.
func TestProjectOnboarding_PruneRemovedLevel(t *testing.T) {
	t.Parallel()
	res := runPrune(t, func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["groups"].(map[string]interface{})["write"] = "K8S_{missing}"
	})
	objs := onboardingObjects(t, res)
	got := pruned(objs, "RoleBinding", "payments-api", "autoshift-write")
	if len(got) != 1 || got[0].remediationAction != "enforce" {
		t.Fatalf("want one enforce mustnothave for payments-api/autoshift-write, got %v", got)
	}
	if roleBindingNamed(objs, "payments-api", "autoshift-admin") == nil {
		t.Error("payments-api must still render autoshift-admin")
	}
}

// Prune on: a declared project that errored this run is skipped, never pruned: its config is
// being fixed.
func TestProjectOnboarding_NoPruneForErroringProject(t *testing.T) {
	t.Parallel()
	t.Run("skipped", func(t *testing.T) {
		res := runPrune(t, func(projects map[string]interface{}) {
			p := projects["payments-api"].(map[string]interface{})
			p["groups"] = map[string]interface{}{"admin": "K8S_{missing}"}
		})
		objs := onboardingObjects(t, res)
		if namespaceNamed(objs, "payments-api") != nil {
			t.Fatal("payments-api has no resolvable admin group, want it skipped")
		}
		if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
			t.Errorf("skipped declared project: want no mustnothave in payments-api, got %d", len(p))
		}
	})
	// team becomes invalid, so example.com/team (applied before, per the stub's project-labels)
	// is in error this run. Neither it nor the dropped example.com/old may be removed meanwhile.
	t.Run("rendered with errors", func(t *testing.T) {
		res := runPrune(t, func(projects map[string]interface{}) {
			p := projects["payments-api"].(map[string]interface{})
			p["groups"].(map[string]interface{})["write"] = "K8S_{missing}"
			p["team"] = strings.Repeat("a", 70)
		})
		objs := onboardingObjects(t, res)
		meta := namespaceNamed(objs, "payments-api")
		if meta == nil {
			t.Fatal("an invalid label does not skip the project")
		}
		if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
			t.Errorf("project with errors: want no mustnothave in payments-api, got %d", len(p))
		}
		lbls := stringMap(meta, "labels")
		if lbls["example.com/team"] != "payments" || lbls["example.com/old"] != "x" {
			t.Errorf("project with errors: previously applied labels must be kept, got %v", lbls)
		}
		if lbls["example.com/environment"] != "NONPROD" {
			t.Errorf("project with errors: this run's valid labels must still apply, got %v", lbls)
		}
		applied := stringMap(meta, "annotations")["autoshift.io/project-labels"]
		for _, k := range []string{"example.com/team", "example.com/old", "example.com/environment"} {
			if !listsKey(applied, k) {
				t.Errorf("project with errors: project-labels %q must list %s (union of earlier and this run's keys)", applied, k)
			}
		}
	})
}

// A namespace that exists unowned without adopt is not this project's: nothing in it is pruned or
// reported, whatever the prune setting.
func TestProjectOnboarding_NoPruneInUnadoptedNamespace(t *testing.T) {
	t.Parallel()
	mutate := func(projects map[string]interface{}) {
		projects["legacy-app"].(map[string]interface{})["adopt"] = false
	}
	for label, res := range map[string]ChartResult{"on": runPruneAllShards(t, mutate), "off": runOnboardingProjectsAllShards(t, mutate)} {
		if p := pruned(onboardingObjects(t, res), "RoleBinding", "legacy-app", ""); len(p) != 0 {
			t.Errorf("prune %s: legacy-app is not adopted, want no mustnothave in it, got %d", label, len(p))
		}
	}
}

// Prune on: an inform project never has anything deleted, so its prune entries are
// only in the inform ConfigurationPolicy.
func TestProjectOnboarding_NoPruneInInform(t *testing.T) {
	t.Parallel()
	got := pruned(onboardingObjects(t, onboardingPruneBase(t)), "RoleBinding", "legacy-app", "")
	if len(got) == 0 {
		t.Fatal("legacy-app/autoshift-stale is owned and undeclared, want a mustnothave in the inform ConfigurationPolicy")
	}
	for _, o := range got {
		if o.remediationAction != "inform" {
			t.Errorf("legacy-app is inform, prune entry found in the %q ConfigurationPolicy", o.remediationAction)
		}
	}
}

func countPruned(objs []onboardingObject) int {
	var n int
	for _, o := range objs {
		if o.complianceType == "mustnothave" && !isStatus(o) {
			n++
		}
	}
	return n
}

// Prune on: a rendered config without a projects key (a values restructure, a misplaced file) must
// not be read as "every project removed": the sweep is off. An explicit empty map still prunes.
func TestProjectOnboarding_NoSweepWithoutProjectsKey(t *testing.T) {
	t.Parallel()
	res := runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) { delete(cfg, "projects") })
	if n := countPruned(onboardingObjects(t, res)); n != 0 {
		t.Errorf("config has no projects key, want no mustnothave, got %d", n)
	}
	res = runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) { cfg["projects"] = map[string]interface{}{} })
	if p := pruned(onboardingObjects(t, res), "RoleBinding", "removed-project", "autoshift-admin"); len(p) != 1 {
		t.Errorf("projects: {} is a deliberate removal, want removed-project/autoshift-admin pruned, got %d", len(p))
	}
}

// Prune on, every hub config key removed: the rendered config is {clusterSet: hub} (the synthetic
// cluster config always carries clusterSet), so no sweep.
func TestProjectOnboarding_NoSweepWithoutHubConfig(t *testing.T) {
	t.Parallel()
	res := runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) {
		for k := range cfg {
			delete(cfg, k)
		}
	})
	if n := countPruned(onboardingObjects(t, res)); n != 0 {
		t.Errorf("rendered config carries no hub config, want no mustnothave, got %d", n)
	}
}

// A transient miss of the rendered config must never prune. RunPipeline always regenerates the
// rendered-config ConfigMap, so this drives hub and spoke resolution directly with a seed of only
// the testdata stubs (stale owned bindings included): first without the ConfigMap, then with one
// whose config is empty. Prune is on, so only the guard can stop the sweep.
func TestProjectOnboarding_NoSweepWithoutRenderedConfig(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	raw, err := KustomizeBuildWithRemediation(filepath.Join(root, "policies", "stable", "project-onboarding"), "enforce")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	stubs, err := LoadTestResources(filepath.Join(root, "tools", "testdata"))
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	for _, s := range stubs {
		// Only this cluster's rendered-config matters: testdata may carry another cluster's.
		if s.GetKind() == "ConfigMap" && s.GetName() == "lint-cluster.rendered-config" {
			t.Fatalf("testdata unexpectedly carries %s; this test needs it absent", s.GetName())
		}
	}
	emptyCM := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "lint-cluster.rendered-config", "namespace": "policies-autoshift"},
		"data":     map[string]interface{}{"config": ""},
	}}
	for name, seed := range map[string][]unstructured.Unstructured{
		"ConfigMap absent": stubs,
		"config empty":     append(append([]unstructured.Unstructured{}, stubs...), emptyCM),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: map[string]string{
				"autoshift.io/project-onboarding":       "true",
				"autoshift.io/project-onboarding-prune": "true",
			}}
			hubR, err := NewResolver(seed)
			if err != nil {
				t.Fatal(err)
			}
			hub := hubR.ResolvePolicy(raw, ctx)
			if len(hub.Errors) > 0 {
				t.Fatalf("hub resolve: %v", hub.Errors)
			}
			spokeR, err := NewSpokeResolver(seed)
			if err != nil {
				t.Fatal(err)
			}
			spoke := spokeR.ResolveSpokeTemplates(hub.Resolved, ctx)
			if len(spoke.Errors) > 0 {
				t.Fatalf("spoke resolve: %v", spoke.Errors)
			}
			objs := onboardingObjects(t, ChartResult{ResolvedYAML: spoke.Resolved})
			if n := countPruned(objs); n != 0 {
				t.Errorf("%s: want no mustnothave, got %d", name, n)
			}
			// Fail closed: the status is written, never cleared, so the inform gate stays
			// NonCompliant while nothing can be checked.
			status := onboardingStatus(t, objs)
			if want := "cannot read lint-cluster.rendered-config"; status["rendered-config"] != want || len(status) != 1 {
				t.Errorf("%s: want only status[rendered-config] = %q, got %v", name, want, status)
			}
			if n := len(statusEntries(objs, "mustnothave", "inform")); n != onboardingShards {
				t.Errorf("%s: want the inform gate of every shard, got %d", name, n)
			}
		})
	}
}

// A readable config with no projects key has no projects: nothing to report, so the status is
// cleared. A projects key that is not a map is malformed: reported, and nothing is swept.
func TestProjectOnboarding_ProjectsKeyShapes(t *testing.T) {
	t.Parallel()
	t.Run("absent", func(t *testing.T) {
		res := runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) { delete(cfg, "projects") })
		if status := onboardingStatus(t, onboardingObjects(t, res)); len(status) != 0 {
			t.Errorf("no projects key: want the status cleared, got %v", status)
		}
	})
	for name, value := range map[string]interface{}{"string": "oops", "list": []interface{}{"payments-api"}} {
		t.Run(name, func(t *testing.T) {
			res := runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) { cfg["projects"] = value })
			objs := onboardingObjects(t, res)
			if n := countPruned(objs); n != 0 {
				t.Errorf("projects is a %s: want no mustnothave, got %d", name, n)
			}
			want := "config.projects in lint-cluster.rendered-config is not a map"
			if status := onboardingStatus(t, objs); status["rendered-config"] != want || len(status) != 1 {
				t.Errorf("projects is a %s: want only status[rendered-config] = %q, got %v", name, want, status)
			}
		})
	}
}

// otherProjectsRender asserts that the example projects other than skip still render their
// Namespace and admin RoleBinding.
func otherProjectsRender(t *testing.T, objs []onboardingObject, skip string) {
	t.Helper()
	for _, project := range []string{"payments-api", "reporting", "legacy-app"} {
		if project == skip {
			continue
		}
		if namespaceNamed(objs, project) == nil {
			t.Errorf("%s must still render its Namespace", project)
		}
		if roleBindingNamed(objs, project, "autoshift-admin") == nil {
			t.Errorf("%s must still render autoshift-admin", project)
		}
	}
}

// A project entry that is not a map is skipped and reported; it never breaks hub resolution for
// the other projects.
func TestProjectOnboarding_NonMapProject(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]interface{}{
		"bad":    "oops",
		"nulled": nil,
		"listed": []interface{}{"a", "b"},
	} {
		t.Run(name, func(t *testing.T) {
			res := runOnboarding(t, func(projects map[string]interface{}) { projects[name] = value })
			objs := onboardingObjects(t, res)
			otherProjectsRender(t, objs, "")
			status := onboardingStatus(t, objs)
			if want := "project entry is not a map"; status[name] != want || len(status) != 1 {
				t.Errorf("want only status[%s] = %q, got %v", name, want, status)
			}
			if namespaceNamed(objs, name) != nil {
				t.Errorf("%s is invalid, want no Namespace", name)
			}
		})
	}
}

// A non-map catalog entry, labels map or groups map is an error for that item only: the rest of
// the project still renders.
func TestProjectOnboarding_NonMapProjectFields(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		set  func(p map[string]interface{})
		want string
	}{
		"new catalog entry":     {func(p map[string]interface{}) { p["labels"] = map[string]interface{}{"tier": "gold"} }, "labels entry tier is not a map"},
		"default catalog entry": {func(p map[string]interface{}) { p["labels"] = map[string]interface{}{"team": "gold"} }, "labels entry team is not a map"},
		"null catalog entry":    {func(p map[string]interface{}) { p["labels"] = map[string]interface{}{"tier": nil} }, "labels entry tier is not a map"},
		"labels":                {func(p map[string]interface{}) { p["labels"] = "gold" }, "labels is not a map"},
		"annotations":           {func(p map[string]interface{}) { p["annotations"] = []interface{}{"x"} }, "annotations is not a map"},
		"groups":                {func(p map[string]interface{}) { p["groups"] = "K8S_ADMINS" }, "groups is not a map"},
		"group level": {func(p map[string]interface{}) {
			p["groups"] = map[string]interface{}{"read": map[string]interface{}{"name": "x"}}
		}, "group read is not a string"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := runOnboarding(t, func(projects map[string]interface{}) { c.set(projects["payments-api"].(map[string]interface{})) })
			objs := onboardingObjects(t, res)
			otherProjectsRender(t, objs, "")
			status := onboardingStatus(t, objs)
			if status["payments-api"] != c.want || len(status) != 1 {
				t.Errorf("want only status[payments-api] = %q, got %v", c.want, status)
			}
			if lbls := stringMap(namespaceNamed(objs, "payments-api"), "labels"); lbls["example.com/environment"] != "NONPROD" {
				t.Errorf("the rest of payments-api must still render, labels %v", lbls)
			}
		})
	}
}

// A group name is a free-form subject name: an apostrophe is legal, and no quote may break the
// rendered YAML.
func TestProjectOnboarding_GroupNameEscaped(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"O'Brien-team", `team "Q": x`} {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["groups"].(map[string]interface{})["read"] = group
		})
		objs := onboardingObjects(t, res)
		rb := roleBindingNamed(objs, "payments-api", "autoshift-read")
		if rb == nil {
			t.Fatalf("group %q: no autoshift-read RoleBinding", group)
		}
		if got := rb["subjects"].([]interface{})[0].(map[string]interface{})["name"]; got != group {
			t.Errorf("group %q: subject name = %v", group, got)
		}
		otherProjectsRender(t, objs, "")
	}
}

// The namespace and the project name are DNS-1123 labels; anything else is a per-project error,
// so no quote or uppercase reaches the rendered YAML or a status ConfigMap key.
func TestProjectOnboarding_InvalidNamespaceOverride(t *testing.T) {
	t.Parallel()
	for _, ns := range []string{`pay"ments`, "Payments", strings.Repeat("a", 64)} {
		res := runPrune(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["namespace"] = ns
		})
		objs := onboardingObjects(t, res)
		status := onboardingStatus(t, objs)
		want := "namespace " + ns + " is not a valid DNS-1123 label"
		if status["payments-api"] != want || len(status) != 1 {
			t.Errorf("namespace %q: want only status[payments-api] = %q, got %v", ns, want, status)
		}
		if namespaceNamed(objs, ns) != nil {
			t.Errorf("namespace %q: want no Namespace", ns)
		}
		otherProjectsRender(t, objs, "payments-api")
		// The project is declared but skipped, so its live objects are never pruned.
		if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
			t.Errorf("namespace %q: payments-api is in error, want no mustnothave in it, got %d", ns, len(p))
		}
	}
}

func TestProjectOnboarding_InvalidProjectName(t *testing.T) {
	t.Parallel()
	for name, key := range map[string]string{
		"Bad_Name":              "Bad_Name",
		"bad name":              "bad_name",
		strings.Repeat("a", 64): strings.Repeat("a", 64),
	} {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			addProject(projects, name, "onb-fresh")
		})
		objs := onboardingObjects(t, res)
		status := onboardingStatus(t, objs)
		want := "project name " + name + " is not a valid DNS-1123 label"
		if status[key] != want || len(status) != 1 {
			t.Errorf("project %q: want only status[%s] = %q, got %v", name, key, want, status)
		}
		if namespaceNamed(objs, "onb-fresh") != nil {
			t.Errorf("project %q is invalid, want no Namespace", name)
		}
		otherProjectsRender(t, objs, "")
	}
}

// A mode that is neither enforce nor inform is reported and falls back to inform, so a typo never
// skips the adoption preview.
func TestProjectOnboarding_InvalidMode(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ project, mode string }{{"legacy-app", "infrom"}, {"payments-api", "Inform"}} {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			projects[c.project].(map[string]interface{})["mode"] = c.mode
		})
		objs := onboardingObjects(t, res)
		if got := remediationOf(objs, c.project); got != "inform" {
			t.Errorf("%s with mode %q rendered with %q, want inform", c.project, c.mode, got)
		}
		want := "mode " + c.mode + " is not enforce or inform"
		if got := onboardingStatus(t, objs)[c.project]; got != want {
			t.Errorf("status[%s] = %q, want %q", c.project, got, want)
		}
	}
}

// Prune on: a project moved to a namespace it cannot take (it exists, unowned, without adopt) did
// not render this run, so the bindings left in its old namespace are kept. While the new namespace
// is only being created they are kept too (the old namespace still carries the project label, so the
// project's EgressIP and AdminNetworkPolicy still select it, P1B-08); once the new namespace is live
// on the cluster, they are pruned.
func TestProjectOnboarding_NamespaceChangePruneGuard(t *testing.T) {
	t.Parallel()
	t.Run("new namespace blocked", func(t *testing.T) {
		res := runPrune(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["namespace"] = "onb-unowned"
		})
		objs := onboardingObjects(t, res)
		if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
			t.Errorf("payments-api did not render onb-unowned, want no mustnothave in its old namespace, got %d", len(p))
		}
		want := "namespace onb-unowned exists and is not owned by AutoShift; set adopt: true to take it over"
		if got := onboardingStatus(t, objs)["payments-api"]; got != want {
			t.Errorf("status[payments-api] = %q, want %q", got, want)
		}
	})
	t.Run("new namespace being created", func(t *testing.T) {
		res := runPrune(t, func(projects map[string]interface{}) {
			projects["payments-api"].(map[string]interface{})["namespace"] = "onb-fresh"
		})
		objs := onboardingObjects(t, res)
		if namespaceNamed(objs, "onb-fresh") == nil {
			t.Fatal("onb-fresh does not exist, want it rendered")
		}
		if p := pruned(objs, "RoleBinding", "payments-api", ""); len(p) != 0 {
			t.Errorf("payments-api moving to onb-fresh, which is not live yet: want its old bindings kept, got %d mustnothave", len(p))
		}
	})
	t.Run("new namespace live", func(t *testing.T) {
		r := stressRun(t, stressOpts{labels: pruneOn, extra: `apiVersion: v1
kind: Namespace
metadata:
  name: onb-fresh
  labels:
    autoshift.io/project: payments-api
    autoshift.io/managed-by: project-onboarding
`, mutate: func(cfg map[string]interface{}) {
			stressProject(cfg, "payments-api")["namespace"] = "onb-fresh"
		}})
		if namespaceNamed(r.objs, "onb-fresh") == nil {
			t.Fatalf("onb-fresh not rendered, status %v", stressStatus(t, r.objs))
		}
		if p := pruned(r.objs, "RoleBinding", "payments-api", "autoshift-write"); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("payments-api moved to the live onb-fresh, want its old autoshift-write pruned by enforce, got %v", p)
		}
	})
}

// After a move to a live namespace, the old Namespace still carries autoshift.io/project, so the
// project's EgressIP and AdminNetworkPolicy keep selecting it. With prune on, the ownership labels
// are removed from it (other labels and annotations kept); with prune off it is only reported by
// the inform ConfigurationPolicy. While the new namespace is not live, the old one is untouched.
func TestProjectOnboarding_MovedNamespaceReleased(t *testing.T) {
	t.Parallel()
	fresh := `apiVersion: v1
kind: Namespace
metadata:
  name: onb-fresh
  labels:
    autoshift.io/project: payments-api
    autoshift.io/managed-by: project-onboarding
`
	move := func(cfg map[string]interface{}) { stressProject(cfg, "payments-api")["namespace"] = "onb-fresh" }
	released := func(t *testing.T, objs []onboardingObject) []onboardingObject {
		t.Helper()
		var out []onboardingObject
		for _, o := range objs {
			meta, _ := o.obj["metadata"].(map[string]interface{})
			if o.obj["kind"] == "Namespace" && meta["name"] == "payments-api" && o.complianceType == "musthave" {
				out = append(out, o)
			}
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   string
	}{{"prune on", pruneOn, "enforce"}, {"prune off", nil, "inform"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := stressRun(t, stressOpts{labels: tc.labels, extra: fresh, mutate: move})
			got := released(t, r.objs)
			if len(got) != 1 || got[0].remediationAction != tc.want || got[0].item["metadataComplianceType"] != "mustonlyhave" {
				t.Fatalf("want one exact payments-api Namespace in the %s ConfigurationPolicy, got %d", tc.want, len(got))
			}
			lbls := stringMap(got[0].obj["metadata"].(map[string]interface{}), "labels")
			if _, ok := lbls["autoshift.io/project"]; ok {
				t.Errorf("old namespace keeps autoshift.io/project: %v", lbls)
			}
			if _, ok := lbls["autoshift.io/managed-by"]; ok {
				t.Errorf("old namespace keeps autoshift.io/managed-by: %v", lbls)
			}
			if lbls["other.tool/keep"] != "y" {
				t.Errorf("labels of other tools must be kept, got %v", lbls)
			}
		})
	}
	t.Run("new namespace not live", func(t *testing.T) {
		r := stressRun(t, stressOpts{labels: pruneOn, mutate: move})
		if got := released(t, r.objs); len(got) != 0 {
			t.Errorf("onb-fresh is not live yet, want the old namespace untouched, got %d Namespace checks", len(got))
		}
	})
}

// A platform namespace is never released, even when it carries the label of a project that moved.
func TestProjectOnboarding_PlatformNamespaceNeverReleased(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{labels: pruneOn, extra: `apiVersion: v1
kind: Namespace
metadata:
  name: onb-fresh
  labels: {autoshift.io/project: payments-api, autoshift.io/managed-by: project-onboarding}
---
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-example
  labels: {autoshift.io/project: payments-api, autoshift.io/managed-by: project-onboarding}
`, mutate: func(cfg map[string]interface{}) { stressProject(cfg, "payments-api")["namespace"] = "onb-fresh" }})
	if namespaceNamed(r.objs, "openshift-example") != nil {
		t.Error("openshift-example is a platform namespace, want its labels left alone")
	}
	if namespaceNamed(r.objs, "payments-api") == nil {
		t.Error("the old namespace payments-api must still be released")
	}
}

// namespace takes a string. false (also YAML 1.1 no) or a number would otherwise become a valid
// namespace name.
func TestProjectOnboarding_NamespaceNotAString(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{false, 123, map[string]interface{}{"a": "b"}} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) { stressProject(cfg, "reporting")["namespace"] = v }})
			if !strings.Contains(stressStatus(t, r.objs)["reporting"], "is not a string") {
				t.Errorf("namespace %#v: want reported, status %v", v, stressStatus(t, r.objs))
			}
			for k := range stressRendered(r.objs) {
				if strings.Contains(k, "|false|") || strings.Contains(k, "|123|") || k == "Namespace||false" || k == "Namespace||123" || k == "Namespace||reporting" {
					t.Errorf("namespace %#v: %s rendered", v, k)
				}
			}
		})
	}
}

// A project's enabled: false turns a catalog entry off even when the catalog spells enabled: true
// (a plain merge would keep true, since false is a zero value).
func TestProjectOnboarding_CatalogEnabledFalseOverride(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["labels"].(map[string]interface{})["team"].(map[string]interface{})["enabled"] = true
		stressProject(cfg, "reporting")["labels"] = map[string]interface{}{"team": map[string]interface{}{"enabled": false}}
	}})
	if v, ok := stringMap(namespaceNamed(r.objs, "reporting"), "labels")["example.com/team"]; ok {
		t.Errorf("reporting turned the team entry off, but example.com/team=%s is applied", v)
	}
	if _, ok := stringMap(namespaceNamed(r.objs, "payments-api"), "labels")["example.com/team"]; !ok {
		t.Error("payments-api keeps the team entry on")
	}
}

// A declared namespace whose autoshift.io/project label is empty must not hold back pruning: an
// owned object without that label (here a stale admin AdminNetworkPolicy) is still pruned.
func TestProjectOnboarding_EmptyProjectLabelHoldsNothing(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{labels: pruneOn, extra: `apiVersion: v1
kind: Namespace
metadata:
  name: reporting
  labels:
    autoshift.io/project: ""
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-admin-stale
  labels:
    autoshift.io/managed-by: project-onboarding
`})
	if p := stressPruned(r.objs)["AdminNetworkPolicy||autoshift-admin-stale"]; p != "enforce" {
		t.Errorf("stale autoshift-admin-stale: want pruned by enforce, got %q", p)
	}
}

func TestProjectOnboarding_NamespaceNeverPruned(t *testing.T) {
	t.Parallel()
	removeAll := func(projects map[string]interface{}) {
		for name := range projects {
			delete(projects, name)
		}
	}
	for _, res := range []ChartResult{onboardingBase(t), onboardingPruneBase(t), runOnboardingProjectsAllShards(t, removeAll), runPruneAllShards(t, removeAll)} {
		for _, o := range onboardingObjects(t, res) {
			if o.complianceType == "mustnothave" && o.obj["kind"] == "Namespace" {
				t.Errorf("Namespace %v is pruned, namespaces must never be deleted", o.obj["metadata"])
			}
		}
	}
}

// objectNamed returns the rendered (not mustnothave) object of kind kind called name in namespace,
// or nil.
func objectNamed(objs []onboardingObject, kind, namespace, name string) *onboardingObject {
	for i, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == kind && meta["namespace"] == namespace && meta["name"] == name && o.complianceType != "mustnothave" {
			return &objs[i]
		}
	}
	return nil
}

// quotaHard returns spec.hard of the project's autoshift-quota ResourceQuota, or nil when none is
// rendered.
func quotaHard(objs []onboardingObject, namespace string) map[string]interface{} {
	o := objectNamed(objs, "ResourceQuota", namespace, "autoshift-quota")
	if o == nil {
		return nil
	}
	hard, _ := o.obj["spec"].(map[string]interface{})["hard"].(map[string]interface{})
	return hard
}

// containerLimits returns the single Container item of the project's autoshift-limits LimitRange,
// or nil when none is rendered.
func containerLimits(t *testing.T, objs []onboardingObject, namespace string) map[string]interface{} {
	t.Helper()
	o := objectNamed(objs, "LimitRange", namespace, "autoshift-limits")
	if o == nil {
		return nil
	}
	limits, _ := o.obj["spec"].(map[string]interface{})["limits"].([]interface{})
	if len(limits) != 1 {
		t.Fatalf("autoshift-limits in %s: want one limits item, got %v", namespace, limits)
	}
	item, _ := limits[0].(map[string]interface{})
	if item["type"] != "Container" {
		t.Errorf("autoshift-limits in %s: type = %v, want Container", namespace, item["type"])
	}
	return item
}

// payments-api selects size small and overrides pods. Quota values stay strings, since ResourceQuota
// hard values are quantities.
func TestProjectOnboarding_Quota(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	rq := objectNamed(objs, "ResourceQuota", "payments-api", "autoshift-quota")
	if rq == nil {
		t.Fatal("payments-api has size small, want ResourceQuota autoshift-quota")
	}
	want := map[string]interface{}{"requests.cpu": "4", "requests.memory": "8Gi", "limits.memory": "16Gi", "pods": "80"}
	hard := quotaHard(objs, "payments-api")
	if len(hard) != len(want) {
		t.Errorf("payments-api quota = %v, want %v", hard, want)
	}
	for k, v := range want {
		if hard[k] != v {
			t.Errorf("payments-api quota %s = %#v, want %#v", k, hard[k], v)
		}
	}
	lr := objectNamed(objs, "LimitRange", "payments-api", "autoshift-limits")
	if lr == nil {
		t.Fatal("payments-api has size small, want LimitRange autoshift-limits")
	}
	item := containerLimits(t, objs, "payments-api")
	def, _ := item["default"].(map[string]interface{})
	req, _ := item["defaultRequest"].(map[string]interface{})
	if def["cpu"] != "500m" || def["memory"] != "512Mi" || req["cpu"] != "100m" || req["memory"] != "128Mi" {
		t.Errorf("payments-api LimitRange = %v, want default 500m/512Mi and defaultRequest 100m/128Mi", item)
	}
	for _, o := range []*onboardingObject{rq, lr} {
		meta := o.obj["metadata"].(map[string]interface{})
		lbls := stringMap(meta, "labels")
		if lbls["autoshift.io/project"] != "payments-api" || lbls["autoshift.io/managed-by"] != "project-onboarding" {
			t.Errorf("%s ownership labels = %v", o.obj["kind"], lbls)
		}
		// Exact, so a key removed from the size or the project goes away on the cluster.
		if o.complianceType != "mustonlyhave" {
			t.Errorf("%s complianceType = %q, want mustonlyhave", o.obj["kind"], o.complianceType)
		}
		if o.remediationAction != "enforce" {
			t.Errorf("%s rendered by the %q ConfigurationPolicy, want enforce", o.obj["kind"], o.remediationAction)
		}
	}
}

// A size that projectDefaults.sizes does not define is a required input: the project is skipped
// whole and reported, and the other projects still render.
func TestProjectOnboarding_UndefinedSize(t *testing.T) {
	t.Parallel()
	res := runPrune(t, func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["size"] = "huge"
	})
	objs := onboardingObjects(t, res)
	status := onboardingStatus(t, objs)
	if want := "size huge is not defined in projectDefaults.sizes"; status["payments-api"] != want || len(status) != 1 {
		t.Errorf("want only status[payments-api] = %q, got %v", want, status)
	}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if (meta["name"] == "payments-api" || meta["namespace"] == "payments-api") && !isStatus(o) {
			t.Errorf("payments-api is skipped, want nothing rendered for it, got %s %v (%s)", o.obj["kind"], meta["name"], o.complianceType)
		}
	}
	otherProjectsRender(t, objs, "payments-api")
}

func TestProjectOnboarding_NoSize(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	if namespaceNamed(objs, "reporting") == nil {
		t.Fatal("no Namespace named reporting")
	}
	if o := objectNamed(objs, "ResourceQuota", "reporting", "autoshift-quota"); o != nil {
		t.Errorf("reporting has no size, want no ResourceQuota, got %v", o.obj)
	}
	if o := objectNamed(objs, "LimitRange", "reporting", "autoshift-limits"); o != nil {
		t.Errorf("reporting has no size, want no LimitRange, got %v", o.obj)
	}
}

// A project override merges into its size key by key, and never leaks into another project of the
// same size.
func TestProjectOnboarding_QuotaOverrideMerge(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		p := projects["payments-api"].(map[string]interface{})
		p["limitRange"] = map[string]interface{}{"default": map[string]interface{}{"memory": "1Gi"}, "max": map[string]interface{}{"cpu": "2"}}
		projects["reporting"].(map[string]interface{})["size"] = "small"
	})
	objs := onboardingObjects(t, res)
	item := containerLimits(t, objs, "payments-api")
	def, _ := item["default"].(map[string]interface{})
	max, _ := item["max"].(map[string]interface{})
	if def["cpu"] != "500m" || def["memory"] != "1Gi" || max["cpu"] != "2" {
		t.Errorf("payments-api LimitRange = %v, want default cpu 500m (size), memory 1Gi and max cpu 2 (project)", item)
	}
	if hard := quotaHard(objs, "reporting"); hard["pods"] != "50" || hard["requests.cpu"] != "4" {
		t.Errorf("reporting quota = %v, want size small unchanged (pods 50)", hard)
	}
	rItem := containerLimits(t, objs, "reporting")
	if d, _ := rItem["default"].(map[string]interface{}); d["memory"] != "512Mi" {
		t.Errorf("reporting LimitRange default = %v, payments-api override leaked into the shared size", d)
	}
	if _, ok := rItem["max"]; ok {
		t.Errorf("reporting LimitRange = %v, payments-api max leaked into the shared size", rItem)
	}
}

// A quota on an inform project is rendered by the inform ConfigurationPolicy.
func TestProjectOnboarding_QuotaFollowsMode(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["legacy-app"].(map[string]interface{})["size"] = "small"
	})
	objs := onboardingObjects(t, res)
	for _, kind := range []string{"ResourceQuota", "LimitRange"} {
		name := map[string]string{"ResourceQuota": "autoshift-quota", "LimitRange": "autoshift-limits"}[kind]
		o := objectNamed(objs, kind, "legacy-app", name)
		if o == nil || o.remediationAction != "inform" {
			t.Errorf("legacy-app is inform, want its %s in the inform ConfigurationPolicy, got %v", kind, o)
		}
	}
}

// Malformed size input. A size that does not resolve skips the project; a bad quota or LimitRange
// entry skips only that object, so the live one is left as it is rather than loosened.
func TestProjectOnboarding_QuotaInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		set          func(cfg map[string]interface{}, p map[string]interface{})
		want         string
		skipped      bool
		quota, limit bool
		project      string // the project under test, payments-api when empty
	}{
		"size not a string": {func(_, p map[string]interface{}) { p["size"] = map[string]interface{}{"name": "small"} },
			"size is not a string", true, false, false, ""},
		"size entry not a map": {func(cfg, _ map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["sizes"].(map[string]interface{})["small"] = "4cpu"
		}, "projectDefaults.sizes.small is not a map", true, false, false, ""},
		"quota not a map": {func(_, p map[string]interface{}) { p["quota"] = "80" },
			"quota is not a map", false, false, true, ""},
		"limitRange not a map": {func(_, p map[string]interface{}) { p["limitRange"] = []interface{}{"x"} },
			"limitRange is not a map", false, true, false, ""},
		"size quota not a map": {func(cfg, _ map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["sizes"].(map[string]interface{})["small"].(map[string]interface{})["quota"] = "x"
		}, "projectDefaults.sizes.small.quota is not a map", false, false, true, ""},
		"bad quantity": {func(_, p map[string]interface{}) { p["quota"] = map[string]interface{}{"pods": "lots"} },
			"quota pods=lots is not a valid quantity", false, false, true, ""},
		"negative quantity": {func(_, p map[string]interface{}) { p["quota"] = map[string]interface{}{"pods": "-1"} },
			"quota pods=-1 is not a valid quantity", false, false, true, ""},
		"negative limit": {func(_, p map[string]interface{}) {
			p["limitRange"] = map[string]interface{}{"min": map[string]interface{}{"cpu": "-100m"}}
		}, "limitRange.min cpu=-100m is not a valid quantity", false, true, false, ""},
		// reporting has no size: its overrides are reported, the rest of it renders, and no quota
		// object is declared for it.
		"quota without size": {func(cfg, _ map[string]interface{}) {
			r := cfg["projects"].(map[string]interface{})["reporting"].(map[string]interface{})
			r["quota"] = map[string]interface{}{"pods": "80"}
			r["limitRange"] = map[string]interface{}{"max": map[string]interface{}{"cpu": "2"}}
		}, "quota is set without size; limitRange is set without size", false, false, false, "reporting"},
		"bad resource name": {func(_, p map[string]interface{}) { p["quota"] = map[string]interface{}{`pods": x`: "1"} },
			`quota key pods": x is not a valid resource name`, false, false, true, ""},
		"map quantity": {func(_, p map[string]interface{}) {
			p["quota"] = map[string]interface{}{"pods": map[string]interface{}{"a": "b"}}
		},
			"quota pods=map[a:b] is not a valid quantity", false, false, true, ""},
		"unknown limitRange field": {func(_, p map[string]interface{}) {
			p["limitRange"] = map[string]interface{}{"defaults": map[string]interface{}{"cpu": "1"}}
		}, "limitRange field defaults is not one of default, defaultRequest, max, min, maxLimitRequestRatio", false, true, false, ""},
		"limitRange field not a map": {func(_, p map[string]interface{}) { p["limitRange"] = map[string]interface{}{"max": "2"} },
			"limitRange.max is not a map", false, true, false, ""},
		"bad limitRange quantity": {func(_, p map[string]interface{}) {
			p["limitRange"] = map[string]interface{}{"default": map[string]interface{}{"cpu": "1 core"}}
		}, "limitRange.default cpu=1 core is not a valid quantity", false, true, false, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			proj := c.project
			if proj == "" {
				proj = "payments-api"
			}
			res := runOnboardingLabels(t, pruneOn, func(cfg map[string]interface{}) {
				c.set(cfg, cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{}))
			})
			objs := onboardingObjects(t, res)
			status := onboardingStatus(t, objs)
			if status[proj] != c.want || len(status) != 1 {
				t.Errorf("want only status[%s] = %q, got %v", proj, c.want, status)
			}
			if c.skipped {
				if namespaceNamed(objs, proj) != nil {
					t.Errorf("%s is skipped, want no Namespace", proj)
				}
				otherProjectsRender(t, objs, proj)
			} else {
				otherProjectsRender(t, objs, "")
				for _, rb := range []string{"autoshift-admin", "autoshift-write", "autoshift-read"} {
					if roleBindingNamed(objs, proj, rb) == nil {
						t.Errorf("%s must still render RoleBinding %s", proj, rb)
					}
				}
			}
			gotQuota := objectNamed(objs, "ResourceQuota", proj, "autoshift-quota") != nil
			gotLimit := objectNamed(objs, "LimitRange", proj, "autoshift-limits") != nil
			if gotQuota != c.quota || gotLimit != c.limit {
				t.Errorf("rendered ResourceQuota %v, LimitRange %v; want %v, %v", gotQuota, gotLimit, c.quota, c.limit)
			}
			if n := len(pruned(objs, "ResourceQuota", proj, "")) + len(pruned(objs, "LimitRange", proj, "")); n != 0 {
				t.Errorf("%s is in error, want its live quota objects kept, got %d mustnothave", proj, n)
			}
		})
	}
}

// The quota kinds join the prune sweep: a removed project's quota objects, and a project's quota
// objects once it drops its size, are pruned with prune on and only reported with prune off.
func TestProjectOnboarding_PruneQuota(t *testing.T) {
	t.Parallel()
	kinds := map[string]string{"ResourceQuota": "autoshift-quota", "LimitRange": "autoshift-limits"}
	t.Run("removed project", func(t *testing.T) {
		on := onboardingObjects(t, onboardingPruneBase(t))
		off := onboardingObjects(t, onboardingBase(t))
		for kind, name := range kinds {
			if p := pruned(on, kind, "removed-project", name); len(p) != 1 || p[0].remediationAction != "enforce" {
				t.Errorf("prune on: want removed-project/%s %s pruned by enforce, got %v", name, kind, p)
			}
			if p := pruned(off, kind, "removed-project", name); len(p) != 1 || p[0].remediationAction != "inform" {
				t.Errorf("prune off: want removed-project/%s %s reported by inform, got %v", name, kind, p)
			}
			// Declared: the live stub in payments-api is corrected in place, never pruned.
			if p := pruned(on, kind, "payments-api", ""); len(p) != 0 {
				t.Errorf("payments-api declares its %s, want no mustnothave, got %d", kind, len(p))
			}
		}
	})
	// Only size removed: the quota override is now set without size, an error, so nothing is pruned.
	t.Run("size removed, override kept", func(t *testing.T) {
		res := runPruneAllShards(t, func(projects map[string]interface{}) {
			delete(projects["payments-api"].(map[string]interface{}), "size")
		})
		objs := onboardingObjects(t, res)
		if got := onboardingStatus(t, objs)["payments-api"]; got != "quota is set without size" {
			t.Errorf("status[payments-api] = %q, want %q", got, "quota is set without size")
		}
		for kind := range kinds {
			if p := pruned(objs, kind, "payments-api", ""); len(p) != 0 {
				t.Errorf("payments-api is in error, want its live %s kept, got %d mustnothave", kind, len(p))
			}
		}
	})
	t.Run("size removed", func(t *testing.T) {
		res := runPruneAllShards(t, func(projects map[string]interface{}) {
			delete(projects["payments-api"].(map[string]interface{}), "size")
			delete(projects["payments-api"].(map[string]interface{}), "quota")
		})
		objs := onboardingObjects(t, res)
		for kind, name := range kinds {
			if objectNamed(objs, kind, "payments-api", name) != nil {
				t.Errorf("payments-api has no size, want no %s rendered", kind)
			}
			if p := pruned(objs, kind, "payments-api", name); len(p) != 1 || p[0].remediationAction != "enforce" {
				t.Errorf("payments-api dropped its size, want its %s pruned by enforce, got %v", kind, p)
			}
		}
	})
}

// The API server fills a LimitRange default from max, and defaultRequest from default and then min.
// The hub applies the same defaulting, so the exact (mustonlyhave) object matches what is stored.
func TestProjectOnboarding_LimitRangeDefaulting(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		sizes := cfg["projectDefaults"].(map[string]interface{})["sizes"].(map[string]interface{})
		sizes["small"].(map[string]interface{})["limitRange"] = map[string]interface{}{
			"max": map[string]interface{}{"cpu": "2", "memory": "2Gi"},
			"min": map[string]interface{}{"ephemeral-storage": "1Gi"},
		}
		sizes["only-default"] = map[string]interface{}{
			"limitRange": map[string]interface{}{"default": map[string]interface{}{"cpu": "500m"}},
		}
		cfg["projects"].(map[string]interface{})["reporting"].(map[string]interface{})["size"] = "only-default"
	})
	objs := onboardingObjects(t, res)
	str := func(item map[string]interface{}, field string) map[string]interface{} {
		m, _ := item[field].(map[string]interface{})
		return m
	}
	item := containerLimits(t, objs, "payments-api")
	if item == nil {
		t.Fatal("payments-api: no LimitRange")
	}
	if d := str(item, "default"); d["cpu"] != "2" || d["memory"] != "2Gi" || len(d) != 2 {
		t.Errorf("payments-api default = %v, want filled from max (cpu 2, memory 2Gi)", d)
	}
	if r := str(item, "defaultRequest"); r["cpu"] != "2" || r["memory"] != "2Gi" || r["ephemeral-storage"] != "1Gi" || len(r) != 3 {
		t.Errorf("payments-api defaultRequest = %v, want filled from default and min", r)
	}
	item = containerLimits(t, objs, "reporting")
	if item == nil {
		t.Fatal("reporting: no LimitRange")
	}
	if r := str(item, "defaultRequest"); r["cpu"] != "500m" || len(r) != 1 {
		t.Errorf("reporting defaultRequest = %v, want filled from default (cpu 500m)", r)
	}
	if _, ok := item["max"]; ok {
		t.Errorf("reporting LimitRange = %v, want no max", item)
	}
	if o := objectNamed(objs, "ResourceQuota", "reporting", "autoshift-quota"); o != nil {
		t.Errorf("size only-default has no quota, want no ResourceQuota, got %v", o.obj)
	}
}

// onboardingNetworkStubs is the testdata file holding the managed cluster's Network config (and the
// CRD that gates its lookup). testdataWithout drops it to model a cluster whose CIDRs are unknown.
const onboardingNetworkStubs = "project-onboarding-network.yaml"

// testdataWithout copies tools/testdata into a temp dir without the file skip, plus extra (a YAML
// document stream) as a new file when not empty, and returns the dir.
func testdataWithout(t *testing.T, skip, extra string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "tools", "testdata")
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == skip {
			found = true
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatalf("testdata has no %s to drop", skip)
	}
	if extra != "" {
		if err := os.WriteFile(filepath.Join(dst, "zz-extra.yaml"), []byte(extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// networkPolicyNamed returns the rendered (not mustnothave) NetworkPolicy name in namespace, or nil.
func networkPolicyNamed(objs []onboardingObject, namespace, name string) *onboardingObject {
	return objectNamed(objs, "NetworkPolicy", namespace, name)
}

func npSpec(o *onboardingObject) map[string]interface{} {
	spec, _ := o.obj["spec"].(map[string]interface{})
	return spec
}

// npRules returns spec.ingress or spec.egress of a NetworkPolicy as maps.
func npRules(o *onboardingObject, direction string) []map[string]interface{} {
	raw, _ := npSpec(o)[direction].([]interface{})
	var out []map[string]interface{}
	for _, r := range raw {
		m, _ := r.(map[string]interface{})
		out = append(out, m)
	}
	return out
}

// asJSON renders v as compact JSON, for comparing nested structures in one string.
func asJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func networkPoliciesIn(objs []onboardingObject, namespace string) []string {
	var names []string
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "NetworkPolicy" && meta["namespace"] == namespace && o.complianceType != "mustnothave" {
			names = append(names, meta["name"].(string))
		}
	}
	return names
}

// checkOwnedExact asserts the ownership labels, mustonlyhave and the ConfigurationPolicy of o.
func checkOwnedExact(t *testing.T, o *onboardingObject, project, remediation string) {
	t.Helper()
	meta := o.obj["metadata"].(map[string]interface{})
	lbls := stringMap(meta, "labels")
	if lbls["autoshift.io/project"] != project || lbls["autoshift.io/managed-by"] != "project-onboarding" {
		t.Errorf("%s %v ownership labels = %v", o.obj["kind"], meta["name"], lbls)
	}
	if o.complianceType != "mustonlyhave" {
		t.Errorf("%s %v complianceType = %q, want mustonlyhave", o.obj["kind"], meta["name"], o.complianceType)
	}
	if o.remediationAction != remediation {
		t.Errorf("%s %v rendered by the %q ConfigurationPolicy, want %s", o.obj["kind"], meta["name"], o.remediationAction, remediation)
	}
}

// payments-api selects the strict profile: default deny both ways. The egress deny admits only
// cluster-external destinations, 0.0.0.0/0 except the cluster and service networks of the managed
// cluster's Network config (the testdata stub), so external egress is left to the EgressFirewall.
func TestProjectOnboarding_NetworkDefaultDeny(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	in := networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-ingress")
	if in == nil {
		t.Fatalf("payments-api is strict, want autoshift-default-deny-ingress, got %v", networkPoliciesIn(objs, "payments-api"))
	}
	checkOwnedExact(t, in, "payments-api", "enforce")
	if got := asJSON(npSpec(in)); got != `{"podSelector":{},"policyTypes":["Ingress"]}` {
		t.Errorf("autoshift-default-deny-ingress spec = %s, want an empty pod selector and no ingress allows", got)
	}
	eg := networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-egress")
	if eg == nil {
		t.Fatalf("payments-api is strict, want autoshift-default-deny-egress, got %v", networkPoliciesIn(objs, "payments-api"))
	}
	checkOwnedExact(t, eg, "payments-api", "enforce")
	want := `{"egress":[{"to":[{"ipBlock":{"cidr":"0.0.0.0/0","except":["10.128.0.0/14","172.30.0.0/16"]}}]}],"podSelector":{},"policyTypes":["Egress"]}`
	if got := asJSON(npSpec(eg)); got != want {
		t.Errorf("autoshift-default-deny-egress spec = %s, want %s", got, want)
	}
	if n := networkPoliciesIn(objs, "reporting"); len(n) != 0 {
		t.Errorf("reporting has no network profile or rules, want no NetworkPolicy, got %v", n)
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}

	// config.networking (the cluster-install shape: clusterNetwork a map or a list, serviceNetwork a
	// list) is the fallback when the managed cluster has no Network config; the live config wins
	// over it. The lint cluster's rendered config carries the cluster-install example networking at
	// cluster level, so the mutation goes there.
	setCfgNetwork := func(c *ExampleConfigs) {
		c.ClusterInstallConfig["networking"] = map[string]interface{}{
			"clusterNetwork": []interface{}{map[string]interface{}{"cidr": "10.132.0.0/14", "hostPrefix": 23}},
			"serviceNetwork": []interface{}{"172.31.0.0/16"},
		}
	}
	t.Run("config.networking fallback", func(t *testing.T) {
		res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), nil, setCfgNetwork)
		objs := onboardingObjects(t, res)
		eg := networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-egress")
		if eg == nil {
			t.Fatal("want autoshift-default-deny-egress from config.networking")
		}
		if got := asJSON(npRules(eg, "egress")); got != `[{"to":[{"ipBlock":{"cidr":"0.0.0.0/0","except":["10.132.0.0/14","172.31.0.0/16"]}}]}]` {
			t.Errorf("egress = %s, want the config.networking CIDRs excepted", got)
		}
	})
	// The cluster-install examples write clusterNetwork as a single map.
	t.Run("config.networking map form", func(t *testing.T) {
		res := runOnboardingIn(t, testdataWithout(t, onboardingNetworkStubs, ""), nil, nil)
		eg := networkPolicyNamed(onboardingObjects(t, res), "payments-api", "autoshift-default-deny-egress")
		if eg == nil || asJSON(npRules(eg, "egress")) != `[{"to":[{"ipBlock":{"cidr":"0.0.0.0/0","except":["10.128.0.0/14","172.30.0.0/16"]}}]}]` {
			t.Errorf("want the cluster-install example CIDRs excepted, got %v", eg)
		}
	})
	t.Run("live config wins", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingAll(t, filepath.Join(repoRoot(t), "tools", "testdata"), nil, setCfgNetwork))
		eg := networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-egress")
		if eg == nil || !strings.Contains(asJSON(npSpec(eg)), `"except":["10.128.0.0/14","172.30.0.0/16"]`) {
			t.Errorf("want the live Network config CIDRs excepted, got %v", eg)
		}
	})
	// Lists replace whole in the merge, so empty ones blank the example networking.
	noCfgNetwork := func(c *ExampleConfigs) {
		c.ClusterInstallConfig["networking"] = map[string]interface{}{"clusterNetwork": []interface{}{}, "serviceNetwork": []interface{}{}}
	}
	// Unknown CIDRs never widen the project: autoshift-default-deny-egress is still rendered, as a
	// full deny (no egress key, so no ipBlock), and the project is reported. The report is
	// informational, not a project error: the rest renders and pruning still applies, so a stale
	// allow cannot survive and reopen the project.
	t.Run("CIDRs unknown", func(t *testing.T) {
		res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), pruneOn, noCfgNetwork)
		objs := onboardingObjects(t, res)
		for _, o := range objs {
			if o.obj["kind"] == "NetworkPolicy" && o.complianceType != "mustnothave" && strings.Contains(asJSON(npSpec(&o)), "ipBlock") {
				t.Errorf("CIDRs unknown: want no ipBlock in any NetworkPolicy, got %s", asJSON(npSpec(&o)))
			}
		}
		eg := networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-egress")
		if eg == nil {
			t.Fatalf("CIDRs unknown: want autoshift-default-deny-egress as a full deny, got %v", networkPoliciesIn(objs, "payments-api"))
		}
		checkOwnedExact(t, eg, "payments-api", "enforce")
		if got := asJSON(npSpec(eg)); got != `{"podSelector":{},"policyTypes":["Egress"]}` {
			t.Errorf("autoshift-default-deny-egress spec = %s, want a full egress deny", got)
		}
		if networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-ingress") == nil || networkPolicyNamed(objs, "payments-api", "autoshift-rule-from-frontend") == nil {
			t.Errorf("CIDRs unknown: the other NetworkPolicies must still render, got %v", networkPoliciesIn(objs, "payments-api"))
		}
		want := "cannot read the cluster and service network CIDRs, so autoshift-default-deny-egress denies all egress"
		if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
			t.Errorf("want only status[payments-api] = %q, got %v", want, status)
		}
		if p := pruned(objs, "NetworkPolicy", "payments-api", "autoshift-rule-old"); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("CIDRs unknown is informational: want the stale autoshift-rule-old still pruned by enforce, got %v", p)
		}
		if p := pruned(objs, "NetworkPolicy", "payments-api", "autoshift-default-deny-egress"); len(p) != 0 {
			t.Errorf("the full deny is declared, want it never pruned, got %v", p)
		}
	})
	t.Run("dual stack", func(t *testing.T) {
		dual := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: networks.config.openshift.io
spec:
  group: config.openshift.io
  names: {kind: Network, plural: networks}
  scope: Cluster
---
apiVersion: config.openshift.io/v1
kind: Network
metadata:
  name: cluster
spec:
  clusterNetwork:
    - {cidr: 10.128.0.0/14, hostPrefix: 23}
    - {cidr: 'fd01::/48', hostPrefix: 64}
  serviceNetwork: [172.30.0.0/16, 'fd02::/112']
`
		res := runOnboardingIn(t, testdataWithout(t, onboardingNetworkStubs, dual), nil, nil)
		eg := networkPolicyNamed(onboardingObjects(t, res), "payments-api", "autoshift-default-deny-egress")
		if eg == nil {
			t.Fatal("want autoshift-default-deny-egress")
		}
		want := `[{"to":[{"ipBlock":{"cidr":"0.0.0.0/0","except":["10.128.0.0/14","172.30.0.0/16"]}},{"ipBlock":{"cidr":"::/0","except":["fd01::/48","fd02::/112"]}}]}]`
		if got := asJSON(npRules(eg, "egress")); got != want {
			t.Errorf("egress = %s, want %s", got, want)
		}
	})
}

// An in-cluster allow rule becomes one NetworkPolicy autoshift-rule-<name>. An egress rule to cidrs
// belongs to the EgressFirewall, so it renders no NetworkPolicy.
func TestProjectOnboarding_NetworkAllowFromNamespaces(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	np := networkPolicyNamed(objs, "payments-api", "autoshift-rule-from-frontend")
	if np == nil {
		t.Fatalf("want autoshift-rule-from-frontend in payments-api, got %v", networkPoliciesIn(objs, "payments-api"))
	}
	checkOwnedExact(t, np, "payments-api", "enforce")
	want := `{"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"example.com/team":"web"}}}],"ports":[{"port":8443,"protocol":"TCP"}]}],"podSelector":{},"policyTypes":["Ingress"]}`
	if got := asJSON(npSpec(np)); got != want {
		t.Errorf("autoshift-rule-from-frontend spec = %s, want %s", got, want)
	}
	if networkPolicyNamed(objs, "payments-api", "autoshift-rule-to-database") != nil {
		t.Error("to-database is an egress rule to cidrs, an EgressFirewall rule, want no NetworkPolicy")
	}
	t.Run("names", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			rules := projects["payments-api"].(map[string]interface{})["network"].(map[string]interface{})["rules"].(map[string]interface{})
			rules["from-frontend"].(map[string]interface{})["from"] = map[string]interface{}{
				"namespaces": map[string]interface{}{"names": []interface{}{"web", "web-canary"}},
			}
		})
		np := networkPolicyNamed(onboardingObjects(t, res), "payments-api", "autoshift-rule-from-frontend")
		if np == nil {
			t.Fatal("no autoshift-rule-from-frontend")
		}
		want := `[{"from":[{"namespaceSelector":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["web","web-canary"]}]}}],"ports":[{"port":8443,"protocol":"TCP"}]}]`
		if got := asJSON(npRules(np, "ingress")); got != want {
			t.Errorf("ingress = %s, want %s", got, want)
		}
	})
}

// strictProfile gives the project the strict profile (dns egress allowed) without rule overrides.
func strictProfile(project string) func(projects map[string]interface{}) {
	return func(projects map[string]interface{}) {
		projects[project].(map[string]interface{})["networkProfile"] = "strict"
	}
}

// Project rules override profile rules by name: payments-api disables the strict profile's dns
// rule, so it gets no autoshift-rule-dns, while another strict project keeps it.
func TestProjectOnboarding_ProfileOverrideByName(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, strictProfile("reporting"))
	objs := onboardingObjects(t, res)
	if np := networkPolicyNamed(objs, "payments-api", "autoshift-rule-dns"); np != nil {
		t.Errorf("payments-api sets dns: {enabled: false}, want no autoshift-rule-dns, got %v", np.obj)
	}
	dns := networkPolicyNamed(objs, "reporting", "autoshift-rule-dns")
	if dns == nil {
		t.Fatalf("reporting is strict without overrides, want autoshift-rule-dns, got %v", networkPoliciesIn(objs, "reporting"))
	}
	checkOwnedExact(t, dns, "reporting", "enforce")
	want := `{"egress":[{"ports":[{"port":5353,"protocol":"UDP"},{"port":5353,"protocol":"TCP"}],"to":[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"openshift-dns"}}}]}],"podSelector":{},"policyTypes":["Egress"]}`
	if got := asJSON(npSpec(dns)); got != want {
		t.Errorf("autoshift-rule-dns spec = %s, want %s", got, want)
	}
	if n := networkPoliciesIn(objs, "reporting"); len(n) != 3 {
		t.Errorf("reporting: want the two default denies and autoshift-rule-dns, got %v", n)
	}

	// Same name, deep merge: the project's fields replace the profile's, and a peer set to false
	// drops the inherited one.
	t.Run("deep merge", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			r := projects["reporting"].(map[string]interface{})
			r["networkProfile"] = "permissive"
			r["network"] = map[string]interface{}{"rules": map[string]interface{}{
				"router": map[string]interface{}{"from": map[string]interface{}{
					"router": false, "namespaces": map[string]interface{}{"names": []interface{}{"openshift-ingress"}},
				}, "ports": []interface{}{8080}},
			}}
		})
		np := networkPolicyNamed(onboardingObjects(t, res), "reporting", "autoshift-rule-router")
		if np == nil {
			t.Fatal("no autoshift-rule-router")
		}
		want := `[{"from":[{"namespaceSelector":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["openshift-ingress"]}]}}],"ports":[{"port":8080,"protocol":"TCP"}]}]`
		if got := asJSON(npRules(np, "ingress")); got != want {
			t.Errorf("ingress = %s, want %s", got, want)
		}
	})
	// A project can also switch a profile's default deny off by name.
	t.Run("defaultDeny override", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			r := projects["reporting"].(map[string]interface{})
			r["networkProfile"] = "strict"
			r["network"] = map[string]interface{}{"defaultDeny": map[string]interface{}{"egress": false}}
		})
		objs := onboardingObjects(t, res)
		if networkPolicyNamed(objs, "reporting", "autoshift-default-deny-egress") != nil {
			t.Error("reporting sets defaultDeny.egress: false, want no autoshift-default-deny-egress")
		}
		if networkPolicyNamed(objs, "reporting", "autoshift-default-deny-ingress") == nil {
			t.Error("reporting keeps the profile's defaultDeny.ingress, want autoshift-default-deny-ingress")
		}
	})
}

// The peer shortcuts of the permissive profile, on OpenShift 4.22.
func TestProjectOnboarding_PeerShortcuts(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projects["reporting"].(map[string]interface{})["networkProfile"] = "permissive"
	})
	objs := onboardingObjects(t, res)
	want := map[string]string{
		"autoshift-rule-same-namespace": `[{"from":[{"podSelector":{}}]}]`,
		"autoshift-rule-router":         `[{"from":[{"namespaceSelector":{"matchLabels":{"network.openshift.io/policy-group":"ingress"}}}]}]`,
		"autoshift-rule-monitoring":     `[{"from":[{"namespaceSelector":{"matchLabels":{"network.openshift.io/policy-group":"monitoring"}}}]}]`,
	}
	for name, ingress := range want {
		np := networkPolicyNamed(objs, "reporting", name)
		if np == nil {
			t.Errorf("reporting is permissive, want %s", name)
			continue
		}
		if got := asJSON(npRules(np, "ingress")); got != ingress {
			t.Errorf("%s ingress = %s, want %s", name, got, ingress)
		}
	}
	if networkPolicyNamed(objs, "reporting", "autoshift-default-deny-egress") != nil {
		t.Error("permissive has defaultDeny.egress false, want no autoshift-default-deny-egress")
	}
	if networkPolicyNamed(objs, "reporting", "autoshift-default-deny-ingress") == nil {
		t.Error("permissive has defaultDeny.ingress true, want autoshift-default-deny-ingress")
	}
}

// A port without a protocol is TCP; an explicit protocol is kept.
func TestProjectOnboarding_DefaultProtocolTCP(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		rules := projects["payments-api"].(map[string]interface{})["network"].(map[string]interface{})["rules"].(map[string]interface{})
		rules["from-frontend"].(map[string]interface{})["ports"] = []interface{}{
			8443, "9443", map[string]interface{}{"port": 9090}, map[string]interface{}{"port": 53, "protocol": "UDP"}, "metrics",
		}
	})
	np := networkPolicyNamed(onboardingObjects(t, res), "payments-api", "autoshift-rule-from-frontend")
	if np == nil {
		t.Fatal("no autoshift-rule-from-frontend")
	}
	want := `[{"port":8443,"protocol":"TCP"},{"port":9443,"protocol":"TCP"},{"port":9090,"protocol":"TCP"},{"port":53,"protocol":"UDP"},{"port":"metrics","protocol":"TCP"}]`
	if got := asJSON(npRules(np, "ingress")[0]["ports"]); got != want {
		t.Errorf("ports = %s, want %s", got, want)
	}
}

// A networkProfile that projectDefaults.networkProfiles does not define is a required input: the
// project is skipped whole and reported, the others still render, and nothing of it is pruned.
func TestProjectOnboarding_UndefinedProfile(t *testing.T) {
	t.Parallel()
	res := runPrune(t, func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["networkProfile"] = "huge"
	})
	objs := onboardingObjects(t, res)
	want := "networkProfile huge is not defined in projectDefaults.networkProfiles"
	if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
		t.Errorf("want only status[payments-api] = %q, got %v", want, status)
	}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if (meta["name"] == "payments-api" || meta["namespace"] == "payments-api") && !isStatus(o) {
			t.Errorf("payments-api is skipped, want nothing rendered for it, got %s %v (%s)", o.obj["kind"], meta["name"], o.complianceType)
		}
	}
	otherProjectsRender(t, objs, "payments-api")
}

// A rule that cannot be rendered fails its own project loudly: the project is skipped whole and
// reported (rendering the rest could leave it more open than declared), and the others render.
func TestProjectOnboarding_RuleInvalid(t *testing.T) {
	t.Parallel()
	rule := func(r interface{}) func(cfg map[string]interface{}) {
		return func(cfg map[string]interface{}) {
			p := cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})
			p["network"].(map[string]interface{})["rules"].(map[string]interface{})["bad"] = r
		}
	}
	in := func(from map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"direction": "ingress", "action": "allow", "from": from}
	}
	ns := map[string]interface{}{"sameNamespace": true}
	cases := map[string]struct {
		set  func(cfg map[string]interface{})
		want string
	}{
		"not a map":     {rule("allow-all"), "rule bad is not a map"},
		"bad direction": {rule(map[string]interface{}{"direction": "inbound", "action": "allow", "from": ns}), "rule bad: direction inbound is not ingress or egress"},
		"bad action":    {rule(map[string]interface{}{"direction": "ingress", "from": ns}), "rule bad: action is not set"},
		"no peers":      {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "ports": []interface{}{80}}), "rule bad: an ingress rule needs from"},
		"all peers off": {rule(in(map[string]interface{}{"router": false})), "rule bad has no peers"},
		"wrong side":    {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "from": ns, "to": ns}), "rule bad: an ingress rule takes from, not to"},
		"unknown key": {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "from": ns, "port": []interface{}{80}}),
			"rule bad: key port is not one of direction, action, from, to, ports, enabled, description"},
		"unknown peer":     {rule(in(map[string]interface{}{"everyone": true})), "rule bad: unknown peer everyone"},
		"peer not bool":    {rule(in(map[string]interface{}{"router": "yes"})), "rule bad: peer router is not true or false"},
		"dns ingress":      {rule(in(map[string]interface{}{"dns": true})), "rule bad: dns is an egress-only peer"},
		"dnsNames ingress": {rule(in(map[string]interface{}{"dnsNames": []interface{}{"example.com"}})), "rule bad: dnsNames is an egress-only peer"},
		"empty namespaces": {rule(in(map[string]interface{}{"namespaces": map[string]interface{}{}})), "rule bad: namespaces needs labels or names"},
		"bad selector": {rule(in(map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"team": "a b"}}})),
			"rule bad: label team=a b is not a valid Kubernetes label"},
		"bad namespace name": {rule(in(map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"Web"}}})),
			"rule bad: namespace Web is not a valid DNS-1123 label"},
		"bad cidr": {rule(in(map[string]interface{}{"cidrs": []interface{}{"10.0.0.0/33"}})), "rule bad: cidr 10.0.0.0/33 is not a valid CIDR"},
		"bad port": {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "from": ns, "ports": []interface{}{70000}}),
			"rule bad: port 70000 is not a port number or name"},
		"bad protocol": {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "from": ns,
			"ports": []interface{}{map[string]interface{}{"port": 80, "protocol": "tcp"}}}), "rule bad: protocol tcp is not TCP, UDP or SCTP"},
		"ports not a list": {rule(map[string]interface{}{"direction": "ingress", "action": "allow", "from": ns, "ports": 80}), "rule bad: ports is not a list"},
		"ingress deny cidrs": {rule(map[string]interface{}{"direction": "ingress", "action": "deny", "from": map[string]interface{}{"cidrs": []interface{}{"10.99.0.0/24"}}}),
			"rule bad: " + ingressExternalDenyMsg},
		"egress named port": {rule(map[string]interface{}{"direction": "egress", "action": "allow", "to": map[string]interface{}{"cidrs": []interface{}{"10.99.0.0/24"}}, "ports": []interface{}{"https"}}),
			"rule bad: named port https cannot be used with cidrs or dnsNames; an EgressFirewall takes port numbers"},
		"bad ipv6 cidr":   {rule(in(map[string]interface{}{"cidrs": []interface{}{"fd01::1::2/64"}})), "rule bad: cidr fd01::1::2/64 is not a valid CIDR"},
		"short ipv6 cidr": {rule(in(map[string]interface{}{"cidrs": []interface{}{"fd01:1:2/64"}})), "rule bad: cidr fd01:1:2/64 is not a valid CIDR"},
		"bad rule name": {func(cfg map[string]interface{}) {
			p := cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})
			p["network"].(map[string]interface{})["rules"].(map[string]interface{})["Bad_Rule"] = in(ns)
		}, "rule Bad_Rule: name is not a valid DNS-1123 label"},
		"network not a map": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["network"] = "open"
		}, "network is not a map"},
		"rules not a map": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["network"] = map[string]interface{}{"rules": []interface{}{"x"}}
		}, "network.rules is not a map"},
		"defaultDeny not bool": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["network"].(map[string]interface{})["defaultDeny"] = map[string]interface{}{"egress": "off"}
		}, "network.defaultDeny.egress is not true or false"},
		"profile not a map": {func(cfg map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"] = "deny"
		}, "projectDefaults.networkProfiles.strict is not a map"},
		"profile not a string": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["networkProfile"] = []interface{}{"strict"}
		}, "networkProfile is not a string"},
		"profile false": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["networkProfile"] = false
		}, "networkProfile is not a string"},
		"profile zero": {func(cfg map[string]interface{}) {
			cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{})["networkProfile"] = 0
		}, "networkProfile is not a string"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := runOnboardingLabels(t, pruneOn, c.set)
			objs := onboardingObjects(t, res)
			if status := onboardingStatus(t, objs); status["payments-api"] != c.want || len(status) != 1 {
				t.Errorf("want only status[payments-api] = %q, got %v", c.want, status)
			}
			if namespaceNamed(objs, "payments-api") != nil || len(networkPoliciesIn(objs, "payments-api")) != 0 {
				t.Error("payments-api is skipped, want no Namespace and no NetworkPolicy")
			}
			if n := len(pruned(objs, "NetworkPolicy", "payments-api", "")) + len(pruned(objs, "RoleBinding", "payments-api", "")); n != 0 {
				t.Errorf("payments-api is in error, want nothing of it pruned, got %d mustnothave", n)
			}
			otherProjectsRender(t, objs, "payments-api")
		})
	}
}

// NetworkPolicies follow the project's mode, like every other project object.
func TestProjectOnboarding_NetworkFollowsMode(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, runOnboarding(t, strictProfile("legacy-app")))
	names := networkPoliciesIn(objs, "legacy-app")
	if len(names) != 3 {
		t.Fatalf("legacy-app is strict, want three NetworkPolicies, got %v", names)
	}
	for _, name := range names {
		checkOwnedExact(t, networkPolicyNamed(objs, "legacy-app", name), "legacy-app", "inform")
	}
}

// NetworkPolicy joins the prune sweep: an owned policy no longer declared (a removed rule, a removed
// project) is pruned with prune on and reported with prune off. Declared ones never are.
func TestProjectOnboarding_PruneNetworkPolicy(t *testing.T) {
	t.Parallel()
	on := onboardingObjects(t, onboardingPruneBase(t))
	off := onboardingObjects(t, onboardingBase(t))
	for _, c := range []struct{ ns, name string }{{"payments-api", "autoshift-rule-old"}, {"removed-project", "autoshift-default-deny-ingress"}} {
		if p := pruned(on, "NetworkPolicy", c.ns, c.name); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("prune on: want %s/%s pruned by enforce, got %v", c.ns, c.name, p)
		}
		if p := pruned(off, "NetworkPolicy", c.ns, c.name); len(p) != 1 || p[0].remediationAction != "inform" {
			t.Errorf("prune off: want %s/%s reported by inform, got %v", c.ns, c.name, p)
		}
	}
	for _, name := range []string{"autoshift-rule-from-frontend", "autoshift-default-deny-ingress", "autoshift-default-deny-egress"} {
		if p := pruned(on, "NetworkPolicy", "payments-api", name); len(p) != 0 {
			t.Errorf("payments-api declares %s, want no mustnothave, got %d", name, len(p))
		}
	}
	// A disabled rule is no longer declared, so its live policy is pruned.
	t.Run("rule disabled", func(t *testing.T) {
		res := runPruneAllShards(t, func(projects map[string]interface{}) {
			rules := projects["payments-api"].(map[string]interface{})["network"].(map[string]interface{})["rules"].(map[string]interface{})
			rules["from-frontend"] = map[string]interface{}{"enabled": false}
		})
		objs := onboardingObjects(t, res)
		if networkPolicyNamed(objs, "payments-api", "autoshift-rule-from-frontend") != nil {
			t.Error("from-frontend is disabled, want no autoshift-rule-from-frontend")
		}
		if p := pruned(objs, "NetworkPolicy", "payments-api", "autoshift-rule-from-frontend"); len(p) != 1 {
			t.Errorf("want the disabled rule's live policy pruned, got %v", p)
		}
	})
}

// onboardingEgressFirewallStubs is the testdata file holding the EgressFirewall CRD and the owned
// EgressFirewalls of an earlier run. testdataWithout drops it to model a cluster without the CRD.
const onboardingEgressFirewallStubs = "project-onboarding-egressfirewall.yaml"

// ingressExternalDenyMsg is the error for an ingress deny from cidrs, which no object can express.
const ingressExternalDenyMsg = "cannot deny inbound from external CIDRs; use Route allowlists (the haproxy.router.openshift.io/ip_whitelist annotation on the Route)"

// egressFirewallIn returns the rendered (not mustnothave) EgressFirewall default of namespace, or nil.
func egressFirewallIn(objs []onboardingObject, namespace string) *onboardingObject {
	return objectNamed(objs, "EgressFirewall", namespace, "default")
}

// efEgress returns spec.egress of an EgressFirewall as compact JSON.
func efEgress(o *onboardingObject) string {
	spec, _ := o.obj["spec"].(map[string]interface{})
	return asJSON(spec["egress"])
}

// projectRules returns network.rules of project, creating network and rules when absent.
func projectRules(projects map[string]interface{}, project string) map[string]interface{} {
	p := projects[project].(map[string]interface{})
	network, ok := p["network"].(map[string]interface{})
	if !ok {
		network = map[string]interface{}{}
		p["network"] = network
	}
	rules, ok := network["rules"].(map[string]interface{})
	if !ok {
		rules = map[string]interface{}{}
		network["rules"] = rules
	}
	return rules
}

func egressTo(action string, to map[string]interface{}, ports ...interface{}) map[string]interface{} {
	r := map[string]interface{}{"direction": "egress", "action": action, "to": to}
	if len(ports) > 0 {
		r["ports"] = ports
	}
	return r
}

func cidrs(c ...interface{}) map[string]interface{} { return map[string]interface{}{"cidrs": c} }

const (
	efDeny4 = `{"to":{"cidrSelector":"0.0.0.0/0"},"type":"Deny"}`
	efDeny6 = `{"to":{"cidrSelector":"::/0"},"type":"Deny"}`
	efToDB  = `{"ports":[{"port":5432,"protocol":"TCP"}],"to":{"cidrSelector":"10.20.30.0/24"},"type":"Allow"}`
)

// payments-api is strict and allows egress to 10.20.30.0/24 on 5432: one EgressFirewall default
// with that Allow before the final Deny 0.0.0.0/0. Project rules come first in rule-name order, then
// profile rules in rule-name order, then the final deny.
func TestProjectOnboarding_EgressFirewallOrder(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	ef := egressFirewallIn(objs, "payments-api")
	if ef == nil {
		t.Fatal("payments-api has an egress cidrs rule and defaultDeny.egress, want EgressFirewall default")
	}
	checkOwnedExact(t, ef, "payments-api", "enforce")
	if ef.obj["apiVersion"] != "k8s.ovn.org/v1" {
		t.Errorf("EgressFirewall apiVersion = %v, want k8s.ovn.org/v1", ef.obj["apiVersion"])
	}
	if got, want := asJSON(ef.obj["spec"]), `{"egress":[`+efToDB+`,`+efDeny4+`]}`; got != want {
		t.Errorf("payments-api EgressFirewall spec = %s, want %s", got, want)
	}
	if o := egressFirewallIn(objs, "reporting"); o != nil {
		t.Errorf("reporting has no network rules, want no EgressFirewall, got %v", o.obj)
	}

	t.Run("project before profile", func(t *testing.T) {
		res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
			strict := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"].(map[string]interface{})
			pr := strict["rules"].(map[string]interface{})
			pr["aaa-registry"] = egressTo("allow", map[string]interface{}{"dnsNames": []interface{}{"registry.example.com"}})
			pr["shared"] = egressTo("allow", cidrs("10.40.0.0/16"))
			projects := cfg["projects"].(map[string]interface{})
			rules := projectRules(projects, "payments-api")
			rules["block-db-replica"] = egressTo("deny", cidrs("10.20.30.99/32", "10.20.31.0/24"), map[string]interface{}{"port": 5432})
			rules["shared"] = map[string]interface{}{"to": cidrs("10.41.0.0/16")}
			projects["reporting"].(map[string]interface{})["networkProfile"] = "strict"
		})
		objs := onboardingObjects(t, res)
		ef := egressFirewallIn(objs, "payments-api")
		if ef == nil {
			t.Fatal("no payments-api EgressFirewall")
		}
		want := `[` +
			`{"ports":[{"port":5432,"protocol":"TCP"}],"to":{"cidrSelector":"10.20.30.99/32"},"type":"Deny"},` +
			`{"ports":[{"port":5432,"protocol":"TCP"}],"to":{"cidrSelector":"10.20.31.0/24"},"type":"Deny"},` +
			`{"to":{"cidrSelector":"10.41.0.0/16"},"type":"Allow"},` +
			efToDB + `,` +
			`{"to":{"dnsName":"registry.example.com"},"type":"Allow"},` +
			efDeny4 + `]`
		if got := efEgress(ef); got != want {
			t.Errorf("payments-api egress =\n%s\nwant (project rules by name, then profile rules by name, then the deny)\n%s", got, want)
		}
		rep := egressFirewallIn(objs, "reporting")
		if rep == nil {
			t.Fatal("reporting is strict, want an EgressFirewall")
		}
		want = `[{"to":{"dnsName":"registry.example.com"},"type":"Allow"},{"to":{"cidrSelector":"10.40.0.0/16"},"type":"Allow"},` + efDeny4 + `]`
		if got := efEgress(rep); got != want {
			t.Errorf("reporting egress = %s, want %s", got, want)
		}
	})
	t.Run("dual stack", func(t *testing.T) {
		res := runOnboardingIn(t, testdataWithout(t, onboardingNetworkStubs, dualStackNetwork), nil, nil)
		ef := egressFirewallIn(onboardingObjects(t, res), "payments-api")
		if ef == nil || efEgress(ef) != `[`+efToDB+`,`+efDeny4+`,`+efDeny6+`]` {
			t.Errorf("dual stack: want the final deny for both families, got %v", ef)
		}
	})
	// Unknown CIDRs leave the stack unknown, so both final denies are rendered. An allow to cidrs is
	// still safe: it never opens what a deny closes.
	t.Run("CIDRs unknown", func(t *testing.T) {
		res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), nil, func(c *ExampleConfigs) {
			c.ClusterInstallConfig["networking"] = map[string]interface{}{"clusterNetwork": []interface{}{}, "serviceNetwork": []interface{}{}}
		})
		ef := egressFirewallIn(onboardingObjects(t, res), "payments-api")
		if ef == nil || efEgress(ef) != `[`+efToDB+`,`+efDeny4+`,`+efDeny6+`]` {
			t.Errorf("CIDRs unknown: want both final denies, got %v", ef)
		}
	})
	// Without defaultDeny.egress there is no final deny, and an egress deny still renders.
	t.Run("no default deny", func(t *testing.T) {
		res := runOnboarding(t, func(projects map[string]interface{}) {
			p := projects["payments-api"].(map[string]interface{})
			p["network"].(map[string]interface{})["defaultDeny"] = map[string]interface{}{"egress": false}
			projectRules(projects, "payments-api")["to-database"].(map[string]interface{})["action"] = "deny"
		})
		ef := egressFirewallIn(onboardingObjects(t, res), "payments-api")
		want := `[{"ports":[{"port":5432,"protocol":"TCP"}],"to":{"cidrSelector":"10.20.30.0/24"},"type":"Deny"}]`
		if ef == nil || efEgress(ef) != want {
			t.Errorf("want only the deny rule, got %v", ef)
		}
	})
	t.Run("follows mode", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboarding(t, strictProfile("legacy-app")))
		ef := egressFirewallIn(objs, "legacy-app")
		if ef == nil {
			t.Fatal("legacy-app is strict, want an EgressFirewall")
		}
		checkOwnedExact(t, ef, "legacy-app", "inform")
		if got := efEgress(ef); got != `[`+efDeny4+`]` {
			t.Errorf("legacy-app egress = %s, want only the final deny", got)
		}
	})
}

// dualStackNetwork is a Network config stub with an IPv6 cluster and service network.
const dualStackNetwork = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: networks.config.openshift.io
spec:
  group: config.openshift.io
  names: {kind: Network, plural: networks}
  scope: Cluster
---
apiVersion: config.openshift.io/v1
kind: Network
metadata:
  name: cluster
spec:
  clusterNetwork:
    - {cidr: 10.128.0.0/14, hostPrefix: 23}
    - {cidr: 'fd01::/48', hostPrefix: 64}
  serviceNetwork: [172.30.0.0/16, 'fd02::/112']
`

// An egress rule to dnsNames is an EgressFirewall dnsName entry, one per name, with the rule's ports.
func TestProjectOnboarding_EgressDnsName(t *testing.T) {
	t.Parallel()
	res := runOnboarding(t, func(projects map[string]interface{}) {
		projectRules(projects, "payments-api")["to-api"] = egressTo("allow", map[string]interface{}{"dnsNames": []interface{}{"api.example.com"}}, 443)
	})
	objs := onboardingObjects(t, res)
	ef := egressFirewallIn(objs, "payments-api")
	if ef == nil {
		t.Fatal("no payments-api EgressFirewall")
	}
	want := `[{"ports":[{"port":443,"protocol":"TCP"}],"to":{"dnsName":"api.example.com"},"type":"Allow"},` + efToDB + `,` + efDeny4 + `]`
	if got := efEgress(ef); got != want {
		t.Errorf("egress = %s, want %s", got, want)
	}
	if networkPolicyNamed(objs, "payments-api", "autoshift-rule-to-api") != nil {
		t.Error("to-api is an egress rule to dnsNames, want no NetworkPolicy")
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}
}

// skippedWhole asserts project rendered nothing and had nothing pruned, and the others rendered.
func skippedWhole(t *testing.T, objs []onboardingObject, project string) {
	t.Helper()
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if (meta["name"] == project || meta["namespace"] == project) && !isStatus(o) {
			t.Errorf("%s is skipped, want nothing rendered or pruned for it, got %s %v (%s)", project, o.obj["kind"], meta["name"], o.complianceType)
		}
	}
	otherProjectsRender(t, objs, project)
}

// No object can deny inbound traffic from external CIDRs: Route traffic arrives from the router,
// and the EgressFirewall is egress only. The project is reported with a pointer to Route allowlists.
func TestProjectOnboarding_IngressExternalDeny(t *testing.T) {
	t.Parallel()
	res := runPrune(t, func(projects map[string]interface{}) {
		projectRules(projects, "payments-api")["block-scanner"] = map[string]interface{}{
			"direction": "ingress", "action": "deny", "from": cidrs("10.99.0.0/24"),
		}
	})
	objs := onboardingObjects(t, res)
	want := "rule block-scanner: " + ingressExternalDenyMsg
	if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
		t.Errorf("want only status[payments-api] = %q, got %v", want, status)
	}
	skippedWhole(t, objs, "payments-api")
}

// OVN-Kubernetes never matches pod network destinations in an EgressFirewall, and service IPs are
// matched after DNAT. An egress allow inside the cluster or service network therefore cannot be
// enforced, and a deny overlapping one would silently not apply to its in-cluster part: both are
// errors of the project, which is skipped whole. A wider allow renders (the EgressFirewall applies
// only its external part). Node IPs are external.
func TestProjectOnboarding_EgressCidrInCluster(t *testing.T) {
	t.Parallel()
	td := filepath.Join(repoRoot(t), "tools", "testdata")
	const inside = "; use namespaces or pods peers for in-cluster destinations"
	const overlaps = ", which an EgressFirewall does not match, so the deny may not apply; use namespaces or pods peers for in-cluster destinations"
	cases := map[string]struct {
		testdata string
		action   string
		cidr     string
		want     string
	}{
		"pod network allow":            {td, "allow", "10.128.4.0/24", "lies inside the cluster or service network 10.128.0.0/14" + inside},
		"pod network equal allow":      {td, "allow", "10.128.0.0/14", "lies inside the cluster or service network 10.128.0.0/14" + inside},
		"service network allow":        {td, "allow", "172.30.0.10/32", "lies inside the cluster or service network 172.30.0.0/16" + inside},
		"service network deny":         {td, "deny", "172.30.0.10/32", "overlaps the cluster or service network 172.30.0.0/16" + overlaps},
		"supernet deny":                {td, "deny", "10.0.0.0/8", "overlaps the cluster or service network 10.128.0.0/14" + overlaps},
		"ipv6 inside dual stack allow": {"dual", "allow", "fd01:0:0:5::/64", "lies inside the cluster or service network fd01::/48" + inside},
		"ipv6 inside dual stack deny":  {"dual", "deny", "fd01:0:0:5::/64", "overlaps the cluster or service network fd01::/48" + overlaps},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := c.testdata
			if dir == "dual" {
				dir = testdataWithout(t, onboardingNetworkStubs, dualStackNetwork)
			}
			res := runOnboardingIn(t, dir, pruneOn, func(cfg map[string]interface{}) {
				r := projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["to-database"].(map[string]interface{})
				r["action"] = c.action
				r["to"] = cidrs(c.cidr)
			})
			objs := onboardingObjects(t, res)
			want := "rule to-database: cidr " + c.cidr + " " + c.want
			if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
				t.Errorf("want only status[payments-api] = %q, got %v", want, status)
			}
			skippedWhole(t, objs, "payments-api")
		})
	}
	// Outside every cluster network: node IPs (the machine network) and an IPv6 range next to the
	// cluster's. Wider than a cluster network: an allow of a supernet or of everything. All render as
	// declared.
	for name, c := range map[string]struct{ testdata, cidr string }{
		"node IP":             {td, "192.0.2.11/32"},
		"ipv6 next to /48":    {"dual", "fd01:1::/64"},
		"supernet allow":      {td, "10.0.0.0/8"},
		"all IPv4 allow":      {td, "0.0.0.0/0"},
		"ipv6 supernet allow": {"dual", "fd00::/8"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := c.testdata
			if dir == "dual" {
				dir = testdataWithout(t, onboardingNetworkStubs, dualStackNetwork)
			}
			res := runOnboardingIn(t, dir, nil, func(cfg map[string]interface{}) {
				projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["to-database"].(map[string]interface{})["to"] = cidrs(c.cidr)
			})
			objs := onboardingObjects(t, res)
			if status := onboardingStatus(t, objs); len(status) != 0 {
				t.Errorf("want no status entries, got %v", status)
			}
			ef := egressFirewallIn(objs, "payments-api")
			if ef == nil || !strings.HasPrefix(efEgress(ef), `[{"ports":[{"port":5432,"protocol":"TCP"}],"to":{"cidrSelector":"`+c.cidr+`"},"type":"Allow"}`) {
				t.Errorf("want the Allow to %s rendered, got %v", c.cidr, ef)
			}
		})
	}
	// With the cluster networks unknown, an overlap cannot be checked. A deny to cidrs could then
	// silently fail to apply, so it is an error; an allow never opens what a deny closes, so it renders
	// (TestProjectOnboarding_EgressFirewallOrder/CIDRs unknown).
	t.Run("CIDRs unknown deny", func(t *testing.T) {
		res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), pruneOn, func(c *ExampleConfigs) {
			c.ClusterInstallConfig["networking"] = map[string]interface{}{"clusterNetwork": []interface{}{}, "serviceNetwork": []interface{}{}}
			projectRules(c.HubConfig["projects"].(map[string]interface{}), "payments-api")["to-database"].(map[string]interface{})["action"] = "deny"
		})
		objs := onboardingObjects(t, res)
		want := "rule to-database: cannot check cidr 10.20.30.0/24 against the cluster and service networks, whose CIDRs cannot be read, so the deny may not apply"
		if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
			t.Errorf("want only status[payments-api] = %q, got %v", want, status)
		}
		skippedWhole(t, objs, "payments-api")
	})
}

// Without the EgressFirewall CRD, a project that needs one is reported and skipped whole rather than
// left open externally; the prune sweep skips the kind instead of failing the template.
func TestProjectOnboarding_EgressFirewallCRDMissing(t *testing.T) {
	t.Parallel()
	res := runOnboardingIn(t, testdataWithout(t, onboardingEgressFirewallStubs, ""), pruneOn, nil)
	objs := onboardingObjects(t, res)
	want := "the EgressFirewall CRD (egressfirewalls.k8s.ovn.org) is not installed, so the external egress rules and default deny cannot be enforced"
	if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
		t.Errorf("want only status[payments-api] = %q, got %v", want, status)
	}
	skippedWhole(t, objs, "payments-api")
	for _, o := range objs {
		if o.obj["kind"] == "EgressFirewall" {
			t.Errorf("no CRD, want no EgressFirewall entry, got %v (%s)", o.obj["metadata"], o.complianceType)
		}
	}
}

// OVN-Kubernetes allows at most 8000 rules in an EgressFirewall. The managed cluster counts the final
// denies it renders: one on a single-stack cluster, two on dual stack. The entries are DNS names
// without ports, the smallest entries: 8000 CIDR entries with ports make a hub-resolved Policy of
// about 2.9 MB, over the etcd request limit, so the size guard skips such a project (last case).
func TestProjectOnboarding_EgressFirewallLimit(t *testing.T) {
	t.Parallel()
	many := func(n int) func(cfg map[string]interface{}) {
		return func(cfg map[string]interface{}) {
			list := make([]interface{}, n)
			for i := range list {
				list[i] = fmt.Sprintf("h%d.example.com", i)
			}
			rule := projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["to-database"].(map[string]interface{})
			rule["to"] = map[string]interface{}{"dnsNames": list}
			delete(rule, "ports")
		}
	}
	td := filepath.Join(repoRoot(t), "tools", "testdata")
	t.Run("8000 cidrs exceed the size limit", func(t *testing.T) {
		if os.Getenv("STRESS_SCALE") == "" {
			// The per-project size guard, which skips this project, is covered in every run by
			// TestStress_Scale_RealisticSizeGuard and TestProjectOnboarding_SizeGuardPerShard: the
			// project that crosses the limit and every later one are not rendered and are listed
			// under project-onboarding.size.
			t.Skip("set STRESS_SCALE=1; covered in every run by TestStress_Scale_RealisticSizeGuard")
		}
		objs := onboardingObjects(t, runOnboardingIn(t, td, nil, func(cfg map[string]interface{}) {
			list := make([]interface{}, 7999)
			for i := range list {
				list[i] = fmt.Sprintf("10.64.%d.%d/32", i/256, i%256)
			}
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["to-database"].(map[string]interface{})["to"] = cidrs(list...)
		}))
		status := onboardingStatus(t, objs)
		if !strings.Contains(status["project-onboarding.size"], "over the 1048576 bytes") || !strings.Contains(status["payments-api"], "size limit") {
			t.Errorf("want payments-api skipped by the size guard, status %v", status)
		}
		if egressFirewallIn(objs, "payments-api") != nil {
			t.Error("payments-api is over the size limit, want nothing of it rendered")
		}
	})
	t.Run("at the limit", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingIn(t, td, nil, many(7999)))
		ef := egressFirewallIn(objs, "payments-api")
		if ef == nil {
			t.Fatal("7999 entries and the final deny fit, want an EgressFirewall")
		}
		if n := len(ef.obj["spec"].(map[string]interface{})["egress"].([]interface{})); n != 8000 {
			t.Errorf("want 7999 entries and the IPv4 final deny, got %d", n)
		}
	})
	over := func(t *testing.T, objs []onboardingObject, n int) {
		t.Helper()
		want := fmt.Sprintf("the EgressFirewall would need %d rules, more than the 8000 that OVN-Kubernetes allows", n)
		if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
			t.Errorf("want only status[payments-api] = %q, got %v", want, status)
		}
		skippedWhole(t, objs, "payments-api")
	}
	t.Run("over the limit", func(t *testing.T) {
		over(t, onboardingObjects(t, runOnboardingIn(t, td, pruneOn, many(8000))), 8001)
	})
	t.Run("dual stack counts both final denies", func(t *testing.T) {
		dir := testdataWithout(t, onboardingNetworkStubs, dualStackNetwork)
		over(t, onboardingObjects(t, runOnboardingIn(t, dir, pruneOn, many(7999))), 8001)
	})
}

// EgressFirewall joins the prune sweep: the removed project's is pruned with prune on and reported
// with prune off; a declared one never is; one a project no longer needs is pruned.
func TestProjectOnboarding_PruneEgressFirewall(t *testing.T) {
	t.Parallel()
	on := onboardingObjects(t, onboardingPruneBase(t))
	off := onboardingObjects(t, onboardingBase(t))
	if p := pruned(on, "EgressFirewall", "removed-project", "default"); len(p) != 1 || p[0].remediationAction != "enforce" {
		t.Errorf("prune on: want removed-project/default pruned by enforce, got %v", p)
	}
	if p := pruned(off, "EgressFirewall", "removed-project", "default"); len(p) != 1 || p[0].remediationAction != "inform" {
		t.Errorf("prune off: want removed-project/default reported by inform, got %v", p)
	}
	if p := pruned(on, "EgressFirewall", "payments-api", ""); len(p) != 0 {
		t.Errorf("payments-api declares its EgressFirewall, want it never pruned, got %v", p)
	}
	t.Run("no longer needed", func(t *testing.T) {
		res := runPrune(t, func(projects map[string]interface{}) {
			p := projects["payments-api"].(map[string]interface{})
			p["network"].(map[string]interface{})["defaultDeny"] = map[string]interface{}{"egress": false}
			projectRules(projects, "payments-api")["to-database"] = map[string]interface{}{"enabled": false}
		})
		objs := onboardingObjects(t, res)
		if o := egressFirewallIn(objs, "payments-api"); o != nil {
			t.Errorf("payments-api has no external egress rule or deny, want no EgressFirewall, got %v", o.obj)
		}
		if p := pruned(objs, "EgressFirewall", "payments-api", "default"); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("want the live EgressFirewall pruned by enforce, got %v", p)
		}
	})
}

// onboardingANPStubs is the testdata file holding the AdminNetworkPolicy CRD and the owned
// AdminNetworkPolicy of a removed project. testdataWithout drops it to model a cluster without the CRD.
const onboardingANPStubs = "project-onboarding-anp.yaml"

// paymentsANPStubs replaces onboardingANPStubs with the CRD and a live AdminNetworkPolicy of
// payments-api from an earlier run.
const paymentsANPStubs = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: adminnetworkpolicies.policy.networking.k8s.io
spec:
  group: policy.networking.k8s.io
  names: {kind: AdminNetworkPolicy, plural: adminnetworkpolicies}
  scope: Cluster
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-project-payments-api
  labels:
    autoshift.io/project: payments-api
    autoshift.io/managed-by: project-onboarding
spec:
  priority: 50
  subject:
    namespaces:
      matchLabels:
        autoshift.io/project: payments-api
`

// anpNamed returns the rendered (not mustnothave) AdminNetworkPolicy name, or nil. It is cluster
// scoped, so it must carry no namespace.
func anpNamed(objs []onboardingObject, name string) *onboardingObject {
	for i, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "AdminNetworkPolicy" && meta["name"] == name && o.complianceType != "mustnothave" {
			return &objs[i]
		}
	}
	return nil
}

// anpPruned returns the mustnothave entries for the AdminNetworkPolicy name (any name when empty).
func anpPruned(t *testing.T, objs []onboardingObject, name string) []onboardingObject {
	t.Helper()
	var out []onboardingObject
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.complianceType != "mustnothave" || o.obj["kind"] != "AdminNetworkPolicy" {
			continue
		}
		if _, ok := meta["namespace"]; ok {
			t.Errorf("AdminNetworkPolicy %v is cluster scoped, want no namespace in its mustnothave, got %v", meta["name"], meta["namespace"])
		}
		if name == "" || meta["name"] == name {
			out = append(out, o)
		}
	}
	return out
}

// withProfileDeny adds an in-cluster deny to the strict profile: ingress from namespaces labelled
// example.com/team: web, the peer payments-api's from-frontend allows.
func withProfileDeny(cfg map[string]interface{}) {
	strict := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"].(map[string]interface{})
	strict["rules"].(map[string]interface{})["deny-web"] = map[string]interface{}{
		"direction": "ingress", "action": "deny",
		"from": map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"example.com/team": "web"}}},
	}
}

const (
	anpFromWeb   = `[{"namespaces":{"matchLabels":{"example.com/team":"web"}}}]`
	anpDenyWeb   = `{"action":"Deny","from":` + anpFromWeb + `,"name":"deny-web"}`
	anpAllowWeb  = `{"action":"Allow","from":` + anpFromWeb + `,"name":"from-frontend","ports":[{"portNumber":{"port":8443,"protocol":"TCP"}}]}`
	anpDnsDeny   = `{"action":"Deny","name":"block-dns","ports":[{"portNumber":{"port":5353,"protocol":"UDP"}},{"portNumber":{"port":5353,"protocol":"TCP"}}],"to":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"openshift-dns"}}}]}`
	anpSubjectOf = `"priority":50,"subject":{"namespaces":{"matchLabels":{"autoshift.io/project":"%s"}}}`
)

// An in-cluster deny renders the project's AdminNetworkPolicy autoshift-project-<name> at priority 50,
// selecting the project namespace by its autoshift.io/project label. A project allow that a profile
// deny in the same direction may cover is repeated there as Allow, before the profile's Deny, so the
// project rule wins. Its NetworkPolicy still renders.
func TestProjectOnboarding_ProfileDenyProjectAllow(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, runOnboardingConfig(t, withProfileDeny))
	anp := anpNamed(objs, "autoshift-project-payments-api")
	if anp == nil {
		t.Fatal("the strict profile has an in-cluster deny, want AdminNetworkPolicy autoshift-project-payments-api")
	}
	checkOwnedExact(t, anp, "payments-api", "enforce")
	if anp.obj["apiVersion"] != "policy.networking.k8s.io/v1alpha1" {
		t.Errorf("apiVersion = %v, want policy.networking.k8s.io/v1alpha1", anp.obj["apiVersion"])
	}
	if ns, ok := anp.obj["metadata"].(map[string]interface{})["namespace"]; ok {
		t.Errorf("AdminNetworkPolicy is cluster scoped, want no namespace, got %v", ns)
	}
	want := `{"ingress":[` + anpAllowWeb + `,` + anpDenyWeb + `],` + fmt.Sprintf(anpSubjectOf, "payments-api") + `}`
	if got := asJSON(anp.obj["spec"]); got != want {
		t.Errorf("spec =\n%s\nwant\n%s", got, want)
	}
	if networkPolicyNamed(objs, "payments-api", "autoshift-rule-from-frontend") == nil {
		t.Error("want autoshift-rule-from-frontend still rendered")
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}

	// Project denies first (rule-name order), then the project allows a profile deny may cover, then
	// the profile denies. Peers map to AdminNetworkPolicy peers: pods of the project namespace, a
	// namespace list, sameNamespace, and dns (openshift-dns on 5353, its own rule). Only a direction
	// with a profile deny repeats the project allows.
	t.Run("order and peers", func(t *testing.T) {
		res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			projects := cfg["projects"].(map[string]interface{})
			rules := projectRules(projects, "payments-api")
			rules["deny-batch"] = map[string]interface{}{"direction": "ingress", "action": "deny", "ports": []interface{}{9090},
				"from": map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"app": "batch"}}}}
			rules["block-dns"] = egressTo("deny", map[string]interface{}{"dns": true, "namespaces": map[string]interface{}{"names": []interface{}{"kube-system"}}})
			rules["to-cache"] = egressTo("allow", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"cache"}}})
			projects["reporting"].(map[string]interface{})["networkProfile"] = "strict"
			projectRules(projects, "reporting")["same-namespace"] = map[string]interface{}{"direction": "ingress", "action": "allow", "from": map[string]interface{}{"sameNamespace": true}}
		})
		objs := onboardingObjects(t, res)
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("want no status entries, got %v", status)
		}
		anp := anpNamed(objs, "autoshift-project-payments-api")
		if anp == nil {
			t.Fatal("no payments-api AdminNetworkPolicy")
		}
		want := `{"egress":[` +
			`{"action":"Deny","name":"block-dns","to":[{"namespaces":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["kube-system"]}]}}]},` +
			anpDnsDeny + `],` +
			`"ingress":[` +
			`{"action":"Deny","from":[{"pods":{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"payments-api"}},"podSelector":{"matchLabels":{"app":"batch"}}}}],"name":"deny-batch","ports":[{"portNumber":{"port":9090,"protocol":"TCP"}}]},` +
			anpAllowWeb + `,` + anpDenyWeb + `],` + fmt.Sprintf(anpSubjectOf, "payments-api") + `}`
		if got := asJSON(anp.obj["spec"]); got != want {
			t.Errorf("payments-api spec =\n%s\nwant\n%s", got, want)
		}
		rep := anpNamed(objs, "autoshift-project-reporting")
		if rep == nil {
			t.Fatal("reporting is strict, want its AdminNetworkPolicy")
		}
		want = `{"ingress":[{"action":"Allow","from":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"reporting"}}}],"name":"same-namespace"},` +
			anpDenyWeb + `],` + fmt.Sprintf(anpSubjectOf, "reporting") + `}`
		if got := asJSON(rep.obj["spec"]); got != want {
			t.Errorf("reporting spec =\n%s\nwant\n%s", got, want)
		}
	})
	// An AdminNetworkPolicy has no ingress IP peer, so an allow from cidrs stays a NetworkPolicy
	// ipBlock only; its named port is then no AdminNetworkPolicy error.
	t.Run("ingress cidrs not carried over", func(t *testing.T) {
		res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["from-office"] = map[string]interface{}{
				"direction": "ingress", "action": "allow", "from": cidrs("10.99.0.0/24"), "ports": []interface{}{"https"}}
		})
		objs := onboardingObjects(t, res)
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("want no status entries, got %v", status)
		}
		if anp := anpNamed(objs, "autoshift-project-payments-api"); anp == nil || asJSON(anp.obj["spec"]) != want {
			t.Errorf("want the AdminNetworkPolicy without from-office, got %v", anp)
		}
		if networkPolicyNamed(objs, "payments-api", "autoshift-rule-from-office") == nil {
			t.Error("want autoshift-rule-from-office rendered")
		}
	})
	t.Run("follows mode", func(t *testing.T) {
		res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			cfg["projects"].(map[string]interface{})["legacy-app"].(map[string]interface{})["networkProfile"] = "strict"
		})
		anp := anpNamed(onboardingObjects(t, res), "autoshift-project-legacy-app")
		if anp == nil {
			t.Fatal("legacy-app is strict, want its AdminNetworkPolicy")
		}
		checkOwnedExact(t, anp, "legacy-app", "inform")
	})
}

// A profile egress deny is overridden the same way: the project's in-cluster egress allows are
// repeated as Allow before the profile's Deny, and a dns peer becomes its own Allow to openshift-dns
// on 5353 without the rule's ports. The egress allow to cidrs only stays an EgressFirewall entry, and
// ingress, with no profile deny, gets no Allow.
func TestProjectOnboarding_ProfileEgressDenyProjectAllow(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		strict := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"].(map[string]interface{})
		strict["rules"].(map[string]interface{})["deny-kube"] = egressTo("deny", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"kube-system"}}})
		projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["to-cache"] = egressTo("allow",
			map[string]interface{}{"dns": true, "namespaces": map[string]interface{}{"names": []interface{}{"cache"}}}, 6379)
	})
	objs := onboardingObjects(t, res)
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}
	anp := anpNamed(objs, "autoshift-project-payments-api")
	if anp == nil {
		t.Fatal("the strict profile has an in-cluster egress deny, want AdminNetworkPolicy autoshift-project-payments-api")
	}
	want := `{"egress":[` +
		`{"action":"Allow","name":"to-cache","ports":[{"portNumber":{"port":6379,"protocol":"TCP"}}],"to":[{"namespaces":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["cache"]}]}}]},` +
		`{"action":"Allow","name":"to-cache","ports":[{"portNumber":{"port":5353,"protocol":"UDP"}},{"portNumber":{"port":5353,"protocol":"TCP"}}],"to":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"openshift-dns"}}}]},` +
		`{"action":"Deny","name":"deny-kube","to":[{"namespaces":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["kube-system"]}]}}]}],` +
		fmt.Sprintf(anpSubjectOf, "payments-api") + `}`
	if got := asJSON(anp.obj["spec"]); got != want {
		t.Errorf("spec =\n%s\nwant\n%s", got, want)
	}
	if networkPolicyNamed(objs, "payments-api", "autoshift-rule-to-cache") == nil {
		t.Error("want autoshift-rule-to-cache still rendered")
	}
}

// A project rule that overrides a profile deny by name counts as a project rule: it moves into the
// project-deny pass, ahead of the repeated project allows, and the profile pass keeps only the
// profile denies the project left alone.
func TestProjectOnboarding_ProjectOverridesProfileDeny(t *testing.T) {
	t.Parallel()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		withProfileDeny(cfg)
		strict := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"].(map[string]interface{})
		strict["rules"].(map[string]interface{})["deny-batch"] = map[string]interface{}{"direction": "ingress", "action": "deny",
			"from": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"batch"}}}}
		projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["deny-web"] = map[string]interface{}{"ports": []interface{}{9000}}
	})
	objs := onboardingObjects(t, res)
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}
	anp := anpNamed(objs, "autoshift-project-payments-api")
	if anp == nil {
		t.Fatal("no payments-api AdminNetworkPolicy")
	}
	want := `{"ingress":[` +
		`{"action":"Deny","from":` + anpFromWeb + `,"name":"deny-web","ports":[{"portNumber":{"port":9000,"protocol":"TCP"}}]},` +
		anpAllowWeb + `,` +
		`{"action":"Deny","from":[{"namespaces":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["batch"]}]}}],"name":"deny-batch"}],` +
		fmt.Sprintf(anpSubjectOf, "payments-api") + `}`
	if got := asJSON(anp.obj["spec"]); got != want {
		t.Errorf("spec =\n%s\nwant\n%s", got, want)
	}
	// The override changes only payments-api: reporting, also strict, keeps the profile's deny-web.
	t.Run("profile unchanged", func(t *testing.T) {
		res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			projects := cfg["projects"].(map[string]interface{})
			projects["reporting"].(map[string]interface{})["networkProfile"] = "strict"
			projectRules(projects, "payments-api")["deny-web"] = map[string]interface{}{"ports": []interface{}{9000}}
		})
		rep := anpNamed(onboardingObjects(t, res), "autoshift-project-reporting")
		if rep == nil {
			t.Fatal("reporting is strict, want its AdminNetworkPolicy")
		}
		want := `{"ingress":[` + anpDenyWeb + `],` + fmt.Sprintf(anpSubjectOf, "reporting") + `}`
		if got := asJSON(rep.obj["spec"]); got != want {
			t.Errorf("reporting spec =\n%s\nwant\n%s", got, want)
		}
	})
}

// Without an in-cluster deny no AdminNetworkPolicy is rendered: NetworkPolicy allows are enough, and an
// egress deny to cidrs is an EgressFirewall rule.
func TestProjectOnboarding_NoANPWithoutDeny(t *testing.T) {
	t.Parallel()
	for name, objs := range map[string][]onboardingObject{
		"baseline": onboardingObjects(t, onboardingBase(t)),
		"egress deny to cidrs": onboardingObjects(t, runOnboarding(t, func(projects map[string]interface{}) {
			projectRules(projects, "payments-api")["to-database"].(map[string]interface{})["action"] = "deny"
		})),
	} {
		t.Run(name, func(t *testing.T) {
			if anpNamed(objs, "autoshift-project-reporting") != nil {
				t.Error("reporting has no deny, want no AdminNetworkPolicy")
			}
			for _, o := range objs {
				name, _ := o.obj["metadata"].(map[string]interface{})["name"].(string)
				if o.obj["kind"] == "AdminNetworkPolicy" && o.complianceType != "mustnothave" && !strings.HasPrefix(name, "autoshift-admin-") {
					t.Errorf("no project has an in-cluster deny, want no project AdminNetworkPolicy, got %v", o.obj["metadata"])
				}
			}
		})
	}
}

// Rules an AdminNetworkPolicy cannot express as declared fail their project loudly: the project is
// skipped whole, and nothing of it is pruned.
func TestProjectOnboarding_ANPInvalid(t *testing.T) {
	t.Parallel()
	deny := func(from map[string]interface{}, ports ...interface{}) map[string]interface{} {
		r := map[string]interface{}{"direction": "ingress", "action": "deny", "from": from}
		if len(ports) > 0 {
			r["ports"] = ports
		}
		return r
	}
	sameNs := map[string]interface{}{"sameNamespace": true}
	manyPorts := make([]interface{}, 101)
	for i := range manyPorts {
		manyPorts[i] = 1000 + i
	}
	cases := map[string]struct {
		set  func(cfg map[string]interface{})
		want string
	}{
		"router deny": {func(cfg map[string]interface{}) {
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["bad"] = deny(map[string]interface{}{"router": true})
		}, "rule bad: cannot deny the router peer: router pods can use the host network, which an AdminNetworkPolicy does not match"},
		"named port deny": {func(cfg map[string]interface{}) {
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["bad"] = deny(sameNs, "metrics")
		}, "rule bad: named port metrics cannot be used in an AdminNetworkPolicy rule; use a port number"},
		"named port override": {func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["from-frontend"].(map[string]interface{})["ports"] = []interface{}{"https"}
		}, "rule from-frontend: named port https cannot be used in an AdminNetworkPolicy rule; use a port number"},
		"too many ports": {func(cfg map[string]interface{}) {
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["bad"] = deny(sameNs, manyPorts...)
		}, "rule bad: 101 ports, more than the 100 an AdminNetworkPolicy rule allows"},
		"too many rules": {func(cfg map[string]interface{}) {
			rules := projectRules(cfg["projects"].(map[string]interface{}), "payments-api")
			for i := 0; i < 101; i++ {
				rules[fmt.Sprintf("deny-%03d", i)] = deny(sameNs)
			}
		}, "the AdminNetworkPolicy would need 101 ingress rules, more than the 100 an AdminNetworkPolicy allows"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			objs := onboardingObjects(t, runOnboardingLabels(t, pruneOn, c.set))
			if status := onboardingStatus(t, objs); status["payments-api"] != c.want || len(status) != 1 {
				t.Errorf("want only status[payments-api] = %q, got %v", c.want, status)
			}
			skippedWhole(t, objs, "payments-api")
			if anpNamed(objs, "autoshift-project-payments-api") != nil {
				t.Error("payments-api is skipped, want no AdminNetworkPolicy")
			}
		})
	}
	// At the limit: 100 ingress rules fit.
	t.Run("at the limit", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, func(cfg map[string]interface{}) {
			rules := projectRules(cfg["projects"].(map[string]interface{}), "payments-api")
			for i := 0; i < 100; i++ {
				rules[fmt.Sprintf("deny-%03d", i)] = deny(sameNs)
			}
		}))
		anp := anpNamed(objs, "autoshift-project-payments-api")
		if anp == nil {
			t.Fatalf("100 ingress rules fit, want an AdminNetworkPolicy, status %v", onboardingStatus(t, objs))
		}
		if n := len(anp.obj["spec"].(map[string]interface{})["ingress"].([]interface{})); n != 100 {
			t.Errorf("want 100 ingress rules, got %d", n)
		}
	})
}

// Without the AdminNetworkPolicy CRD, a project with an in-cluster deny is reported and skipped whole
// rather than left more open than declared; the prune sweep skips the kind instead of failing.
func TestProjectOnboarding_ANPCRDMissing(t *testing.T) {
	t.Parallel()
	res := runOnboardingIn(t, testdataWithout(t, onboardingANPStubs, ""), pruneOn, withProfileDeny)
	objs := onboardingObjects(t, res)
	want := "the AdminNetworkPolicy CRD (adminnetworkpolicies.policy.networking.k8s.io) is not installed, so the in-cluster deny rules cannot be enforced"
	// The example's admin deny-metadata reports the missing CRD under its own global key.
	wantAdmin := "the AdminNetworkPolicy CRD (adminnetworkpolicies.policy.networking.k8s.io) is not installed, so the admin network policies cannot be enforced"
	if status := onboardingStatus(t, objs); status["payments-api"] != want || status[adminStatusKey] != wantAdmin || len(status) != 2 {
		t.Errorf("want status[payments-api] = %q and status[%s] = %q only, got %v", want, adminStatusKey, wantAdmin, status)
	}
	skippedWhole(t, objs, "payments-api")
	for _, o := range objs {
		if o.obj["kind"] == "AdminNetworkPolicy" {
			t.Errorf("no CRD, want no AdminNetworkPolicy entry, got %v (%s)", o.obj["metadata"], o.complianceType)
		}
	}
}

// AdminNetworkPolicy joins the prune sweep. It is cluster scoped, so it belongs to the project its
// autoshift.io/project label names: the removed project's is pruned with prune on and reported with
// prune off; a declared one never is; one no longer needed is pruned; an errored project keeps its own.
func TestProjectOnboarding_PruneANP(t *testing.T) {
	t.Parallel()
	const removed, payments = "autoshift-project-removed-project", "autoshift-project-payments-api"
	if p := anpPruned(t, onboardingObjects(t, onboardingPruneBase(t)), removed); len(p) != 1 || p[0].remediationAction != "enforce" {
		t.Errorf("prune on: want %s pruned by enforce, got %v", removed, p)
	}
	if p := anpPruned(t, onboardingObjects(t, onboardingBase(t)), removed); len(p) != 1 || p[0].remediationAction != "inform" {
		t.Errorf("prune off: want %s reported by inform, got %v", removed, p)
	}
	run := func(t *testing.T, mutate func(cfg map[string]interface{})) []onboardingObject {
		return onboardingObjects(t, runOnboardingInAllShards(t, testdataWithout(t, onboardingANPStubs, paymentsANPStubs), pruneOn, mutate))
	}
	t.Run("declared", func(t *testing.T) {
		objs := run(t, withProfileDeny)
		if anpNamed(objs, payments) == nil {
			t.Error("want payments-api's AdminNetworkPolicy rendered")
		}
		if p := anpPruned(t, objs, payments); len(p) != 0 {
			t.Errorf("payments-api declares its AdminNetworkPolicy, want it never pruned, got %v", p)
		}
	})
	t.Run("no longer needed", func(t *testing.T) {
		if p := anpPruned(t, run(t, nil), payments); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("payments-api has no in-cluster deny, want its live AdminNetworkPolicy pruned by enforce, got %v", p)
		}
	})
	t.Run("errored project", func(t *testing.T) {
		objs := run(t, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			projectRules(cfg["projects"].(map[string]interface{}), "payments-api")["bad"] = "allow-all"
		})
		if p := anpPruned(t, objs, payments); len(p) != 0 {
			t.Errorf("payments-api is in error, want its AdminNetworkPolicy kept, got %v", p)
		}
	})
}

// adminStatusKey is the status ConfigMap key of the admin network policy tier, which is global, not
// per project.
const adminStatusKey = "projectDefaults.adminNetworkPolicies"

// adminANPs returns the rendered (not mustnothave) admin AdminNetworkPolicies, autoshift-admin-*, by name.
func adminANPs(objs []onboardingObject) map[string]*onboardingObject {
	out := map[string]*onboardingObject{}
	for i, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if o.obj["kind"] == "AdminNetworkPolicy" && strings.HasPrefix(name, "autoshift-admin-") && o.complianceType != "mustnothave" {
			out[name] = &objs[i]
		}
	}
	return out
}

// setAdmin replaces projectDefaults.adminNetworkPolicies with entries.
func setAdmin(entries map[string]interface{}) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = entries
	}
}

// adminEntry is an admin network policy entry applying to every onboarded project.
func adminEntry(direction, action string, peers map[string]interface{}, ports ...interface{}) map[string]interface{} {
	e := map[string]interface{}{"appliesTo": map[string]interface{}{"allProjects": true}, "direction": direction, "action": action}
	e[map[string]string{"ingress": "from", "egress": "to"}[direction]] = peers
	if len(ports) > 0 {
		e["ports"] = ports
	}
	return e
}

const (
	adminAllProjects  = `"subject":{"namespaces":{"matchExpressions":[{"key":"autoshift.io/project","operator":"Exists"}]}}`
	adminDenyMetadata = `{"egress":[{"action":"Deny","name":"deny-metadata","to":[{"networks":["169.254.169.254/32"]}]}],"priority":10,` + adminAllProjects + `}`
)

// The example's deny-metadata renders one AdminNetworkPolicy autoshift-admin-deny-metadata in the
// enforce ConfigurationPolicy only, independent of any project: priority 10, Deny egress to the
// metadata address as a networks peer, subject every onboarded project namespace (autoshift.io/project
// Exists). It is owned (managed-by) but belongs to no project.
func TestProjectOnboarding_AdminNetworkPolicy(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	admin := adminANPs(objs)
	anp := admin["autoshift-admin-deny-metadata"]
	if anp == nil || len(admin) != 1 {
		t.Fatalf("want only autoshift-admin-deny-metadata, got %v", admin)
	}
	meta := anp.obj["metadata"].(map[string]interface{})
	if lbls := stringMap(meta, "labels"); len(lbls) != 1 || lbls["autoshift.io/managed-by"] != "project-onboarding" {
		t.Errorf("labels = %v, want only autoshift.io/managed-by: project-onboarding", lbls)
	}
	if _, ok := meta["namespace"]; ok {
		t.Errorf("AdminNetworkPolicy is cluster scoped, want no namespace, got %v", meta["namespace"])
	}
	if anp.complianceType != "mustonlyhave" || anp.remediationAction != "enforce" || anp.obj["apiVersion"] != "policy.networking.k8s.io/v1alpha1" {
		t.Errorf("want mustonlyhave policy.networking.k8s.io/v1alpha1 in the enforce ConfigurationPolicy, got %s %v %s", anp.complianceType, anp.obj["apiVersion"], anp.remediationAction)
	}
	if got := asJSON(anp.obj["spec"]); got != adminDenyMetadata {
		t.Errorf("spec =\n%s\nwant\n%s", got, adminDenyMetadata)
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}

	t.Run("independent of projects", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, func(cfg map[string]interface{}) { delete(cfg, "projects") }))
		if anp := adminANPs(objs)["autoshift-admin-deny-metadata"]; anp == nil || asJSON(anp.obj["spec"]) != adminDenyMetadata {
			t.Errorf("no projects key, want deny-metadata still rendered, got %v", anp)
		}
	})
	t.Run("none by default", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, func(cfg map[string]interface{}) {
			delete(cfg["projectDefaults"].(map[string]interface{}), "adminNetworkPolicies")
		}))
		if admin := adminANPs(objs); len(admin) != 0 {
			t.Errorf("no adminNetworkPolicies, want none rendered, got %v", admin)
		}
	})
	t.Run("enabled false removes", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, func(cfg map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"].(map[string]interface{})["deny-metadata"].(map[string]interface{})["enabled"] = false
		}))
		if admin := adminANPs(objs); len(admin) != 0 {
			t.Errorf("deny-metadata disabled, want none rendered, got %v", admin)
		}
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("want no status entries, got %v", status)
		}
	})
	// appliesTo.labels narrows to onboarded projects with those labels; namespaceSelector is passed
	// through as is and can reach namespaces that are not projects.
	t.Run("subjects and peers", func(t *testing.T) {
		many := make([]interface{}, 26)
		for i := range many {
			many[i] = fmt.Sprintf("198.51.100.%d/32", i)
		}
		objs := onboardingObjects(t, runOnboardingConfig(t, setAdmin(map[string]interface{}{
			"a-team": map[string]interface{}{
				"appliesTo": map[string]interface{}{"labels": map[string]interface{}{"example.com/team": "payments"}},
				"direction": "ingress", "action": "allow", "from": map[string]interface{}{"monitoring": true}, "ports": []interface{}{9090, map[string]interface{}{"port": 9091, "protocol": "UDP"}},
			},
			"b-select": map[string]interface{}{
				"appliesTo": map[string]interface{}{"namespaceSelector": map[string]interface{}{
					"matchLabels":      map[string]interface{}{"example.com/tier": "gold"},
					"matchExpressions": []interface{}{map[string]interface{}{"key": "example.com/env", "operator": "NotIn", "values": []interface{}{"dev"}}},
				}},
				"direction": "egress", "action": "deny", "to": map[string]interface{}{"dns": true, "namespaces": map[string]interface{}{"names": []interface{}{"kube-system"}}, "cidrs": many},
			},
		})))
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("want no status entries, got %v", status)
		}
		admin := adminANPs(objs)
		want := map[string]string{
			"autoshift-admin-a-team": `{"ingress":[{"action":"Allow","from":[{"namespaces":{"matchLabels":{"network.openshift.io/policy-group":"monitoring"}}}],"name":"a-team","ports":[{"portNumber":{"port":9090,"protocol":"TCP"}},{"portNumber":{"port":9091,"protocol":"UDP"}}]}],"priority":10,` +
				`"subject":{"namespaces":{"matchExpressions":[{"key":"autoshift.io/project","operator":"Exists"}],"matchLabels":{"example.com/team":"payments"}}}}`,
			"autoshift-admin-b-select": `{"egress":[{"action":"Deny","name":"b-select","to":[{"namespaces":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["kube-system"]}]}},` +
				`{"networks":` + asJSON(many[:25]) + `},{"networks":` + asJSON(many[25:]) + `}]},` +
				`{"action":"Deny","name":"b-select","ports":[{"portNumber":{"port":5353,"protocol":"UDP"}},{"portNumber":{"port":5353,"protocol":"TCP"}}],"to":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"openshift-dns"}}}]}],"priority":11,` +
				`"subject":{"namespaces":{"matchExpressions":[{"key":"example.com/env","operator":"NotIn","values":["dev"]}],"matchLabels":{"example.com/tier":"gold"}}}}`,
		}
		if len(admin) != len(want) {
			t.Errorf("want %d admin AdminNetworkPolicies, got %v", len(want), admin)
		}
		for name, w := range want {
			if anp := admin[name]; anp == nil {
				t.Errorf("want %s", name)
			} else if got := asJSON(anp.obj["spec"]); got != w {
				t.Errorf("%s spec =\n%s\nwant\n%s", name, got, w)
			}
		}
	})
}

// Priorities run from 10 upward in entry-name order, so every admin AdminNetworkPolicy outranks the
// project ones at 50. 40 entries fill 10 to 49; a 41st is a loud error and none renders.
func TestProjectOnboarding_AdminPriorityBelow50(t *testing.T) {
	t.Parallel()
	entries := func(n int) map[string]interface{} {
		m := map[string]interface{}{}
		for i := 0; i < n; i++ {
			m[fmt.Sprintf("deny-%02d", i)] = adminEntry("egress", "deny", cidrs(fmt.Sprintf("198.51.100.%d/32", i)))
		}
		return m
	}
	objs := onboardingObjects(t, runOnboardingConfig(t, setAdmin(map[string]interface{}{
		"c-third":  adminEntry("egress", "deny", cidrs("198.51.100.3/32")),
		"a-first":  adminEntry("egress", "deny", cidrs("198.51.100.1/32")),
		"b-second": adminEntry("egress", "deny", cidrs("198.51.100.2/32")),
	})))
	admin := adminANPs(objs)
	for name, want := range map[string]float64{"autoshift-admin-a-first": 10, "autoshift-admin-b-second": 11, "autoshift-admin-c-third": 12} {
		if anp := admin[name]; anp == nil {
			t.Errorf("want %s", name)
		} else if got := anp.obj["spec"].(map[string]interface{})["priority"]; got != want {
			t.Errorf("%s priority = %v, want %v", name, got, want)
		}
	}
	t.Run("40 fit", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, setAdmin(entries(40))))
		admin := adminANPs(objs)
		if len(admin) != 40 {
			t.Fatalf("want 40 admin AdminNetworkPolicies, got %d (status %v)", len(admin), onboardingStatus(t, objs))
		}
		if got := admin["autoshift-admin-deny-39"].obj["spec"].(map[string]interface{})["priority"]; got != float64(49) {
			t.Errorf("last priority = %v, want 49", got)
		}
	})
	t.Run("41 fail closed", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, setAdmin(entries(41))))
		if admin := adminANPs(objs); len(admin) != 0 {
			t.Errorf("41 entries, want none rendered, got %d", len(admin))
		}
		want := "41 enabled entries, more than the 40 that fit at priorities 10 to 49 (below the project AdminNetworkPolicies at 50)"
		if status := onboardingStatus(t, objs); status[adminStatusKey] != want || len(status) != 1 {
			t.Errorf("want only status[%s] = %q, got %v", adminStatusKey, want, status)
		}
	})
}

// An entry the AdminNetworkPolicy API cannot express as declared is a loud error under the global
// status key, and the tier fails closed: no admin AdminNetworkPolicy renders, not even the valid
// deny-metadata, so priorities never shift under a partial set. Projects still render.
func TestProjectOnboarding_AdminInvalid(t *testing.T) {
	t.Parallel()
	ok := adminEntry("egress", "deny", cidrs("198.51.100.0/24"))
	with := func(mod func(e map[string]interface{})) map[string]interface{} {
		e := adminEntry("egress", "deny", cidrs("198.51.100.0/24"))
		mod(e)
		return e
	}
	cases := map[string]struct {
		entry interface{}
		name  string
		want  string
	}{
		"not a map":    {"deny-all", "bad", "entry bad is not a map"},
		"invalid name": {ok, "Bad_Name", "entry Bad_Name: name is not a valid DNS-1123 label"},
		"enabled":      {with(func(e map[string]interface{}) { e["enabled"] = "maybe" }), "bad", "entry bad: enabled is not true or false"},
		"unknown key":  {with(func(e map[string]interface{}) { e["priority"] = 5 }), "bad", "entry bad: key priority is not one of appliesTo, direction, action, from, to, ports, enabled, description"},
		"no appliesTo": {with(func(e map[string]interface{}) { delete(e, "appliesTo") }), "bad", "entry bad: appliesTo is not set"},
		"appliesTo two keys": {with(func(e map[string]interface{}) {
			e["appliesTo"] = map[string]interface{}{"allProjects": true, "labels": map[string]interface{}{"a": "b"}}
		}), "bad", "entry bad: appliesTo needs exactly one of allProjects, labels, namespaceSelector"},
		"allProjects false": {with(func(e map[string]interface{}) { e["appliesTo"] = map[string]interface{}{"allProjects": false} }), "bad", "entry bad: appliesTo.allProjects is not true"},
		"bad selector op": {with(func(e map[string]interface{}) {
			e["appliesTo"] = map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "Near"}}}}
		}), "bad", "entry bad: appliesTo.namespaceSelector: operator Near is not In, NotIn, Exists or DoesNotExist"},
		"In without values": {with(func(e map[string]interface{}) {
			e["appliesTo"] = map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "In"}}}}
		}), "bad", "entry bad: appliesTo.namespaceSelector: operator In needs a non-empty values list"},
		"bad label": {with(func(e map[string]interface{}) {
			e["appliesTo"] = map[string]interface{}{"labels": map[string]interface{}{"a": "has space"}}
		}), "bad", "entry bad: label a=has space is not a valid Kubernetes label"},
		"direction":         {with(func(e map[string]interface{}) { e["direction"] = "both" }), "bad", "entry bad: direction both is not ingress or egress"},
		"action pass":       {with(func(e map[string]interface{}) { e["action"] = "pass" }), "bad", "entry bad: action pass is not allow or deny"},
		"ingress cidrs":     {adminEntry("ingress", "deny", cidrs("198.51.100.0/24")), "bad", "entry bad: an AdminNetworkPolicy has no ingress CIDR peer; use namespaces, or a Route allowlist for external clients"},
		"bad cidr":          {adminEntry("egress", "deny", cidrs("2001:db8:::/64")), "bad", "entry bad: cidr 2001:db8:::/64 is not a valid CIDR"},
		"dnsNames":          {adminEntry("egress", "deny", map[string]interface{}{"dnsNames": []interface{}{"example.com"}}), "bad", "entry bad: peer dnsNames is not supported here: an AdminNetworkPolicy has no DNS name peer; use cidrs"},
		"sameNamespace":     {adminEntry("ingress", "allow", map[string]interface{}{"sameNamespace": true}), "bad", "entry bad: peer sameNamespace is not supported here: an AdminNetworkPolicy peer cannot name the subject's own namespace; use namespaces"},
		"pods":              {adminEntry("ingress", "allow", map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"app": "x"}}}), "bad", "entry bad: peer pods is not supported here: an admin entry has no namespace to scope pod labels to; use namespaces"},
		"router deny":       {adminEntry("ingress", "deny", map[string]interface{}{"router": true}), "bad", "entry bad: cannot deny the router peer: router pods can use the host network, which an AdminNetworkPolicy does not match"},
		"named port":        {adminEntry("egress", "deny", cidrs("198.51.100.0/24"), "https"), "bad", "entry bad: named port https cannot be used in an AdminNetworkPolicy rule; use a port number"},
		"bad port":          {adminEntry("egress", "deny", cidrs("198.51.100.0/24"), 70000), "bad", "entry bad: port 70000 is not a port number"},
		"no peers":          {adminEntry("egress", "deny", map[string]interface{}{"dns": false}), "bad", "entry bad: has no peers"},
		"ports on dns only": {adminEntry("egress", "deny", map[string]interface{}{"dns": true}, 53), "bad", "entry bad: ports cannot be set when dns is the only peer: the dns rule always uses port 5353, TCP and UDP"},
		"unknown peer":      {adminEntry("egress", "deny", map[string]interface{}{"nodes": true}), "bad", "entry bad: unknown peer nodes"},
		"wrong side":        {with(func(e map[string]interface{}) { e["from"] = cidrs("198.51.100.0/24") }), "bad", "entry bad: an egress rule takes to, not from"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			objs := onboardingObjects(t, runOnboardingLabels(t, pruneOn, func(cfg map[string]interface{}) {
				cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"].(map[string]interface{})[c.name] = c.entry
			}))
			if status := onboardingStatus(t, objs); status[adminStatusKey] != c.want || len(status) != 1 {
				t.Errorf("want only status[%s] = %q, got %v", adminStatusKey, c.want, status)
			}
			if admin := adminANPs(objs); len(admin) != 0 {
				t.Errorf("the tier is in error, want no admin AdminNetworkPolicy, got %v", admin)
			}
			otherProjectsRender(t, objs, "")
		})
	}
	t.Run("section not a map", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingConfig(t, func(cfg map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = []interface{}{"deny-metadata"}
		}))
		want := "projectDefaults.adminNetworkPolicies is not a map"
		if status := onboardingStatus(t, objs); status[adminStatusKey] != want || len(status) != 1 {
			t.Errorf("want only status[%s] = %q, got %v", adminStatusKey, want, status)
		}
		otherProjectsRender(t, objs, "")
	})
}

// Without the AdminNetworkPolicy CRD, declared admin entries are a loud global error; nothing renders.
func TestProjectOnboarding_AdminCRDMissing(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, runOnboardingIn(t, testdataWithout(t, onboardingANPStubs, ""), pruneOn, nil))
	want := "the AdminNetworkPolicy CRD (adminnetworkpolicies.policy.networking.k8s.io) is not installed, so the admin network policies cannot be enforced"
	if status := onboardingStatus(t, objs); status[adminStatusKey] != want || len(status) != 1 {
		t.Errorf("want only status[%s] = %q, got %v", adminStatusKey, want, status)
	}
	for _, o := range objs {
		if o.obj["kind"] == "AdminNetworkPolicy" {
			t.Errorf("no CRD, want no AdminNetworkPolicy entry, got %v (%s)", o.obj["metadata"], o.complianceType)
		}
	}
}

// staleAdminStubs is the CRD plus live admin AdminNetworkPolicies from an earlier run: deny-metadata,
// still declared, and retired, no longer declared.
const staleAdminStubs = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: adminnetworkpolicies.policy.networking.k8s.io
spec:
  group: policy.networking.k8s.io
  names: {kind: AdminNetworkPolicy, plural: adminnetworkpolicies}
  scope: Cluster
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-admin-deny-metadata
  labels:
    autoshift.io/managed-by: project-onboarding
spec:
  priority: 10
  subject:
    namespaces: {}
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-admin-retired
  labels:
    autoshift.io/managed-by: project-onboarding
spec:
  priority: 11
  subject:
    namespaces: {}
`

// Admin AdminNetworkPolicies join the prune sweep. An entry no longer declared is pruned by enforce
// with prune on and reported by inform with prune off; a declared one never is; while the tier is in
// error none is pruned (fail closed).
func TestProjectOnboarding_PruneAdminANP(t *testing.T) {
	t.Parallel()
	dir := testdataWithout(t, onboardingANPStubs, staleAdminStubs)
	run := func(t *testing.T, labels map[string]string, mutate func(cfg map[string]interface{})) []onboardingObject {
		return onboardingObjects(t, runOnboardingIn(t, dir, labels, mutate))
	}
	objs := run(t, pruneOn, nil)
	if p := anpPruned(t, objs, "autoshift-admin-retired"); len(p) != 1 || p[0].remediationAction != "enforce" {
		t.Errorf("prune on: want autoshift-admin-retired pruned by enforce, got %v", p)
	}
	if p := anpPruned(t, objs, "autoshift-admin-deny-metadata"); len(p) != 0 {
		t.Errorf("deny-metadata is declared, want it never pruned, got %v", p)
	}
	if p := anpPruned(t, run(t, nil, nil), "autoshift-admin-retired"); len(p) != 1 || p[0].remediationAction != "inform" {
		t.Errorf("prune off: want autoshift-admin-retired reported by inform, got %v", p)
	}
	t.Run("tier in error", func(t *testing.T) {
		objs := run(t, pruneOn, func(cfg map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"].(map[string]interface{})["bad"] = "deny-all"
		})
		for _, name := range []string{"autoshift-admin-retired", "autoshift-admin-deny-metadata"} {
			if p := anpPruned(t, objs, name); len(p) != 0 {
				t.Errorf("the tier is in error, want %s kept, got %v", name, p)
			}
		}
	})
}

// onboardingEgressIPStubs is the testdata file holding the EgressIP CRD and the owned EgressIPs of
// payments-api and of a removed project. testdataWithout drops it to model a cluster without the CRD.
const onboardingEgressIPStubs = "project-onboarding-egressip.yaml"

// egressIPNamed returns the rendered (not mustnothave) EgressIP name, or nil. It is cluster scoped,
// so it must carry no namespace.
func egressIPNamed(t *testing.T, objs []onboardingObject, name string) *onboardingObject {
	t.Helper()
	for i, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "EgressIP" && meta["name"] == name && o.complianceType != "mustnothave" {
			if _, ok := meta["namespace"]; ok {
				t.Errorf("EgressIP %s is cluster scoped, want no namespace, got %v", name, meta["namespace"])
			}
			return &objs[i]
		}
	}
	return nil
}

// egressIPPruned returns the mustnothave entries for the EgressIP name (any name when empty).
func egressIPPruned(t *testing.T, objs []onboardingObject, name string) []onboardingObject {
	t.Helper()
	var out []onboardingObject
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.complianceType != "mustnothave" || o.obj["kind"] != "EgressIP" {
			continue
		}
		if _, ok := meta["namespace"]; ok {
			t.Errorf("EgressIP %v is cluster scoped, want no namespace in its mustnothave, got %v", meta["name"], meta["namespace"])
		}
		if name == "" || meta["name"] == name {
			out = append(out, o)
		}
	}
	return out
}

// setEgressIP sets egressIP of project to v.
func setEgressIP(project string, v interface{}) func(projects map[string]interface{}) {
	return func(projects map[string]interface{}) {
		projects[project].(map[string]interface{})["egressIP"] = v
	}
}

const eipSpecOf = `{"egressIPs":%s,"namespaceSelector":{"matchLabels":{"autoshift.io/project":"%s"}}}`

// payments-api declares egressIP ['198.51.100.50']: one cluster-scoped EgressIP
// autoshift-payments-api with those addresses, selecting the project namespace by its
// autoshift.io/project label. It is owned and exact (mustonlyhave), so a changed address replaces the
// earlier one instead of being added to it, and it follows the project mode.
func TestProjectOnboarding_EgressIP(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	eip := egressIPNamed(t, objs, "autoshift-payments-api")
	if eip == nil {
		t.Fatal("payments-api declares egressIP, want EgressIP autoshift-payments-api")
	}
	checkOwnedExact(t, eip, "payments-api", "enforce")
	if eip.obj["apiVersion"] != "k8s.ovn.org/v1" {
		t.Errorf("EgressIP apiVersion = %v, want k8s.ovn.org/v1", eip.obj["apiVersion"])
	}
	if got, want := asJSON(eip.obj["spec"]), fmt.Sprintf(eipSpecOf, `["198.51.100.50"]`, "payments-api"); got != want {
		t.Errorf("payments-api EgressIP spec = %s, want %s", got, want)
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}

	t.Run("IPv6 and duplicates", func(t *testing.T) {
		// Written forms of one address count once; the first form is kept.
		objs := onboardingObjects(t, runOnboarding(t, setEgressIP("payments-api",
			[]interface{}{"2001:db8::10", "198.51.100.51", "2001:DB8:0:0:0:0:0:10", "198.51.100.51", "2001:db8:0::10"})))
		eip := egressIPNamed(t, objs, "autoshift-payments-api")
		if eip == nil {
			t.Fatal("want EgressIP autoshift-payments-api")
		}
		if got, want := asJSON(eip.obj["spec"]), fmt.Sprintf(eipSpecOf, `["2001:db8::10","198.51.100.51"]`, "payments-api"); got != want {
			t.Errorf("spec = %s, want %s", got, want)
		}
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("duplicates within a list are merged, want no status entries, got %v", status)
		}
	})
	t.Run("trailing double colon", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboarding(t, setEgressIP("payments-api", []interface{}{"2001:db8:1:2:3:4:5::"})))
		eip := egressIPNamed(t, objs, "autoshift-payments-api")
		if eip == nil {
			t.Fatal("want EgressIP autoshift-payments-api")
		}
		if got, want := asJSON(eip.obj["spec"]), fmt.Sprintf(eipSpecOf, `["2001:db8:1:2:3:4:5::"]`, "payments-api"); got != want {
			t.Errorf("spec = %s, want %s", got, want)
		}
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("want no status entries, got %v", status)
		}
	})
	t.Run("follows mode", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboarding(t, setEgressIP("legacy-app", []interface{}{"198.51.100.60"})))
		eip := egressIPNamed(t, objs, "autoshift-legacy-app")
		if eip == nil {
			t.Fatal("want EgressIP autoshift-legacy-app")
		}
		checkOwnedExact(t, eip, "legacy-app", "inform")
	})
}

// egressIP is optional: reporting declares none and gets no EgressIP, nor does an empty list.
func TestProjectOnboarding_NoEgressIP(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, onboardingBase(t))
	if o := egressIPNamed(t, objs, "autoshift-reporting"); o != nil {
		t.Errorf("reporting declares no egressIP, want no EgressIP, got %v", o.obj)
	}
	objs = onboardingObjects(t, runOnboarding(t, setEgressIP("payments-api", []interface{}{})))
	if o := egressIPNamed(t, objs, "autoshift-payments-api"); o != nil {
		t.Errorf("egressIP: [] declares no address, want no EgressIP, got %v", o.obj)
	}
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Errorf("want no status entries, got %v", status)
	}
}

// An egressIP that is not a list, or an entry that is not a plain IPv4 or IPv6 address, skips the
// project whole and is reported; the other projects render.
func TestProjectOnboarding_EgressIPInvalid(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		value interface{}
		want  string
	}{
		{"not a list", "198.51.100.50", "egressIP is not a list"},
		{"map", map[string]interface{}{"ip": "198.51.100.50"}, "egressIP is not a list"},
		{"prefix length", []interface{}{"198.51.100.50/32"}, "egressIP entry 198.51.100.50/32 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"octet out of range", []interface{}{"198.51.100.256"}, "egressIP entry 198.51.100.256 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"leading zero", []interface{}{"198.51.100.050"}, "egressIP entry 198.51.100.050 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"two double colons", []interface{}{"2001:db8::1::2"}, "egressIP entry 2001:db8::1::2 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"too few groups", []interface{}{"2001:db8:1"}, "egressIP entry 2001:db8:1 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"IPv6 prefix length", []interface{}{"2001:db8::1/128"}, "egressIP entry 2001:db8::1/128 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"hostname", []interface{}{"egress.example.com"}, "egressIP entry egress.example.com is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"nested list", []interface{}{[]interface{}{"198.51.100.50"}}, "egressIP entry [198.51.100.50] is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"one bad of two", []interface{}{"198.51.100.50", "198.51.100"}, "egressIP entry 198.51.100 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"IPv6 unspecified", []interface{}{"::"}, "egressIP entry :: is the unspecified address"},
		{"IPv4 unspecified", []interface{}{"0.0.0.0"}, "egressIP entry 0.0.0.0 is the unspecified address"},
		{"zone ID", []interface{}{"fe80::1%eth0"}, "egressIP entry fe80::1%eth0 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		{"embedded IPv4", []interface{}{"::ffff:198.51.100.1"}, "egressIP entry ::ffff:198.51.100.1 is not an IPv4 or IPv6 address (write it without a prefix length)"},
		// Hex form of ::ffff:198.51.100.50: OVN-Kubernetes treats it as the IPv4 address, so it
		// would slip past the cross-project duplicate check keyed on the written family.
		{"IPv4-mapped hex", []interface{}{"::ffff:c633:6432"}, "egressIP entry ::ffff:c633:6432 is an IPv4-mapped IPv6 address (write the IPv4 address instead)"},
		{"IPv4-mapped expanded", []interface{}{"0:0:0:0:0:FFFF:c633:6432"}, "egressIP entry 0:0:0:0:0:FFFF:c633:6432 is an IPv4-mapped IPv6 address (write the IPv4 address instead)"},
		{"number", []interface{}{12345}, "egressIP entry 12345 is not an IPv4 or IPv6 address (write it without a prefix length)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			objs := onboardingObjects(t, runPrune(t, setEgressIP("payments-api", c.value)))
			if status := onboardingStatus(t, objs); status["payments-api"] != c.want || len(status) != 1 {
				t.Errorf("want only status[payments-api] = %q, got %v", c.want, status)
			}
			skippedWhole(t, objs, "payments-api")
			if p := egressIPPruned(t, objs, "autoshift-payments-api"); len(p) != 0 {
				t.Errorf("payments-api is in error, want its live EgressIP kept, got %v", p)
			}
		})
	}
}

// The same address declared by two projects is an error on both: OVN-Kubernetes assigns an address
// to one EgressIP only. Both are skipped whole; a third project renders. Written forms of one IPv6
// address are the same address.
func TestProjectOnboarding_EgressIPConflict(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                string
		payments, reporting string
	}{
		{"IPv4", "198.51.100.50", "198.51.100.50"},
		{"IPv6 written differently", "2001:db8::1", "2001:DB8:0:0:0:0:0:1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			objs := onboardingObjects(t, runPrune(t, func(projects map[string]interface{}) {
				setEgressIP("payments-api", []interface{}{"198.51.100.70", c.payments})(projects)
				setEgressIP("reporting", []interface{}{c.reporting})(projects)
			}))
			want := map[string]string{
				"payments-api": fmt.Sprintf("egressIP %s is also declared by project reporting", c.payments),
				"reporting":    fmt.Sprintf("egressIP %s is also declared by project payments-api", c.reporting),
			}
			if status := onboardingStatus(t, objs); asJSON(status) != asJSON(want) {
				t.Errorf("status = %v, want %v", status, want)
			}
			for _, o := range objs {
				meta, _ := o.obj["metadata"].(map[string]interface{})
				for _, project := range []string{"payments-api", "reporting"} {
					if (meta["namespace"] == project || meta["name"] == project || meta["name"] == "autoshift-"+project) && !isStatus(o) {
						t.Errorf("%s is skipped, want nothing rendered or pruned for it, got %s %v (%s)", project, o.obj["kind"], meta["name"], o.complianceType)
					}
				}
			}
			if namespaceNamed(objs, "legacy-app") == nil {
				t.Error("legacy-app must still render its Namespace")
			}
		})
	}
	t.Run("three projects", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboarding(t, func(projects map[string]interface{}) {
			for _, p := range []string{"payments-api", "reporting", "legacy-app"} {
				setEgressIP(p, []interface{}{"198.51.100.50"})(projects)
			}
		}))
		status := onboardingStatus(t, objs)
		if got, want := status["reporting"], "egressIP 198.51.100.50 is also declared by project legacy-app, payments-api"; got != want {
			t.Errorf("status[reporting] = %q, want %q", got, want)
		}
		if len(status) != 3 {
			t.Errorf("want all three projects reported, got %v", status)
		}
	})
}

// Without the EgressIP CRD a project that declares egressIP cannot get its addresses: it is skipped
// whole and reported, and nothing of the kind is looked up or pruned.
func TestProjectOnboarding_EgressIPCRDMissing(t *testing.T) {
	t.Parallel()
	objs := onboardingObjects(t, runOnboardingIn(t, testdataWithout(t, onboardingEgressIPStubs, ""), pruneOn, nil))
	want := "the EgressIP CRD (egressips.k8s.ovn.org) is not installed, so the egress IP addresses cannot be assigned"
	if status := onboardingStatus(t, objs); status["payments-api"] != want || len(status) != 1 {
		t.Errorf("want only status[payments-api] = %q, got %v", want, status)
	}
	skippedWhole(t, objs, "payments-api")
	for _, o := range objs {
		if o.obj["kind"] == "EgressIP" {
			t.Errorf("no CRD, want no EgressIP entry, got %v (%s)", o.obj["metadata"], o.complianceType)
		}
	}
	t.Run("not declared", func(t *testing.T) {
		objs := onboardingObjects(t, runOnboardingIn(t, testdataWithout(t, onboardingEgressIPStubs, ""), pruneOn, func(cfg map[string]interface{}) {
			delete(cfg["projects"].(map[string]interface{})["payments-api"].(map[string]interface{}), "egressIP")
		}))
		if status := onboardingStatus(t, objs); len(status) != 0 {
			t.Errorf("no project declares egressIP, want no status entries, got %v", status)
		}
	})
}

// EgressIP joins the prune sweep. It is cluster scoped, so it belongs to the project its
// autoshift.io/project label names: the removed project's is pruned with prune on and reported with
// prune off; a declared one never is; one no longer declared is pruned; an errored project keeps its own.
func TestProjectOnboarding_PruneEgressIP(t *testing.T) {
	t.Parallel()
	const removed, payments = "autoshift-removed-project", "autoshift-payments-api"
	on := onboardingObjects(t, onboardingPruneBase(t))
	if p := egressIPPruned(t, on, removed); len(p) != 1 || p[0].remediationAction != "enforce" {
		t.Errorf("prune on: want %s pruned by enforce, got %v", removed, p)
	}
	if p := egressIPPruned(t, onboardingObjects(t, onboardingBase(t)), removed); len(p) != 1 || p[0].remediationAction != "inform" {
		t.Errorf("prune off: want %s reported by inform, got %v", removed, p)
	}
	if p := egressIPPruned(t, on, payments); len(p) != 0 {
		t.Errorf("payments-api declares its EgressIP, want it never pruned, got %v", p)
	}
	t.Run("no longer declared", func(t *testing.T) {
		objs := onboardingObjects(t, runPruneAllShards(t, func(projects map[string]interface{}) {
			delete(projects["payments-api"].(map[string]interface{}), "egressIP")
		}))
		if o := egressIPNamed(t, objs, payments); o != nil {
			t.Errorf("payments-api declares no egressIP, want no EgressIP, got %v", o.obj)
		}
		if p := egressIPPruned(t, objs, payments); len(p) != 1 || p[0].remediationAction != "enforce" {
			t.Errorf("want the live EgressIP pruned by enforce, got %v", p)
		}
	})
	t.Run("errored project", func(t *testing.T) {
		objs := onboardingObjects(t, runPruneAllShards(t, func(projects map[string]interface{}) {
			delete(projects["payments-api"].(map[string]interface{}), "egressIP")
			projectRules(projects, "payments-api")["bad"] = "allow-all"
		}))
		if p := egressIPPruned(t, objs, payments); len(p) != 0 {
			t.Errorf("payments-api is in error, want its EgressIP kept, got %v", p)
		}
	})
}
