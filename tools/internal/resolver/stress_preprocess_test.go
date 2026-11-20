//go:build integration

package resolver

// go-template-utils rewrites template text with regexes before it parses it (processForDataTypes,
// processForAutoIndent in pkg/templates/templates.go). Both match inside a quoted string literal,
// so user data passed to the managed cluster as `toJson | quote` can still be rewritten or break
// the parse. Each field below must reach the cluster literally, or be refused on the hub.

import (
	"fmt"
	"strings"
	"testing"
)

// preprocessPayloads: two partial templates, and two that complete a template so the
// regexes certainly match.
var preprocessPayloads = []string{
	`x: {{ y | toInt`,
	`x | autoindent }}`,
	`a: '{{ "1" | toInt }}'`,
	`{{ "a" | autoindent }}`,
}

func TestStress_Preprocess_SCCs(t *testing.T) {
	t.Parallel()
	type field struct {
		name string
		spec func(p string) map[string]interface{}
		get  func(o *onboardingObject) interface{}
	}
	seOpt := func(k string) field {
		return field{"seLinuxOptions." + k, func(p string) map[string]interface{} {
			return map[string]interface{}{"seLinuxContext": map[string]interface{}{"type": "MustRunAs", "seLinuxOptions": map[string]interface{}{k: p}}}
		}, func(o *onboardingObject) interface{} {
			return o.obj["seLinuxContext"].(map[string]interface{})["seLinuxOptions"].(map[string]interface{})[k]
		}}
	}
	list := func(k string) field {
		return field{k, func(p string) map[string]interface{} { return map[string]interface{}{k: []interface{}{p}} },
			func(o *onboardingObject) interface{} { return strList(o.obj[k])[0] }}
	}
	fields := []field{list("forbiddenSysctls"), list("requiredDropCapabilities"), seOpt("user"), seOpt("role"), seOpt("level"),
		{"allowedFlexVolumes.driver", func(p string) map[string]interface{} {
			return map[string]interface{}{"allowedFlexVolumes": []interface{}{map[string]interface{}{"driver": p}}}
		}, func(o *onboardingObject) interface{} {
			return o.obj["allowedFlexVolumes"].([]interface{})[0].(map[string]interface{})["driver"]
		}},
		{"fsGroup.ranges.max", func(p string) map[string]interface{} {
			return map[string]interface{}{"fsGroup": map[string]interface{}{"type": "MustRunAs", "ranges": []interface{}{map[string]interface{}{"min": 1, "max": p}}}}
		}, func(o *onboardingObject) interface{} {
			return o.obj["fsGroup"].(map[string]interface{})["ranges"].([]interface{})[0].(map[string]interface{})["max"]
		}},
	}
	for _, f := range fields {
		for i, p := range preprocessPayloads {
			t.Run(fmt.Sprintf("%s/%d", f.name, i), func(t *testing.T) {
				objs := stressSCCs(t, "", func(sccs map[string]interface{}) { sccs["example-pp"] = sccEntry(f.spec(p)) })
				o := sccObject(objs, sccEnforceCP, "SecurityContextConstraints", "", "example-pp")
				if o == nil {
					if sccStatusData(t, objs)["example-pp"] == "" {
						t.Errorf("%s %q: neither rendered nor reported", f.name, p)
					}
					return
				}
				if got := f.get(o); got != p {
					t.Errorf("%s %q rendered as %q", f.name, p, got)
				}
			})
		}
	}
	// Status messages echo user strings: a refused serviceAccounts key must be reported literally.
	for i, p := range preprocessPayloads {
		t.Run(fmt.Sprintf("serviceAccounts key/%d", i), func(t *testing.T) {
			objs := stressSCCs(t, "", func(sccs map[string]interface{}) {
				sccs["example-pp"] = map[string]interface{}{"serviceAccounts": map[string]interface{}{p: true}}
			})
			if got := sccStatusData(t, objs)["example-pp"]; !strings.Contains(got, "serviceAccounts."+p+" is not") {
				t.Errorf("serviceAccounts key %q reported as %q", p, got)
			}
		})
	}
}

func TestStress_Preprocess_Networks(t *testing.T) {
	t.Parallel()
	sel := func(m map[string]interface{}) func(nets map[string]interface{}) {
		return setNet("namespaceSelector", m)
	}
	for _, p := range preprocessPayloads {
		cases := map[string]func(nets map[string]interface{}){
			"physicalNetworkName": setNet("physicalNetworkName", p),
			"subnet":              setNet("subnets", []interface{}{p}),
			"matchLabels key":     sel(map[string]interface{}{"matchLabels": map[string]interface{}{p: "v"}}),
			"matchLabels value":   sel(map[string]interface{}{"matchLabels": map[string]interface{}{"k": p}}),
			"expression key":      sel(map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": p, "operator": "Exists"}}}),
			"expression value": sel(map[string]interface{}{"matchExpressions": []interface{}{
				map[string]interface{}{"key": "k", "operator": "In", "values": []interface{}{p}}}}),
		}
		for name, mutate := range cases {
			t.Run(fmt.Sprintf("%s/%q", name, p), func(t *testing.T) {
				// runNetworks fails the test on any managed-side template error.
				tpls := runNetworks(t, "enforce", "", mutate)
				if e, i := networksHubErr(tpls); e != "" && i != "" {
					return // refused on the hub
				}
				item := cudnItems(networksTemplateNamed(t, tpls, networksEnforceCP), "")["vlan-70"]
				if b := asJSON(item["objectDefinition"].(map[string]interface{})["spec"]); !strings.Contains(b, asJSON(p)) {
					t.Errorf("%s %q not rendered literally: %s", name, p, b)
				}
			})
		}
	}
}
