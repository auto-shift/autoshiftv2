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

const projectNetworksPolicy = "stable/project-networks"

// The ConfigurationPolicies of the policy: the CUDNs it applies, and the report of held and
// undeclared ones.
const (
	networksEnforceCP = "policy-project-networks"
	networksInformCP  = "policy-project-networks-test"
)

const networksStubs = "project-networks.yaml"

// networksTemplate is one resolved ConfigurationPolicy of stable/project-networks.
type networksTemplate struct {
	name        string
	remediation string
	hubErr      string // hub template error, empty when it resolved
	items       []map[string]interface{}
}

// runNetworks renders stable/project-networks with the example config.projectNetworks optionally
// mutated, against the stubs in testdataDir ("" for tools/testdata), and resolves each policy
// template on its own: the RHACM propagator resolves hub templates per policy template.
func runNetworks(t *testing.T, remediation, testdataDir string, mutate func(nets map[string]interface{})) []networksTemplate {
	t.Helper()
	return runNetworksConfig(t, remediation, testdataDir, func(cfg map[string]interface{}) {
		if mutate == nil {
			return
		}
		nets, ok := cfg["projectNetworks"].(map[string]interface{})
		if !ok {
			t.Fatal("_example.yaml config.projectNetworks is missing")
		}
		mutate(nets)
	})
}

func runNetworksConfig(t *testing.T, remediation, testdataDir string, mutate func(cfg map[string]interface{})) []networksTemplate {
	t.Helper()
	root := repoRoot(t)
	if testdataDir == "" {
		testdataDir = filepath.Join(root, "tools", "testdata")
	}
	configs, err := ExtractExampleConfigs(filepath.Join(root, "autoshift", "values"))
	if err != nil {
		t.Fatalf("ExtractExampleConfigs: %v", err)
	}
	raw, _ := json.Marshal(configs)
	var cloned ExampleConfigs
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("clone configs: %v", err)
	}
	if mutate != nil {
		mutate(cloned.HubConfig)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: map[string]string{}}
	cms, err := GenerateSyntheticConfigMaps(&cloned, ctx.ManagedClusterName, "policies-autoshift")
	if err != nil {
		t.Fatalf("GenerateSyntheticConfigMaps: %v", err)
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
	rendered, err := KustomizeBuildWithRemediation(filepath.Join(root, "policies", projectNetworksPolicy), remediation)
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
	// The inform checks are their own Policy with an inform root, so no root value can override
	// them; the enforcing Policy follows REMEDIATION.
	assertEnforceTestSplit(t, rendered, remediation, splitPolicyDirs[projectNetworksPolicy])
	var out []networksTemplate
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
			res := networksTemplate{name: def["metadata"].(map[string]interface{})["name"].(string)}
			hub := hubR.ResolvePolicy(string(doc), ctx)
			if len(hub.Errors) > 0 {
				res.hubErr = strings.Join(hub.Errors, "; ")
				out = append(out, res)
				continue
			}
			resolved := hub.Resolved
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
			}
			out = append(out, res)
		}
	}
	return out
}

func networksTemplateNamed(t *testing.T, tpls []networksTemplate, name string) networksTemplate {
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
	return networksTemplate{}
}

// cudnItems returns the ClusterUserDefinedNetwork items of tpl with the given complianceType ("" for
// any but mustnothave), keyed by name.
func cudnItems(tpl networksTemplate, complianceType string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for _, item := range tpl.items {
		obj, _ := item["objectDefinition"].(map[string]interface{})
		ct, _ := item["complianceType"].(string)
		if obj["kind"] != "ClusterUserDefinedNetwork" {
			continue
		}
		if complianceType == "" && ct == "mustnothave" || complianceType != "" && ct != complianceType {
			continue
		}
		out[obj["metadata"].(map[string]interface{})["name"].(string)] = item
	}
	return out
}

func cudnLocalnet(t *testing.T, item map[string]interface{}) map[string]interface{} {
	t.Helper()
	obj, ok := item["objectDefinition"].(map[string]interface{})
	if !ok {
		t.Fatalf("no ClusterUserDefinedNetwork item: %v", item)
	}
	spec, _ := obj["spec"].(map[string]interface{})
	network, _ := spec["network"].(map[string]interface{})
	if network["topology"] != "Localnet" {
		t.Errorf("topology = %v, want Localnet", network["topology"])
	}
	local, ok := network["localnet"].(map[string]interface{})
	if !ok {
		t.Fatalf("no spec.network.localnet in %v", obj)
	}
	return local
}

// liveCUDN is a stub ClusterUserDefinedNetwork vlan-70 with the given managed-by label ("" for
// none) and access VLAN.
func liveCUDN(managedBy string, vlan int) string {
	labels := ""
	if managedBy != "" {
		labels = "\n  labels:\n    autoshift.io/managed-by: " + managedBy
	}
	return `apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: vlan-70` + labels + `
spec:
  namespaceSelector:
    matchLabels:
      network.example.com/old: 'true'
  network:
    topology: Localnet
    localnet:
      role: Secondary
      physicalNetworkName: physnet-vlan70
      vlan:
        mode: Access
        access:
          id: ` + jsonInt(vlan) + `
      subnets:
        - 10.70.0.0/24
`
}

func jsonInt(i int) string { b, _ := json.Marshal(i); return string(b) }

// withStubs is tools/testdata plus extra as another stub file.
func withStubs(t *testing.T, extra string) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "tools", "testdata")
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || (extra == "" && e.Name() == networksStubs) {
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
	if extra != "" {
		if err := os.WriteFile(filepath.Join(dst, "zz-networks.yaml"), []byte(extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func TestProjectNetworks_Structure(t *testing.T) {
	t.Parallel()
	for _, remediation := range []string{"enforce", "inform"} {
		tpls := runNetworks(t, remediation, "", nil)
		want := map[string]string{networksEnforceCP: remediation, networksInformCP: "inform"}
		if len(tpls) != len(want) {
			t.Fatalf("REMEDIATION=%s: want ConfigurationPolicies %v, got %d", remediation, want, len(tpls))
		}
		for name, ra := range want {
			if got := networksTemplateNamed(t, tpls, name).remediation; got != ra {
				t.Errorf("REMEDIATION=%s: %s remediationAction = %q, want %q", remediation, name, got, ra)
			}
		}
	}
}

func TestProjectNetworks_CUDN(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", "", nil)
	applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")
	item, ok := applied["vlan-70"]
	if !ok || len(applied) != 1 {
		t.Fatalf("want exactly vlan-70 applied, got %v", applied)
	}
	if item["complianceType"] != "mustonlyhave" || item["metadataComplianceType"] != "musthave" {
		t.Errorf("complianceType = %v, metadataComplianceType = %v, want mustonlyhave and musthave",
			item["complianceType"], item["metadataComplianceType"])
	}
	obj := item["objectDefinition"].(map[string]interface{})
	if obj["apiVersion"] != "k8s.ovn.org/v1" {
		t.Errorf("apiVersion = %v, want k8s.ovn.org/v1", obj["apiVersion"])
	}
	meta := obj["metadata"].(map[string]interface{})
	if got := stringMap(meta, "labels")["autoshift.io/managed-by"]; got != "project-networks" {
		t.Errorf("managed-by label = %q, want project-networks", got)
	}
	local := cudnLocalnet(t, item)
	want := `{"physicalNetworkName":"physnet-vlan70","role":"Secondary","subnets":["10.70.0.0/24"],"vlan":{"access":{"id":70},"mode":"Access"}}`
	if got := asJSON(local); got != want {
		t.Errorf("localnet = %s\nwant      %s", got, want)
	}
	sel := obj["spec"].(map[string]interface{})["namespaceSelector"]
	if got := asJSON(sel); got != `{"matchLabels":{"network.example.com/vlan-70":"true"}}` {
		t.Errorf("namespaceSelector = %s", got)
	}
	for _, item := range networksTemplateNamed(t, tpls, networksEnforceCP).items {
		if item["complianceType"] == "mustnothave" {
			t.Errorf("enforce ConfigurationPolicy carries a mustnothave %v; deletion must stay manual", item["objectDefinition"])
		}
	}
	if held := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustonlyhave"); len(held) != 0 {
		t.Errorf("nothing is held on a cluster without a live vlan-70, got %v", held)
	}
}

func TestProjectNetworks_NoSubnets(t *testing.T) {
	t.Parallel()
	for _, subnets := range []interface{}{nil, []interface{}{}} {
		tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
			e := nets["vlan-70"].(map[string]interface{})
			if subnets == nil {
				delete(e, "subnets")
			} else {
				e["subnets"] = subnets
			}
		})
		local := cudnLocalnet(t, cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"])
		if _, ok := local["subnets"]; ok {
			t.Errorf("subnets %v: localnet.subnets rendered, want omitted", subnets)
		}
		if got := asJSON(local["ipam"]); got != `{"mode":"Disabled"}` {
			t.Errorf("subnets %v: ipam = %s, want {\"mode\":\"Disabled\"}", subnets, got)
		}
	}
}

// Invalid input fails the hub template of both ConfigurationPolicies, with a message naming the
// entry and the problem, so nothing is applied and nothing is reported as undeclared.
func TestProjectNetworks_InvalidVlan(t *testing.T) {
	t.Parallel()
	set := func(key string, v interface{}) func(nets map[string]interface{}) {
		return func(nets map[string]interface{}) { nets["vlan-70"].(map[string]interface{})[key] = v }
	}
	del := func(key string) func(nets map[string]interface{}) {
		return func(nets map[string]interface{}) { delete(nets["vlan-70"].(map[string]interface{}), key) }
	}
	const at = "config.projectNetworks.vlan-70: "
	cases := []struct {
		name   string
		mutate func(nets map[string]interface{})
		want   string
	}{
		{"vlan 5000", set("vlan", 5000), at + "vlan is required and must be an integer from 1 to 4094"},
		{"vlan 0", set("vlan", 0), at + "vlan is required and must be an integer from 1 to 4094"},
		{"vlan string", set("vlan", "70"), at + "vlan is required and must be an integer from 1 to 4094"},
		{"vlan fraction", set("vlan", 70.5), at + "vlan is required and must be an integer from 1 to 4094"},
		{"vlan missing", del("vlan"), at + "vlan is required and must be an integer from 1 to 4094"},
		{"physicalNetworkName missing", del("physicalNetworkName"), at + "physicalNetworkName is required"},
		{"physicalNetworkName colon", set("physicalNetworkName", "a:b"), at + "physicalNetworkName must not contain , or :"},
		{"physicalNetworkName delimiters", set("physicalNetworkName", `x{{ "y" }}`), at + "physicalNetworkName contains template delimiters"},
		{"subnets not a list", set("subnets", "10.70.0.0/24"), at + "subnets must be a list of one or two CIDRs"},
		{"subnets three", set("subnets", []interface{}{"10.70.0.0/24", "fd00::/64", "10.71.0.0/24"}), at + "subnets must be a list of one or two CIDRs"},
		{"subnet not a CIDR", set("subnets", []interface{}{"10.70.0.0"}), at + "subnets must be a list of one or two CIDRs"},
		{"subnets two IPv4", set("subnets", []interface{}{"10.70.0.0/24", "10.71.0.0/24"}), at + "subnets must not hold two subnets of the same IP family"},
		{"subnets two IPv6", set("subnets", []interface{}{"fd00:70::/64", "fd00:71::/64"}), at + "subnets must not hold two subnets of the same IP family"},
		{"expression not a map", set("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{"a"}}), at + "namespaceSelector.matchExpressions entry is not a map"},
		{"expression unknown key", set("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "Exists", "vals": []interface{}{"x"}}}}), at + "namespaceSelector.matchExpressions entry has key vals, not one of key, operator, values"},
		{"expression bad operator", set("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "Equals", "values": []interface{}{"x"}}}}), at + "namespaceSelector.matchExpressions entry a: operator must be In, NotIn, Exists or DoesNotExist"},
		{"expression In without values", set("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "In"}}}), at + "namespaceSelector.matchExpressions entry a: values is required with operator In"},
		{"expression Exists with values", set("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "Exists", "values": []interface{}{"x"}}}}), at + "namespaceSelector.matchExpressions entry a: values must be omitted with operator Exists"},
		{"selector missing", del("namespaceSelector"), at + "namespaceSelector is required"},
		{"selector empty", set("namespaceSelector", map[string]interface{}{}), at + "namespaceSelector is required"},
		{"selector unknown key", set("namespaceSelector", map[string]interface{}{"labels": map[string]interface{}{"a": "b"}}), at + "namespaceSelector key labels is not one of matchLabels, matchExpressions"},
		{"selector label not a string", set("namespaceSelector", map[string]interface{}{"matchLabels": map[string]interface{}{"a": true}}), at + "namespaceSelector.matchLabels value of a is not a string"},
		{"selector delimiters", set("namespaceSelector", map[string]interface{}{"matchLabels": map[string]interface{}{"a": "}}{{"}}), at + "namespaceSelector contains template delimiters"},
		{"unknown key", set("role", "Primary"), at + "key role is not one of vlan, physicalNetworkName, subnets, namespaceSelector"},
		{"entry not a map", func(nets map[string]interface{}) { nets["vlan-70"] = "70" }, at + "entry is not a map"},
		{"invalid name", func(nets map[string]interface{}) {
			nets["VLAN_70"] = nets["vlan-70"]
		}, "config.projectNetworks.VLAN_70: name is not a valid DNS-1123 label"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpls := runNetworks(t, "enforce", "", tc.mutate)
			for _, name := range []string{networksEnforceCP, networksInformCP} {
				var got string
				for _, tpl := range tpls {
					if tpl.name == name {
						got = tpl.hubErr
					}
				}
				if !strings.Contains(got, tc.want) {
					t.Errorf("%s hub error = %q, want it to contain %q", name, got, tc.want)
				}
			}
		})
	}
	t.Run("projectNetworks not a map", func(t *testing.T) {
		tpls := runNetworksConfig(t, "enforce", "", func(cfg map[string]interface{}) {
			cfg["projectNetworks"] = []interface{}{"vlan-70"}
		})
		if got := tpls[0].hubErr; !strings.Contains(got, "config.projectNetworks is not a map keyed by network name") {
			t.Errorf("hub error = %q", got)
		}
	})
}

// An owned CUDN that is no longer declared is reported by the inform ConfigurationPolicy, never
// deleted; one without the managed-by label is never touched.
func TestProjectNetworks_UndeclaredReported(t *testing.T) {
	t.Parallel()
	for _, remediation := range []string{"enforce", "inform"} {
		tpls := runNetworks(t, remediation, "", nil)
		stale := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustnothave")
		if _, ok := stale["vlan-99"]; !ok || len(stale) != 1 {
			t.Errorf("REMEDIATION=%s: want exactly vlan-99 reported, got %v", remediation, stale)
		}
	}
	// With no config.projectNetworks at all, every owned CUDN is undeclared.
	tpls := runNetworksConfig(t, "enforce", "", func(cfg map[string]interface{}) { delete(cfg, "projectNetworks") })
	if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
		t.Errorf("no entries: want nothing applied, got %v", applied)
	}
	if stale := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustnothave"); len(stale) != 1 {
		t.Errorf("no entries: want vlan-99 reported, got %v", stale)
	}
}

// A live vlan-70 without the managed-by label is never taken over: the enforce ConfigurationPolicy
// skips it and the inform one reports the difference.
func TestProjectNetworks_UnownedHeld(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", withStubs(t, liveCUDN("", 70)), nil)
	if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
		t.Errorf("want unowned vlan-70 held, got applied %v", applied)
	}
	if _, ok := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustonlyhave")["vlan-70"]; !ok {
		t.Error("want held vlan-70 reported by the inform ConfigurationPolicy")
	}
	other := runNetworks(t, "enforce", withStubs(t, liveCUDN("someone-else", 70)), nil)
	if applied := cudnItems(networksTemplateNamed(t, other, networksEnforceCP), ""); len(applied) != 0 {
		t.Errorf("want vlan-70 managed by someone-else held, got applied %v", applied)
	}
}

// spec.network is immutable, so an owned CUDN whose network differs from the entry is reported,
// not applied (the API server would reject it on every evaluation).
func TestProjectNetworks_NetworkChangeHeld(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", withStubs(t, liveCUDN("project-networks", 71)), nil)
	if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
		t.Errorf("want vlan-70 with a changed network held, got applied %v", applied)
	}
	held := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustonlyhave")
	if item, ok := held["vlan-70"]; !ok {
		t.Error("want held vlan-70 reported by the inform ConfigurationPolicy")
	} else if id := asJSON(cudnLocalnet(t, item)["vlan"]); id != `{"access":{"id":70},"mode":"Access"}` {
		t.Errorf("report renders vlan %s, want the declared 70", id)
	}
}

// An owned CUDN with the same network is applied, which updates the mutable namespaceSelector.
func TestProjectNetworks_OwnedSameNetworkApplied(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", withStubs(t, liveCUDN("project-networks", 70)), nil)
	if _, ok := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]; !ok {
		t.Error("want owned vlan-70 with the same network applied")
	}
	if held := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustonlyhave"); len(held) != 0 {
		t.Errorf("want nothing held, got %v", held)
	}
}

// Without the CUDN CRD nothing is applied and the inform ConfigurationPolicy reports the CRD as
// missing.
func TestProjectNetworks_CRDMissing(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", withStubs(t, ""), nil)
	if items := networksTemplateNamed(t, tpls, networksEnforceCP).items; len(items) != 0 {
		t.Errorf("want nothing applied without the CRD, got %v", items)
	}
	found := false
	for _, item := range networksTemplateNamed(t, tpls, networksInformCP).items {
		obj := item["objectDefinition"].(map[string]interface{})
		if obj["kind"] == "CustomResourceDefinition" && item["complianceType"] == "musthave" &&
			obj["metadata"].(map[string]interface{})["name"] == "clusteruserdefinednetworks.k8s.ovn.org" {
			found = true
		}
	}
	if !found {
		t.Error("want the inform ConfigurationPolicy to assert the ClusterUserDefinedNetwork CRD")
	}
}

// The two manifests are identical except for the $want line.
func TestProjectNetworks_TemplatesInSync(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "policies", projectNetworksPolicy)
	read := func(sub string) string {
		b, err := os.ReadFile(filepath.Join(dir, sub, "project-networks.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	enforce, inform := read("manifests"), read("test")
	const e, i = `{{hub- $want := "enforce" hub}}`, `{{hub- $want := "inform" hub}}`
	if strings.Count(enforce, e) != 1 || strings.Count(inform, i) != 1 {
		t.Fatalf("each manifest must set $want exactly once")
	}
	if strings.Replace(enforce, e, i, 1) != inform {
		t.Error("manifests/ and test/ differ beyond the $want line; keep them in sync")
	}
}

// A matchExpressions selector and dual-stack subnets are valid and pass through unchanged.
func TestProjectNetworks_MatchExpressionsDualStack(t *testing.T) {
	t.Parallel()
	tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
		e := nets["vlan-70"].(map[string]interface{})
		e["subnets"] = []interface{}{"10.70.0.0/24", "fd00:70::/64"}
		e["namespaceSelector"] = map[string]interface{}{"matchExpressions": []interface{}{
			map[string]interface{}{"key": "network.example.com/vlan", "operator": "In", "values": []interface{}{"70"}},
		}}
	})
	item := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
	if got := asJSON(cudnLocalnet(t, item)["subnets"]); got != `["10.70.0.0/24","fd00:70::/64"]` {
		t.Errorf("subnets = %s", got)
	}
	sel := item["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})["namespaceSelector"]
	if got := asJSON(sel); got != `{"matchExpressions":[{"key":"network.example.com/vlan","operator":"In","values":["70"]}]}` {
		t.Errorf("namespaceSelector = %s", got)
	}
}

// Valid CIDRs in the forms OVN-Kubernetes accepts, and selectors with a positive requirement, are
// accepted; a platform namespace named through kubernetes.io/metadata.name is refused.
func TestProjectNetworks_ValidCIDRsAndSelectorGuard(t *testing.T) {
	t.Parallel()
	for _, subnets := range [][]interface{}{{"10.70.0.0/24"}, {"0.0.0.0/0"}, {"192.168.1.0/32"}, {"fd00:70::/64"},
		{"2001:db8:0:0:0:0:0:0/128"}, {"::/0"}, {"10.70.0.0/24", "FD00:70::/64"}} {
		tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
			nets["vlan-70"].(map[string]interface{})["subnets"] = subnets
		})
		if e, _ := networksHubErr(tpls); e != "" {
			t.Errorf("subnets %v rejected: %s", subnets, e)
		}
	}
	expr := func(xs ...map[string]interface{}) map[string]interface{} {
		l := []interface{}{}
		for _, x := range xs {
			l = append(l, x)
		}
		return map[string]interface{}{"matchExpressions": l}
	}
	ok := map[string]map[string]interface{}{
		"Exists on a project label": expr(map[string]interface{}{"key": "network.example.com/vlan-70", "operator": "Exists"}),
		"In on metadata.name":       expr(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "In", "values": []interface{}{"team-a", "team-b"}}),
		"NotIn plus In": expr(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "NotIn", "values": []interface{}{"team-c"}},
			map[string]interface{}{"key": "network.example.com/vlan", "operator": "In", "values": []interface{}{"70"}}),
	}
	for name, sel := range ok {
		tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
			nets["vlan-70"].(map[string]interface{})["namespaceSelector"] = sel
		})
		if e, _ := networksHubErr(tpls); e != "" {
			t.Errorf("%s rejected: %s", name, e)
		}
	}
	refused := map[string]map[string]interface{}{
		"In openshift-monitoring": expr(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "In", "values": []interface{}{"team-a", "openshift-monitoring"}}),
		"matchLabels kube-system": {"matchLabels": map[string]interface{}{"kubernetes.io/metadata.name": "kube-system"}},
		"In default":              expr(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "In", "values": []interface{}{"default"}}),
	}
	for name, sel := range refused {
		tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
			nets["vlan-70"].(map[string]interface{})["namespaceSelector"] = sel
		})
		if e, _ := networksHubErr(tpls); !strings.Contains(e, "platform namespace") {
			t.Errorf("%s: hub error %q, want a platform namespace refusal", name, e)
		}
	}
}
