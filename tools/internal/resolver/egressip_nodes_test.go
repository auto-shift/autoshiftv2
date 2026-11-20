//go:build integration

package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

const egressIPNodesPolicy = "stable/egressip-nodes"

// The ConfigurationPolicies of the directory, each named after its Policy: the node labels plus
// status ConfigMap (policy-egressip-nodes), and the inform assertion that the status ConfigMap is
// absent (policy-egressip-nodes-test).
const (
	egressEnforceCP = "policy-egressip-nodes"
	egressInformCP  = "policy-egressip-nodes-test"
	egressStatus    = "egressip-nodes-status"
	egressLabel     = "k8s.ovn.org/egress-assignable"
)

// Nodes in tools/testdata/cluster-nodes.yaml.
const (
	egressWorker0 = "worker-0.test-cluster.test.example.com"
	egressWorker1 = "worker-1.test-cluster.test.example.com"
	egressWorker2 = "worker-2.test-cluster.test.example.com"
)

// egressTemplate is one resolved ConfigurationPolicy of stable/egressip-nodes.
type egressTemplate struct {
	name        string
	remediation string
	hubErr      string // hub template error, empty when it resolved
	hubResolved string // the Policy after the hub pass, before the managed-cluster pass
	items       []map[string]interface{}
}

// runEgressIPNodes renders stable/egressip-nodes with config.egressIPNodes replaced by nodes (the
// example config when nodes is nil), against tools/testdata plus extraStubs, and resolves each
// policy template on its own: the RHACM propagator resolves hub templates per policy template.
func runEgressIPNodes(t *testing.T, remediation string, nodes interface{}, extraStubs string) []egressTemplate {
	t.Helper()
	root := repoRoot(t)
	configs, err := ExtractExampleConfigs(filepath.Join(root, "autoshift", "values"))
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	raw, _ := json.Marshal(configs)
	var cloned ExampleConfigs
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("clone configs: %v", err)
	}
	if _, ok := cloned.HubConfig["egressIPNodes"]; !ok {
		t.Fatal("_example.yaml config.egressIPNodes is missing")
	}
	if nodes != nil {
		cloned.HubConfig["egressIPNodes"] = nodes
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: map[string]string{}}
	cms, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
	}
	testdataDir := filepath.Join(root, "tools", "testdata")
	if extraStubs != "" {
		testdataDir = egressStubDir(t, testdataDir, extraStubs)
	}
	testResources, err := LoadTestResources(testdataDir)
	if err != nil {
		t.Fatalf("LoadTestResources: %v", err)
	}
	seed := append(cms, testResources...)
	hubR, err := NewResolver(seed)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	spokeR, err := NewSpokeResolver(seed)
	if err != nil {
		t.Fatalf("NewSpokeResolver: %v", err)
	}
	rendered, err := KustomizeBuildWithRemediation(filepath.Join(root, "policies", egressIPNodesPolicy), remediation)
	if err != nil {
		t.Fatalf("kustomize build: %v", err)
	}
	var policies []map[string]interface{}
	for _, doc := range splitYAMLDocuments(rendered) {
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &obj); err == nil && obj["kind"] == "Policy" {
			policies = append(policies, obj)
		}
	}
	if len(policies) == 0 {
		t.Fatal("no Policy in the rendered output")
	}
	// The inform assertion is its own Policy with an inform root, so no root value can override it;
	// the enforcing Policy follows REMEDIATION.
	assertEnforceTestSplit(t, rendered, remediation, splitPolicyDirs[egressIPNodesPolicy])
	var out []egressTemplate
	for _, policy := range policies {
		spec := policy["spec"].(map[string]interface{})
		for _, tpl := range spec["policy-templates"].([]interface{}) {
			single := map[string]interface{}{}
			for k, v := range policy {
				single[k] = v
			}
			singleSpec := map[string]interface{}{}
			for k, v := range spec {
				singleSpec[k] = v
			}
			singleSpec["policy-templates"] = []interface{}{tpl}
			single["spec"] = singleSpec
			doc, _ := sigsyaml.Marshal(single)

			def := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})
			res := egressTemplate{name: def["metadata"].(map[string]interface{})["name"].(string)}
			hub := hubR.ResolvePolicy(string(doc), ctx)
			if len(hub.Errors) > 0 {
				res.hubErr = strings.Join(hub.Errors, "; ")
				out = append(out, res)
				continue
			}
			resolved := hub.Resolved
			res.hubResolved = resolved
			if strings.Contains(resolved, "{{") {
				spoke := spokeR.ResolveSpokeTemplates(resolved, ctx)
				if len(spoke.Errors) > 0 {
					t.Fatalf("%s: spoke resolution: %v", res.name, spoke.Errors)
				}
				resolved = spoke.Resolved
			}
			if strings.Contains(resolved, "<no value>") {
				t.Errorf("%s: <no value> leaked into the resolved policy", res.name)
			}
			var p map[string]interface{}
			if err := sigsyaml.Unmarshal([]byte(resolved), &p); err != nil {
				t.Fatalf("%s: resolved Policy: %v", res.name, err)
			}
			cp := p["spec"].(map[string]interface{})["policy-templates"].([]interface{})[0].(map[string]interface{})["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})
			res.remediation, _ = cp["remediationAction"].(string)
			if rawBlock, ok := cp["object-templates-raw"].(string); ok {
				if err := sigsyaml.Unmarshal([]byte(rawBlock), &res.items); err != nil {
					t.Fatalf("%s: object-templates-raw is not a YAML list: %v\n%s", res.name, err, rawBlock)
				}
			} else if list, ok := cp["object-templates"].([]interface{}); ok {
				for _, item := range list {
					m, _ := item.(map[string]interface{})
					res.items = append(res.items, m)
				}
			}
			out = append(out, res)
		}
	}
	return out
}

// egressStubDir is a copy of tools/testdata plus extra as another stub file.
func egressStubDir(t *testing.T, src, extra string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
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
	if err := os.WriteFile(filepath.Join(dst, "zz-egressip-nodes.yaml"), []byte(extra), 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func egressTemplateNamed(t *testing.T, tpls []egressTemplate, name string) egressTemplate {
	t.Helper()
	for _, tpl := range tpls {
		if tpl.name == name {
			if tpl.hubErr != "" {
				t.Fatalf("%s: hub template error: %s", name, tpl.hubErr)
			}
			return tpl
		}
	}
	t.Fatalf("no ConfigurationPolicy %s", name)
	return egressTemplate{}
}

// egressEnforce runs the policy with enforce remediation and returns the labeled nodes (name ->
// item) and the status ConfigMap data (nil when the ConfigMap is asserted absent).
func egressEnforce(t *testing.T, nodes interface{}, extraStubs string) (map[string]map[string]interface{}, map[string]string) {
	t.Helper()
	tpl := egressTemplateNamed(t, runEgressIPNodes(t, "enforce", nodes, extraStubs), egressEnforceCP)
	labeled := map[string]map[string]interface{}{}
	var status map[string]string
	statusSeen := false
	for _, item := range tpl.items {
		obj, _ := item["objectDefinition"].(map[string]interface{})
		meta, _ := obj["metadata"].(map[string]interface{})
		ct, _ := item["complianceType"].(string)
		switch obj["kind"] {
		case "Node":
			if ct != "musthave" {
				t.Errorf("Node %v: complianceType = %s, want musthave", meta["name"], ct)
			}
			labeled[meta["name"].(string)] = item
		case "ConfigMap":
			if meta["name"] != egressStatus || meta["namespace"] != "open-cluster-management-agent-addon" {
				t.Errorf("unexpected ConfigMap %v/%v", meta["namespace"], meta["name"])
			}
			if statusSeen {
				t.Error("status ConfigMap rendered twice")
			}
			statusSeen = true
			if ct == "mustonlyhave" {
				status = stringMap(obj, "data")
				if status == nil {
					status = map[string]string{}
				}
			} else if ct != "mustnothave" {
				t.Errorf("status ConfigMap complianceType = %s", ct)
			}
		default:
			t.Errorf("unexpected object kind %v", obj["kind"])
		}
	}
	if !statusSeen {
		t.Error("status ConfigMap not rendered")
	}
	return labeled, status
}

func egressHubErr(t *testing.T, nodes interface{}) string {
	t.Helper()
	for _, tpl := range runEgressIPNodes(t, "enforce", nodes, "") {
		if tpl.name == egressEnforceCP {
			return tpl.hubErr
		}
	}
	t.Fatalf("no ConfigurationPolicy %s", egressEnforceCP)
	return ""
}

func TestEgressIPNodes_Structure(t *testing.T) {
	t.Parallel()
	for _, remediation := range []string{"enforce", "inform"} {
		tpls := runEgressIPNodes(t, remediation, nil, "")
		want := map[string]string{egressEnforceCP: remediation, egressInformCP: "inform"}
		if len(tpls) != len(want) {
			t.Fatalf("%s: %d ConfigurationPolicies, want %d", remediation, len(tpls), len(want))
		}
		for name, ra := range want {
			if got := egressTemplateNamed(t, tpls, name).remediation; got != ra {
				t.Errorf("%s: %s remediationAction = %s, want %s", remediation, name, got, ra)
			}
		}
		inform := egressTemplateNamed(t, tpls, egressInformCP)
		if len(inform.items) != 1 {
			t.Fatalf("inform ConfigurationPolicy has %d objects, want 1", len(inform.items))
		}
		obj, _ := inform.items[0]["objectDefinition"].(map[string]interface{})
		meta, _ := obj["metadata"].(map[string]interface{})
		if inform.items[0]["complianceType"] != "mustnothave" || obj["kind"] != "ConfigMap" || meta["name"] != egressStatus {
			t.Errorf("inform ConfigurationPolicy must only assert %s absent, got %v", egressStatus, inform.items[0])
		}
	}
}

// The example config names nodes that are not in the testdata: nothing is labeled, the true one
// is reported.
func TestEgressIPNodes_Example(t *testing.T) {
	t.Parallel()
	labeled, status := egressEnforce(t, nil, "")
	if len(labeled) != 0 {
		t.Errorf("labeled %v, want none", labeled)
	}
	if !strings.Contains(status["worker-0.example.com"], "does not exist") {
		t.Errorf("status = %v, want worker-0.example.com reported missing", status)
	}
	if _, ok := status["worker-1.example.com"]; ok {
		t.Error("a node set to false must not be reported")
	}
}

func TestEgressIPNodes_NodeFoundLabelled(t *testing.T) {
	t.Parallel()
	labeled, status := egressEnforce(t, map[string]interface{}{egressWorker0: true, egressWorker1: false}, "")
	if len(labeled) != 1 || labeled[egressWorker0] == nil {
		t.Fatalf("labeled %v, want only %s", labeled, egressWorker0)
	}
	if status != nil {
		t.Errorf("status = %v, want none", status)
	}
	obj := labeled[egressWorker0]["objectDefinition"].(map[string]interface{})
	if len(obj) != 3 {
		t.Errorf("Node object must hold only apiVersion, kind and metadata, got %v", obj)
	}
	meta := obj["metadata"].(map[string]interface{})
	if len(meta) != 2 {
		t.Errorf("Node metadata must hold only name and labels, got %v", meta)
	}
	labels, _ := meta["labels"].(map[string]interface{})
	if v, ok := labels[egressLabel]; !ok || v != "" || len(labels) != 1 {
		t.Errorf("Node labels = %v, want only %s: \"\"", labels, egressLabel)
	}
}

func TestEgressIPNodes_NodeMissingReported(t *testing.T) {
	t.Parallel()
	labeled, status := egressEnforce(t, map[string]interface{}{"absent.example.com": true, egressWorker2: true}, "")
	if _, ok := labeled["absent.example.com"]; ok {
		t.Error("a missing node must not be rendered: musthave would create it")
	}
	if labeled[egressWorker2] == nil {
		t.Errorf("%s not labeled", egressWorker2)
	}
	if !strings.Contains(status["absent.example.com"], "does not exist") {
		t.Errorf("status = %v, want absent.example.com reported", status)
	}
}

func TestEgressIPNodes_NodeSelector(t *testing.T) {
	t.Parallel()
	cfg := map[string]interface{}{
		egressWorker1: false,
		"nodeSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"node-role.kubernetes.io/worker": ""},
		},
	}
	labeled, status := egressEnforce(t, cfg, "")
	if len(labeled) != 2 || labeled[egressWorker0] == nil || labeled[egressWorker2] == nil {
		t.Errorf("labeled %v, want %s and %s (worker-1 is false)", egressNames(labeled), egressWorker0, egressWorker2)
	}
	if status != nil {
		t.Errorf("status = %v, want none", status)
	}

	cfg = map[string]interface{}{
		"nodeSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"node-role.kubernetes.io/egress": "yes"},
		},
	}
	labeled, status = egressEnforce(t, cfg, "")
	if len(labeled) != 0 {
		t.Errorf("labeled %v, want none", egressNames(labeled))
	}
	if !strings.Contains(status["nodeSelector"], "matches no node") {
		t.Errorf("status = %v, want nodeSelector reported", status)
	}
}

// A node that carries the label but is no longer selected is reported, never unlabeled.
func TestEgressIPNodes_UnselectedReported(t *testing.T) {
	t.Parallel()
	stub := `apiVersion: v1
kind: Node
metadata:
  name: egress-old.example.com
  labels:
    kubernetes.io/hostname: egress-old.example.com
    k8s.ovn.org/egress-assignable: ""
`
	labeled, status := egressEnforce(t, map[string]interface{}{egressWorker0: true}, stub)
	if _, ok := labeled["egress-old.example.com"]; ok {
		t.Error("an unselected node must not be rendered")
	}
	msg := status["egress-old.example.com"]
	if !strings.Contains(msg, "not selected") || !strings.Contains(msg, "oc label node egress-old.example.com k8s.ovn.org/egress-assignable-") {
		t.Errorf("status = %v, want egress-old.example.com reported with the removal command", status)
	}
	if _, ok := status[egressWorker0]; ok {
		t.Error("a selected node must not be reported")
	}
}

func TestEgressIPNodes_InvalidInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		nodes interface{}
		want  string
	}{
		{"not a map", []interface{}{"worker-0"}, "config.egressIPNodes is not a map"},
		{"bad node name", map[string]interface{}{"Worker_0": true}, `config.egressIPNodes."Worker_0": is not a valid node name`},
		{"value not bool", map[string]interface{}{"worker-0.example.com": "yes"}, "config.egressIPNodes.worker-0.example.com: value is not true or false"},
		{"selector not a map", map[string]interface{}{"nodeSelector": "worker"}, "config.egressIPNodes.nodeSelector: is not a map"},
		{"selector matchExpressions", map[string]interface{}{"nodeSelector": map[string]interface{}{
			"matchLabels":      map[string]interface{}{"a": "b"},
			"matchExpressions": []interface{}{},
		}}, `nodeSelector: key "matchExpressions" is not matchLabels`},
		{"selector empty", map[string]interface{}{"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{}}}, "matchLabels is not a non-empty map"},
		{"selector bad key", map[string]interface{}{"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"a/b/c": "x"}}}, `"a/b/c" is not a valid label key`},
		{"selector value bool", map[string]interface{}{"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"egress": true}}}, "nodeSelector.matchLabels.egress: value is not a valid label value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := egressHubErr(t, c.nodes)
			if !strings.Contains(got, c.want) {
				t.Errorf("hub error = %q, want it to contain %q", got, c.want)
			}
		})
	}

	// Every error is listed, not only the first.
	got := egressHubErr(t, map[string]interface{}{"Bad_A": true, "b.example.com": 1})
	if !strings.Contains(got, `"Bad_A"`) || !strings.Contains(got, "b.example.com: value is not true or false") {
		t.Errorf("hub error = %q, want both keys named", got)
	}
}

// Template delimiters in config never reach the managed-cluster pass: they are rejected on the hub,
// and valid data crosses only as a quoted JSON string.
func TestEgressIPNodes_Injection(t *testing.T) {
	t.Parallel()
	for _, nodes := range []interface{}{
		map[string]interface{}{`{{ fail "x" }}`: true},
		map[string]interface{}{"a}}{{b": true},
		map[string]interface{}{"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"egress": `{{ fail "x" }}`}}},
		map[string]interface{}{"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{`{{x}}`: "y"}}},
	} {
		if got := egressHubErr(t, nodes); !strings.Contains(got, "egressip-nodes: config.egressIPNodes.") {
			t.Errorf("config %v: hub error = %q, want a validation failure", nodes, got)
		}
	}

	cfg := map[string]interface{}{
		egressWorker0:  true,
		"nodeSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"node-role.kubernetes.io/worker": ""}},
	}
	tpl := egressTemplateNamed(t, runEgressIPNodes(t, "enforce", cfg, ""), egressEnforceCP)
	if !strings.Contains(tpl.hubResolved, `$hub := "{\"nodes\":{\"`+egressWorker0+`\":true},\"selector\":\"node-role.kubernetes.io/worker=\"}" | fromJson`) {
		t.Errorf("hub data must reach the managed-cluster pass as one quoted JSON string:\n%s", tpl.hubResolved)
	}
}

func egressNames(m map[string]map[string]interface{}) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
