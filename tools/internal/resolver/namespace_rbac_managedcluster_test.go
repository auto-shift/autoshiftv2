//go:build integration

package resolver

// namespace-rbac refuses ManagedCluster namespaces with a lookup on the managed cluster, not in the
// hub template: a hub template resolves on the hub that placed the policy, which on a managed hub
// in a hub of hubs is its parent. These tests give the hub resolver and the spoke resolver
// different objects, so each topology is modelled the way it runs: the hub template sees the
// placing hub, the regular template sees the cluster the policy lands on.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"github.com/stolostron/go-template-utils/v7/pkg/templates"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	// A spoke of the cluster the policy lands on, when that cluster is a hub.
	nsRbacChild = "example-child"
	// A spoke of the hub that placed the policy, but a plain namespace on the target.
	nsRbacSibling = "example-sibling"
)

// nsRbacMCs renders one ManagedCluster per name, without the CRD (nsdelManagedClusters has both).
func nsRbacMCs(names ...string) string {
	var b string
	for _, n := range names {
		b += fmt.Sprintf("apiVersion: cluster.open-cluster-management.io/v1\nkind: ManagedCluster\nmetadata:\n  name: %s\nspec:\n  hubAcceptsClient: true\n---\n", n)
	}
	return b
}

// nsRbacLabelledNs renders a Namespace with one label.
func nsRbacLabelledNs(name, key string) string {
	return fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n  labels:\n    %s: %s\n---\n", name, key, name)
}

// nsRbacStaleRB renders a labelled RoleBinding that config does not declare, so the inform sweep
// reports it unless its namespace is skipped as a whole.
func nsRbacStaleRB(ns string) string {
	return fmt.Sprintf("apiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding\nmetadata:\n  name: x-stale\n  namespace: %s\n  labels:\n    autoshift.io/managed-by: namespace-rbac\nroleRef:\n  apiGroup: rbac.authorization.k8s.io\n  kind: Role\n  name: x-stale\nsubjects: []\n---\n", ns)
}

// nsRbacTopoConfig declares an entry and a subject in both namespaces.
func nsRbacTopoConfig(nsMap map[string]interface{}) {
	rbs := exampleNs(nsMap)["roleBindings"].(map[string]interface{})
	for _, ns := range []string{nsRbacChild, nsRbacSibling} {
		nsMap[ns] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-sa": true}}
		rbs["x-"+ns] = map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{ns + "/x-sa": true}}
	}
}

// managedCluster selects how the spoke resolver serves ManagedClusters.
type managedCluster int

const (
	mcSeeded    managedCluster = iota // as seeded: listed when ManagedCluster objects are in the seed
	mcEmpty                           // the kind is served, the list is empty
	mcForbidden                       // the kind is served, listing it returns Forbidden
)

type nsRbacTopo struct {
	hub      string // extra stubs of the hub that resolves the hub template
	spoke    string // extra stubs of the cluster the policy lands on
	spokeMCs bool   // keep the ManagedCluster objects of tools/testdata on the spoke
	mc       managedCluster
	mutate   func(nsMap map[string]interface{})
}

// nsRbacProfile is one cluster profile's resolution of namespace-rbac.
type nsRbacProfile struct {
	name       string
	spokeWarns []string
	objs       []onboardingObject // nil when the regular template failed
	resolved   string
}

// isManagedCluster reports whether a seed object is a ManagedCluster.
func isManagedCluster(u unstructured.Unstructured) bool {
	return u.GetKind() == "ManagedCluster" && u.GetAPIVersion() == "cluster.open-cluster-management.io/v1"
}

// newNsRbacSpokeResolver is NewSpokeResolver with ManagedClusters served as mc says. For mcEmpty and
// mcForbidden the kind is registered in discovery but no ManagedCluster object is seeded, so lists
// reach the fake dynamic client, which answers Forbidden for mcForbidden.
func newNsRbacSpokeResolver(t *testing.T, seed []unstructured.Unstructured, mc managedCluster) *Resolver {
	t.Helper()
	if mc == mcSeeded {
		r, err := NewSpokeResolver(seed)
		if err != nil {
			t.Fatalf("NewSpokeResolver: %v", err)
		}
		return r
	}
	var kept []unstructured.Unstructured
	for _, u := range seed {
		if !isManagedCluster(u) {
			kept = append(kept, u)
		}
	}
	reg := unstructured.Unstructured{}
	reg.SetAPIVersion("cluster.open-cluster-management.io/v1")
	reg.SetKind("ManagedCluster")
	reg.SetName("registration-only")
	listKinds, apiLists := buildRegistryFromResources(append(append([]unstructured.Unstructured{}, kept...), reg))
	dyn := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, toRuntimeObjects(kept)...)
	if mc == mcForbidden {
		dyn.PrependReactor("list", "managedclusters", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "cluster.open-cluster-management.io", Resource: "managedclusters"}, "", fmt.Errorf("RBAC: access denied"))
		})
	}
	cs := fakeclientset.NewSimpleClientset()
	cs.Discovery().(*fakediscovery.FakeDiscovery).Resources = apiLists
	tr, err := templates.NewResolverWithClients(dyn, cs.Discovery(), templates.Config{StartDelim: "{{", StopDelim: "}}", AdditionalIndentation: 8})
	if err != nil {
		t.Fatal(err)
	}
	tr.WithLocalResources(kept)
	return &Resolver{inner: tr}
}

// runNsRbacTopo resolves namespace-rbac against every cluster profile of the e2e suite (the primary
// hub context and one managed profile per _example-cluster-install-*.yaml), with the hub
// template resolved on o.hub and the regular template on o.spoke. Every profile's rendered-config
// merges the hub config, so each carries the same config.namespaceRbac.
func runNsRbacTopo(t *testing.T, o nsRbacTopo) []nsRbacProfile {
	t.Helper()
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(configs)
	var cloned ExampleConfigs
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	if o.mutate != nil {
		o.mutate(cloned.HubConfig["namespaceRbac"].(map[string]interface{}))
	}
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatal(err)
	}
	lbls := BuildSyntheticLabels(declared)
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: lbls}
	var platforms []string
	for p := range cloned.ClusterInstallExtra {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	var extra []NamedContext
	for _, p := range platforms {
		ml := map[string]string{}
		for k, v := range lbls {
			ml[k] = v
		}
		ml["autoshift.io/self-managed"] = "false"
		extra = append(extra, NamedContext{Name: "managed-" + p, Ctx: HubContext{ManagedClusterName: ctx.ManagedClusterName + "-" + p, ManagedClusterLabels: ml}})
	}
	if len(extra) == 0 {
		t.Fatal("no managed profiles: ExtractExampleConfigs found no _example-cluster-install-*.yaml")
	}
	cms, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatal(err)
	}
	// The pipeline gives both resolvers one set of local resources, read from testdataDir and served
	// before each resolver's own dynamic client. That set is tools/testdata without its
	// ManagedClusters (lint-cluster and local-cluster, which belong to the placing hub). Each
	// cluster's ManagedClusters, CRDs and Namespaces live only in its own resolver's dynamic client.
	base, err := LoadTestResources(filepath.Join(root, "tools", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	var common, baseMCs []unstructured.Unstructured
	var docs []string
	for _, u := range base {
		if isManagedCluster(u) {
			baseMCs = append(baseMCs, u)
			continue
		}
		common = append(common, u)
		b, err := sigsyaml.Marshal(u.Object)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, string(b))
	}
	testdataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(testdataDir, "common.yaml"), []byte(strings.Join(docs, "---\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	objects := func(stubs string) []unstructured.Unstructured {
		if stubs == "" {
			return nil
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "stubs.yaml"), []byte(stubs), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := LoadTestResources(dir)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	seed := func(more ...[]unstructured.Unstructured) []unstructured.Unstructured {
		out := append(append([]unstructured.Unstructured{}, cms...), common...)
		for _, m := range more {
			out = append(out, m...)
		}
		return out
	}
	hubSeed := seed(baseMCs, objects(o.hub))
	spokeSeed := seed(objects(o.spoke))
	if o.spokeMCs {
		spokeSeed = append(spokeSeed, baseMCs...)
	}
	r, err := NewResolver(hubSeed)
	if err != nil {
		t.Fatal(err)
	}
	spokeR := newNsRbacSpokeResolver(t, spokeSeed, o.mc)
	only := func(policy string) bool { return policy == nsRbacPolicy }
	_, results, err := runPipelineTransform(filepath.Join(root, "policies"), only, cachedRender, nil, ctx, extra, r, spokeR, declared, &cloned, testdataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if res.Policy != nsRbacPolicy {
			continue
		}
		if res.Err != nil || !res.ResolveOK || len(res.YAMLErrors) > 0 {
			t.Fatalf("namespace-rbac hub template did not resolve: err=%v warns=%v yaml=%v", res.Err, res.ResolveWarns, res.YAMLErrors)
		}
		parse := func(name, y string, warns []string) nsRbacProfile {
			p := nsRbacProfile{name: name, spokeWarns: warns, resolved: y}
			if len(warns) > 0 {
				return p // unresolved template text is left in place; there are no objects
			}
			objs, err := parseOnboardingObjects(y)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			p.objs = objs
			return p
		}
		out := []nsRbacProfile{parse("hub", res.ResolvedYAML, res.SpokeWarns)}
		for _, ec := range extra {
			er := res.ExtraResults[ec.Name]
			if !er.ResolveOK || len(er.YAMLErrors) > 0 {
				t.Fatalf("%s: hub template did not resolve: %v %v", ec.Name, er.ResolveWarns, er.YAMLErrors)
			}
			out = append(out, parse(ec.Name, er.ResolvedYAML, er.SpokeWarns))
		}
		return out
	}
	t.Fatal("namespace-rbac not in pipeline results")
	return nil
}

// requireResolved fails unless the regular template resolved for the profile.
func requireResolved(t *testing.T, p nsRbacProfile) {
	t.Helper()
	if len(p.spokeWarns) > 0 {
		t.Fatalf("%s: regular template did not resolve: %v", p.name, p.spokeWarns)
	}
}

// assertNsRbacGate checks one profile: each refused namespace renders nothing, has the namespace
// message, is skipped by the stale sweep, and refuses the RoleBinding naming it; each accepted one
// renders its ServiceAccount and RoleBinding.
func assertNsRbacGate(t *testing.T, p nsRbacProfile, refused map[string]string, accepted []string) {
	t.Helper()
	requireResolved(t, p)
	got := nsRbacRendered(p.objs)
	status := nsRbacStatusData(t, p.objs)
	stale := nsRbacStale(p.objs, nsRbacInformCP)
	for ns, why := range refused {
		for _, k := range []string{"ServiceAccount|" + ns + "|x-sa", "RoleBinding|" + nsRbacExampleNs + "|x-" + ns} {
			if _, ok := got[k]; ok {
				t.Errorf("%s: %s rendered, want it refused", p.name, k)
			}
		}
		if want := "namespace " + ns + " " + why; status[ns] != want {
			t.Errorf("%s: status[%s] = %q, want exactly %q", p.name, ns, status[ns], want)
		}
		if want := fmt.Sprintf("roleBindings.%q: serviceAccounts.%s/x-sa: %s %s", "x-"+ns, ns, ns, strings.Replace(why, "the ManagedCluster "+ns, "a ManagedCluster", 1)); !strings.Contains(status[nsRbacExampleNs], want) {
			t.Errorf("%s: status[%s] = %q, want it to contain %q", p.name, nsRbacExampleNs, status[nsRbacExampleNs], want)
		}
		if stale["RoleBinding|"+ns+"|x-stale"] {
			t.Errorf("%s: the sweep reports RoleBinding %s/x-stale in a refused namespace", p.name, ns)
		}
	}
	for _, ns := range accepted {
		for _, k := range []string{"ServiceAccount|" + ns + "|x-sa", "RoleBinding|" + nsRbacExampleNs + "|x-" + ns} {
			if _, ok := got[k]; !ok {
				t.Errorf("%s: %s not rendered, want it accepted", p.name, k)
			}
		}
		if v, ok := status[ns]; ok {
			t.Errorf("%s: status[%s] = %q, want no entry", p.name, ns, v)
		}
		if !stale["RoleBinding|"+ns+"|x-stale"] {
			t.Errorf("%s: the sweep does not report RoleBinding %s/x-stale", p.name, ns)
		}
	}
	assertExampleIntact(t, p.objs)
}

const (
	whyListed = "is the namespace of the ManagedCluster %s on the hub, where namespace-rbac never grants access"
	whyLabel  = "carries the label %s of a ManagedCluster namespace, where namespace-rbac never grants access"
)

// The decision follows the ManagedClusters of the cluster the policy lands on, on every profile.
func TestNamespaceRbac_ManagedClusterTopologies(t *testing.T) {
	t.Parallel()
	targetNs := nsStubs(nsRbacChild, nsRbacSibling) + "---\n" + nsRbacStaleRB(nsRbacChild) + nsRbacStaleRB(nsRbacSibling)
	listed := fmt.Sprintf(whyListed, nsRbacChild)
	cases := map[string]struct {
		topo     nsRbacTopo
		refused  map[string]string
		accepted []string
	}{
		// One cluster resolves both templates: its spoke example-child is refused.
		"self-managed hub": {nsRbacTopo{
			hub:      nsdelManagedClusters(nsRbacChild),
			spoke:    nsdelManagedClusters(nsRbacChild) + targetNs,
			spokeMCs: true,
		}, map[string]string{nsRbacChild: listed}, []string{nsRbacSibling}},
		// The parent places the policy on a managed hub. The parent's spoke example-sibling is a
		// plain namespace there; the managed hub's own spoke example-child is refused. The hub
		// template saw it the other way round.
		"managed hub in a hub of hubs": {nsRbacTopo{
			hub:   nsdelManagedClusters(nsRbacSibling),
			spoke: nsdelManagedClusters(nsRbacChild) + targetNs,
		}, map[string]string{nsRbacChild: listed}, []string{nsRbacSibling}},
		// A managed cluster that is not a hub has no ManagedCluster CRD: the hub's cluster names
		// mean nothing there, and the list is never read.
		"managed spoke": {nsRbacTopo{
			hub:   nsdelManagedClusters(nsRbacChild, nsRbacSibling),
			spoke: targetNs,
		}, nil, []string{nsRbacChild, nsRbacSibling}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c.topo.mutate = nsRbacTopoConfig
			for _, p := range runNsRbacTopo(t, c.topo) {
				assertNsRbacGate(t, p, c.refused, c.accepted)
			}
		})
	}
}

// Fail closed: a cluster namespace is refused by its label when the list cannot show it, and a
// lookup that fails stops the whole template, so nothing is applied.
func TestNamespaceRbac_ManagedClusterFailClosed(t *testing.T) {
	t.Parallel()
	const (
		ocmLabel = "cluster.open-cluster-management.io/managedCluster"
		regLabel = "open-cluster-management.io/cluster-name"
	)
	labelled := nsRbacLabelledNs(nsRbacChild, ocmLabel) + nsRbacLabelledNs(nsRbacSibling, regLabel) +
		nsRbacStaleRB(nsRbacChild) + nsRbacStaleRB(nsRbacSibling)
	byLabel := map[string]string{nsRbacChild: fmt.Sprintf(whyLabel, ocmLabel), nsRbacSibling: fmt.Sprintf(whyLabel, regLabel)}
	for name, topo := range map[string]nsRbacTopo{
		// The CRD exists and the list is empty: the labels still refuse both namespaces.
		"empty list": {hub: nsdelManagedClusters(nsRbacChild, nsRbacSibling), spoke: nsdelManagedClusters() + labelled, mc: mcEmpty},
		// No CRD: the list is skipped, the labels are not.
		"no CRD": {spoke: labelled},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			topo.mutate = nsRbacTopoConfig
			for _, p := range runNsRbacTopo(t, topo) {
				assertNsRbacGate(t, p, byLabel, nil)
			}
		})
	}
	// The list fails: the regular template fails on every profile, so the ConfigurationPolicy keeps
	// its unresolved template and the controller applies nothing (no object, no status change).
	for name, c := range map[string]struct {
		mc   managedCluster
		crd  string
		want string
	}{
		"list forbidden": {mcForbidden, nsdelManagedClusters(), "forbidden"},
		// The CRD exists but serves no ManagedCluster API (not registered in discovery).
		"CRD without the API": {mcSeeded, nsdelManagedClusters(), "not installed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			profiles := runNsRbacTopo(t, nsRbacTopo{spoke: c.crd + nsStubs(nsRbacChild), mc: c.mc, mutate: nsRbacTopoConfig})
			for _, p := range profiles {
				if !strings.Contains(strings.ToLower(strings.Join(p.spokeWarns, " ")), c.want) {
					t.Errorf("%s: spoke warnings %v, want a %q lookup error", p.name, p.spokeWarns, c.want)
				}
				if p.objs != nil || !strings.Contains(p.resolved, "{{- $hub := ") {
					t.Errorf("%s: the regular template resolved after a failed lookup", p.name)
				}
			}
		})
	}
	// With nothing declared the list is not read, so a failing lookup cannot block an empty config.
	t.Run("no entries, no lookup", func(t *testing.T) {
		t.Parallel()
		for _, p := range runNsRbacTopo(t, nsRbacTopo{spoke: nsdelManagedClusters(), mc: mcForbidden, mutate: func(m map[string]interface{}) {
			for k := range m {
				delete(m, k)
			}
		}}) {
			requireResolved(t, p)
		}
	})
}

// No hub template reads anything but the rendered config in the policy namespace, so neither
// Policy needs a hub template service account.
func TestNamespaceRbac_HubTemplateReadsPolicyNamespaceOnly(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"manifests/namespace-rbac.yaml", "test/namespace-rbac.yaml"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), "policies", nsRbacPolicy, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "{{hub") {
				continue
			}
			for i := strings.Index(line, "lookup "); i >= 0; i = strings.Index(line, "lookup ") {
				if !strings.HasPrefix(line[i:], `lookup "v1" "ConfigMap" .PolicyMetadata.namespace `) {
					t.Errorf("%s: hub template lookup outside the policy namespace: %s", f, strings.TrimSpace(line))
				}
				line = line[i+len("lookup "):]
			}
		}
	}
}
