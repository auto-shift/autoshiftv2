//go:build integration

package resolver

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// P1b stress: conflicts and documented limits of project-onboarding.

func stressDefaults(cfg map[string]interface{}) map[string]interface{} {
	d, ok := cfg["projectDefaults"].(map[string]interface{})
	if !ok {
		d = map[string]interface{}{}
		cfg["projectDefaults"] = d
	}
	return d
}

func stressAdminEntries(n int) map[string]interface{} {
	out := map[string]interface{}{}
	for i := 0; i < n; i++ {
		out[fmt.Sprintf("e%03d", i)] = map[string]interface{}{
			"appliesTo": map[string]interface{}{"allProjects": true},
			"direction": "egress", "action": "deny",
			"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("blocked-%d", i)}}},
		}
	}
	return out
}

const stressLiveAdminANP = `
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-admin-live-old
  labels:
    autoshift.io/managed-by: project-onboarding
spec:
  priority: 10
  subject:
    namespaces: {}
  egress: []
`

func stressAdminRendered(objs []onboardingObject) []string {
	var out []string
	for k := range stressRendered(objs) {
		if strings.HasPrefix(k, "AdminNetworkPolicy||autoshift-admin-") {
			out = append(out, k)
		}
	}
	return out
}

// 40 admin entries fit (priorities 10..49); 41 fail the tier closed: nothing renders, nothing
// admin is pruned, and the error is reported.
func TestStress_Limits_AdminEntries40And41(t *testing.T) {
	t.Parallel()
	for _, n := range []int{40, 41} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := stressRun(t, stressOpts{labels: pruneOn, extra: stressLiveAdminANP, mutate: func(cfg map[string]interface{}) {
				stressDefaults(cfg)["adminNetworkPolicies"] = stressAdminEntries(n)
			}})
			got := stressAdminRendered(r.objs)
			st := stressStatus(t, r.objs)["projectDefaults.adminNetworkPolicies"]
			_, livePruned := stressPruned(r.objs)["AdminNetworkPolicy||autoshift-admin-live-old"]
			if n == 40 {
				if len(got) != 40 || st != "" || !livePruned {
					t.Errorf("40 entries: rendered %d, status %q, stale admin pruned %v", len(got), st, livePruned)
				}
				for _, o := range r.objs {
					if o.complianceType == "mustnothave" || stressKey(o) != "AdminNetworkPolicy||autoshift-admin-e039" {
						continue
					}
					if p := o.obj["spec"].(map[string]interface{})["priority"]; fmt.Sprint(p) != "49" {
						t.Errorf("last admin entry priority %v, want 49", p)
					}
				}
				return
			}
			if len(got) != 0 || !strings.Contains(st, "41 enabled entries") || livePruned {
				t.Errorf("41 entries: rendered %d, status %q, stale admin pruned %v", len(got), st, livePruned)
			}
		})
	}
}

// One invalid admin entry fails the tier closed and does not affect projects.
func TestStress_Limits_AdminInvalidEntryFailsClosed(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{labels: pruneOn, extra: stressLiveAdminANP, mutate: func(cfg map[string]interface{}) {
		e := stressAdminEntries(3)
		e["bad"] = map[string]interface{}{"appliesTo": map[string]interface{}{"allProjects": true}, "direction": "sideways"}
		stressDefaults(cfg)["adminNetworkPolicies"] = e
	}})
	if got := stressAdminRendered(r.objs); len(got) != 0 {
		t.Errorf("tier in error but rendered %v", got)
	}
	if _, ok := stressPruned(r.objs)["AdminNetworkPolicy||autoshift-admin-live-old"]; ok {
		t.Error("tier in error but a live admin AdminNetworkPolicy is pruned")
	}
	if namespaceNamed(r.objs, "payments-api") == nil {
		t.Error("an admin tier error must not block projects")
	}
}

func stressDenyRules(n int, prefix string) map[string]interface{} {
	rules := map[string]interface{}{}
	for i := 0; i < n; i++ {
		rules[fmt.Sprintf("%s%03d", prefix, i)] = map[string]interface{}{
			"direction": "egress", "action": "deny",
			"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("ns-%d", i)}}},
		}
	}
	return rules
}

func stressANPRules(objs []onboardingObject, name, dir string) int {
	o := stressObj(objs, "AdminNetworkPolicy", "", name)
	if o == nil {
		return -1
	}
	l, _ := o.obj["spec"].(map[string]interface{})[dir].([]interface{})
	return len(l)
}

// A project AdminNetworkPolicy holds at most 100 rules per direction; 101 is a project error.
func TestStress_Limits_ProjectANPRules100And101(t *testing.T) {
	t.Parallel()
	for _, n := range []int{100, 101} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
				stressProjects(cfg)["big-anp"] = map[string]interface{}{"team": "x", "environment": "PROD",
					"network": map[string]interface{}{"rules": stressDenyRules(n, "d")}}
			}})
			got := stressANPRules(r.objs, "autoshift-project-big-anp", "egress")
			st := stressStatus(t, r.objs)["big-anp"]
			if n == 100 && (got != 100 || st != "") {
				t.Errorf("100 rules: rendered %d, status %q", got, st)
			}
			if n == 101 && (got != -1 || !strings.Contains(st, "101 egress rules")) {
				t.Errorf("101 rules: rendered %d, status %q", got, st)
			}
		})
	}
}

// The override stage counts too: 60 profile denies plus 41 project in-cluster allows repeated as
// Allow make 101 egress rules.
func TestStress_Limits_ProjectANPRulesWithOverrides(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		profiles := stressDefaults(cfg)["networkProfiles"].(map[string]interface{})
		profiles["many-deny"] = map[string]interface{}{"rules": stressDenyRules(60, "pd")}
		allows := map[string]interface{}{}
		for i := 0; i < 41; i++ {
			allows[fmt.Sprintf("a%03d", i)] = map[string]interface{}{"direction": "egress", "action": "allow",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"example.com/app": fmt.Sprintf("app-%d", i)}}}}
		}
		stressProjects(cfg)["ovr"] = map[string]interface{}{"team": "x", "environment": "PROD", "networkProfile": "many-deny",
			"network": map[string]interface{}{"rules": allows}}
	}})
	st := stressStatus(t, r.objs)["ovr"]
	if !strings.Contains(st, "101 egress rules") {
		t.Errorf("want the 101-rule limit reported, status %q, rendered %d", st, stressANPRules(r.objs, "autoshift-project-ovr", "egress"))
	}
}

// Profile deny vs project allow: the project allow is placed ahead of the profile deny. Project
// deny vs profile allow: the deny is in the AdminNetworkPolicy, which OVN-K evaluates before the
// NetworkPolicy allow.
func TestStress_Conflict_ProfileDenyProjectAllow(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		profiles := stressDefaults(cfg)["networkProfiles"].(map[string]interface{})
		profiles["deny-db"] = map[string]interface{}{"rules": map[string]interface{}{
			"db": map[string]interface{}{"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"db"}}}},
			"web": map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"web"}}}},
		}}
		stressProjects(cfg)["cf"] = map[string]interface{}{"team": "x", "environment": "PROD", "networkProfile": "deny-db",
			"network": map[string]interface{}{"rules": map[string]interface{}{
				"db-ok":  map[string]interface{}{"direction": "egress", "action": "allow", "to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"db"}}}},
				"no-web": map[string]interface{}{"direction": "ingress", "action": "deny", "from": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"web"}}}},
			}}}
	}})
	o := stressObj(r.objs, "AdminNetworkPolicy", "", "autoshift-project-cf")
	if o == nil {
		t.Fatalf("no project ANP; status %v", stressStatus(t, r.objs))
	}
	spec := o.obj["spec"].(map[string]interface{})
	var order []string
	for _, e := range spec["egress"].([]interface{}) {
		m := e.(map[string]interface{})
		order = append(order, fmt.Sprintf("%v:%v", m["name"], m["action"]))
	}
	if strings.Join(order, ",") != "db-ok:Allow,db:Deny" {
		t.Errorf("egress order %v, want the project allow ahead of the profile deny", order)
	}
	var ing []string
	for _, e := range spec["ingress"].([]interface{}) {
		m := e.(map[string]interface{})
		ing = append(ing, fmt.Sprintf("%v:%v", m["name"], m["action"]))
	}
	if strings.Join(ing, ",") != "no-web:Deny" {
		t.Errorf("ingress %v, want the project deny alone (profile allow stays a NetworkPolicy)", ing)
	}
}

// Admin allProjects deny vs project ANP allow: admin priorities 10..49 sit ahead of 50.
func TestStress_Conflict_AdminDenyBeatsProjectAllow(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		stressProjects(cfg)["md"] = map[string]interface{}{"team": "x", "environment": "PROD",
			"network": map[string]interface{}{"rules": map[string]interface{}{
				"x":  map[string]interface{}{"direction": "egress", "action": "deny", "to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"other"}}}},
				"md": map[string]interface{}{"direction": "egress", "action": "allow", "to": map[string]interface{}{"cidrs": []interface{}{"169.254.169.254/32"}}},
			}}}
	}})
	admin := stressObj(r.objs, "AdminNetworkPolicy", "", "autoshift-admin-deny-metadata")
	proj := stressObj(r.objs, "AdminNetworkPolicy", "", "autoshift-project-md")
	if admin == nil || proj == nil {
		t.Fatalf("admin %v project %v; status %v", admin != nil, proj != nil, stressStatus(t, r.objs))
	}
	ap := fmt.Sprint(admin.obj["spec"].(map[string]interface{})["priority"])
	pp := fmt.Sprint(proj.obj["spec"].(map[string]interface{})["priority"])
	if ap >= pp {
		t.Errorf("admin priority %s not ahead of project %s", ap, pp)
	}
}

// EgressFirewall: 8000 rules fit, 8001 is a project error (counted on the managed cluster, final
// denies included).
func TestStress_Limits_EgressFirewall8000(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		names    int
		deny     bool
		wantFail bool
	}{{8000, false, false}, {8001, false, true}, {7999, true, false}, {8000, true, true}} {
		t.Run(fmt.Sprintf("%d+deny=%v", tc.names, tc.deny), func(t *testing.T) {
			if tc.deny && os.Getenv("STRESS_SCALE") == "" {
				// The same boundary with the final deny counted runs in every run in
				// TestProjectOnboarding_EgressFirewallLimit ("at the limit": 7999 entries and the
				// final deny fit; "over the limit": 8000 and the final deny do not).
				t.Skip("set STRESS_SCALE=1; covered in every run by TestProjectOnboarding_EgressFirewallLimit")
			}
			r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
				var names []interface{}
				for i := 0; i < tc.names; i++ {
					names = append(names, fmt.Sprintf("h%d.example.com", i))
				}
				stressProjects(cfg)["big-ef"] = map[string]interface{}{"team": "x", "environment": "PROD",
					"network": map[string]interface{}{
						"defaultDeny": map[string]interface{}{"egress": tc.deny},
						"rules": map[string]interface{}{"ext": map[string]interface{}{"direction": "egress", "action": "allow",
							"to": map[string]interface{}{"dnsNames": names}}}}}
			}})
			st := stressStatus(t, r.objs)["big-ef"]
			ef := stressObj(r.objs, "EgressFirewall", "big-ef", "default")
			hubSize, _ := stressPolicyJSONSize(t, r.hub)
			t.Logf("hub-resolved Policy %d bytes, hub %v, spoke %v", hubSize, r.hubDur, r.spokeDur)
			if tc.wantFail && (ef != nil || !strings.Contains(st, "more than the 8000")) {
				t.Errorf("want the 8000-rule limit reported and nothing rendered; rendered %v, status %q", ef != nil, st)
			}
			if !tc.wantFail && (ef == nil || st != "") {
				t.Errorf("want the EgressFirewall rendered; rendered %v, status %q", ef != nil, st)
			}
		})
	}
}

// Undefined size or profile skips the project and keeps its objects (covered for size in the state
// machine; here for the profile).
func TestStress_Undefined_ProfileKeepsObjects(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
		stressAddAB(cfg)
		stressProject(cfg, "stress-a")["networkProfile"] = "no-such"
	}})
	stressCheckNever(t, r.objs)
	for k := range stressPruned(r.objs) {
		if strings.Contains(k, "stress-a") && !strings.HasPrefix(k, "RoleBinding|stress-b|") {
			t.Errorf("errored stress-a has a prune entry %s", k)
		}
	}
	if !strings.Contains(stressStatus(t, r.objs)["stress-a"], "networkProfile no-such is not defined") {
		t.Errorf("status %v", stressStatus(t, r.objs))
	}
}

// Status keys: invalid project names are reported under the name with invalid characters replaced
// by "_". Distinct names that map to the same key overwrite each other, so one error is lost.
func TestStress_Status_KeyCollision(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		p := stressProjects(cfg)
		p["a b"] = map[string]interface{}{"team": "x", "environment": "PROD", "mode": "bogus1"}
		p["a_b"] = map[string]interface{}{"team": "x", "environment": "PROD", "mode": "bogus2"}
		p["a?b"] = map[string]interface{}{"team": "x", "environment": "PROD", "mode": "bogus3"}
	}})
	st := stressStatus(t, r.objs)
	joined := ""
	for _, v := range st {
		joined += v + "\n"
	}
	for _, m := range []string{"bogus1", "bogus2", "bogus3"} {
		if !strings.Contains(joined, m) {
			t.Errorf("MEDIUM: the error of the project with mode %s is lost (status keys collide on %q): %v", m, "a_b", st)
		}
	}
}

// Duplicate project names after case folding: only the DNS-1123 one is valid, the other is
// reported; neither takes the other's namespace.
func TestStress_Dup_CaseFolding(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		p := stressProjects(cfg)
		p["Payments-API"] = map[string]interface{}{"team": "evil", "environment": "PROD", "namespace": "payments-api2"}
		p["casefold"] = map[string]interface{}{"team": "x", "environment": "PROD"}
		p["CaseFold"] = map[string]interface{}{"team": "x", "environment": "PROD"}
	}})
	st := stressStatus(t, r.objs)
	for _, k := range []string{"Payments-API", "CaseFold"} {
		if !strings.Contains(st[k], "is not a valid DNS-1123 label") {
			t.Errorf("%s: status %q", k, st[k])
		}
	}
	if namespaceNamed(r.objs, "payments-api2") != nil || namespaceNamed(r.objs, "casefold") == nil || namespaceNamed(r.objs, "payments-api") == nil {
		t.Errorf("rendered set wrong: %v", st)
	}
}

// Duplicate EgressIP in one list is rendered once; the same address in two projects skips both.
func TestStress_EgressIP_DuplicateInListAndAcross(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{mutate: func(cfg map[string]interface{}) {
		p := stressProjects(cfg)
		p["eip-a"] = map[string]interface{}{"team": "x", "environment": "PROD", "egressIP": []interface{}{"198.51.100.110", "198.51.100.110"}}
		p["eip-b"] = map[string]interface{}{"team": "x", "environment": "PROD", "egressIP": []interface{}{"198.51.100.120"}}
		p["eip-c"] = map[string]interface{}{"team": "x", "environment": "PROD", "egressIP": []interface{}{"198.51.100.120", "198.51.100.121"}}
	}})
	a := stressObj(r.objs, "EgressIP", "", "autoshift-eip-a")
	if a == nil {
		t.Fatalf("eip-a not rendered: %v", stressStatus(t, r.objs))
	}
	if l := a.obj["spec"].(map[string]interface{})["egressIPs"].([]interface{}); len(l) != 1 {
		t.Errorf("duplicate in one list rendered %d times", len(l))
	}
	for _, n := range []string{"eip-b", "eip-c"} {
		if stressObj(r.objs, "EgressIP", "", "autoshift-"+n) != nil || namespaceNamed(r.objs, n) != nil {
			t.Errorf("%s shares an address but is rendered", n)
		}
	}
}

// A project moved to a new namespace prunes everything in the old one (its default denies and
// RoleBindings included), but the old Namespace keeps autoshift.io/project=<project>. The project's
// EgressIP and AdminNetworkPolicy select by that label, so they keep applying to the abandoned
// namespace, and the namespace stays claimed so no other project can take it. Nothing reports it.
func TestStress_Prune_MovedNamespaceLeavesLabeledOrphan(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{stubs: []string{stressPruneStubs}, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
		stressAddAB(cfg)
		stressProject(cfg, "stress-a")["namespace"] = "stress-a-moved"
	}})
	stressCheckNever(t, r.objs)
	p := stressPruned(r.objs)
	_, denyGone := p["NetworkPolicy|stress-a|autoshift-default-deny-ingress"]
	_, eipKept := stressRendered(r.objs)["EgressIP||autoshift-stress-a"]
	oldNs := namespaceNamed(r.objs, "stress-a")
	stripped := oldNs != nil && stringMap(oldNs, "labels")["autoshift.io/project"] == ""
	if denyGone && eipKept && !stripped {
		t.Errorf("MEDIUM: old namespace stress-a loses its default denies (pruned) yet keeps autoshift.io/project=stress-a, "+
			"so EgressIP autoshift-stress-a and the project AdminNetworkPolicy still select it; status %v", stressStatus(t, r.objs))
	}
}

// stressObj is objectNamed that also finds cluster-scoped objects (no metadata.namespace).
func stressObj(objs []onboardingObject, kind, namespace, name string) *onboardingObject {
	want := kind + "|" + namespace + "|" + name
	for i, o := range objs {
		if o.complianceType != "mustnothave" && !isStatus(o) && stressKey(o) == want {
			return &objs[i]
		}
	}
	return nil
}
