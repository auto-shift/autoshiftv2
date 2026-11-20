//go:build integration

package resolver

// Adversarial input tests for the network part of stable/project-onboarding (stress pass P1a):
// network rules, peers, ports, CIDRs, DNS names, defaultDeny, admin entries and egressIP. Each test
// asserts the contract in policies/stable/project-onboarding/README.md; a failing test is a finding.

import (
	"fmt"
	"strings"
	"testing"
)

// withRule sets reporting's networkProfile (when profile is not "") and adds rule name to its
// network.rules.
func withRule(profile, name string, rule interface{}) func(projects map[string]interface{}) {
	return func(projects map[string]interface{}) {
		if profile != "" {
			proj(projects, "reporting")["networkProfile"] = profile
		}
		projectRules(projects, "reporting")[name] = rule
	}
}

func ingressFrom(action string, from map[string]interface{}, ports ...interface{}) map[string]interface{} {
	r := map[string]interface{}{"direction": "ingress", "action": action, "from": from}
	if len(ports) > 0 {
		r["ports"] = ports
	}
	return r
}

// reportedSkipped asserts that reporting is reported (status contains want, or any error when want
// is "") and none of its NetworkPolicies, EgressFirewall or Namespace render.
func reportedSkipped(t *testing.T, objs []onboardingObject, want string) {
	t.Helper()
	got := onboardingStatus(t, objs)["reporting"]
	if got == "" || !strings.Contains(got, want) {
		t.Errorf("want reporting reported (%q), got status %q; rendered NetworkPolicies %v, EgressFirewall %v",
			want, got, networkPoliciesIn(objs, "reporting"), egressFirewallIn(objs, "reporting") != nil)
	}
	otherProjectsRender(t, objs, "reporting")
}

// ---- network keys ----------------------------------------------------------------------------

// "A profile, and a project's network, have two keys: defaultDeny and rules." A misspelt key is
// silently ignored today, so defaultdeny: {egress: true} leaves egress open with no report.
func TestStress_Network_UnknownKeySilent(t *testing.T) {
	t.Parallel()
	cases := map[string]func(cfg map[string]interface{}){
		"project defaultdeny": func(cfg map[string]interface{}) {
			p := proj(cfg["projects"].(map[string]interface{}), "reporting")
			p["networkProfile"] = "permissive"
			p["network"] = map[string]interface{}{"defaultdeny": map[string]interface{}{"egress": true}}
		},
		"project rule (singular)": func(cfg map[string]interface{}) {
			p := proj(cfg["projects"].(map[string]interface{}), "reporting")
			p["networkProfile"] = "strict"
			p["network"] = map[string]interface{}{"rule": map[string]interface{}{"x": egressTo("deny", cidrs("198.51.100.0/24"))}}
		},
		"profile default-deny": func(cfg map[string]interface{}) {
			profiles := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})
			profiles["closed"] = map[string]interface{}{"default-deny": map[string]interface{}{"ingress": true, "egress": true}}
			proj(cfg["projects"].(map[string]interface{}), "reporting")["networkProfile"] = "closed"
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			objs := stressObjs(t, stressRunInput(t, "", nil, set))
			if onboardingStatus(t, objs)["reporting"] == "" {
				t.Errorf("unknown network key ignored with no report (NetworkPolicies %v)", networkPoliciesIn(objs, "reporting"))
			}
		})
	}
}

// ---- ingress cidrs ---------------------------------------------------------------------------

// The cidrs peer is "IP ranges outside the cluster network and service network". On egress an
// in-cluster allow is an error and a wider allow applies to the external part only. On ingress the
// CIDR becomes a NetworkPolicy ipBlock with no except: OVN-Kubernetes matches pod IPs with it, so
// 0.0.0.0/0 or the pod network opens ingress from every pod of every namespace.
func TestStress_Network_IngressCidrCoversCluster(t *testing.T) {
	t.Parallel()
	for _, c := range []string{"0.0.0.0/0", "10.0.0.0/8", "10.128.0.0/14", "10.129.4.0/24", "172.30.0.0/16"} {
		t.Run(c, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, withRule("strict", "ext", ingressFrom("allow", cidrs(c), 443))))
			np := networkPolicyNamed(objs, "reporting", "autoshift-rule-ext")
			if np == nil {
				if onboardingStatus(t, objs)["reporting"] == "" {
					t.Error("no NetworkPolicy and no status error")
				}
				return
			}
			got := asJSON(npRules(np, "ingress"))
			if !strings.Contains(got, `"except"`) {
				t.Errorf("ingress cidrs %s rendered %s: an ipBlock without the cluster and service networks excepted admits every pod in the cluster", c, got)
			}
		})
	}
}

// ---- rule merge ------------------------------------------------------------------------------

// enabled: false removes an inherited rule, also when the profile spells enabled: true out.
func TestStress_Network_DisableOverExplicitEnabled(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
		strict := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})["strict"].(map[string]interface{})
		strict["rules"].(map[string]interface{})["dns"].(map[string]interface{})["enabled"] = true
		p := proj(cfg["projects"].(map[string]interface{}), "reporting")
		p["networkProfile"] = "strict"
		p["network"] = map[string]interface{}{"rules": map[string]interface{}{"dns": map[string]interface{}{"enabled": false}}}
	})
	if np := networkPolicyNamed(stressObjs(t, res), "reporting", "autoshift-rule-dns"); np != nil {
		t.Errorf("profile dns rule enabled: true, project enabled: false: the rule still renders")
	}
}

// A project override of a profile rule replaces lists whole. An empty list is a value too: ports: []
// over the profile's ports must not leave the profile ports silently (nor open all ports silently).
func TestStress_Network_EmptyListOverride(t *testing.T) {
	t.Parallel()
	res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
		profiles := cfg["projectDefaults"].(map[string]interface{})["networkProfiles"].(map[string]interface{})
		profiles["web"] = map[string]interface{}{"rules": map[string]interface{}{
			"web": ingressFrom("allow", map[string]interface{}{"router": true}, 8080),
		}}
		p := proj(cfg["projects"].(map[string]interface{}), "reporting")
		p["networkProfile"] = "web"
		p["network"] = map[string]interface{}{"rules": map[string]interface{}{"web": map[string]interface{}{"ports": []interface{}{}}}}
	})
	objs := stressObjs(t, res)
	np := networkPolicyNamed(objs, "reporting", "autoshift-rule-web")
	if np != nil && strings.Contains(asJSON(npRules(np, "ingress")), "8080") && onboardingStatus(t, objs)["reporting"] == "" {
		t.Errorf("ports: [] override kept the profile's 8080 with no report: %s", asJSON(npRules(np, "ingress")))
	}
}

// "Without ports, every port is allowed." An explicit empty ports list is not "without ports": a
// templated or emptied list silently opening every port is more open than written.
func TestStress_Network_EmptyPortsAllPorts(t *testing.T) {
	t.Parallel()
	objs := stressObjs(t, stressProjectsInput(t, withRule("strict", "web", map[string]interface{}{
		"direction": "ingress", "action": "allow", "from": map[string]interface{}{"router": true}, "ports": []interface{}{},
	})))
	np := networkPolicyNamed(objs, "reporting", "autoshift-rule-web")
	if np != nil && !strings.Contains(asJSON(npRules(np, "ingress")), `"ports"`) && onboardingStatus(t, objs)["reporting"] == "" {
		t.Errorf("ports: [] rendered an all-ports rule with no report: %s", asJSON(npRules(np, "ingress")))
	}
}

// ---- ports -----------------------------------------------------------------------------------

func TestStress_Network_PortBoundaries(t *testing.T) {
	t.Parallel()
	bad := map[string]interface{}{
		"0": 0, "-1": -1, "65536": 65536, "1.5": 1.5, "1e6": 1000000, "space": " 80", "empty": "",
		"16-char name": "abcdefghijklmnop", "double dash": "a--b", "leading dash": "-http", "upper": "HTTP",
		"endPort":        map[string]interface{}{"port": 80, "endPort": 90},
		"endPort < port": map[string]interface{}{"port": 90, "endPort": 80},
		"protocol empty": map[string]interface{}{"port": 80, "protocol": ""},
		"protocol null":  map[string]interface{}{"port": 80, "protocol": nil},
		"port null":      map[string]interface{}{"port": nil},
		"list":           []interface{}{80},
		"payload":        "{{ fail `boom` }}",
	}
	for name, p := range bad {
		t.Run("bad "+name, func(t *testing.T) {
			res := stressProjectsInput(t, withRule("strict", "web", ingressFrom("allow", map[string]interface{}{"router": true}, p)))
			reportedSkipped(t, stressObjs(t, res), "rule web")
		})
	}
	good := map[string]string{"1": "1", "65535": "65535", "080": "80", "15-char name": "abcdefghijklmno", "digit-led name": "1abc"}
	inputs := map[string]interface{}{"1": 1, "65535": 65535, "080": "080", "15-char name": "abcdefghijklmno", "digit-led name": "1abc"}
	for name, want := range good {
		t.Run("ok "+name, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, withRule("strict", "web", ingressFrom("allow", map[string]interface{}{"router": true}, inputs[name]))))
			np := networkPolicyNamed(objs, "reporting", "autoshift-rule-web")
			if np == nil || !strings.Contains(asJSON(npRules(np, "ingress")), want) {
				t.Errorf("port %v: want rendered %s, status %q", inputs[name], want, onboardingStatus(t, objs)["reporting"])
			}
		})
	}
}

// ---- CIDRs -----------------------------------------------------------------------------------

// Egress CIDR edge cases against the managed cluster's networks 10.128.0.0/14 and 172.30.0.0/16
// (testdata stub).
func TestStress_Network_EgressCidrEdgeCases(t *testing.T) {
	t.Parallel()
	type c struct {
		action, cidr, want string // want "" = must render; otherwise a status substring
	}
	cases := []c{
		// host bits set: the overlap check must use the network, not the written address.
		{"deny", "10.200.0.1/8", "overlaps the cluster or service network"},
		{"deny", "172.31.255.255/15", "overlaps the cluster or service network"},
		{"allow", "10.130.5.5/16", "lies inside the cluster or service network"},
		{"allow", "172.30.9.9/24", "lies inside the cluster or service network"},
		// IPv4-mapped IPv6 forms of an external and an in-cluster range. OVN-Kubernetes treats a
		// mapped address as IPv4 (egressIP rejects them); a mapped CIDR must not slip past the checks.
		{"deny", "::ffff:c633:6400/120", "cidr ::ffff:c633:6400/120"},
		{"deny", "::ffff:a80:0/110", "cidr ::ffff:a80:0/110"},
		// parse boundaries
		{"allow", "198.51.100.0/33", "is not a valid CIDR"},
		{"allow", "2001:db8::/129", "is not a valid CIDR"},
		{"allow", "198.051.100.0/24", "is not a valid CIDR"}, // gitleaks:allow (leading-zero edge case of TEST-NET-2)
		{"allow", "198.51.100.0", "is not a valid CIDR"},
		{"allow", "198.51.100.0/", "is not a valid CIDR"},
		{"allow", "198.51.100.0/024", "is not a valid CIDR"},
		{"allow", "fe80::1%eth0/64", "is not a valid CIDR"},
		{"allow", " 198.51.100.0/24", "is not a valid CIDR"},
		{"allow", "x{{y/24", "is not a valid CIDR"},
		{"allow", "2001:db8::/32", ""},
		{"allow", "2001:DB8::/32", ""},
		{"allow", "198.51.100.0/24", ""},
		{"allow", "0.0.0.0/0", ""},
		{"deny", "198.51.100.7/32", ""},
	}
	for _, tc := range cases {
		t.Run(tc.action+" "+tc.cidr, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, withRule("strict", "ext", egressTo(tc.action, cidrs(tc.cidr), 443))))
			got := onboardingStatus(t, objs)["reporting"]
			if tc.want == "" {
				if got != "" || egressFirewallIn(objs, "reporting") == nil {
					t.Errorf("%s %s must render, status %q", tc.action, tc.cidr, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				ef := ""
				if o := egressFirewallIn(objs, "reporting"); o != nil {
					ef = efEgress(o)
				}
				t.Errorf("%s %s: want status containing %q, got %q (EgressFirewall %s)", tc.action, tc.cidr, tc.want, got, ef)
			}
		})
	}
}

// ---- DNS names -------------------------------------------------------------------------------

func TestStress_Network_DnsNameEdgeCases(t *testing.T) {
	t.Parallel()
	label64 := strings.Repeat("a", 64)
	long := strings.Repeat("a.", 126) + "ab" // 254
	bad := map[string]string{
		"label over 63":     label64 + ".example.com",
		"254 chars":         long,
		"bare wildcard":     "*",
		"double wildcard":   "*.*.example.com",
		"inner wildcard":    "a.*.example.com",
		"empty label":       "a..example.com",
		"uppercase":         "Example.com",
		"underscore":        "_svc.example.com",
		"space":             "example.com ",
		"payload":           "x{{y.example.com",
		"in-cluster IP":     "10.128.0.5",
		"external IP":       "198.51.100.7",
		"service DNS":       "kubernetes.default.svc.cluster.local",
		"leading dash":      "-a.example.com",
		"two trailing dots": "example.com..",
	}
	for name, d := range bad {
		t.Run("bad "+name, func(t *testing.T) {
			res := stressProjectsInput(t, withRule("strict", "ext", egressTo("deny", map[string]interface{}{"dnsNames": []interface{}{d}}, 443)))
			objs := stressObjs(t, res)
			if got := onboardingStatus(t, objs)["reporting"]; got == "" {
				ef := ""
				if o := egressFirewallIn(objs, "reporting"); o != nil {
					ef = efEgress(o)
				}
				t.Errorf("dnsName %q rendered with no report (EgressFirewall %s)", d, ef)
			}
		})
	}
	for _, d := range []string{"example.com", "*.example.com", "example.com.", "xn--caf-dma.example", "a-b.example.com", "1.example.com", strings.Repeat("a", 63) + ".example.com"} {
		t.Run("ok "+d, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, withRule("strict", "ext", egressTo("allow", map[string]interface{}{"dnsNames": []interface{}{d}}, 443))))
			if ef := egressFirewallIn(objs, "reporting"); ef == nil || !strings.Contains(efEgress(ef), d) {
				t.Errorf("dnsName %q must render, status %q", d, onboardingStatus(t, objs)["reporting"])
			}
		})
	}
}

// ---- injection through network strings -------------------------------------------------------

// Every network string that reaches the output (peer label keys and values, namespace names, CIDRs,
// DNS names, rule names, profile names) is validated on the hub. Whatever it holds, the policy
// must resolve and only reporting is reported.
func TestStress_Network_TemplateInjection(t *testing.T) {
	t.Parallel()
	payloads := []string{"x{{y", "{{ fail `boom` }}", `{{hub fail "boom" hub}}`, "{{ (lookup `v1` `Secret` `openshift-config` `pull-secret`).data }}"}
	fields := map[string]func(p string) func(projects map[string]interface{}){
		"peer label key": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{p: "a"}}}))
		},
		"peer label value": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"app": p}}}))
		},
		"namespace name": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{p}}}))
		},
		"cidr": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", egressTo("allow", cidrs(p)))
		},
		"dnsName": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", egressTo("allow", map[string]interface{}{"dnsNames": []interface{}{p}}))
		},
		"rule name": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", p, ingressFrom("allow", map[string]interface{}{"sameNamespace": true}))
		},
		"named port": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"sameNamespace": true}, p))
		},
		"protocol": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"sameNamespace": true}, map[string]interface{}{"port": 80, "protocol": p}))
		},
		"direction": func(p string) func(projects map[string]interface{}) {
			return withRule("strict", "x", map[string]interface{}{"direction": p, "action": "allow", "from": map[string]interface{}{"sameNamespace": true}})
		},
		"profile name": func(p string) func(projects map[string]interface{}) {
			return func(projects map[string]interface{}) { proj(projects, "reporting")["networkProfile"] = p }
		},
		"size name": func(p string) func(projects map[string]interface{}) {
			return func(projects map[string]interface{}) { proj(projects, "reporting")["size"] = p }
		},
		"quota key": func(p string) func(projects map[string]interface{}) {
			return func(projects map[string]interface{}) {
				r := proj(projects, "reporting")
				r["size"] = "small"
				r["quota"] = map[string]interface{}{p: "1"}
			}
		},
		"quota value": func(p string) func(projects map[string]interface{}) {
			return func(projects map[string]interface{}) {
				r := proj(projects, "reporting")
				r["size"] = "small"
				r["quota"] = map[string]interface{}{"pods": p}
			}
		},
		"egressIP": func(p string) func(projects map[string]interface{}) {
			return setEgressIP("reporting", []interface{}{p})
		},
		"mode": func(p string) func(projects map[string]interface{}) {
			return func(projects map[string]interface{}) { proj(projects, "reporting")["mode"] = p }
		},
	}
	for field, mk := range fields {
		for i, p := range payloads {
			t.Run(fmt.Sprintf("%s/%d", field, i), func(t *testing.T) {
				res := stressProjectsInput(t, mk(p))
				if why := stressHealthy(t, res); why != "" {
					t.Fatalf("%s = %q broke the whole policy: %s", field, p, why)
				}
				objs, _ := stressParse(res)
				otherProjectsRender(t, objs, "reporting")
				if onboardingStatus(t, objs)["reporting"] == "" {
					t.Errorf("%s = %q: want reporting reported", field, p)
				}
			})
		}
	}
}

// The admin tier strings: entry name, appliesTo label keys and values, matchExpressions key,
// operator and values, cidrs, peer namespaces.
func TestStress_Admin_TemplateInjection(t *testing.T) {
	t.Parallel()
	payloads := []string{"x{{y", "{{ fail `boom` }}", `{{hub fail "boom" hub}}`}
	entry := func(appliesTo map[string]interface{}, to map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"appliesTo": appliesTo, "direction": "egress", "action": "deny", "to": to}
	}
	all := map[string]interface{}{"allProjects": true}
	ext := cidrs("198.51.100.0/24")
	fields := map[string]func(p string) map[string]interface{}{
		"entry name": func(p string) map[string]interface{} { return map[string]interface{}{p: entry(all, ext)} },
		"labels key": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(map[string]interface{}{"labels": map[string]interface{}{p: "a"}}, ext)}
		},
		"labels value": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(map[string]interface{}{"labels": map[string]interface{}{"a": p}}, ext)}
		},
		"expr key": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": p, "operator": "Exists"}}}}, ext)}
		},
		"expr operator": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": p}}}}, ext)}
		},
		"expr value": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "a", "operator": "In", "values": []interface{}{p}}}}}, ext)}
		},
		"cidr": func(p string) map[string]interface{} { return map[string]interface{}{"e": entry(all, cidrs(p))} },
		"namespace": func(p string) map[string]interface{} {
			return map[string]interface{}{"e": entry(all, map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{p}}})}
		},
	}
	for field, mk := range fields {
		for i, p := range payloads {
			t.Run(fmt.Sprintf("%s/%d", field, i), func(t *testing.T) {
				res := stressRunInput(t, "", nil, setAdmin(mk(p)))
				if why := stressHealthy(t, res); why != "" {
					t.Fatalf("admin %s = %q broke the whole policy: %s", field, p, why)
				}
				objs, _ := stressParse(res)
				otherProjectsRender(t, objs, "")
				if onboardingStatus(t, objs)["projectDefaults.adminNetworkPolicies"] == "" {
					t.Errorf("admin %s = %q: want the admin tier reported", field, p)
				}
			})
		}
	}
}

// ---- rule counts -----------------------------------------------------------------------------

// Zero rules and a thousand rules: neither breaks the policy; a thousand allow rules render a
// thousand NetworkPolicies (or are reported), never a silent subset.
func TestStress_Network_RuleCounts(t *testing.T) {
	t.Parallel()
	t.Run("empty rules", func(t *testing.T) {
		objs := stressObjs(t, stressProjectsInput(t, func(projects map[string]interface{}) {
			proj(projects, "reporting")["networkProfile"] = "strict"
			proj(projects, "reporting")["network"] = map[string]interface{}{"rules": map[string]interface{}{}}
		}))
		if n := networkPoliciesIn(objs, "reporting"); len(n) != 3 {
			t.Errorf("strict + rules: {} want the two default denies and the dns rule, got %v", n)
		}
	})
	t.Run("1000 rules", func(t *testing.T) {
		res := stressProjectsInput(t, func(projects map[string]interface{}) {
			proj(projects, "reporting")["networkProfile"] = "strict"
			rules := projectRules(projects, "reporting")
			for i := 0; i < 1000; i++ {
				rules[fmt.Sprintf("r%04d", i)] = ingressFrom("allow", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("ns-%d", i)}}}, 8000+i)
			}
		})
		objs := stressObjs(t, res)
		n := len(networkPoliciesIn(objs, "reporting"))
		if n != 1003 && onboardingStatus(t, objs)["reporting"] == "" {
			t.Errorf("1000 allow rules rendered %d NetworkPolicies, want 1003 or a report", n)
		}
	})
	t.Run("101 overrides of a profile deny", func(t *testing.T) {
		res := stressRunInput(t, "", nil, func(cfg map[string]interface{}) {
			withProfileDeny(cfg)
			proj(cfg["projects"].(map[string]interface{}), "reporting")["networkProfile"] = "strict"
			rules := projectRules(cfg["projects"].(map[string]interface{}), "reporting")
			for i := 0; i < 101; i++ {
				rules[fmt.Sprintf("r%03d", i)] = ingressFrom("allow", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("ns-%d", i)}}})
			}
		})
		objs := stressObjs(t, res)
		if anp := anpNamed(objs, "autoshift-project-reporting"); anp != nil {
			t.Errorf("reporting needs over 100 ANP rules, want it reported, got an AdminNetworkPolicy (status %q)", onboardingStatus(t, objs)["reporting"])
		}
	})
}

// ---- defaultDeny and rule field types --------------------------------------------------------

func TestStress_Network_FieldTypes(t *testing.T) {
	t.Parallel()
	cases := map[string]func(projects map[string]interface{}){
		"defaultDeny.ingress 1": func(projects map[string]interface{}) {
			proj(projects, "reporting")["network"] = map[string]interface{}{"defaultDeny": map[string]interface{}{"ingress": 1}}
		},
		"defaultDeny.Ingress": func(projects map[string]interface{}) {
			proj(projects, "reporting")["network"] = map[string]interface{}{"defaultDeny": map[string]interface{}{"Ingress": true}}
		},
		"defaultDeny list": func(projects map[string]interface{}) {
			proj(projects, "reporting")["network"] = map[string]interface{}{"defaultDeny": []interface{}{"ingress"}}
		},
		"direction list":  withRule("strict", "x", map[string]interface{}{"direction": []interface{}{"ingress"}, "action": "allow", "from": map[string]interface{}{"sameNamespace": true}}),
		"action bool":     withRule("strict", "x", map[string]interface{}{"direction": "ingress", "action": true, "from": map[string]interface{}{"sameNamespace": true}}),
		"action Allow":    withRule("strict", "x", map[string]interface{}{"direction": "ingress", "action": "Allow", "from": map[string]interface{}{"sameNamespace": true}}),
		"enabled no":      withRule("strict", "x", map[string]interface{}{"enabled": "no", "direction": "ingress", "action": "allow", "from": map[string]interface{}{"sameNamespace": true}}),
		"from list":       withRule("strict", "x", map[string]interface{}{"direction": "ingress", "action": "allow", "from": []interface{}{"sameNamespace"}}),
		"cidrs string":    withRule("strict", "x", egressTo("allow", map[string]interface{}{"cidrs": "198.51.100.0/24"})),
		"cidrs empty":     withRule("strict", "x", egressTo("allow", map[string]interface{}{"cidrs": []interface{}{}})),
		"cidrs null item": withRule("strict", "x", egressTo("allow", map[string]interface{}{"cidrs": []interface{}{nil}})),
		"names empty str": withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{""}}})),
		"labels null":     withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"pods": map[string]interface{}{"labels": nil}})),
		"label key empty": withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"pods": map[string]interface{}{"labels": map[string]interface{}{"": "a"}}})),
		"rule name 64":    withRule("strict", strings.Repeat("a", 64), ingressFrom("allow", map[string]interface{}{"sameNamespace": true})),
		"rule name -x":    withRule("strict", "-x", ingressFrom("allow", map[string]interface{}{"sameNamespace": true})),
		"rule name dots":  withRule("strict", "a.b", ingressFrom("allow", map[string]interface{}{"sameNamespace": true})),
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, set))
			if onboardingStatus(t, objs)["reporting"] == "" {
				t.Errorf("%s: want reporting reported, NetworkPolicies %v", name, networkPoliciesIn(objs, "reporting"))
			}
			otherProjectsRender(t, objs, "reporting")
		})
	}
	// Peers and defaultDeny take true or false; a string "true" is not a bool.
	for name, set := range map[string]func(projects map[string]interface{}){
		"peer 'true'": withRule("strict", "x", ingressFrom("allow", map[string]interface{}{"sameNamespace": "true"})),
		"defaultDeny 'true'": func(projects map[string]interface{}) {
			proj(projects, "reporting")["network"] = map[string]interface{}{"defaultDeny": map[string]interface{}{"ingress": "true"}}
		},
	} {
		t.Run("string bool "+name, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, set))
			if onboardingStatus(t, objs)["reporting"] == "" {
				t.Errorf("%s accepted as a bool with no report", name)
			}
		})
	}
}

// A rule name of 63 characters (the limit) renders autoshift-rule-<63> (78 characters), a valid
// object name.
func TestStress_Network_RuleName63(t *testing.T) {
	t.Parallel()
	n := strings.Repeat("a", 63)
	objs := stressObjs(t, stressProjectsInput(t, withRule("strict", n, ingressFrom("allow", map[string]interface{}{"sameNamespace": true}))))
	if networkPolicyNamed(objs, "reporting", "autoshift-rule-"+n) == nil {
		t.Errorf("rule name of 63 characters not rendered, status %q", onboardingStatus(t, objs)["reporting"])
	}
}

// ---- admin entries ---------------------------------------------------------------------------

func TestStress_Admin_EdgeCases(t *testing.T) {
	t.Parallel()
	ext := cidrs("198.51.100.0/24")
	cases := map[string]map[string]interface{}{
		"allProjects 'true'": {"e": map[string]interface{}{"appliesTo": map[string]interface{}{"allProjects": "true"}, "direction": "egress", "action": "deny", "to": ext}},
		"host bits cidr":     {"e": adminEntry("egress", "deny", cidrs("198.51.100.7/24"))},
		"mapped ipv6 cidr":   {"e": adminEntry("egress", "deny", cidrs("::ffff:c633:6400/120"))},
		"ports empty": {"e": func() map[string]interface{} {
			e := adminEntry("egress", "deny", ext)
			e["ports"] = []interface{}{}
			return e
		}()},
		"name 64":      {strings.Repeat("a", 64): adminEntry("egress", "deny", ext)},
		"enabled 'no'": {"e": func() map[string]interface{} { e := adminEntry("egress", "deny", ext); e["enabled"] = "no"; return e }()},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			objs := stressObjs(t, stressRunInput(t, "", nil, setAdmin(entries)))
			if onboardingStatus(t, objs)["projectDefaults.adminNetworkPolicies"] == "" {
				var got []string
				for n, a := range adminANPs(objs) {
					got = append(got, n+" "+asJSON(a.obj["spec"]))
				}
				t.Errorf("admin %s rendered with no report: %v", name, got)
			}
		})
	}
	// Exists with values: null is a valid selector (Kubernetes drops the null).
	t.Run("ok Exists values null", func(t *testing.T) {
		objs := stressObjs(t, stressRunInput(t, "", nil, setAdmin(map[string]interface{}{"e": map[string]interface{}{
			"appliesTo": map[string]interface{}{"namespaceSelector": map[string]interface{}{"matchExpressions": []interface{}{
				map[string]interface{}{"key": "example.com/tier", "operator": "Exists", "values": nil}}}},
			"direction": "egress", "action": "deny", "to": ext}})))
		if len(adminANPs(objs)) != 1 {
			t.Errorf("Exists with values: null not rendered, status %q", onboardingStatus(t, objs)["projectDefaults.adminNetworkPolicies"])
		}
	})
}

// ---- egressIP --------------------------------------------------------------------------------

func TestStress_EgressIP_EdgeCases(t *testing.T) {
	t.Parallel()
	bad := map[string]interface{}{
		"leading space": " 198.51.100.5", "trailing space": "198.51.100.5 ", "leading zero": "198.051.100.5", // gitleaks:allow (leading-zero edge case of TEST-NET-2)
		"octet 256": "198.51.100.256", "prefix": "2001:db8::1/128", "zone": "fe80::1%eth0", "expanded mapped": "0:0:0:0:0:ffff:c633:6432",
		"upper mapped": "::FFFF:c633:6432", "triple colon": "2001:db8:::1", "9 groups": "1:2:3:4:5:6:7:8:9", "empty": "",
		"null": nil, "number": 3325256709, "map": map[string]interface{}{"ip": "198.51.100.5"},
		"loopback": "127.0.0.1", "multicast": "224.0.0.1", "broadcast": "255.255.255.255", "link-local": "169.254.169.254", // gitleaks:allow (special-purpose addresses, not hosts)
		"v6 loopback": "::1", "v6 multicast": "ff02::1",
		"pod network": "10.128.0.5", "service network": "172.30.0.10",
	}
	for name, v := range bad {
		t.Run("bad "+name, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, setEgressIP("reporting", []interface{}{v})))
			if eip := egressIPNamed(t, objs, "autoshift-reporting"); eip != nil {
				t.Errorf("egressIP %#v rendered %s with status %q", v, asJSON(eip.obj["spec"]), onboardingStatus(t, objs)["reporting"])
			}
		})
	}
	// Duplicates by value across projects, in other spellings.
	for _, other := range []string{"2001:DB8::1", "2001:0db8:0000::0001", "2001:db8:0:0:0:0:0:1"} {
		t.Run("conflict "+other, func(t *testing.T) {
			objs := stressObjs(t, stressProjectsInput(t, func(projects map[string]interface{}) {
				setEgressIP("reporting", []interface{}{"2001:db8::1"})(projects)
				setEgressIP("legacy-app", []interface{}{other})(projects)
			}))
			if !strings.Contains(onboardingStatus(t, objs)["reporting"], "also declared by project legacy-app") {
				t.Errorf("2001:db8::1 vs %s: want a conflict, status %v", other, onboardingStatus(t, objs))
			}
		})
	}
}
