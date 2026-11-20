//go:build integration

package resolver

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Any NetworkPolicy isolates the pods it selects in its direction: everything it does not allow is
// denied. An allow rule in a direction without a default deny therefore renders no NetworkPolicy,
// or it would silently deny all other traffic of that direction (found live: a project with a
// profile deny and one project allow lost DNS and all other egress). Such a rule is still carried
// by the AdminNetworkPolicy (an override of a profile deny) and the EgressFirewall (cidrs and
// dnsNames).

// isolationRules are allow rules of every peer type, in both directions.
func isolationRules() map[string]interface{} {
	in := func(from map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"direction": "ingress", "action": "allow", "from": from}
	}
	out := func(to map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"direction": "egress", "action": "allow", "to": to}
	}
	return map[string]interface{}{
		"allow-client": map[string]interface{}{"direction": "egress", "action": "allow", "ports": []interface{}{8080},
			"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"client"}}}},
		"to-pods":         out(map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"app": "db"}}}),
		"to-self":         out(map[string]interface{}{"sameNamespace": true}),
		"to-router":       out(map[string]interface{}{"router": true}),
		"to-monitoring":   out(map[string]interface{}{"monitoring": true}),
		"to-dns":          out(map[string]interface{}{"dns": true}),
		"to-ext":          out(map[string]interface{}{"cidrs": []interface{}{"198.51.100.0/24"}}),
		"to-name":         out(map[string]interface{}{"dnsNames": []interface{}{"api.example.com"}}),
		"from-ns":         in(map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"example.com/team": "web"}}}),
		"from-pods":       in(map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"app": "web"}}}),
		"from-self":       in(map[string]interface{}{"sameNamespace": true}),
		"from-router":     in(map[string]interface{}{"router": true}),
		"from-monitoring": in(map[string]interface{}{"monitoring": true}),
		"from-ext":        in(map[string]interface{}{"cidrs": []interface{}{"198.51.100.0/24"}}),
	}
}

// isolationRun renders reporting with the deny-client profile (an egress deny to namespace client,
// no default deny) and isolationRules, and defaultDeny when it is not nil.
func isolationRun(t *testing.T, defaultDeny map[string]interface{}) []onboardingObject {
	t.Helper()
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["deny-client"] = map[string]interface{}{
			"rules": map[string]interface{}{"deny-client": map[string]interface{}{"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"client"}}}}},
		}
		p := cfg["projects"].(map[string]interface{})["reporting"].(map[string]interface{})
		p["networkProfile"] = "deny-client"
		network := map[string]interface{}{"rules": isolationRules()}
		if defaultDeny != nil {
			network["defaultDeny"] = defaultDeny
		}
		p["network"] = network
	})
	objs := onboardingObjects(t, res)
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Fatalf("want no status entries, got %v", status)
	}
	return objs
}

// isolationANPEgress is the AdminNetworkPolicy egress of isolationRun: with a profile egress deny,
// every project egress allow with an in-cluster peer is repeated as an Allow ahead of it.
const isolationANPEgress = "allow-client:Allow,to-dns:Allow,to-monitoring:Allow,to-pods:Allow,to-router:Allow,to-self:Allow,deny-client:Deny"

// anpOrder returns the name:action list of the project AdminNetworkPolicy rules of direction.
func anpOrder(t *testing.T, objs []onboardingObject, project, direction string) string {
	t.Helper()
	o := stressObj(objs, "AdminNetworkPolicy", "", "autoshift-project-"+project)
	if o == nil {
		t.Fatalf("no autoshift-project-%s", project)
	}
	rules, _ := o.obj["spec"].(map[string]interface{})[direction].([]interface{})
	var order []string
	for _, r := range rules {
		m := r.(map[string]interface{})
		order = append(order, fmt.Sprintf("%v:%v", m["name"], m["action"]))
	}
	return strings.Join(order, ",")
}

// The override shape: no default deny, so no NetworkPolicy at all, while the AdminNetworkPolicy
// still places the project allow ahead of the profile deny and the EgressFirewall still carries the
// external allows, without a final deny.
func TestProjectOnboarding_AllowWithoutDefaultDenyNotIsolating(t *testing.T) {
	t.Parallel()
	objs := isolationRun(t, nil)
	if n := networkPoliciesIn(objs, "reporting"); len(n) != 0 {
		t.Errorf("reporting has no default deny, want no NetworkPolicy (each would isolate the pods), got %v", n)
	}
	if got := anpOrder(t, objs, "reporting", "egress"); got != isolationANPEgress {
		t.Errorf("AdminNetworkPolicy egress = %s, want %s: the project in-cluster allows ahead of the profile deny", got, isolationANPEgress)
	}
	ef := egressFirewallIn(objs, "reporting")
	if ef == nil {
		t.Fatal("the egress cidrs and dnsNames allows still need the EgressFirewall")
	}
	want := `[{"to":{"cidrSelector":"198.51.100.0/24"},"type":"Allow"},{"to":{"dnsName":"api.example.com"},"type":"Allow"}]`
	if got := efEgress(ef); got != want {
		t.Errorf("EgressFirewall egress = %s, want %s", got, want)
	}
}

// With a default deny both ways, every allow rule with an in-cluster or ingress cidrs peer renders
// its NetworkPolicy as before; only the egress cidrs and dnsNames rules stay EgressFirewall-only.
func TestProjectOnboarding_AllowWithDefaultDenyRendersNetworkPolicies(t *testing.T) {
	t.Parallel()
	objs := isolationRun(t, map[string]interface{}{"ingress": true, "egress": true})
	var want []string
	for name := range isolationRules() {
		if name != "to-ext" && name != "to-name" {
			want = append(want, "autoshift-rule-"+name)
		}
	}
	want = append(want, "autoshift-default-deny-ingress", "autoshift-default-deny-egress")
	got := networkPoliciesIn(objs, "reporting")
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("NetworkPolicies = %v, want %v", got, want)
	}
	for _, name := range got {
		checkOwnedExact(t, networkPolicyNamed(objs, "reporting", name), "reporting", "enforce")
	}
	np := networkPolicyNamed(objs, "reporting", "autoshift-rule-allow-client")
	if np == nil || asJSON(npSpec(np)) != `{"egress":[{"ports":[{"port":8080,"protocol":"TCP"}],"to":[{"namespaceSelector":{"matchExpressions":[{"key":"kubernetes.io/metadata.name","operator":"In","values":["client"]}]}}]}],"podSelector":{},"policyTypes":["Egress"]}` {
		t.Errorf("autoshift-rule-allow-client = %v", np)
	}
	if got := anpOrder(t, objs, "reporting", "egress"); got != isolationANPEgress {
		t.Errorf("AdminNetworkPolicy egress = %s, want %s: the project in-cluster allows ahead of the profile deny", got, isolationANPEgress)
	}
	if ef := egressFirewallIn(objs, "reporting"); ef == nil || !strings.HasSuffix(efEgress(ef), efDeny4+"]") {
		t.Errorf("defaultDeny.egress: want the EgressFirewall to end in the final deny, got %v", ef)
	}
}

// Directions are independent: a default deny on ingress only renders the ingress allows, and no
// egress NetworkPolicy.
func TestProjectOnboarding_AllowIsolationPerDirection(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"ingress", "egress"} {
		t.Run(dir, func(t *testing.T) {
			objs := isolationRun(t, map[string]interface{}{dir: true})
			if len(networkPoliciesIn(objs, "reporting")) == 0 {
				t.Fatal("want NetworkPolicies in the isolated direction")
			}
			for _, name := range networkPoliciesIn(objs, "reporting") {
				types := asJSON(npSpec(networkPolicyNamed(objs, "reporting", name))["policyTypes"])
				if types != fmt.Sprintf(`["%s"]`, strings.ToUpper(dir[:1])+dir[1:]) {
					t.Errorf("only %s has a default deny, got %s with policyTypes %s", dir, name, types)
				}
			}
		})
	}
}

// The ingress counterpart of the override shape: a project allow overriding a profile ingress deny,
// without a default deny, is only an AdminNetworkPolicy Allow.
func TestProjectOnboarding_IngressOverrideWithoutDefaultDeny(t *testing.T) {
	t.Parallel()
	web := map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"web"}}}
	res := runOnboardingConfig(t, func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["deny-web"] = map[string]interface{}{
			"rules": map[string]interface{}{"deny-web": map[string]interface{}{"direction": "ingress", "action": "deny", "from": web}},
		}
		p := cfg["projects"].(map[string]interface{})["reporting"].(map[string]interface{})
		p["networkProfile"] = "deny-web"
		p["network"] = map[string]interface{}{"rules": map[string]interface{}{
			"allow-web": map[string]interface{}{"direction": "ingress", "action": "allow", "from": web, "ports": []interface{}{8080}},
		}}
	})
	objs := onboardingObjects(t, res)
	if status := onboardingStatus(t, objs); len(status) != 0 {
		t.Fatalf("want no status entries, got %v", status)
	}
	if n := networkPoliciesIn(objs, "reporting"); len(n) != 0 {
		t.Errorf("reporting has no default deny, want no NetworkPolicy, got %v", n)
	}
	if got := anpOrder(t, objs, "reporting", "ingress"); got != "allow-web:Allow,deny-web:Deny" {
		t.Errorf("AdminNetworkPolicy ingress = %s, want the project allow ahead of the profile deny", got)
	}
}

// An ingress cidrs allow without a default deny renders nothing, so its managed-cluster checks do
// not run either: unknown cluster networks do not skip a project for a rule that is not rendered.
func TestProjectOnboarding_IngressCidrsWithoutDefaultDenyUnchecked(t *testing.T) {
	t.Parallel()
	res := runOnboardingAll(t, testdataWithout(t, onboardingNetworkStubs, ""), nil, func(c *ExampleConfigs) {
		c.ClusterInstallConfig["networking"] = map[string]interface{}{"clusterNetwork": []interface{}{}, "serviceNetwork": []interface{}{}}
		p := c.HubConfig["projects"].(map[string]interface{})["reporting"].(map[string]interface{})
		p["network"] = map[string]interface{}{"rules": map[string]interface{}{
			"from-ext": map[string]interface{}{"direction": "ingress", "action": "allow", "from": map[string]interface{}{"cidrs": []interface{}{"198.51.100.0/24"}}},
		}}
	})
	objs := onboardingObjects(t, res)
	if s := onboardingStatus(t, objs)["reporting"]; s != "" {
		t.Errorf("want no status for reporting, got %q", s)
	}
	if namespaceNamed(objs, "reporting") == nil || len(networkPoliciesIn(objs, "reporting")) != 0 {
		t.Errorf("want reporting rendered without a NetworkPolicy, got %v", networkPoliciesIn(objs, "reporting"))
	}
}

// An allow NetworkPolicy rendered before (here payments-api's from-frontend, live in the testdata)
// becomes stale once its direction has no default deny: pruned with prune on, reported with prune
// off, like any owned object no longer declared. The project itself stays clean.
func TestProjectOnboarding_PruneAllowWithoutDefaultDeny(t *testing.T) {
	t.Parallel()
	open := func(projects map[string]interface{}) {
		projects["payments-api"].(map[string]interface{})["network"].(map[string]interface{})["defaultDeny"] = map[string]interface{}{"ingress": false}
	}
	for _, c := range []struct {
		name   string
		run    func(*testing.T, func(map[string]interface{})) ChartResult
		action string
	}{{"prune on", runPrune, "enforce"}, {"prune off", runOnboarding, "inform"}} {
		t.Run(c.name, func(t *testing.T) {
			objs := onboardingObjects(t, c.run(t, open))
			if status := onboardingStatus(t, objs); status["payments-api"] != "" {
				t.Errorf("want no status for payments-api, got %q", status["payments-api"])
			}
			for _, name := range []string{"autoshift-rule-from-frontend", "autoshift-default-deny-ingress"} {
				if networkPolicyNamed(objs, "payments-api", name) != nil {
					t.Errorf("ingress has no default deny, want no %s", name)
				}
			}
			if p := pruned(objs, "NetworkPolicy", "payments-api", "autoshift-rule-from-frontend"); len(p) != 1 || p[0].remediationAction != c.action {
				t.Errorf("want the stale autoshift-rule-from-frontend handled by %s, got %v", c.action, p)
			}
			if networkPolicyNamed(objs, "payments-api", "autoshift-default-deny-egress") == nil {
				t.Error("egress keeps the strict profile's default deny, want autoshift-default-deny-egress")
			}
		})
	}
}
