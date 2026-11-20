//go:build integration

package resolver

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Stress tests of the behaviour and state of project-onboarding. Each test asserts the contract of
// the policy README; a failing test means the policy breaks it.

const stressPruneStubs = "stress-onboarding-prune.yaml"

// stressOnly keeps the entries whose key mentions a stress object.
func stressOnly(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if strings.Contains(k, "stress-") {
			out[k] = v
		}
	}
	return out
}

func stressDiff(got, want map[string]string) string {
	var lines []string
	for k, v := range want {
		if g, ok := got[k]; !ok {
			lines = append(lines, fmt.Sprintf("  missing %s (want %s)", k, v))
		} else if g != v {
			lines = append(lines, fmt.Sprintf("  %s in %s, want %s", k, g, v))
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			lines = append(lines, fmt.Sprintf("  unexpected %s (%s)", k, v))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// Keys that must never be in any prune list, whatever the configuration.
var stressNeverPruned = []string{
	"RoleBinding|stress-a|unlabeled",  // no managed-by label
	"RoleBinding|stress-a|other-tool", // managed by another tool
	"Namespace||stress-a", "Namespace||stress-b", "Namespace||kube-system", "Namespace||stress-tool",
}

func stressCheckNever(t *testing.T, objs []onboardingObject) {
	t.Helper()
	p := stressPruned(objs)
	for _, k := range stressNeverPruned {
		if v, ok := p[k]; ok {
			t.Errorf("CRITICAL: %s is in the %s prune list; it must never be touched", k, v)
		}
	}
	for k := range p {
		if strings.HasPrefix(k, "Namespace|") {
			t.Errorf("CRITICAL: a Namespace is pruned: %s", k)
		}
	}
}

// The prune state machine, prune on. stress-a starts fully featured (every swept kind) and each
// case changes one thing; the want map is the full set of stress-* prune entries and the
// ConfigurationPolicy carrying each.
func TestStress_Prune_StateMachine(t *testing.T) {
	t.Parallel()
	baseline := map[string]string{
		"RoleBinding|stress-a|autoshift-stale":         "enforce",
		"NetworkPolicy|stress-a|autoshift-rule-old":    "enforce",
		"RoleBinding|stress-b|forged-a-in-b":           "enforce", // namespace owner stress-b wins over the label
		"RoleBinding|stress-elsewhere|stress-orphan-b": "enforce", // owned through its label by stress-b
	}
	with := func(extra map[string]string, drop ...string) map[string]string {
		out := map[string]string{}
		for k, v := range baseline {
			out[k] = v
		}
		for _, d := range drop {
			delete(out, d)
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	allA := map[string]string{
		"RoleBinding|stress-a|autoshift-admin":                  "enforce",
		"RoleBinding|stress-a|autoshift-write":                  "enforce",
		"RoleBinding|stress-a|autoshift-stale":                  "enforce",
		"ResourceQuota|stress-a|autoshift-quota":                "enforce",
		"LimitRange|stress-a|autoshift-limits":                  "enforce",
		"NetworkPolicy|stress-a|autoshift-default-deny-ingress": "enforce",
		"NetworkPolicy|stress-a|autoshift-default-deny-egress":  "enforce",
		"NetworkPolicy|stress-a|autoshift-rule-from-web":        "enforce",
		"NetworkPolicy|stress-a|autoshift-rule-old":             "enforce",
		"EgressFirewall|stress-a|default":                       "enforce",
		"AdminNetworkPolicy||autoshift-project-stress-a":        "enforce",
		"EgressIP||autoshift-stress-a":                          "enforce",
	}
	rules := func(cfg map[string]interface{}) map[string]interface{} {
		return stressProject(cfg, "stress-a")["network"].(map[string]interface{})["rules"].(map[string]interface{})
	}
	cases := []struct {
		name   string
		mutate func(cfg map[string]interface{})
		want   map[string]string
	}{
		{"baseline", nil, baseline},
		{"removed rule", func(cfg map[string]interface{}) { delete(rules(cfg), "from-web") },
			with(map[string]string{"NetworkPolicy|stress-a|autoshift-rule-from-web": "enforce"})},
		{"rule disabled", func(cfg map[string]interface{}) { rules(cfg)["from-web"].(map[string]interface{})["enabled"] = false },
			with(map[string]string{"NetworkPolicy|stress-a|autoshift-rule-from-web": "enforce"})},
		// The project's own override is not enough: projectDefaults.groups.write resolves for stress-a
		// too, so the level is removed from both.
		{"removed group", func(cfg map[string]interface{}) {
			delete(stressProject(cfg, "stress-a")["groups"].(map[string]interface{}), "write")
			delete(cfg["projectDefaults"].(map[string]interface{})["groups"].(map[string]interface{}), "write")
		}, with(map[string]string{"RoleBinding|stress-a|autoshift-write": "enforce"})},
		{"size removed", func(cfg map[string]interface{}) { delete(stressProject(cfg, "stress-a"), "size") },
			with(map[string]string{"ResourceQuota|stress-a|autoshift-quota": "enforce", "LimitRange|stress-a|autoshift-limits": "enforce"})},
		{"profile switched", func(cfg map[string]interface{}) { stressProject(cfg, "stress-a")["networkProfile"] = "permissive" },
			with(map[string]string{"NetworkPolicy|stress-a|autoshift-default-deny-egress": "enforce", "EgressFirewall|stress-a|default": "enforce"})},
		{"deny rule removed", func(cfg map[string]interface{}) { delete(rules(cfg), "deny-db") },
			with(map[string]string{"AdminNetworkPolicy||autoshift-project-stress-a": "enforce"})},
		{"egressIP removed", func(cfg map[string]interface{}) { delete(stressProject(cfg, "stress-a"), "egressIP") },
			with(map[string]string{"EgressIP||autoshift-stress-a": "enforce"})},
		{"mode inform", func(cfg map[string]interface{}) { stressProject(cfg, "stress-a")["mode"] = "inform" },
			with(map[string]string{"RoleBinding|stress-a|autoshift-stale": "inform", "NetworkPolicy|stress-a|autoshift-rule-old": "inform"})},
		{"newly errored", func(cfg map[string]interface{}) { stressProject(cfg, "stress-a")["team"] = "has space" },
			with(nil, "RoleBinding|stress-a|autoshift-stale", "NetworkPolicy|stress-a|autoshift-rule-old")},
		{"newly errored, skipped whole", func(cfg map[string]interface{}) { stressProject(cfg, "stress-a")["size"] = "huge" },
			with(nil, "RoleBinding|stress-a|autoshift-stale", "NetworkPolicy|stress-a|autoshift-rule-old")},
		{"newly errored, network", func(cfg map[string]interface{}) {
			rules(cfg)["broken"] = map[string]interface{}{"direction": "sideways"}
		}, with(nil, "RoleBinding|stress-a|autoshift-stale", "NetworkPolicy|stress-a|autoshift-rule-old")},
		{"newly errored, namespace claimed earlier", func(cfg map[string]interface{}) {
			stressProjects(cfg)["stress-0"] = map[string]interface{}{"namespace": "stress-a", "groups": map[string]interface{}{"admin": "Z"}}
		}, with(nil, "RoleBinding|stress-a|autoshift-stale", "NetworkPolicy|stress-a|autoshift-rule-old")},
		{"project removed", func(cfg map[string]interface{}) { delete(stressProjects(cfg), "stress-a") },
			with(allA)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mutate := func(cfg map[string]interface{}) {
				stressAddAB(cfg)
				if c.mutate != nil {
					c.mutate(cfg)
				}
			}
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: mutate})
			stressCheckNever(t, r.objs)
			if d := stressDiff(stressOnly(stressPruned(r.objs)), c.want); d != "" {
				t.Errorf("prune entries differ:\n%s", d)
			}
			// With prune off the same candidates are only reported, by the inform ConfigurationPolicy.
			r = stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: mutate})
			stressCheckNever(t, r.objs)
			off := map[string]string{}
			for k := range c.want {
				off[k] = "inform"
			}
			if d := stressDiff(stressOnly(stressPruned(r.objs)), off); d != "" {
				t.Errorf("prune off: entries differ:\n%s", d)
			}
		})
	}
}

// A project that did not render its declared namespace keeps its old objects (README: "never a
// candidate when that project ... did not render its currently declared namespace"). That guard is
// keyed by namespace for namespaced objects but by the project label for cluster-scoped ones, so
// when the project is renamed (or another project takes over the namespace) and is blocked by the
// namespace's old ownership label, the namespaced objects are kept while the AdminNetworkPolicy and
// EgressIP that apply to that very namespace are deleted: the namespace ends up more open than
// either the old or the new declaration.
func TestStress_Prune_RenameDeletesClusterScopedGuards(t *testing.T) {
	t.Parallel()
	cases := map[string]func(cfg map[string]interface{}){
		"renamed, namespace kept": func(cfg map[string]interface{}) {
			p := stressProjects(cfg)
			a := p["stress-a"].(map[string]interface{})
			a["namespace"] = "stress-a"
			p["stress-a2"] = a
			delete(p, "stress-a")
		},
		"removed, namespace taken over with adopt": func(cfg map[string]interface{}) {
			p := stressProjects(cfg)
			a := p["stress-a"].(map[string]interface{})
			a["namespace"] = "stress-a"
			a["adopt"] = true
			a["mode"] = "enforce"
			p["successor"] = a
			delete(p, "stress-a")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
				stressAddAB(cfg)
				mutate(cfg)
			}})
			stressCheckNever(t, r.objs)
			status := stressStatus(t, r.objs)
			blocked := false
			for _, v := range status {
				if strings.Contains(v, "namespace stress-a is owned by project stress-a") {
					blocked = true
				}
			}
			if !blocked {
				t.Fatalf("precondition: the new project must be blocked by the old ownership label, status %v", status)
			}
			p := stressPruned(r.objs)
			for k := range p {
				if strings.Contains(k, "|stress-a|") {
					t.Errorf("namespaced object %s pruned while its namespace is declared but blocked", k)
				}
			}
			for _, k := range []string{"AdminNetworkPolicy||autoshift-project-stress-a", "EgressIP||autoshift-stress-a"} {
				if v, ok := p[k]; ok {
					t.Errorf("CRITICAL: %s (selects namespace stress-a) is pruned by %s while namespace stress-a is declared but blocked; "+
						"its deny rules / egress address vanish and the namespace is more open than declared", k, v)
				}
			}
		})
	}
}

// Only the exact label value "true" turns deletion on.
func TestStress_Prune_LabelValues(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"false", "True", "TRUE", "yes", "1", "on", " true", "true ", "", "\x00absent"} {
		t.Run(fmt.Sprintf("%q", v), func(t *testing.T) {
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: map[string]string{"autoshift.io/project-onboarding-prune": v}, mutate: stressAddAB})
			for k, ra := range stressPruned(r.objs) {
				if ra != "inform" {
					t.Errorf("prune label %q: %s pruned by %s, want inform only", v, k, ra)
				}
			}
			stressCheckNever(t, r.objs)
		})
	}
}

// Prune off and every project removed: everything owned is reported, nothing deleted, no
// Namespace in any list.
func TestStress_Prune_AllRemovedPruneOff(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
		cfg["projects"] = map[string]interface{}{}
	}})
	stressCheckNever(t, r.objs)
	p := stressPruned(r.objs)
	if len(p) == 0 {
		t.Fatal("want the owned objects reported")
	}
	for k, v := range p {
		if v != "inform" {
			t.Errorf("prune off: %s in %s", k, v)
		}
	}
}

// Adoption matrix: namespace state x adopt x mode.
func TestStress_Adopt_Matrix(t *testing.T) {
	t.Parallel()
	type c struct {
		project, namespace string
		adopt              interface{}
		mode               interface{}
		wantCP             string // "" = blocked
		wantStatus         string // substring, "" = no status entry
	}
	cases := []c{
		{"p-unowned", "onb-unowned", nil, nil, "", "exists and is not owned by AutoShift"},
		{"p-unowned", "onb-unowned", false, "enforce", "", "exists and is not owned by AutoShift"},
		{"p-unowned", "onb-unowned", true, nil, "inform", ""},
		{"p-unowned", "onb-unowned", true, "enforce", "enforce", ""},
		{"p-unowned", "onb-unowned", "true", nil, "inform", ""},
		{"p-unowned", "onb-unowned", "yes", nil, "", "exists and is not owned by AutoShift"},
		{"p-other", "onb-owned-other", true, "enforce", "", "is owned by project someone-else"},
		{"p-other", "onb-owned-other", false, nil, "", "is owned by project someone-else"},
		{"selfowned", "onb-owned-self", false, nil, "enforce", ""},
		{"selfowned", "onb-owned-self", true, nil, "inform", ""},
		{"p-tool", "stress-tool", nil, nil, "", "exists and is not owned by AutoShift"},
		{"p-tool", "stress-tool", true, "enforce", "enforce", ""},
		{"p-new", "stress-brand-new", nil, nil, "enforce", ""},
		{"p-new", "stress-brand-new", true, nil, "inform", ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/adopt=%v/mode=%v", tc.namespace, tc.adopt, tc.mode), func(t *testing.T) {
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
				p := map[string]interface{}{"namespace": tc.namespace, "team": "x", "environment": "PROD"}
				if tc.adopt != nil {
					p["adopt"] = tc.adopt
				}
				if tc.mode != nil {
					p["mode"] = tc.mode
				}
				stressProjects(cfg)[tc.project] = p
			}})
			got := stressRendered(r.objs)["Namespace||"+tc.namespace]
			if tc.namespace == "stress-tool" || tc.namespace == "onb-unowned" {
				// the inform drop report never applies to a namespace this run does not render
			}
			if got != tc.wantCP && !(tc.wantCP == "enforce" && got == "enforce+inform") {
				t.Errorf("Namespace %s rendered by %q, want %q", tc.namespace, got, tc.wantCP)
			}
			st := stressStatus(t, r.objs)[tc.project]
			if tc.wantStatus == "" && st != "" {
				t.Errorf("unexpected status %q", st)
			}
			if tc.wantStatus != "" && !strings.Contains(st, tc.wantStatus) {
				t.Errorf("status %q, want it to contain %q", st, tc.wantStatus)
			}
			if tc.wantCP == "" {
				for k := range stressRendered(r.objs) {
					if strings.Contains(k, "|"+tc.namespace+"|") {
						t.Errorf("blocked project rendered %s", k)
					}
				}
				for k, v := range stressPruned(r.objs) {
					if strings.Contains(k, "|"+tc.namespace+"|") {
						t.Errorf("blocked project's namespace has a prune entry %s (%s)", k, v)
					}
				}
			}
		})
	}
}

// Adopting a namespace another tool created: the other tool's labels are kept, except a key the
// catalog sets and the reserved ownership keys, which project onboarding takes.
func TestStress_Adopt_OtherToolLabels(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
		stressProjects(cfg)["p-tool"] = map[string]interface{}{"namespace": "stress-tool", "team": "x", "environment": "PROD", "adopt": true, "mode": "enforce"}
	}})
	ns := namespaceIn(r.objs, "stress-tool", "enforce")
	if len(ns) != 1 {
		t.Fatalf("want one enforced stress-tool Namespace, got %d", len(ns))
	}
	l := stringMap(ns[0].obj["metadata"].(map[string]interface{}), "labels")
	if l["app.kubernetes.io/part-of"] != "other" {
		t.Errorf("other tool's label must be kept, got %v", l)
	}
	t.Logf("adopted stress-tool labels: %v", l)
}

// A mode other than enforce or inform is an error and falls back to inform (README). YAML 1.1
// turns `mode: off` / `mode: no` into the boolean false, which the template reads as "unset".
func TestStress_Mode_NonStringModeNotReported(t *testing.T) {
	t.Parallel()
	for name, mode := range map[string]interface{}{"bool false (mode: off)": false, "zero": 0, "bool true": true, "garbage": "garbage", "Enforce": "Enforce"} {
		t.Run(name, func(t *testing.T) {
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
				stressAddAB(cfg)
				stressProject(cfg, "stress-b")["mode"] = mode
			}})
			if got := remediationOf(r.objs, "stress-b"); got != "inform" {
				t.Errorf("mode %#v: stress-b rendered with %q, want inform (the documented fallback)", mode, got)
			}
			if st := stressStatus(t, r.objs)["stress-b"]; !strings.Contains(st, "is not enforce or inform") {
				t.Errorf("mode %#v: want the mode reported as an error, status %q", mode, st)
			}
		})
	}
}

// enforce -> inform -> enforce: the same objects are declared in every state, nothing declared
// is ever a prune entry, and no ConfigurationPolicy deletes what it stops listing.
func TestStress_Mode_FlipNoDeletion(t *testing.T) {
	t.Parallel()
	var keys []map[string]string
	for _, mode := range []string{"enforce", "inform", "enforce"} {
		r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
			stressAddAB(cfg)
			stressProject(cfg, "stress-a")["mode"] = mode
		}})
		rend := map[string]string{}
		for k, v := range stressRendered(r.objs) {
			if strings.Contains(k, "stress-a") {
				rend[k] = v
				if v != mode && !(k == "Namespace||stress-a" && v == "enforce+inform") {
					t.Errorf("mode %s: %s rendered by %s", mode, k, v)
				}
			}
		}
		for k := range stressPruned(r.objs) {
			if _, ok := rend[k]; ok {
				t.Errorf("CRITICAL: mode %s: %s is both declared and pruned", mode, k)
			}
		}
		keys = append(keys, rend)
		for _, policy := range onboardingPolicies(r.spoke) {
			for _, tpl := range policy["spec"].(map[string]interface{})["policy-templates"].([]interface{}) {
				spec := tpl.(map[string]interface{})["objectDefinition"].(map[string]interface{})["spec"].(map[string]interface{})
				if pob, ok := spec["pruneObjectBehavior"]; ok && pob != "None" {
					t.Errorf("pruneObjectBehavior %v: objects dropped from a ConfigurationPolicy on a mode flip would be deleted", pob)
				}
			}
		}
	}
	if len(keys[0]) != len(keys[1]) || len(keys[1]) != len(keys[2]) {
		t.Errorf("declared object count changes across the flip: %d / %d / %d", len(keys[0]), len(keys[1]), len(keys[2]))
	}
}

// Dry run (REMEDIATION=inform): with prune on, nothing is in an enforcing ConfigurationPolicy.
func TestStress_DryRun_NothingEnforced(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{remediation: "inform", stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: stressAddAB})
	if len(stressPruned(r.objs)) == 0 {
		t.Fatal("want prune entries (reported) under dry run")
	}
	for _, o := range r.objs {
		if o.remediationAction != "inform" {
			t.Fatalf("dry run: %s in a %q ConfigurationPolicy", stressKey(o), o.remediationAction)
		}
	}
}

// Project names or namespace overrides naming cluster system namespaces must be refused (or at
// least contained). Nothing guards them: with adopt: true and mode: enforce, onboarding takes over
// kube-system, default, openshift-dns or the addon namespace that hosts its own status ConfigMap,
// rewrites their metadata exactly (mustonlyhave), binds groups, and with the strict profile puts a
// default deny on them. A new openshift-* namespace is created without adopt.
func TestStress_Adopt_SystemNamespaceRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]interface{}{
		"kube-system":                         {"adopt": true, "mode": "enforce"},
		"default":                             {"adopt": true, "mode": "enforce"},
		"openshift-dns":                       {"adopt": true, "mode": "enforce"},
		"open-cluster-management-agent-addon": {"adopt": true, "mode": "enforce"},
		"via-override":                        {"adopt": true, "mode": "enforce", "namespace": "openshift-dns"},
		"openshift-brand-new":                 {},
		"kube-brand-new":                      {},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ns := name
			if v, ok := p["namespace"]; ok {
				ns = v.(string)
			}
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
				q := map[string]interface{}{"team": "x", "environment": "PROD", "networkProfile": "strict",
					"groups": map[string]interface{}{"admin": "TENANT_ADMINS"}}
				for k, v := range p {
					q[k] = v
				}
				stressProjects(cfg)[name] = q
			}})
			var got []string
			for k, v := range stressRendered(r.objs) {
				if k == "Namespace||"+ns || strings.Contains(k, "|"+ns+"|") {
					got = append(got, k+" ("+v+")")
				}
			}
			sort.Strings(got)
			if len(got) > 0 {
				t.Errorf("HIGH: system namespace %s is onboarded, rendered: %v", ns, got)
			}
		})
	}
}

// A newly added project that sorts earlier and (by mistake) names an existing project's namespace
// takes the claim: the existing, working project is skipped and stops being reconciled. The
// newcomer is then blocked by the namespace's ownership label, so neither renders.
func TestStress_Dup_EarlierNewcomerDisplacesExistingProject(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		stressProjects(cfg)["aaa-typo"] = map[string]interface{}{"namespace": "payments-api", "groups": map[string]interface{}{"admin": "X"}}
	}})
	if p := stressPruned(r.objs); len(pruned(r.objs, "RoleBinding", "payments-api", "")) != 0 {
		t.Errorf("CRITICAL: payments-api objects pruned: %v", p)
	}
	if namespaceNamed(r.objs, "payments-api") == nil {
		t.Errorf("MEDIUM: payments-api (already owns its namespace) is skipped because aaa-typo sorts first; status %v", stressStatus(t, r.objs))
	}
}

// EgressIP addresses are keyed by value, so different notations of one address conflict.
func TestStress_EgressIP_NotationConflict(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
		stressAddAB(cfg)
		stressProject(cfg, "stress-a")["egressIP"] = []interface{}{"2001:db8::a"}
		stressProject(cfg, "stress-b")["egressIP"] = []interface{}{"2001:0DB8:0:0:0:0:0:A"}
	}})
	st := stressStatus(t, r.objs)
	for _, p := range []string{"stress-a", "stress-b"} {
		if !strings.Contains(st[p], "is also declared by project") {
			t.Errorf("%s: want the shared address reported, status %q", p, st[p])
		}
	}
	for k := range stressRendered(r.objs) {
		if strings.HasPrefix(k, "EgressIP||autoshift-stress-") {
			t.Errorf("conflicting EgressIP rendered: %s", k)
		}
	}
}

// Every annotation is applied to the Namespace without validation: an invalid key is not reported
// and makes the API server reject the whole Namespace update (for every project when it is a
// projectDefaults entry). Labels with the same mistake are reported and skipped.
func TestStress_Defaults_InvalidAnnotationKeyNotReported(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"bad key", "a/b/c", "-leading", strings.Repeat("k", 64)} {
		t.Run(key, func(t *testing.T) {
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
				cfg["projects"] = stressDeploymentConfig(t)["projects"]
				cfg["projectDefaults"] = map[string]interface{}{"annotations": map[string]interface{}{
					"note": map[string]interface{}{"key": key, "value": "x"},
				}}
			}})
			ns := namespaceNamed(r.objs, "ops-console")
			if ns == nil {
				t.Fatal("ops-console not rendered")
			}
			if _, ok := stringMap(ns, "annotations")[key]; ok {
				t.Errorf("MEDIUM: invalid annotation key %q rendered on every project's Namespace (the API server rejects the update); status %v",
					key, stressStatus(t, r.objs))
			}
		})
	}
}

// Malformed projectDefaults fails the hub template (documented). Measured here against the
// deployment-shaped projects: every one of them stops being reconciled.
func TestStress_Defaults_MalformedFailsHubForRealProjects(t *testing.T) {
	t.Parallel()
	for name, d := range map[string]interface{}{
		"list":          []interface{}{"x"},
		"labels string": map[string]interface{}{"labels": "x"},
		"groups list":   map[string]interface{}{"groups": []interface{}{"x"}},
		"sizes string":  map[string]interface{}{"sizes": "small"},
		"profiles list": map[string]interface{}{"networkProfiles": []interface{}{"x"}},
		"roles scalar":  map[string]interface{}{"roles": "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			r := stressRun(t, stressOpts{allowHubErr: true, mutate: func(cfg map[string]interface{}) {
				cfg["projects"] = stressDeploymentConfig(t)["projects"]
				cfg["projectDefaults"] = d
			}})
			if len(r.hubErrs) == 0 {
				t.Errorf("projectDefaults %s: want a hub failure", name)
				return
			}
			t.Logf("projectDefaults %s: hub fails for all %d projects: %v", name, len(stressDeploymentConfig(t)["projects"].(map[string]interface{})), r.hubErrs)
		})
	}
}

// The deployment-shaped enforce projects render cleanly: one Namespace and three RoleBindings each, enforce.
func TestStress_RealProjects_Baseline(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		cfg["projects"] = stressDeploymentConfig(t)["projects"]
		delete(cfg, "projectDefaults")
	}})
	var ns, rb int
	for k, v := range stressRendered(r.objs) {
		if v != "enforce" {
			continue
		}
		if strings.HasPrefix(k, "Namespace|") {
			ns++
		}
		if strings.HasPrefix(k, "RoleBinding|") {
			rb++
		}
	}
	n := 0
	for _, p := range stressDeploymentConfig(t)["projects"].(map[string]interface{}) {
		if m, _ := p.(map[string]interface{}); m["mode"] == "enforce" {
			n++
		}
	}
	if ns != n || rb != 3*n {
		t.Errorf("want %d Namespaces and %d RoleBindings, got %d and %d", n, 3*n, ns, rb)
	}
}

// ResourceQuota and LimitRange quantities are rendered verbatim and compared exactly
// (mustonlyhave), but the API server stores them canonicalised (0.5 -> 500m, 1000m -> 1,
// 1024Mi -> 1Gi, +5 -> 5, 1e3 -> 1k). A non-canonical value the template accepts would never match
// the live object: the ResourceQuota is rewritten on every evaluation and stays NonCompliant.
// Suspected, not verified against a live config-policy-controller.
func TestStress_Quota_NonCanonicalQuantity(t *testing.T) {
	t.Parallel()
	for in, canon := range map[string]string{"0.5": "500m", "1000m": "1", "1024Mi": "1Gi", "+5": "5", "1e3": "1k", "1.5Gi": "1536Mi"} {
		t.Run(in, func(t *testing.T) {
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
				stressProject(cfg, "payments-api")["quota"] = map[string]interface{}{"limits.cpu": in}
			}})
			hard := quotaHard(r.objs, "payments-api")
			got := fmt.Sprint(hard["limits.cpu"])
			st := stressStatus(t, r.objs)["payments-api"]
			if got != canon && st == "" {
				t.Errorf("HIGH (suspected flapping): limits.cpu %q rendered as %q, the API server stores %q; not reported", in, got, canon)
			}
		})
	}
}

// A project cannot opt out of an access level projectDefaults.groups defines: an empty or null
// override falls back to the default template (`index $ownGroups $level | default ...`), so a
// read-only project still gets the write RoleBinding and nothing reports it.
func TestStress_Groups_CannotOptOutOfDefaultLevel(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]interface{}{"empty string": "", "null": nil, "false": false} {
		t.Run(name, func(t *testing.T) {
			r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, mutate: func(cfg map[string]interface{}) {
				stressAddAB(cfg)
				stressProject(cfg, "stress-a")["groups"].(map[string]interface{})["write"] = v
			}})
			if a, ok := stressRendered(r.objs)["RoleBinding|stress-a|autoshift-write"]; ok && stressStatus(t, r.objs)["stress-a"] == "" {
				t.Errorf("LOW: groups.write=%#v still renders autoshift-write (%s) from projectDefaults; no way to drop a default level, not reported", v, a)
			}
		})
	}
}
