//go:build integration

package resolver

// Adversarial input for stable/project-networks (ClusterUserDefinedNetwork). The contract (policy
// README): invalid input fails the hub template naming the entry, a foreign network is never taken
// over, spec.network changes are held without a reconcile loop, nothing is ever deleted. A failing
// TestStress_Networks_<Bug> means the policy breaks that contract.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func setNet(key string, v interface{}) func(nets map[string]interface{}) {
	return func(nets map[string]interface{}) { nets["vlan-70"].(map[string]interface{})[key] = v }
}

// networksHubErr returns the hub errors of both ConfigurationPolicies.
func networksHubErr(tpls []networksTemplate) (enforce, inform string) {
	for _, tpl := range tpls {
		switch tpl.name {
		case networksEnforceCP:
			enforce = tpl.hubErr
		case networksInformCP:
			inform = tpl.hubErr
		}
	}
	return
}

func assertNetRejected(t *testing.T, what string, mutate func(nets map[string]interface{})) {
	t.Helper()
	tpls := runNetworks(t, "enforce", "", mutate)
	e, i := networksHubErr(tpls)
	if e == "" || i == "" {
		var applied string
		for _, tpl := range tpls {
			if tpl.name == networksEnforceCP {
				if item, ok := cudnItems(tpl, "")["vlan-70"]; ok {
					applied = asJSON(item["objectDefinition"].(map[string]interface{})["spec"])
				}
			}
		}
		t.Errorf("%s accepted (enforce err %q, inform err %q); applied spec %s", what, e, i, applied)
	}
}

// liveNet is a stub ClusterUserDefinedNetwork vlan-70 with the given labels block and localnet JSON.
func liveNet(managedBy, localnet string) string {
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
    localnet: ` + localnet + "\n"
}

// ---------------------------------------------------------------------------------------------
// Findings.
// ---------------------------------------------------------------------------------------------

// A selector that matches every namespace, openshift-* and kube-system included, is accepted
// without a guard or a warning in the README: OVN-Kubernetes then creates the NAD in every
// namespace, and any user who can create a pod anywhere can attach it to the physical VLAN.
func TestStress_Networks_SelectorMatchesEveryNamespace(t *testing.T) {
	t.Parallel()
	sel := func(x map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"matchExpressions": []interface{}{x}}
	}
	cases := map[string]map[string]interface{}{
		"Exists on kubernetes.io/metadata.name": sel(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "Exists"}),
		"NotIn a name that does not exist":      sel(map[string]interface{}{"key": "kubernetes.io/metadata.name", "operator": "NotIn", "values": []interface{}{"nope"}}),
		"DoesNotExist on an unused label":       sel(map[string]interface{}{"key": "example.com/never-set", "operator": "DoesNotExist"}),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) { assertNetRejected(t, "namespaceSelector "+name, setNet("namespaceSelector", s)) })
	}
}

// Label selector syntax is not validated on the hub: invalid label keys and values, and non-string
// matchExpressions values, reach the API server, which rejects the CUDN on every evaluation.
func TestStress_Networks_SelectorSyntaxNotValidated(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		"label key with space":   {"matchLabels": map[string]interface{}{"bad key!": "true"}},
		"label value with space": {"matchLabels": map[string]interface{}{"network.example.com/vlan-70": "has space"}},
		"label value 64 chars":   {"matchLabels": map[string]interface{}{"network.example.com/vlan-70": strings.Repeat("a", 64)}},
		"label key two slashes":  {"matchLabels": map[string]interface{}{"a/b/c": "true"}},
		"expression value int": {"matchExpressions": []interface{}{map[string]interface{}{
			"key": "network.example.com/vlan", "operator": "In", "values": []interface{}{70}}}},
		"expression value map": {"matchExpressions": []interface{}{map[string]interface{}{
			"key": "network.example.com/vlan", "operator": "In", "values": []interface{}{map[string]interface{}{"a": "b"}}}}},
		"expression key invalid": {"matchExpressions": []interface{}{map[string]interface{}{"key": "not a key", "operator": "Exists"}}},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) { assertNetRejected(t, name, setNet("namespaceSelector", s)) })
	}
}

// The subnet check is a character-class regex, so strings that are not CIDRs pass the hub and are
// rejected by the CRD's CIDR validation on every evaluation instead of naming the entry.
func TestStress_Networks_SubnetNotACIDR(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"999.999.999.999/99", "10.70.0.0/33", "fd00::/200", "..../8", "::::/64", "A/1", "10.70/24"} {
		t.Run(s, func(t *testing.T) { assertNetRejected(t, "subnet "+s, setNet("subnets", []interface{}{s})) })
	}
}

// Falsy non-list subnets ("" {} false 0) skip the "must be a list" check (| default list) and
// silently produce an IPAM-less network, unlike every other wrong type.
func TestStress_Networks_SubnetsFalsyNonListAccepted(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{"", map[string]interface{}{}, false, 0} {
		t.Run(fmt.Sprintf("%#v", v), func(t *testing.T) { assertNetRejected(t, fmt.Sprintf("subnets %#v", v), setNet("subnets", v)) })
	}
}

// physicalNetworkName is checked only for , : and length; whitespace and control characters pass
// the hub and the CRD, so the network never matches an OVN bridge mapping and silently carries no
// traffic.
func TestStress_Networks_PhysicalNetworkNameWhitespace(t *testing.T) {
	t.Parallel()
	for _, p := range []string{" physnet-vlan70", "physnet-vlan70 ", "physnet vlan70", "physnet-vlan70\n", "\t"} {
		t.Run(fmt.Sprintf("%q", p), func(t *testing.T) {
			assertNetRejected(t, fmt.Sprintf("physicalNetworkName %q", p), setNet("physicalNetworkName", p))
		})
	}
}

// ---------------------------------------------------------------------------------------------
// What held up.
// ---------------------------------------------------------------------------------------------

func TestStress_Networks_VlanSpellings(t *testing.T) {
	t.Parallel()
	for _, v := range []interface{}{"70", 70.5, 0, 4095, -1, true, "0x46", 1e10, 4094.5, nil, []interface{}{70}, map[string]interface{}{"id": 70}, " 70", 0.0, -0.0} {
		t.Run(fmt.Sprintf("%#v", v), func(t *testing.T) {
			tpls := runNetworks(t, "enforce", "", setNet("vlan", v))
			if e, i := networksHubErr(tpls); !strings.Contains(e, "vlan is required and must be an integer from 1 to 4094") || i == "" {
				t.Errorf("vlan %#v accepted: %q / %q", v, e, i)
			}
		})
	}
	for v, want := range map[interface{}]string{1: `{"access":{"id":1},"mode":"Access"}`, 4094: `{"access":{"id":4094},"mode":"Access"}`, 70.0: `{"access":{"id":70},"mode":"Access"}`} {
		tpls := runNetworks(t, "enforce", "", setNet("vlan", v))
		item := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
		if got := asJSON(cudnLocalnet(t, item)["vlan"]); got != want {
			t.Errorf("vlan %#v rendered %s, want %s", v, got, want)
		}
	}
}

// Quoting and delimiters in every string: rejected or literal, never evaluated. runNetworks fails
// the test on any managed-side template error, so a payload that was evaluated there cannot pass.
// A refusal must come from the hub with a message naming the field; a render must be literal.
func TestStress_Networks_InjectionLiteral(t *testing.T) {
	t.Parallel()
	quoty := `phys"net\'x` + "`"
	tpls := runNetworks(t, "enforce", "", setNet("physicalNetworkName", quoty))
	item := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
	if got := cudnLocalnet(t, item)["physicalNetworkName"]; got != quoty {
		t.Errorf("physicalNetworkName = %q, want literal %q", got, quoty)
	}
	tpls = runNetworks(t, "enforce", "", setNet("namespaceSelector",
		map[string]interface{}{"matchLabels": map[string]interface{}{`k"\`: `v" | fail "x`}}))
	if e, i := networksHubErr(tpls); e != "" || i != "" {
		if !strings.Contains(e, "namespaceSelector.matchLabels") || !strings.Contains(i, "namespaceSelector.matchLabels") {
			t.Errorf("matchLabels with quotes refused without a clear message: %q / %q", e, i)
		}
	} else {
		item = cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
		sel := item["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})["namespaceSelector"]
		if got := asJSON(sel); got != `{"matchLabels":{"k\"\\":"v\" | fail \"x"}}` {
			t.Errorf("namespaceSelector = %s, want literal", got)
		}
	}
	for name, s := range map[string]map[string]interface{}{
		"expr key":   {"matchExpressions": []interface{}{map[string]interface{}{"key": "a{{", "operator": "Exists"}}},
		"expr value": {"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "In", "values": []interface{}{"ok", "}}"}}}},
		"label key":  {"matchLabels": map[string]interface{}{"{{ fail \"x\" }}": "v"}},
	} {
		tpls := runNetworks(t, "enforce", "", setNet("namespaceSelector", s))
		if e, _ := networksHubErr(tpls); !strings.Contains(e, "template delimiters") {
			t.Errorf("%s with delimiters: %q", name, e)
		}
	}
}

// Every spec.network difference on an owned live network holds the entry (no enforce item, inform
// shows the declared object); a namespaceSelector-only difference is applied.
func TestStress_Networks_ImmutableChangesHeld(t *testing.T) {
	t.Parallel()
	declared := `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24"]}`
	cases := map[string]string{
		"physicalNetworkName": `{"role":"Secondary","physicalNetworkName":"physnet-other","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24"]}`,
		"subnets changed":     `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.71.0.0/24"]}`,
		"subnets added":       `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24","fd00::/64"]}`,
		"ipam disabled live":  `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"ipam":{"mode":"Disabled"}}`,
		"extra mtu":           `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24"],"mtu":1400}`,
		"role primary":        `{"role":"Primary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24"]}`,
		"vlan id as string":   `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":"70"}},"subnets":["10.70.0.0/24"]}`,
	}
	for name, live := range cases {
		t.Run(name, func(t *testing.T) {
			tpls := runNetworks(t, "enforce", withStubs(t, liveNet("project-networks", live)), nil)
			if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
				t.Errorf("changed %s applied by enforce (immutable: rejected every evaluation)", name)
			}
			if _, ok := cudnItems(networksTemplateNamed(t, tpls, networksInformCP), "mustonlyhave")["vlan-70"]; !ok {
				t.Errorf("changed %s not reported by inform", name)
			}
		})
	}
	tpls := runNetworks(t, "enforce", withStubs(t, liveNet("project-networks", declared)), nil)
	if _, ok := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]; !ok {
		t.Error("selector-only change (same network, key order differs) not applied")
	}
	// Dual-stack subnets in the other order are a different list: held, not looped.
	tpls = runNetworks(t, "enforce", withStubs(t, liveNet("project-networks",
		`{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["fd00:70::/64","10.70.0.0/24"]}`)),
		setNet("subnets", []interface{}{"10.70.0.0/24", "fd00:70::/64"}))
	if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
		t.Error("reordered subnets applied by enforce")
	}
}

// A forged managed-by label is, by the README rule, ownership: with an equal network the CUDN is
// managed and its namespaceSelector replaced. Pinned so a change is deliberate. A foreign label
// value in any spelling is never ownership.
func TestStress_Networks_ForgedAndLookalikeLabels(t *testing.T) {
	t.Parallel()
	declared := `{"role":"Secondary","physicalNetworkName":"physnet-vlan70","vlan":{"mode":"Access","access":{"id":70}},"subnets":["10.70.0.0/24"]}`
	tpls := runNetworks(t, "enforce", withStubs(t, liveNet("project-networks", declared)), nil)
	if _, ok := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]; !ok {
		t.Error("labelled CUDN with an equal network not managed")
	}
	for _, label := range []string{"Project-Networks", "'project-networks '", "project_networks", "''"} {
		tpls := runNetworks(t, "enforce", withStubs(t, liveNet(label, declared)), nil)
		if applied := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), ""); len(applied) != 0 {
			t.Errorf("CUDN labelled managed-by %s taken over", label)
		}
	}
}

// No input makes the enforce ConfigurationPolicy carry a mustnothave, under both remediations.
func TestStress_Networks_NeverDeletes(t *testing.T) {
	t.Parallel()
	mutations := map[string]func(cfg map[string]interface{}){
		"removed": func(c map[string]interface{}) { delete(c, "projectNetworks") },
		"empty":   func(c map[string]interface{}) { c["projectNetworks"] = map[string]interface{}{} },
		"null":    func(c map[string]interface{}) { c["projectNetworks"] = nil },
		"renamed": func(c map[string]interface{}) {
			n := c["projectNetworks"].(map[string]interface{})
			n["vlan-70b"] = n["vlan-70"]
			delete(n, "vlan-70")
		},
	}
	for name, m := range mutations {
		for _, remediation := range []string{"enforce", "inform"} {
			tpls := runNetworksConfig(t, remediation, "", m)
			for _, tpl := range tpls {
				if tpl.hubErr != "" {
					t.Errorf("%s/%s: %s: %s", name, remediation, tpl.name, tpl.hubErr)
				}
				for range cudnItems(tpl, "mustnothave") {
					if tpl.remediation != "inform" {
						t.Errorf("%s/%s: %s (%s) would delete", name, remediation, tpl.name, tpl.remediation)
					}
				}
			}
		}
	}
}

// Many entries: each renders exactly one CUDN with its own VLAN.
func TestStress_Networks_ManyEntries(t *testing.T) {
	t.Parallel()
	const n = 400
	tpls := runNetworks(t, "enforce", "", func(nets map[string]interface{}) {
		for i := 1; i <= n; i++ {
			nets[fmt.Sprintf("net-%d", i)] = map[string]interface{}{
				"vlan": i, "physicalNetworkName": "physnet-trunk",
				"namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"example.com/vlan": fmt.Sprint(i)}},
			}
		}
	})
	got := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")
	if len(got) != n+1 {
		t.Fatalf("want %d CUDNs, got %d", n+1, len(got))
	}
	for i := 1; i <= n; i++ {
		item := got[fmt.Sprintf("net-%d", i)]
		if id := asJSON(cudnLocalnet(t, item)["vlan"]); id != fmt.Sprintf(`{"access":{"id":%d},"mode":"Access"}`, i) {
			t.Errorf("net-%d vlan %s", i, id)
		}
	}
}

// The Secret-lookup payload in every project-networks string field: refused on the hub or literal
// (runNetworks fails the test if anything is left for the managed cluster to evaluate and it errors).
func TestStress_Networks_SecretLookupLiteral(t *testing.T) {
	t.Parallel()
	const lookupPayload = "{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}"
	mutations := map[string]func(nets map[string]interface{}){
		"physicalNetworkName": setNet("physicalNetworkName", lookupPayload),
		"vlan":                setNet("vlan", lookupPayload),
		"subnet":              setNet("subnets", []interface{}{lookupPayload}),
		"matchLabels key":     setNet("namespaceSelector", map[string]interface{}{"matchLabels": map[string]interface{}{lookupPayload: "v"}}),
		"matchLabels value":   setNet("namespaceSelector", map[string]interface{}{"matchLabels": map[string]interface{}{"k": lookupPayload}}),
		"expression key": setNet("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{
			map[string]interface{}{"key": lookupPayload, "operator": "Exists"}}}),
		"expression operator": setNet("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{
			map[string]interface{}{"key": "k", "operator": lookupPayload}}}),
		"expression value": setNet("namespaceSelector", map[string]interface{}{"matchExpressions": []interface{}{
			map[string]interface{}{"key": "k", "operator": "In", "values": []interface{}{lookupPayload}}}}),
		"entry name": func(nets map[string]interface{}) { nets[lookupPayload] = nets["vlan-70"] },
	}
	for field, mutate := range mutations {
		tpls := runNetworks(t, "enforce", "", mutate)
		for _, tpl := range tpls {
			if b, _ := json.Marshal(tpl.items); strings.Contains(string(b), "dockerconfigjson") {
				t.Errorf("%s: pull-secret data rendered by %s", field, tpl.name)
			}
		}
		if field == "physicalNetworkName" {
			if e, _ := networksHubErr(tpls); e == "" {
				item := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
				if got := cudnLocalnet(t, item)["physicalNetworkName"]; got != lookupPayload {
					t.Errorf("physicalNetworkName = %q, want literal", got)
				}
			}
		}
	}
}
