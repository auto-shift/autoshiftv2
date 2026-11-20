//go:build integration

package resolver

// Tests for stable/namespace-rbac. The policy grants Kubernetes permissions, so beyond rendering
// they pin its safety contract (policy README): every refusal is reported and renders nothing, other
// entries still apply, a foreign object is never taken over, nothing is ever deleted, user data
// reaches the managed cluster literally, and no token Secret is ever rendered.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	nsRbacPolicy = "stable/namespace-rbac"
	// PolicyGenerator names each ConfigurationPolicy after its Policy: policy-namespace-rbac
	// (manifests/) and policy-namespace-rbac-test (test/).
	nsRbacEnforceCP = "policy-namespace-rbac"
	nsRbacInformCP  = "policy-namespace-rbac-test"
	nsRbacStatus    = "namespace-rbac-status"
	nsRbacExampleNs = "example-team"
	// The status key of the informational token-Secret exception line.
	nsRbacExceptions = "token-secret-exceptions"
	nsRbacVault      = "example-vault"
)

// nsRbacHubStubs makes the managed cluster a hub: the ManagedCluster CRD, which the managed side
// requires before it lists ManagedClusters, and the Namespace of the ManagedCluster stub
// lint-cluster (tools/testdata/acs-managedclusters.yaml).
var nsRbacHubStubs = nsdelManagedClusters() + nsStubs("lint-cluster")

// nsStubs returns one Namespace stub per name, as a multi-document YAML string.
func nsStubs(names ...string) string {
	var docs []string
	for _, n := range names {
		docs = append(docs, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: "+n+"\n")
	}
	return strings.Join(docs, "---\n")
}

// runNsRbac runs stable/namespace-rbac against tools/testdata plus extra as one more stub file (""
// for none), with config.namespaceRbac of the example optionally mutated. It fails the test on any
// render or resolve error, and whenever a Secret is rendered.
func runNsRbac(t *testing.T, extra string, mutate func(nsMap map[string]interface{})) []onboardingObject {
	t.Helper()
	root := repoRoot(t)
	valuesDir := filepath.Join(root, "autoshift", "values")
	testdataDir := filepath.Join(root, "tools", "testdata")
	if extra != "" {
		testdataDir = withStubs(t, extra)
	}
	configs, err := ExtractExampleConfigs(valuesDir)
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	raw, _ := json.Marshal(configs)
	var cloned ExampleConfigs
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("clone configs: %v", err)
	}
	nsMap, ok := cloned.HubConfig["namespaceRbac"].(map[string]interface{})
	if !ok {
		t.Fatal("_example.yaml config.namespaceRbac is missing")
	}
	if mutate != nil {
		mutate(nsMap)
	}
	declared, err := labels.ExtractDeclaredFromTree(valuesDir, false)
	if err != nil {
		t.Fatalf("ExtractDeclaredFromTree: %v", err)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	cms, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	stubs, _ := LoadTestResources(testdataDir)
	seed := append(cms, stubs...)
	r, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	only := func(policy string) bool { return policy == nsRbacPolicy }
	_, results, err := runPipelineTransform(filepath.Join(root, "policies"), only, cachedRender, nil, ctx, nil, r, spokeR, declared, &cloned, testdataDir)
	if err != nil {
		t.Fatalf("runPipeline: %v", err)
	}
	for _, res := range results {
		if res.Policy != nsRbacPolicy {
			continue
		}
		if res.Err != nil || !res.ResolveOK || len(res.SpokeWarns) > 0 || len(res.YAMLErrors) > 0 {
			t.Fatalf("namespace-rbac did not resolve: err=%v warns=%v spoke=%v yaml=%v",
				res.Err, res.ResolveWarns, res.SpokeWarns, res.YAMLErrors)
		}
		objs := onboardingObjects(t, res)
		for _, o := range objs {
			if o.obj["kind"] == "Secret" && o.complianceType != "mustnothave" {
				assertTokenSecretShape(t, o)
			}
		}
		return objs
	}
	t.Fatal("namespace-rbac not in pipeline results")
	return nil
}

// nsRbacRendered returns the objects the enforce ConfigurationPolicy creates or keeps, keyed
// kind|namespace|name, the status ConfigMap excluded.
func nsRbacRendered(objs []onboardingObject) map[string]onboardingObject {
	out := map[string]onboardingObject{}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		ns, _ := meta["namespace"].(string)
		name, _ := meta["name"].(string)
		if o.configPolicy == nsRbacEnforceCP && o.complianceType != "mustnothave" && name != nsRbacStatus {
			out[fmt.Sprintf("%s|%s|%s", o.obj["kind"], ns, name)] = o
		}
	}
	return out
}

// nsRbacStale returns the mustnothave entries of configPolicy, keyed kind|namespace|name, the
// status ConfigMap excluded.
func nsRbacStale(objs []onboardingObject, configPolicy string) map[string]bool {
	out := map[string]bool{}
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		ns, _ := meta["namespace"].(string)
		name, _ := meta["name"].(string)
		if o.configPolicy == configPolicy && o.complianceType == "mustnothave" && name != nsRbacStatus {
			out[fmt.Sprintf("%s|%s|%s", o.obj["kind"], ns, name)] = true
		}
	}
	return out
}

// nsRbacStatusData returns the data of the status ConfigMap, or nil when the enforce
// ConfigurationPolicy asserts it absent.
func nsRbacStatusData(t *testing.T, objs []onboardingObject) map[string]string {
	t.Helper()
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.configPolicy == nsRbacEnforceCP && o.obj["kind"] == "ConfigMap" && meta["name"] == nsRbacStatus {
			if o.complianceType == "mustnothave" {
				return nil
			}
			if o.complianceType != "mustonlyhave" {
				t.Errorf("status ConfigMap complianceType = %s, want mustonlyhave", o.complianceType)
			}
			return stringMap(o.obj, "data")
		}
	}
	t.Fatal("no status ConfigMap entry in the enforce ConfigurationPolicy")
	return nil
}

func asJSONString(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rule is a PolicyRule as the config writes it.
func rule(apiGroups, resources, verbs, resourceNames []string) map[string]interface{} {
	toList := func(s []string) []interface{} {
		out := []interface{}{}
		for _, v := range s {
			out = append(out, v)
		}
		return out
	}
	r := map[string]interface{}{"apiGroups": toList(apiGroups), "resources": toList(resources), "verbs": toList(verbs)}
	if resourceNames != nil {
		r["resourceNames"] = toList(resourceNames)
	}
	return r
}

func exampleNs(nsMap map[string]interface{}) map[string]interface{} {
	return nsMap[nsRbacExampleNs].(map[string]interface{})
}

// assertExampleIntact: the _example objects still render, so one refusal never blocks the others.
func assertExampleIntact(t *testing.T, objs []onboardingObject) {
	t.Helper()
	got := nsRbacRendered(objs)
	for _, k := range []string{
		"ServiceAccount|example-team|example-ci",
		"Role|example-team|example-ci-builds",
		"RoleBinding|example-team|example-ci-builds",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s no longer rendered", k)
		}
	}
}

func TestNamespaceRbac_TemplatesInSync(t *testing.T) {
	t.Parallel()
	assertTemplatesInSync(t, nsRbacPolicy, "namespace-rbac.yaml")
}

// The inform-only stale reports live in their own Policy, policy-namespace-rbac-test, whose root is
// inform: bundled into the enforcing Policy, a root remediationAction would override the
// always-inform child and turn every stale report into a delete. Dry-run makes the enforcing
// Policy inform.
func TestNamespaceRbac_Remediation(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, remediation := range []string{"enforce", "inform"} {
		rendered, err := cachedKustomizeBuild(filepath.Join(root, "policies", nsRbacPolicy), remediation)
		if err != nil {
			t.Fatal(err)
		}
		assertEnforceTestSplit(t, rendered, remediation, splitPolicyDirs[nsRbacPolicy])
		got := map[string]string{}
		for _, doc := range splitYAMLDocuments(rendered) {
			var obj map[string]interface{}
			if sigsyaml.Unmarshal([]byte(doc), &obj) != nil || obj["kind"] != "Policy" {
				continue
			}
			for _, tpl := range obj["spec"].(map[string]interface{})["policy-templates"].([]interface{}) {
				def := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
				spec := def["spec"].(map[string]interface{})
				got[def["metadata"].(map[string]interface{})["name"].(string)], _ = spec["remediationAction"].(string)
				ei, _ := spec["evaluationInterval"].(map[string]interface{})
				if ei["compliant"] == nil || ei["noncompliant"] == nil {
					t.Errorf("%s: evaluationInterval needs compliant and noncompliant: %v", def["metadata"], ei)
				}
			}
		}
		want := map[string]string{nsRbacEnforceCP: remediation, nsRbacInformCP: "inform"}
		if asJSONString(t, got) != asJSONString(t, want) {
			t.Errorf("REMEDIATION=%s: ConfigurationPolicy remediation = %v, want %v", remediation, got, want)
		}
	}
}

// The _example entry: one ServiceAccount, one Role with exact rules, one RoleBinding, all labelled;
// the stale stubs reported by inform only; no status ConfigMap.
func TestNamespaceRbac_Example(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, "", nil)
	got := nsRbacRendered(objs)
	want := []string{
		"Role|example-team|example-ci-builds",
		"RoleBinding|example-team|example-ci-builds",
		"ServiceAccount|example-team|example-ci",
	}
	keys := []string{}
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(want)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("rendered %v, want %v", keys, want)
	}
	for k, o := range got {
		labels, _ := o.obj["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		if labels["autoshift.io/managed-by"] != "namespace-rbac" {
			t.Errorf("%s labels = %v", k, labels)
		}
	}
	if ct := got["ServiceAccount|example-team|example-ci"].complianceType; ct != "musthave" {
		t.Errorf("ServiceAccount complianceType = %s, want musthave", ct)
	}
	role := got["Role|example-team|example-ci-builds"]
	if role.complianceType != "mustonlyhave" {
		t.Errorf("Role complianceType = %s, want mustonlyhave", role.complianceType)
	}
	wantRules := []interface{}{
		rule([]string{"build.openshift.io"}, []string{"buildconfigs"}, []string{"get", "create", "patch"}, nil),
		rule([]string{""}, []string{"secrets"}, []string{"get"}, []string{"example-registry-token"}),
	}
	if asJSONString(t, role.obj["rules"]) != asJSONString(t, wantRules) {
		t.Errorf("Role rules = %s, want %s", asJSONString(t, role.obj["rules"]), asJSONString(t, wantRules))
	}
	rb := got["RoleBinding|example-team|example-ci-builds"]
	if rb.complianceType != "mustonlyhave" || rb.item["recreateOption"] != "IfRequired" {
		t.Errorf("RoleBinding complianceType = %s recreateOption = %v, want mustonlyhave IfRequired", rb.complianceType, rb.item["recreateOption"])
	}
	wantRef := map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "example-ci-builds"}
	if asJSONString(t, rb.obj["roleRef"]) != asJSONString(t, wantRef) {
		t.Errorf("roleRef = %v", rb.obj["roleRef"])
	}
	wantSubj := []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": "example-ci", "namespace": "example-team"}}
	if asJSONString(t, rb.obj["subjects"]) != asJSONString(t, wantSubj) {
		t.Errorf("subjects = %v", rb.obj["subjects"])
	}
	if data := nsRbacStatusData(t, objs); data != nil {
		t.Errorf("status ConfigMap written for a clean config: %v", data)
	}
	wantStale := map[string]bool{
		"ServiceAccount|example-team|example-old-ci": true,
		"Role|example-team|example-retired":          true,
		"RoleBinding|example-team|example-retired":   true,
	}
	if s := nsRbacStale(objs, nsRbacInformCP); asJSONString(t, s) != asJSONString(t, wantStale) {
		t.Errorf("stale report = %v, want %v", s, wantStale)
	}
	if s := nsRbacStale(objs, nsRbacEnforceCP); len(s) > 0 {
		t.Errorf("enforce ConfigurationPolicy has mustnothave entries (would delete): %v", s)
	}
}

// Every refusal of the safety gate: reported in the status ConfigMap under the namespace, nothing
// rendered for the refused object, and the _example objects still apply.
func TestNamespaceRbac_Refusals(t *testing.T) {
	t.Parallel()
	type tc struct {
		mutate  func(nsMap map[string]interface{})
		key     string // status key
		want    string // substring of the status message
		blocked string // kind|namespace|name that must not render ("" for none)
	}
	addRole := func(r map[string]interface{}) func(nsMap map[string]interface{}) {
		return func(nsMap map[string]interface{}) {
			exampleNs(nsMap)["roles"].(map[string]interface{})["x-bad"] = map[string]interface{}{"rules": []interface{}{r}}
		}
	}
	addBinding := func(b map[string]interface{}) func(nsMap map[string]interface{}) {
		return func(nsMap map[string]interface{}) {
			exampleNs(nsMap)["roleBindings"].(map[string]interface{})["x-bad"] = b
		}
	}
	addNs := func(ns string) func(nsMap map[string]interface{}) {
		return func(nsMap map[string]interface{}) {
			nsMap[ns] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-sa": true}}
		}
	}
	core, all := []string{""}, []string{"get"}
	badRole := "Role|example-team|x-bad"
	cases := map[string]tc{
		"wildcard apiGroup":       {addRole(rule([]string{"*"}, []string{"configmaps"}, all, nil)), nsRbacExampleNs, "wildcards are not allowed", badRole},
		"wildcard resource":       {addRole(rule(core, []string{"*"}, all, nil)), nsRbacExampleNs, "wildcards are not allowed", badRole},
		"wildcard subresource":    {addRole(rule(core, []string{"pods/*"}, all, nil)), nsRbacExampleNs, "wildcards are not allowed", badRole},
		"wildcard verb":           {addRole(rule(core, []string{"configmaps"}, []string{"*"}, nil)), nsRbacExampleNs, "wildcards are not allowed", badRole},
		"verb escalate":           {addRole(rule(core, []string{"configmaps"}, []string{"escalate"}, nil)), nsRbacExampleNs, "verbs escalate is never granted", badRole},
		"verb bind":               {addRole(rule(core, []string{"configmaps"}, []string{"bind"}, nil)), nsRbacExampleNs, "verbs bind is never granted", badRole},
		"verb impersonate":        {addRole(rule(core, []string{"serviceaccounts"}, []string{"Impersonate"}, []string{"x"})), nsRbacExampleNs, "verbs impersonate is never granted", badRole},
		"roles":                   {addRole(rule([]string{"rbac.authorization.k8s.io"}, []string{"roles"}, all, nil)), nsRbacExampleNs, "RBAC objects grant permissions", badRole},
		"rolebindings":            {addRole(rule([]string{"rbac.authorization.k8s.io"}, []string{"rolebindings"}, all, nil)), nsRbacExampleNs, "RBAC objects grant permissions", badRole},
		"clusterroles":            {addRole(rule([]string{"rbac.authorization.k8s.io"}, []string{"ClusterRoles"}, all, nil)), nsRbacExampleNs, "RBAC objects grant permissions", badRole},
		"clusterrolebindings":     {addRole(rule([]string{"rbac.authorization.k8s.io"}, []string{"clusterrolebindings"}, all, nil)), nsRbacExampleNs, "RBAC objects grant permissions", badRole},
		"rolebindingrestrictions": {addRole(rule([]string{"authorization.openshift.io"}, []string{"rolebindingrestrictions"}, all, nil)), nsRbacExampleNs, "RBAC objects grant permissions", badRole},
		"scc use":                 {addRole(rule([]string{"security.openshift.io"}, []string{"securitycontextconstraints"}, []string{"use"}, []string{"privileged"})), nsRbacExampleNs, "SCC use", badRole},
		"secrets unnamed":         {addRole(rule(core, []string{"secrets"}, all, nil)), nsRbacExampleNs, `"secrets" needs resourceNames`, badRole},
		"secrets empty names":     {addRole(rule(core, []string{"secrets"}, all, []string{})), nsRbacExampleNs, "resourceNames is not a non-empty list", badRole},
		"token unnamed":           {addRole(rule(core, []string{"serviceaccounts/token"}, []string{"create"}, nil)), nsRbacExampleNs, `"serviceaccounts/token" needs resourceNames`, badRole},
		"sa create unnamed":       {addRole(rule(core, []string{"serviceaccounts"}, []string{"create"}, nil)), nsRbacExampleNs, "with verb create needs resourceNames", badRole},
		"pods/exec":               {addRole(rule(core, []string{"pods/exec"}, []string{"create"}, nil)), nsRbacExampleNs, "reaches into running workloads", badRole},
		"pods/attach":             {addRole(rule(core, []string{"pods/attach"}, []string{"create"}, nil)), nsRbacExampleNs, "reaches into running workloads", badRole},
		"pods/portforward":        {addRole(rule(core, []string{"pods/portforward"}, []string{"create"}, nil)), nsRbacExampleNs, "reaches into running workloads", badRole},
		"pods/ephemeral":          {addRole(rule(core, []string{"pods/ephemeralcontainers"}, []string{"patch"}, nil)), nsRbacExampleNs, "reaches into running workloads", badRole},
		"services/proxy":          {addRole(rule(core, []string{"services/proxy"}, all, nil)), nsRbacExampleNs, "reaches into running workloads", badRole},
		"nodes":                   {addRole(rule(core, []string{"nodes"}, all, nil)), nsRbacExampleNs, "nodes are not namespaced", badRole},
		"nodes/proxy":             {addRole(rule(core, []string{"nodes/proxy"}, all, nil)), nsRbacExampleNs, "nodes are not namespaced", badRole},
		"builds/custom":           {addRole(rule([]string{"build.openshift.io"}, []string{"builds/custom"}, []string{"create"}, nil)), nsRbacExampleNs, "custom builds run privileged", badRole},
		"namespace patch":         {addRole(rule(core, []string{"namespaces"}, []string{"get", "patch"}, nil)), nsRbacExampleNs, "allows only get, list and watch, not patch", badRole},
		"project update":          {addRole(rule([]string{"project.openshift.io"}, []string{"projects"}, []string{"update"}, nil)), nsRbacExampleNs, "allows only get, list and watch, not update", badRole},
		"nonResourceURLs": {addRole(map[string]interface{}{"nonResourceURLs": []interface{}{"/metrics"}, "verbs": []interface{}{"get"}}),
			nsRbacExampleNs, `"nonResourceURLs" is not one of`, badRole},
		"empty rules": {func(m map[string]interface{}) {
			exampleNs(m)["roles"].(map[string]interface{})["x-bad"] = map[string]interface{}{"rules": []interface{}{}}
		}, nsRbacExampleNs, "rules is not a non-empty list", badRole},
		"role bad name": {func(m map[string]interface{}) {
			exampleNs(m)["roles"].(map[string]interface{})["X_Bad"] = map[string]interface{}{"rules": []interface{}{rule(core, []string{"configmaps"}, all, nil)}}
		}, nsRbacExampleNs, "not a valid DNS-1123 subdomain", "Role|example-team|X_Bad"},
		"sa bad name": {func(m map[string]interface{}) {
			exampleNs(m)["serviceAccounts"].(map[string]interface{})["Bad_SA"] = true
		}, nsRbacExampleNs, "not a valid DNS-1123 subdomain", "ServiceAccount|example-team|Bad_SA"},
		"sa not bool": {func(m map[string]interface{}) {
			exampleNs(m)["serviceAccounts"].(map[string]interface{})["x-sa"] = "yes"
		}, nsRbacExampleNs, "is not true, false, or a map with tokenSecret", "ServiceAccount|example-team|x-sa"},
		"binding undeclared role": {addBinding(map[string]interface{}{"role": "admin", "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}),
			nsRbacExampleNs, `role "admin" is not declared in roles`, "RoleBinding|example-team|x-bad"},
		"binding refused role": {func(m map[string]interface{}) {
			addRole(rule(core, []string{"secrets"}, all, nil))(m)
			addBinding(map[string]interface{}{"role": "x-bad", "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}})(m)
		}, nsRbacExampleNs, "role x-bad has errors", "RoleBinding|example-team|x-bad"},
		"binding platform subject": {addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"openshift-config/x": true}}),
			nsRbacExampleNs, "openshift-config is a platform namespace", "RoleBinding|example-team|x-bad"},
		"binding kube-system subject": {addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"kube-system/x": true}}),
			nsRbacExampleNs, "kube-system is a platform namespace", "RoleBinding|example-team|x-bad"},
		"binding default subject": {addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"example-team/default": true}}),
			nsRbacExampleNs, "the default ServiceAccount", "RoleBinding|example-team|x-bad"},
		"binding bad subject": {addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"example-ci": true}}),
			nsRbacExampleNs, "is not <namespace>/<serviceaccount>", "RoleBinding|example-team|x-bad"},
		"binding extra key": {addBinding(map[string]interface{}{"role": "example-ci-builds", "groups": []interface{}{"system:authenticated"}, "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}),
			nsRbacExampleNs, `key "groups" is not one of role, clusterRole, serviceAccounts`, "RoleBinding|example-team|x-bad"},
		"binding groups": {addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": []interface{}{"system:authenticated"}}),
			nsRbacExampleNs, "serviceAccounts is not a map", "RoleBinding|example-team|x-bad"},
	}
	for _, res := range []string{"manifestworks", "policies", "placementbindings", "placements", "managedclusteractions", "managedclusterviews", "ManifestWorks/status"} {
		cases["acm resource "+res] = tc{addRole(rule([]string{"work.open-cluster-management.io", "policy.open-cluster-management.io", "example.com"}, []string{res}, all, nil)),
			nsRbacExampleNs, "acts on managed clusters", badRole}
	}
	cases["sa default key"] = tc{func(m map[string]interface{}) {
		exampleNs(m)["serviceAccounts"].(map[string]interface{})["default"] = true
	},
		nsRbacExampleNs, `serviceAccounts."default" is the ServiceAccount of every pod`, "ServiceAccount|example-team|default"}
	cases["token for default"] = tc{addRole(rule(core, []string{"serviceaccounts/token"}, []string{"create"}, []string{"default"})),
		nsRbacExampleNs, `resourceNames "default": a token can only be requested`, badRole}
	cases["token for undeclared sa"] = tc{addRole(rule(core, []string{"serviceaccounts/token"}, []string{"create"}, []string{"builder"})),
		nsRbacExampleNs, `resourceNames "builder": a token can only be requested for a ServiceAccount set to true`, badRole}
	cases["token for false sa"] = tc{addRole(rule(core, []string{"serviceaccounts/token"}, []string{"create"}, []string{"example-old-ci"})),
		nsRbacExampleNs, `resourceNames "example-old-ci": a token can only be requested`, badRole}
	cases["subject in a ManagedCluster namespace"] = tc{addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"lint-cluster/x": true}}),
		nsRbacExampleNs, "lint-cluster is the namespace of a ManagedCluster", "RoleBinding|example-team|x-bad"}
	for _, ns := range []string{"clusters", "clusters-example-hosted"} {
		cases["subject in "+ns] = tc{addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{ns + "/x": true}}),
			nsRbacExampleNs, ns + " is a platform namespace", "RoleBinding|example-team|x-bad"}
	}
	cases["subject in policies namespace"] = tc{addBinding(map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"policies-autoshift/x": true}}),
		nsRbacExampleNs, "policies-autoshift is a platform namespace", "RoleBinding|example-team|x-bad"}
	// lint-cluster is a ManagedCluster stub (tools/testdata/acs-managedclusters.yaml); nsRbacHubStubs
	// makes the managed cluster a hub that lists it.
	cases["ManagedCluster namespace"] = tc{addNs("lint-cluster"), "lint-cluster", "is the namespace of the ManagedCluster lint-cluster", "ServiceAccount|lint-cluster|x-sa"}
	for _, ns := range []string{"openshift-config", "openshift", "kube-system", "kube-public", "default", "open-cluster-management-agent-addon", "multicluster-engine", "hive", "local-cluster", "policies-autoshift", "hypershift", "clusters", "clusters-example-hosted"} {
		cases["platform namespace "+ns] = tc{addNs(ns), ns, "is a platform namespace", "ServiceAccount|" + ns + "|x-sa"}
	}
	cases["namespace bad name"] = tc{addNs("Example_Team"), "Example_Team", "not a valid DNS-1123 label", "ServiceAccount|Example_Team|x-sa"}
	cases["namespace extra key"] = tc{func(m map[string]interface{}) {
		m["example-extra"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-sa": true}, "clusterRoles": map[string]interface{}{}}
	}, "example-extra", `key "clusterRoles" is not one of`, "ServiceAccount|example-extra|x-sa"}
	hubCases := map[string]bool{"ManagedCluster namespace": true, "subject in a ManagedCluster namespace": true}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			extra := ""
			if hubCases[name] {
				extra = nsRbacHubStubs
			}
			objs := runNsRbac(t, extra, c.mutate)
			if c.blocked != "" {
				if _, ok := nsRbacRendered(objs)[c.blocked]; ok {
					t.Errorf("%s rendered", c.blocked)
				}
			}
			if got := nsRbacStatusData(t, objs)[c.key]; !strings.Contains(got, c.want) {
				t.Errorf("status[%s] = %q, want it to contain %q", c.key, got, c.want)
			}
			assertExampleIntact(t, objs)
		})
	}
}

// Accepted on purpose: names on secrets and tokens, reads of the own namespace, and a resource
// that only resembles a denied one.
func TestNamespaceRbac_AllowedRules(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, "", func(nsMap map[string]interface{}) {
		exampleNs(nsMap)["roles"].(map[string]interface{})["x-ok"] = map[string]interface{}{"rules": []interface{}{
			rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"example-ci"}),
			rule([]string{""}, []string{"namespaces"}, []string{"get", "list", "watch"}, nil),
			rule([]string{""}, []string{"pods", "pods/log"}, []string{"get", "list"}, nil),
		}}
	})
	if data := nsRbacStatusData(t, objs); data != nil {
		t.Errorf("errors reported: %v", data)
	}
	if _, ok := nsRbacRendered(objs)["Role|example-team|x-ok"]; !ok {
		t.Error("Role x-ok not rendered")
	}
}

const nsRbacForeign = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-ci
  namespace: example-team
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: example-ci-builds
  namespace: example-team
  labels:
    autoshift.io/managed-by: someone-else
rules:
  - apiGroups: ['*']
    resources: ['*']
    verbs: ['*']
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example-foreign-rb
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-other
subjects:
  - kind: ServiceAccount
    name: example-other
    namespace: example-team
`

// Objects of a declared name that exist without the managed-by label are held and reported: never
// overwritten, and a RoleBinding to a held Role is not created (it would grant the foreign rules).
func TestNamespaceRbac_ForeignHold(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, nsRbacForeign, func(nsMap map[string]interface{}) {
		exampleNs(nsMap)["roles"].(map[string]interface{})["example-other"] = map[string]interface{}{
			"rules": []interface{}{rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil)}}
		exampleNs(nsMap)["roleBindings"].(map[string]interface{})["example-foreign-rb"] = map[string]interface{}{
			"role": "example-other", "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
	})
	got := nsRbacRendered(objs)
	for _, k := range []string{
		"ServiceAccount|example-team|example-ci",
		"Role|example-team|example-ci-builds",
		"RoleBinding|example-team|example-ci-builds",
		"RoleBinding|example-team|example-foreign-rb",
	} {
		if _, ok := got[k]; ok {
			t.Errorf("foreign or dependent object %s is rendered", k)
		}
	}
	if _, ok := got["Role|example-team|example-other"]; !ok {
		t.Error("Role example-other (no foreign object) not rendered")
	}
	status := nsRbacStatusData(t, objs)[nsRbacExampleNs]
	for _, want := range []string{
		"ServiceAccount example-team/example-ci already exists and is not managed by namespace-rbac",
		"Role example-team/example-ci-builds already exists and is not managed by namespace-rbac",
		"RoleBinding example-team/example-ci-builds is not changed: its Role example-ci-builds is held",
		"RoleBinding example-team/example-foreign-rb already exists and is not managed by namespace-rbac",
	} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q missing %q", status, want)
		}
	}
	if s := nsRbacStale(objs, nsRbacInformCP); s["RoleBinding|example-team|example-foreign-rb"] || s["Role|example-team|example-ci-builds"] {
		t.Errorf("foreign objects reported as stale: %v", s)
	}
}

// Removed, false and emptied entries: reported by the inform ConfigurationPolicy only, never a
// mustnothave in the enforce one. A refused entry stays declared, so it is never reported.
func TestNamespaceRbac_StaleNeverDeletes(t *testing.T) {
	t.Parallel()
	variants := map[string]struct {
		mutate func(nsMap map[string]interface{})
		stale  []string
		quiet  []string
	}{
		"namespace removed": {func(m map[string]interface{}) { delete(m, nsRbacExampleNs) },
			[]string{"ServiceAccount|example-team|example-ci", "Role|example-team|example-ci-builds", "RoleBinding|example-team|example-ci-builds"}, nil},
		"namespace null": {func(m map[string]interface{}) { m[nsRbacExampleNs] = nil },
			[]string{"ServiceAccount|example-team|example-ci", "Role|example-team|example-ci-builds", "RoleBinding|example-team|example-ci-builds"}, nil},
		"sa false": {func(m map[string]interface{}) {
			exampleNs(m)["serviceAccounts"].(map[string]interface{})["example-ci"] = false
		},
			[]string{"ServiceAccount|example-team|example-ci"}, []string{"RoleBinding|example-team|example-ci-builds"}},
		"last subject false": {func(m map[string]interface{}) {
			exampleNs(m)["roleBindings"].(map[string]interface{})["example-ci-builds"].(map[string]interface{})["serviceAccounts"] = map[string]interface{}{"example-team/example-ci": false}
		}, []string{"RoleBinding|example-team|example-ci-builds"}, []string{"Role|example-team|example-ci-builds"}},
		"refused role stays declared": {func(m map[string]interface{}) {
			exampleNs(m)["roles"].(map[string]interface{})["example-ci-builds"] = map[string]interface{}{"rules": []interface{}{rule([]string{"*"}, []string{"*"}, []string{"*"}, nil)}}
		}, nil, []string{"Role|example-team|example-ci-builds", "RoleBinding|example-team|example-ci-builds"}},
		"platform namespace held": {func(m map[string]interface{}) { m["openshift-x"] = map[string]interface{}{} }, nil, nil},
	}
	// Each case also runs against a managed copy of the example objects, so a removal is visible.
	managed := `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-ci
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: example-ci-builds
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
rules:
  - apiGroups: ['']
    resources: [configmaps]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example-ci-builds
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-ci-builds
subjects:
  - kind: ServiceAccount
    name: example-ci
    namespace: example-team
`
	for name, v := range variants {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, managed, v.mutate)
			for _, o := range objs {
				meta := o.obj["metadata"].(map[string]interface{})
				if o.complianceType == "mustnothave" && o.remediationAction != "inform" && meta["name"] != nsRbacStatus {
					t.Errorf("%s ConfigurationPolicy would delete %v %v", o.remediationAction, o.obj["kind"], meta["name"])
				}
			}
			stale := nsRbacStale(objs, nsRbacInformCP)
			for _, k := range v.stale {
				if !stale[k] {
					t.Errorf("%s not reported as stale: %v", k, stale)
				}
			}
			for _, k := range v.quiet {
				if stale[k] {
					t.Errorf("%s reported as stale, but it is still declared", k)
				}
			}
		})
	}
}

func TestNamespaceRbac_MissingNamespace(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, "", func(nsMap map[string]interface{}) {
		nsMap["example-absent"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-sa": true}}
	})
	if _, ok := nsRbacRendered(objs)["ServiceAccount|example-absent|x-sa"]; ok {
		t.Error("object rendered in a namespace that does not exist")
	}
	for _, o := range objs {
		if o.obj["kind"] == "Namespace" {
			t.Errorf("a Namespace is rendered: %v", o.obj["metadata"])
		}
	}
	if got := nsRbacStatusData(t, objs)["example-absent"]; !strings.Contains(got, "namespace example-absent does not exist") {
		t.Errorf("status = %q", got)
	}
	assertExampleIntact(t, objs)
}

// Template payloads, including the two that go-template-utils pre-processing matches inside quoted
// text, in every user-controlled string: refused with the payload reported literally, and the
// managed-side template never breaks (runNsRbac fails on any resolve error).
func TestNamespaceRbac_InjectionAndPreprocess(t *testing.T) {
	t.Parallel()
	payloads := append([]string{
		`"}} {{ fail "pwn" }} {{hub fail "pwn" hub}}\` + "\n",
		"{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}",
	}, preprocessPayloads...)
	for i, p := range payloads {
		cases := map[string]func(nsMap map[string]interface{}){
			"namespace": func(m map[string]interface{}) { m[p] = map[string]interface{}{} },
			"sa name":   func(m map[string]interface{}) { exampleNs(m)["serviceAccounts"].(map[string]interface{})[p] = true },
			"sa value":  func(m map[string]interface{}) { exampleNs(m)["serviceAccounts"].(map[string]interface{})["x-sa"] = p },
			"role name": func(m map[string]interface{}) {
				exampleNs(m)["roles"].(map[string]interface{})[p] = map[string]interface{}{"rules": []interface{}{rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil)}}
			},
			"apiGroup": func(m map[string]interface{}) {
				addRuleTo(m, rule([]string{p}, []string{"configmaps"}, []string{"get"}, nil))
			},
			"resource": func(m map[string]interface{}) { addRuleTo(m, rule([]string{""}, []string{p}, []string{"get"}, nil)) },
			"verb": func(m map[string]interface{}) {
				addRuleTo(m, rule([]string{""}, []string{"configmaps"}, []string{p}, nil))
			},
			"resourceName": func(m map[string]interface{}) {
				addRuleTo(m, rule([]string{""}, []string{"secrets"}, []string{"get"}, []string{p}))
			},
			"binding name": func(m map[string]interface{}) {
				exampleNs(m)["roleBindings"].(map[string]interface{})[p] = map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
			},
			"binding role": func(m map[string]interface{}) {
				exampleNs(m)["roleBindings"].(map[string]interface{})["x-rb"] = map[string]interface{}{"role": p, "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
			},
			"subject": func(m map[string]interface{}) {
				exampleNs(m)["roleBindings"].(map[string]interface{})["x-rb"] = map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{p: true}}
			},
		}
		for field, mutate := range cases {
			t.Run(fmt.Sprintf("%s/%d", field, i), func(t *testing.T) {
				objs := runNsRbac(t, "", mutate)
				data := nsRbacStatusData(t, objs)
				if data == nil {
					t.Fatalf("payload %q in %s neither refused nor reported", p, field)
				}
				all := asJSONString(t, data)
				if !strings.Contains(all, "template delimiters") {
					t.Errorf("payload %q in %s not refused as template delimiters: %s", p, field, all)
				}
				for _, o := range objs {
					if strings.Contains(asJSONString(t, o.obj), "pull-secret\":") || strings.Contains(asJSONString(t, o.obj), "pwn") && o.obj["kind"] != "ConfigMap" {
						t.Errorf("payload reached a rendered object: %v", o.obj)
					}
				}
				assertExampleIntact(t, objs)
			})
		}
		// Names that are refused before parsing are echoed literally in the status message.
		t.Run(fmt.Sprintf("echo/%d", i), func(t *testing.T) {
			objs := runNsRbac(t, "", func(m map[string]interface{}) { exampleNs(m)["serviceAccounts"].(map[string]interface{})[p] = true })
			if got := nsRbacStatusData(t, objs)[nsRbacExampleNs]; !strings.Contains(got, asJSONString(t, p)) {
				t.Errorf("status %q does not echo %q literally", got, p)
			}
		})
	}
}

func addRuleTo(nsMap map[string]interface{}, r map[string]interface{}) {
	exampleNs(nsMap)["roles"].(map[string]interface{})["x-inj"] = map[string]interface{}{"rules": []interface{}{r}}
}

// A secrets resourceName that names a ServiceAccount token or dockercfg Secret holds the Role (and
// its RoleBinding): reading it would hand out a ServiceAccount's credentials.
func TestNamespaceRbac_TokenSecretHeld(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Secret
metadata:
  name: example-sa-token
  namespace: example-team
  annotations:
    kubernetes.io/service-account.name: example-privileged
type: kubernetes.io/service-account-token
---
apiVersion: v1
kind: Secret
metadata:
  name: example-privileged-dockercfg-x1y2z
  namespace: example-team
  annotations:
    kubernetes.io/service-account.name: example-privileged
type: kubernetes.io/dockercfg
---
apiVersion: v1
kind: Secret
metadata:
  name: example-plain
  namespace: example-team
type: Opaque
`
	objs := runNsRbac(t, stub, func(nsMap map[string]interface{}) {
		roles := exampleNs(nsMap)["roles"].(map[string]interface{})
		rbs := exampleNs(nsMap)["roleBindings"].(map[string]interface{})
		for name, secret := range map[string]string{"x-tok": "example-sa-token", "x-dcfg": "example-privileged-dockercfg-x1y2z", "x-plain": "example-plain"} {
			roles[name] = map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"secrets"}, []string{"get"}, []string{"example-other", secret})}}
			rbs[name] = map[string]interface{}{"role": name, "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
		}
	})
	got := nsRbacRendered(objs)
	for _, k := range []string{"Role|example-team|x-tok", "RoleBinding|example-team|x-tok", "Role|example-team|x-dcfg", "RoleBinding|example-team|x-dcfg"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s rendered", k)
		}
	}
	for _, k := range []string{"Role|example-team|x-plain", "RoleBinding|example-team|x-plain"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s (Opaque Secret) not rendered", k)
		}
	}
	status := nsRbacStatusData(t, objs)[nsRbacExampleNs]
	for _, want := range []string{
		"Role example-team/x-tok is not created or changed: secrets resourceNames example-sa-token name ServiceAccount token or dockercfg Secrets",
		"Role example-team/x-dcfg is not created or changed: secrets resourceNames example-privileged-dockercfg-x1y2z name ServiceAccount token or dockercfg Secrets",
		"RoleBinding example-team/x-tok is not changed: its Role x-tok is held",
		"RoleBinding example-team/x-dcfg is not changed: its Role x-dcfg is held",
	} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q missing %q", status, want)
		}
	}
	if s := nsRbacStale(objs, nsRbacInformCP); s["Role|example-team|x-tok"] || s["Role|example-team|x-dcfg"] {
		t.Error("held Role reported as stale")
	}
	assertExampleIntact(t, objs)
}

// A serviceaccounts/token resourceName that names a declared ServiceAccount which already exists
// without the managed-by label (someone else's, possibly with other grants) holds the Role and its
// RoleBindings. A labelled ServiceAccount does not.
func TestNamespaceRbac_TokenForForeignServiceAccountHeld(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-operator
  namespace: example-team
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-owned
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
`
	objs := runNsRbac(t, stub, func(nsMap map[string]interface{}) {
		sas := exampleNs(nsMap)["serviceAccounts"].(map[string]interface{})
		roles := exampleNs(nsMap)["roles"].(map[string]interface{})
		rbs := exampleNs(nsMap)["roleBindings"].(map[string]interface{})
		for role, sa := range map[string]string{"x-mint-foreign": "example-operator", "x-mint-owned": "example-owned"} {
			sas[sa] = true
			roles[role] = map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{sa})}}
			for _, rb := range []string{role, role + "-2"} {
				rbs[rb] = map[string]interface{}{"role": role, "serviceAccounts": map[string]interface{}{"example-team/example-ci": true}}
			}
		}
	})
	got := nsRbacRendered(objs)
	for _, k := range []string{"Role|example-team|x-mint-foreign", "RoleBinding|example-team|x-mint-foreign", "RoleBinding|example-team|x-mint-foreign-2", "ServiceAccount|example-team|example-operator"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s rendered", k)
		}
	}
	for _, k := range []string{"Role|example-team|x-mint-owned", "RoleBinding|example-team|x-mint-owned", "RoleBinding|example-team|x-mint-owned-2", "ServiceAccount|example-team|example-owned"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s not rendered", k)
		}
	}
	status := nsRbacStatusData(t, objs)[nsRbacExampleNs]
	for _, want := range []string{
		"Role example-team/x-mint-foreign is not created or changed: serviceaccounts/token resourceNames example-operator name ServiceAccounts that are not managed by namespace-rbac",
		"RoleBinding example-team/x-mint-foreign is not changed: its Role x-mint-foreign is held",
		"RoleBinding example-team/x-mint-foreign-2 is not changed: its Role x-mint-foreign is held",
		"ServiceAccount example-team/example-operator already exists and is not managed by namespace-rbac",
	} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q missing %q", status, want)
		}
	}
	assertExampleIntact(t, objs)
}

// A subject whose namespace does not exist on the managed cluster is left out and reported, so no
// standing grant waits for whoever creates that namespace. With no subject left, the RoleBinding is
// not rendered at all.
func TestNamespaceRbac_SubjectNamespaceMissing(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, "", func(nsMap map[string]interface{}) {
		rbs := exampleNs(nsMap)["roleBindings"].(map[string]interface{})
		rbs["x-mixed"] = map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{
			"example-team/example-ci": true, "example-absent/squatter": true}}
		rbs["x-none"] = map[string]interface{}{"role": "example-ci-builds", "serviceAccounts": map[string]interface{}{
			"example-absent/squatter": true}}
	})
	got := nsRbacRendered(objs)
	mixed, ok := got["RoleBinding|example-team|x-mixed"]
	if !ok {
		t.Fatal("RoleBinding x-mixed not rendered")
	}
	want := []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": "example-ci", "namespace": "example-team"}}
	if asJSONString(t, mixed.obj["subjects"]) != asJSONString(t, want) {
		t.Errorf("x-mixed subjects = %s, want only example-team/example-ci", asJSONString(t, mixed.obj["subjects"]))
	}
	if _, ok := got["RoleBinding|example-team|x-none"]; ok {
		t.Error("RoleBinding x-none rendered with no existing subject namespace")
	}
	status := nsRbacStatusData(t, objs)[nsRbacExampleNs]
	for _, w := range []string{
		"RoleBinding example-team/x-mixed: subject example-absent/squatter is left out, because namespace example-absent does not exist",
		"RoleBinding example-team/x-none is not created or changed: no subject namespace exists",
	} {
		if !strings.Contains(status, w) {
			t.Errorf("status %q missing %q", status, w)
		}
	}
}

// The hub templates read only the policy namespace (TestNamespaceRbac_HubTemplateReadsPolicyNamespaceOnly),
// so neither Policy runs them as a service account: one would widen what a hub template can read.
func TestNamespaceRbac_HubTemplateServiceAccount(t *testing.T) {
	t.Parallel()
	rendered, err := KustomizeBuildWithRemediation(filepath.Join(repoRoot(t), "policies", nsRbacPolicy), "enforce")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(rendered, "\nkind: Policy\n"); n != 2 {
		t.Fatalf("%d Policies rendered, want 2", n)
	}
	if strings.Contains(rendered, "hubTemplateOptions") || strings.Contains(rendered, "serviceAccountName") {
		t.Error("a Policy sets hubTemplateOptions.serviceAccountName")
	}
}

// assertTokenSecretShape: the only Secret namespace-rbac may render is the approved token Secret
// exception, musthave, type service-account-token, bound to a ServiceAccount by annotation, and
// never carrying a token or any other data (the cluster fills it in).
func assertTokenSecretShape(t *testing.T, o onboardingObject) {
	t.Helper()
	meta, _ := o.obj["metadata"].(map[string]interface{})
	ann, _ := meta["annotations"].(map[string]interface{})
	for _, k := range []string{"data", "stringData", "immutable"} {
		if _, ok := o.obj[k]; ok {
			t.Errorf("Secret %v renders %s: namespace-rbac must never put token data in a Secret", meta["name"], k)
		}
	}
	if o.obj["type"] != "kubernetes.io/service-account-token" || o.complianceType != "musthave" || o.configPolicy != nsRbacEnforceCP {
		t.Errorf("Secret %v: type %v complianceType %s in %s, want a musthave service-account-token in the enforce ConfigurationPolicy",
			meta["name"], o.obj["type"], o.complianceType, o.configPolicy)
	}
	if s, _ := ann["kubernetes.io/service-account.name"].(string); s == "" || len(ann) != 1 {
		t.Errorf("Secret %v annotations = %v, want only kubernetes.io/service-account.name", meta["name"], ann)
	}
}

// minterSetup declares, in example-vault, the ServiceAccount x-minter with a token Secret, and in
// example-team the minter-only Role x-mint (TokenRequest and get for example-ci only) bound to it.

// vaultNs returns the example-vault entry, vaultSAs its serviceAccounts.
func vaultNs(nsMap map[string]interface{}) map[string]interface{} {
	return nsMap[nsRbacVault].(map[string]interface{})
}

func vaultSAs(nsMap map[string]interface{}) map[string]interface{} {
	return vaultNs(nsMap)["serviceAccounts"].(map[string]interface{})
}
func minterSetup(nsMap map[string]interface{}) {
	nsMap[nsRbacVault] = map[string]interface{}{"serviceAccounts": map[string]interface{}{
		"x-minter": map[string]interface{}{"tokenSecret": "x-minter-token"}}}
	ns := exampleNs(nsMap)
	ns["roles"].(map[string]interface{})["x-mint"] = map[string]interface{}{"rules": []interface{}{
		rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"example-ci"}),
		rule([]string{""}, []string{"serviceaccounts"}, []string{"get"}, []string{"example-ci"}),
	}}
	ns["roleBindings"].(map[string]interface{})["x-mint"] = map[string]interface{}{"role": "x-mint",
		"serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}
}

const tokenSecretKey = "Secret|example-vault|x-minter-token"

// The approved exception: a long-lived token Secret for a minter-only ServiceAccount, reported as
// an informational line that does not make the Policy NonCompliant.
func TestNamespaceRbac_TokenSecretException(t *testing.T) {
	t.Parallel()
	objs := runNsRbac(t, "", minterSetup)
	got := nsRbacRendered(objs)
	for _, k := range []string{tokenSecretKey, "ServiceAccount|example-vault|x-minter", "Role|example-team|x-mint", "RoleBinding|example-team|x-mint"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s not rendered", k)
		}
	}
	data := nsRbacStatusData(t, objs)
	if len(data) != 1 || !strings.Contains(data[nsRbacExceptions], "Secret example-vault/x-minter-token holds a long-lived token of the minter-only ServiceAccount x-minter") {
		t.Errorf("status = %v, want only the exception line", data)
	}
	for _, o := range objs {
		meta := o.obj["metadata"].(map[string]interface{})
		if o.configPolicy == nsRbacInformCP && meta["name"] == nsRbacStatus && o.complianceType != "musthave" {
			t.Errorf("with only an exception the inform ConfigurationPolicy asserts the status ConfigMap %s, want musthave", o.complianceType)
		}
	}
	assertExampleIntact(t, objs)
}

// Every way the exception guard refuses: the token Secret is not rendered and the reason is
// reported, while the ServiceAccount itself and every other entry still apply.
func TestNamespaceRbac_TokenSecretGuard(t *testing.T) {
	t.Parallel()
	type tc struct {
		stub   string
		mutate func(nsMap map[string]interface{})
		key    string
		want   string
		// blocked objects (kind|namespace|name) that would widen the token's rights: never rendered.
		blocked []string
		// alsoWant: more status substrings, keyed by status key.
		alsoWant map[string]string
		// secretOK: the token Secret itself is still legitimately rendered (the widening object is
		// refused instead); exampleBroken: the case deliberately binds the minter to the example Role.
		secretOK, exampleBroken bool
	}
	cases := map[string]tc{
		"bound to a non-minter Role": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roleBindings"].(map[string]interface{})["example-ci-builds"].(map[string]interface{})["serviceAccounts"].(map[string]interface{})["example-vault/x-minter"] = true
		}, key: nsRbacVault, want: `RoleBinding example-team/example-ci-builds binds it to Role "example-ci-builds", which is not minter-only`,
			blocked:       []string{"RoleBinding|example-team|example-ci-builds", "Role|example-team|example-ci-builds"},
			alsoWant:      map[string]string{nsRbacExampleNs: `serviceAccounts.example-vault/x-minter has a tokenSecret, so it can only be bound to a minter-only Role, and "example-ci-builds" is not one`},
			exampleBroken: true},
		"bound from another namespace": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			m["example-ci"] = map[string]interface{}{
				"roles": map[string]interface{}{"x-read": map[string]interface{}{"rules": []interface{}{rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil)}}},
				"roleBindings": map[string]interface{}{"x-read": map[string]interface{}{"role": "x-read",
					"serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}}}
		}, key: nsRbacVault, want: `RoleBinding example-ci/x-read binds it to Role "x-read", which is not minter-only`,
			blocked:  []string{"RoleBinding|example-ci|x-read", "Role|example-ci|x-read"},
			alsoWant: map[string]string{"example-ci": `roles."x-read": it is bound to example-vault/x-minter, which has a tokenSecret, so it must be minter-only`}},
		"minter Role gains a non-minter rule": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			r := exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})
			r["rules"] = append(r["rules"].([]interface{}), rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil))
		}, key: nsRbacVault, want: `RoleBinding example-team/x-mint binds it to Role "x-mint", which is not minter-only`,
			blocked:  []string{"Role|example-team|x-mint", "RoleBinding|example-team|x-mint"},
			alsoWant: map[string]string{nsRbacExampleNs: `roles."x-mint": it is bound to example-vault/x-minter, which has a tokenSecret`}},
		"refused tokenSecret still blocks widening": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = map[string]interface{}{"tokenSecret": "Bad_Name"}
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-wide"] = map[string]interface{}{"role": "example-ci-builds",
				"serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}
		}, key: nsRbacVault, want: "tokenSecret is not a valid DNS-1123 subdomain",
			blocked:       []string{"RoleBinding|example-team|x-wide", "Role|example-team|example-ci-builds"},
			alsoWant:      map[string]string{nsRbacExampleNs: `serviceAccounts.example-vault/x-minter has a tokenSecret, so it can only be bound to a minter-only Role`},
			exampleBroken: true},
		"a Role reads the tokenSecret": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-read": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"secrets"}, []string{"get"}, []string{"x-minter-token"})}}}
		}, key: nsRbacVault, want: `resourceNames "x-minter-token" is the tokenSecret of a ServiceAccount, which no Role may read`,
			blocked:  []string{"Role|example-vault|x-read"},
			alsoWant: map[string]string{nsRbacVault: "Roles x-read of namespace example-vault can read Secret x-minter-token"}},
		"a Role mints for the token ServiceAccount": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-self": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"x-minter"})}}}
		}, key: nsRbacVault, want: `resourceNames "x-minter": the ServiceAccount has a tokenSecret, and no Role may mint tokens for it`,
			blocked: []string{"Role|example-vault|x-self"}, secretOK: true},
		"the minter mints for itself": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"x-minter"})}
		}, key: nsRbacVault, want: `binds it to Role "x-mint", which is not minter-only`,
			blocked: []string{"Role|example-team|x-mint", "RoleBinding|example-team|x-mint"}},
		"live token Secret after tokenSecret removal": {stub: `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`, mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = true
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-wide"] = map[string]interface{}{"role": "example-ci-builds",
				"serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}
		}, key: nsRbacExampleNs, want: "RoleBinding example-team/x-wide is not created or changed: subject example-vault/x-minter has a long-lived token Secret managed by namespace-rbac, and Role example-ci-builds is not minter-only",
			blocked: []string{"RoleBinding|example-team|x-wide"}},
		// Ruling 2: live RoleBindings that name the minter are probed before the Secret renders.
		"live unlabelled RoleBinding to a non-minter Role": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-probe
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-ci-builds
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault,
			want: "live objects widen ServiceAccount x-minter beyond a minter-only Role that namespace-rbac renders: RoleBinding example-team/x-probe (Role example-ci-builds)"},
		"live RoleBinding to a ClusterRole": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-probe
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "example-team/x-probe (ClusterRole admin)"},
		"live labelled RoleBinding outside config namespaces": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-old
  namespace: example-elsewhere
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "example-elsewhere/x-old (ClusterRole edit)"},
		"live RoleBinding to a held minter Role": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-mint
  namespace: example-team
rules:
  - apiGroups: ['*']
    resources: ['*']
    verbs: ['*']
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-mint
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-mint
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "example-team/x-mint (Role x-mint)",
			alsoWant: map[string]string{nsRbacExampleNs: "Role example-team/x-mint already exists and is not managed by namespace-rbac"},
			blocked:  []string{"Role|example-team|x-mint", "RoleBinding|example-team|x-mint"}},
		// Ruling 2, hub: any part of the config that cannot be inspected refuses every tokenSecret.
		"subjects as a list": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-list"] = map[string]interface{}{"role": "example-ci-builds",
				"serviceAccounts": []interface{}{"example-vault/x-minter"}}
		}, key: nsRbacVault, want: `config.namespaceRbac cannot be fully inspected (namespaceRbac."example-team".roleBindings."x-list".serviceAccounts is not a map)`,
			blocked: []string{"RoleBinding|example-team|x-list"}},
		"namespace entry a string": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			m["example-ci"] = "example-vault/x-minter"
		}, key: nsRbacVault, want: `cannot be fully inspected (namespaceRbac."example-ci" is not a map)`},
		"roleBindings not a map": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			m["example-ci"] = map[string]interface{}{"roleBindings": []interface{}{map[string]interface{}{"role": "x", "serviceAccounts": map[string]interface{}{"example-vault/x-minter": true}}}}
		}, key: nsRbacVault, want: `cannot be fully inspected (namespaceRbac."example-ci".roleBindings is not a map)`},
		// Ruling 3: workload rules are refused where a token Secret lives.
		"config Role creates pods next to the token": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-run": map[string]interface{}{"rules": []interface{}{
				rule([]string{""}, []string{"pods"}, []string{"get", "create"}, nil)}}}
		}, key: nsRbacVault, want: "Roles x-run of namespace example-vault create or change workloads, which could mount the token Secret"},
		"config Role patches deployments next to the token": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-run": map[string]interface{}{"rules": []interface{}{
				rule([]string{"apps"}, []string{"Deployments"}, []string{"patch"}, nil)}}}
		}, key: nsRbacVault, want: "Roles x-run of namespace example-vault create or change workloads, which could mount the token Secret"},
		"config Role instantiates buildconfigs next to the token": {mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultNs(m)["roles"] = map[string]interface{}{"x-run": map[string]interface{}{"rules": []interface{}{
				rule([]string{"build.openshift.io"}, []string{"buildconfigs/instantiate"}, []string{"create"}, nil)}}}
		}, key: nsRbacVault, want: "Roles x-run of namespace example-vault create or change workloads, which could mount the token Secret"},
		// N1: live Roles in the token namespace, whatever their origin and order.
		"live labelled Role creates pods (P3)": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-old-run
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
rules:
  - apiGroups: ['']
    resources: [pods]
    verbs: [create]
`, mutate: minterSetup, key: nsRbacVault, want: "Role example-vault/x-old-run creates or changes pods"},
		"live Role reads the token Secret (P3b)": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-reader
  namespace: example-vault
rules:
  - apiGroups: ['']
    resources: [secrets]
    verbs: [get]
    resourceNames: [x-minter-token]
`, mutate: minterSetup, key: nsRbacVault, want: "Role example-vault/x-reader can read Secret x-minter-token"},
		"live Role reads every Secret": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-reader
  namespace: example-vault
rules:
  - apiGroups: ['']
    resources: [secrets]
    verbs: [list]
`, mutate: minterSetup, key: nsRbacVault, want: "Role example-vault/x-reader can read Secret x-minter-token"},
		// N2: the User form of a ServiceAccount is the same identity.
		"live RoleBinding with the User form (P5)": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-user
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-ci-builds
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:serviceaccount:example-vault:x-minter
`, mutate: minterSetup, key: nsRbacVault, want: "RoleBinding example-team/x-user (Role example-ci-builds)"},
		"hand-made User-form RoleBinding to ClusterRole admin": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-user-admin
  namespace: example-unrelated
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:serviceaccount:example-vault:x-minter
`, mutate: minterSetup, key: nsRbacVault, want: "RoleBinding example-unrelated/x-user-admin (ClusterRole admin)"},
		"live Role instantiates every buildconfig subresource": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-any-sub
  namespace: example-vault
rules:
  - apiGroups: [build.openshift.io]
    resources: ['*/instantiate']
    verbs: [create]
`, mutate: minterSetup, key: nsRbacVault, want: "Role example-vault/x-any-sub creates or changes */instantiate"},
		// CRB scan and the cluster-wide RoleBinding scan.
		"ClusterRoleBinding names the ServiceAccount": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: x-crb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "ClusterRoleBinding x-crb (ClusterRole view)"},
		"ClusterRoleBinding with the User form": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: x-crb-user
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:serviceaccount:example-vault:x-minter
`, mutate: minterSetup, key: nsRbacVault, want: "ClusterRoleBinding x-crb-user (ClusterRole view)"},
		"hand-made RoleBinding in an unrelated namespace": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-hand
  namespace: example-unrelated
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: edit
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "RoleBinding example-unrelated/x-hand (ClusterRole edit)"},
		// Ruling: a group such as system:serviceaccounts:<ns> is not treated as the ServiceAccount.
		"group binding is not a match": {stub: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: system:image-pullers
  namespace: example-unrelated
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:image-puller
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: Group
    name: system:serviceaccounts:example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "", secretOK: true},
		"minter names a non-owned ServiceAccount": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"builder"})}
		}, key: nsRbacVault, want: "which is not minter-only"},
		"minter names a false ServiceAccount": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"example-old-ci"})}
		}, key: nsRbacVault, want: "which is not minter-only"},
		"minter with an extra verb": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{""}, []string{"serviceaccounts"}, []string{"get", "list"}, []string{"example-ci"})}
		}, key: nsRbacVault, want: "which is not minter-only"},
		"minter with another resource": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{""}, []string{"serviceaccounts/token", "configmaps"}, []string{"create"}, []string{"example-ci"})}
		}, key: nsRbacVault, want: "which is not minter-only"},
		"minter in another API group": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})["rules"] = []interface{}{
				rule([]string{"example.com"}, []string{"serviceaccounts/token"}, []string{"create"}, []string{"example-ci"})}
		}, key: nsRbacVault, want: "which is not minter-only"},
		"binding to an undeclared Role": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-mint"].(map[string]interface{})["role"] = "admin"
		}, key: nsRbacVault, want: `binds it to Role "admin", which is not minter-only`},
		"duplicate tokenSecret": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter-2"] = map[string]interface{}{"tokenSecret": "x-minter-token"}
		}, key: nsRbacVault, want: "is the tokenSecret of more than one ServiceAccount: x-minter, x-minter-2"},
		"tokenSecret not a string": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = map[string]interface{}{"tokenSecret": 5}
		}, key: nsRbacVault, want: "tokenSecret is not a string"},
		"tokenSecret invalid name": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = map[string]interface{}{"tokenSecret": "Bad_Name"}
		}, key: nsRbacVault, want: "tokenSecret is not a valid DNS-1123 subdomain"},
		"tokenSecret delimiters": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = map[string]interface{}{"tokenSecret": "x: {{ y | toInt"}
		}, key: nsRbacVault, want: "tokenSecret contains template delimiters"},
		"extra key": {stub: "", mutate: func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = map[string]interface{}{"tokenSecret": "x-minter-token", "data": "abc"}
		}, key: nsRbacVault, want: `key "data" is not tokenSecret`},
		"platform namespace": {stub: "", mutate: func(m map[string]interface{}) {
			m["openshift-config"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{"x-minter": map[string]interface{}{"tokenSecret": "x-minter-token"}}}
		}, key: "openshift-config", want: "is a platform namespace"},
		"foreign existing Secret": {stub: `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
type: Opaque
`, mutate: minterSetup, key: nsRbacVault, want: "Secret example-vault/x-minter-token already exists and is not managed by namespace-rbac"},
		"labelled Secret of another ServiceAccount": {stub: `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: example-ci
type: kubernetes.io/service-account-token
`, mutate: minterSetup, key: nsRbacVault, want: "is managed by namespace-rbac but is not the token Secret of ServiceAccount x-minter",
		},
		"foreign ServiceAccount": {stub: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: x-minter
  namespace: example-vault
`, mutate: minterSetup, key: nsRbacVault, want: "token Secret example-vault/x-minter-token is not created or changed: its ServiceAccount x-minter is not managed by namespace-rbac"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, c.stub, c.mutate)
			got := nsRbacRendered(objs)
			if _, ok := got[tokenSecretKey]; ok != c.secretOK {
				t.Errorf("token Secret rendered = %v, want %v", ok, c.secretOK)
			}
			for _, k := range c.blocked {
				if _, ok := got[k]; ok {
					t.Errorf("%s rendered: it would widen the long-lived token's rights", k)
				}
			}
			data := nsRbacStatusData(t, objs)
			if got := data[c.key]; c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("status[%s] = %q, want it to contain %q", c.key, got, c.want)
			}
			for k, w := range c.alsoWant {
				if !strings.Contains(data[k], w) {
					t.Errorf("status[%s] = %q, want it to contain %q", k, data[k], w)
				}
			}
			if !c.secretOK && data[nsRbacExceptions] != "" {
				t.Errorf("exception reported for a refused token Secret: %q", data[nsRbacExceptions])
			}
			if !c.exampleBroken {
				assertExampleIntact(t, objs)
			}
		})
	}
}

// A labelled token Secret that is no longer declared is reported by the inform ConfigurationPolicy
// (its token stays valid until deleted), never deleted; a refused one is reported too.
func TestNamespaceRbac_TokenSecretStale(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`
	for name, mutate := range map[string]func(m map[string]interface{}){
		"declared": minterSetup,
		"removed": func(m map[string]interface{}) {
			minterSetup(m)
			vaultSAs(m)["x-minter"] = true
		},
		"refused": func(m map[string]interface{}) {
			minterSetup(m)
			exampleNs(m)["roleBindings"].(map[string]interface{})["x-mint"].(map[string]interface{})["role"] = "example-ci-builds"
		},
	} {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, stub, mutate)
			stale := nsRbacStale(objs, nsRbacInformCP)[tokenSecretKey]
			if stale != (name != "declared") {
				t.Errorf("stale report of the token Secret = %v", stale)
			}
			if nsRbacStale(objs, nsRbacEnforceCP)[tokenSecretKey] {
				t.Error("the enforce ConfigurationPolicy would delete the token Secret")
			}
		})
	}
}

// Ruling 1, the removal path: the tokenSecret is removed from the values but its Secret (and token)
// is still live, and the minter Role is then broadened. The broadened Role must not render (the live
// one keeps its minter rules) and the RoleBinding stays as it is; nothing is deleted, and the live
// token Secret is reported as stale.
func TestNamespaceRbac_TokenRemovalRoleHeld(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: x-mint
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
rules:
  - apiGroups: ['']
    resources: [serviceaccounts/token]
    verbs: [create]
    resourceNames: [example-ci]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-mint
  namespace: example-team
  labels:
    autoshift.io/managed-by: namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-mint
subjects:
  - kind: ServiceAccount
    name: x-minter
    namespace: example-vault
`
	for name, broaden := range map[string]func(r map[string]interface{}){
		"extra rule": func(r map[string]interface{}) {
			r["rules"] = append(r["rules"].([]interface{}), rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil))
		},
		"rules replaced": func(r map[string]interface{}) {
			r["rules"] = []interface{}{rule([]string{"build.openshift.io"}, []string{"builds"}, []string{"get"}, nil)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			objs := runNsRbac(t, stub, func(m map[string]interface{}) {
				minterSetup(m)
				vaultSAs(m)["x-minter"] = true
				broaden(exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{}))
			})
			got := nsRbacRendered(objs)
			for _, k := range []string{"Role|example-team|x-mint", "RoleBinding|example-team|x-mint", tokenSecretKey} {
				if _, ok := got[k]; ok {
					t.Errorf("%s rendered: the live object would change", k)
				}
			}
			status := nsRbacStatusData(t, objs)[nsRbacExampleNs]
			for _, w := range []string{
				"Role example-team/x-mint is not created or changed: it is not minter-only, and it is bound to example-vault/x-minter (RoleBinding x-mint), which has a long-lived token Secret managed by namespace-rbac",
				"RoleBinding example-team/x-mint is not changed: its Role x-mint is held",
			} {
				if !strings.Contains(status, w) {
					t.Errorf("status %q missing %q", status, w)
				}
			}
			for k := range nsRbacStale(objs, nsRbacEnforceCP) {
				t.Errorf("the enforce ConfigurationPolicy would delete %s", k)
			}
			stale := nsRbacStale(objs, nsRbacInformCP)
			if !stale[tokenSecretKey] {
				t.Errorf("the live token Secret is not reported: %v", stale)
			}
			if stale["Role|example-team|x-mint"] || stale["RoleBinding|example-team|x-mint"] {
				t.Errorf("held objects reported as stale: %v", stale)
			}
			assertExampleIntact(t, objs)
		})
	}
}

// N1, order independence: the token Secret is already live, and a Role that creates workloads is
// added to its namespace later. The tokenSecret is refused on the hub, and the Role is held on the
// managed cluster while the live token Secret exists; nothing is deleted and the Secret is reported.
func TestNamespaceRbac_WorkloadRoleNextToLiveToken(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
`
	objs := runNsRbac(t, stub, func(m map[string]interface{}) {
		minterSetup(m)
		vaultNs(m)["roles"] = map[string]interface{}{"x-run": map[string]interface{}{"rules": []interface{}{
			rule([]string{""}, []string{"pods"}, []string{"create"}, nil)}}}
	})
	got := nsRbacRendered(objs)
	for _, k := range []string{"Role|example-vault|x-run", tokenSecretKey} {
		if _, ok := got[k]; ok {
			t.Errorf("%s rendered", k)
		}
	}
	status := nsRbacStatusData(t, objs)[nsRbacVault]
	for _, w := range []string{
		"tokenSecret x-minter-token is refused: Roles x-run of namespace example-vault create or change workloads",
		"Role example-vault/x-run is not created or changed: it creates or changes workloads in a namespace that holds long-lived token Secrets of x-minter",
	} {
		if !strings.Contains(status, w) {
			t.Errorf("status %q missing %q", status, w)
		}
	}
	for k := range nsRbacStale(objs, nsRbacEnforceCP) {
		t.Errorf("the enforce ConfigurationPolicy would delete %s", k)
	}
	if !nsRbacStale(objs, nsRbacInformCP)[tokenSecretKey] {
		t.Error("the live token Secret is not reported")
	}
}

// P1e: the removal path, with the live RoleBinding naming the minter in its User form.
func TestNamespaceRbac_TokenRemovalRoleHeldUserForm(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Secret
metadata:
  name: x-minter-token
  namespace: example-vault
  labels:
    autoshift.io/managed-by: namespace-rbac
  annotations:
    kubernetes.io/service-account.name: x-minter
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: x-user-bind
  namespace: example-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: x-mint
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:serviceaccount:example-vault:x-minter
`
	objs := runNsRbac(t, stub, func(m map[string]interface{}) {
		minterSetup(m)
		vaultSAs(m)["x-minter"] = true
		r := exampleNs(m)["roles"].(map[string]interface{})["x-mint"].(map[string]interface{})
		r["rules"] = append(r["rules"].([]interface{}), rule([]string{""}, []string{"configmaps"}, []string{"get"}, nil))
	})
	if _, ok := nsRbacRendered(objs)["Role|example-team|x-mint"]; ok {
		t.Error("broadened Role x-mint rendered while a live User-form binding names a token holder")
	}
	if w := "it is bound to example-vault/x-minter (RoleBinding x-user-bind)"; !strings.Contains(nsRbacStatusData(t, objs)[nsRbacExampleNs], w) {
		t.Errorf("status missing %q: %v", w, nsRbacStatusData(t, objs))
	}
	for k := range nsRbacStale(objs, nsRbacEnforceCP) {
		t.Errorf("the enforce ConfigurationPolicy would delete %s", k)
	}
}
