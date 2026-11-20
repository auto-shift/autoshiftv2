//go:build integration

package resolver

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The size guard (README "Size limit") keeps the hub-resolved Policy below 1 MiB from an estimate
// the hub computes per project. These tests hold the estimate to the measured size: it must never be
// below it, for any single feature at any count, or a large enough config passes the etcd request
// limit without the size status.

// backstopRe matches the hub failure of one of the size backstops, and nothing else.
var backstopRe = regexp.MustCompile(`error calling fail: project-onboarding: (\d+ projects declared, maximum \d+ per cluster; split the projects across clusters|the policy would be about \d+ bytes (even with \d+ projects not rendered, over the \d+ bytes that keep it below the etcd request limit; split the projects across clusters|from the projectDefaults\.adminNetworkPolicies errors alone, over the \d+ bytes that keep it below the etcd request limit; fix projectDefaults\.adminNetworkPolicies))`)

var sizeEstimateRe = regexp.MustCompile(`project-onboarding size estimate: (\d+) bytes`)

// sizeOf returns the measured compact JSON size of each hub-resolved shard Policy with the hub's
// estimate for it; every shard has its own size guard.
func sizeOf(t *testing.T, r stressResult) []stressPolicySize {
	t.Helper()
	sizes := stressPolicySizes(t, r.hub)
	for _, s := range sizes {
		if s.est < 0 {
			t.Fatalf("%s: no size estimate comment in the hub-resolved Policy", s.name)
		}
	}
	return sizes
}

// shardSkipped reports whether the shard of the Policy name (a shard Policy or its -test Policy,
// which skip the same projects) reported the size status.
func shardSkipped(t *testing.T, objs []onboardingObject, name string) bool {
	t.Helper()
	status := onboardingShardStatus(t, objs)
	if shard := onboardingShardOfPolicy(name); shard >= 0 && shard < len(status) {
		return status[shard]["project-onboarding.size"] != ""
	}
	return false
}

// longName is a 63-character project name, the longest DNS-1123 label.
func longName(i int) string { return fmt.Sprintf("%s-%04d", strings.Repeat("n", 58), i) }

// calProject is a project with only an admin group, for adding one feature to.
func calProject() map[string]interface{} {
	return map[string]interface{}{"team": "cal", "environment": "PROD", "groups": map[string]interface{}{"admin": "CAL_ADMINS"}}
}

func calRule(p map[string]interface{}, name string, rule map[string]interface{}) {
	net, _ := p["network"].(map[string]interface{})
	if net == nil {
		net = map[string]interface{}{"rules": map[string]interface{}{}}
		p["network"] = net
	}
	net["rules"].(map[string]interface{})[name] = rule
}

// calIsolate sets the project's defaultDeny for direction. An allow rule renders its NetworkPolicy
// only in a direction with a default deny, so the NetworkPolicy shapes need one to be measured.
func calIsolate(p map[string]interface{}, direction string) {
	net, _ := p["network"].(map[string]interface{})
	if net == nil {
		net = map[string]interface{}{"rules": map[string]interface{}{}}
		p["network"] = net
	}
	net["defaultDeny"] = map[string]interface{}{direction: true}
}

func calList(n int, f string) []interface{} {
	out := make([]interface{}, n)
	for i := range out {
		out[i] = fmt.Sprintf(f, i/250, i%250+1)
	}
	return out
}

// calShapes builds the projects of one shape at count n; defaults may be changed too.
var calShapes = map[string]func(cfg map[string]interface{}, n int){
	// The static text alone: an empty projects map still renders the sweep.
	"no projects": func(cfg map[string]interface{}, n int) {},
	// Shard 0 adds a status line for each invalid namespace deletion setting.
	"invalid namespace deletion settings": func(cfg map[string]interface{}, n int) {
		d := cfg["projectDefaults"].(map[string]interface{})
		d["maxNamespaceDeletes"] = "x"
		d["namespaceDeleteGracePeriod"] = "1d"
		for i := 0; i < n; i++ {
			stressProjects(cfg)[fmt.Sprintf("cal-%04d", i)] = stressMinimal(i)
		}
	},
	"groups-only projects": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			stressProjects(cfg)[fmt.Sprintf("cal-%04d", i)] = stressMinimal(i)
		}
	},
	"labels": func(cfg map[string]interface{}, n int) {
		l := map[string]interface{}{}
		for i := 0; i < n; i++ {
			l[fmt.Sprintf("l%d", i)] = kv(fmt.Sprintf("example.com/label-%d", i), "value")
		}
		p := calProject()
		p["labels"] = l
		stressProjects(cfg)["cal"] = p
	},
	"annotations": func(cfg map[string]interface{}, n int) {
		a := map[string]interface{}{}
		for i := 0; i < n; i++ {
			a[fmt.Sprintf("a%d", i)] = kv(fmt.Sprintf("example.com/note-%d", i), strings.Repeat("text ", 40))
		}
		p := calProject()
		p["annotations"] = a
		stressProjects(cfg)["cal"] = p
	},
	"quota keys": func(cfg map[string]interface{}, n int) {
		q := map[string]interface{}{}
		for i := 0; i < n; i++ {
			q[fmt.Sprintf("count/r%d.example.com", i)] = "5"
		}
		p := calProject()
		p["size"] = "small"
		p["quota"] = q
		stressProjects(cfg)["cal"] = p
	},
	"limitRange keys": func(cfg map[string]interface{}, n int) {
		d := map[string]interface{}{}
		for i := 0; i < n; i++ {
			d[fmt.Sprintf("example.com/r%d", i)] = "1"
		}
		p := calProject()
		p["size"] = "small"
		p["limitRange"] = map[string]interface{}{"max": d}
		stressProjects(cfg)["cal"] = p
	},
	"ingress cidrs": func(cfg map[string]interface{}, n int) {
		p := calProject()
		calRule(p, "ext", map[string]interface{}{"direction": "ingress", "action": "allow",
			"from": map[string]interface{}{"cidrs": calList(n, "100.64.%d.%d/32")}, "ports": []interface{}{443}})
		calIsolate(p, "ingress")
		stressProjects(cfg)["cal"] = p
	},
	"egress cidrs": func(cfg map[string]interface{}, n int) {
		p := calProject()
		calRule(p, "ext", map[string]interface{}{"direction": "egress", "action": "allow",
			"to": map[string]interface{}{"cidrs": calList(n, "100.64.%d.%d/32")}, "ports": []interface{}{443}})
		stressProjects(cfg)["cal"] = p
	},
	"dnsNames": func(cfg map[string]interface{}, n int) {
		p := calProject()
		calRule(p, "ext", map[string]interface{}{"direction": "egress", "action": "allow",
			"to": map[string]interface{}{"dnsNames": calList(n, "h%d-%d.example.com")}, "ports": []interface{}{443}})
		stressProjects(cfg)["cal"] = p
	},
	"ports": func(cfg map[string]interface{}, n int) {
		ports := make([]interface{}, n)
		for i := range ports {
			ports[i] = map[string]interface{}{"port": 1000 + i, "protocol": "UDP"}
		}
		p := calProject()
		calRule(p, "web", map[string]interface{}{"direction": "ingress", "action": "allow",
			"from": map[string]interface{}{"sameNamespace": true}, "ports": ports})
		calIsolate(p, "ingress")
		stressProjects(cfg)["cal"] = p
	},
	"allow rules": func(cfg map[string]interface{}, n int) {
		p := calProject()
		for i := 0; i < n; i++ {
			calRule(p, fmt.Sprintf("r%04d", i), map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("ns-%d", i)}}}, "ports": []interface{}{8080}})
		}
		calIsolate(p, "ingress")
		stressProjects(cfg)["cal"] = p
	},
	"deny rules (AdminNetworkPolicy)": func(cfg map[string]interface{}, n int) {
		p := calProject()
		for i := 0; i < n; i++ {
			calRule(p, fmt.Sprintf("d%03d", i), map[string]interface{}{"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("ns-%d", i)}}}, "ports": []interface{}{5432}})
		}
		stressProjects(cfg)["cal"] = p
	},
	"strict profile projects (EgressFirewall)": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			p := calProject()
			p["networkProfile"] = "strict"
			stressProjects(cfg)[fmt.Sprintf("cal-%04d", i)] = p
		}
	},
	"egressIP projects": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			p := calProject()
			p["egressIP"] = []interface{}{fmt.Sprintf("100.65.%d.%d", i/250, i%250+1)}
			stressProjects(cfg)[fmt.Sprintf("cal-%04d", i)] = p
		}
	},
	"admin entries": func(cfg map[string]interface{}, n int) {
		e := map[string]interface{}{}
		for i := 0; i < n; i++ {
			e[fmt.Sprintf("e%02d", i)] = adminEntry("egress", "deny", cidrs(calList(20, "100.64.%d.%d/32")...))
		}
		cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = e
		stressProjects(cfg)["cal"] = calProject()
	},
	"realistic projects, 63-character names": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			stressProjects(cfg)[longName(i)] = stressRealistic(i)
		}
	},
	"groups-only projects, 63-character names": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			stressProjects(cfg)[longName(i)] = stressMinimal(i)
		}
	},
	"selector labels (rules of 400 pod labels)": func(cfg map[string]interface{}, n int) {
		sel := map[string]interface{}{}
		for j := 0; j < 400; j++ {
			sel[fmt.Sprintf("example.com/l%d", j)] = "value"
		}
		p := calProject()
		for i := 0; i < n; i++ {
			calRule(p, fmt.Sprintf("r%03d", i), map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"pods": map[string]interface{}{"labels": sel}}})
		}
		calIsolate(p, "ingress")
		stressProjects(cfg)["cal"] = p
	},
	"invalid projects, 63-character names": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			p := calProject()
			p["mode"] = strings.Repeat("x", 1000)
			p["labels"] = map[string]interface{}{"bad": kv("example.com/bad key", "v")}
			stressProjects(cfg)[longName(i)+"_"] = p
		}
	},
	"invalid projects, escaped namespaces": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			p := calProject()
			p["namespace"] = strings.Repeat("<", 1000+i)
			stressProjects(cfg)[fmt.Sprintf("cal-%04d", i)] = p
		}
	},
	"invalid projects": func(cfg map[string]interface{}, n int) {
		for i := 0; i < n; i++ {
			p := calProject()
			p["mode"] = strings.Repeat("x", 1000)
			p["labels"] = map[string]interface{}{"bad": kv("example.com/bad key", strings.Repeat("v", 1000))}
			stressProjects(cfg)[fmt.Sprintf("Bad_%04d", i)] = p
		}
	},
}

// calCounts are measured in every run: each shape at a small count, for the fixed part of its
// estimate, and at a larger one, for the per-item part.
var calCounts = map[string][]int{
	"no projects":                         {0},
	"invalid namespace deletion settings": {1},

	"groups-only projects":            {1, 30},
	"labels":                          {50, 400},
	"annotations":                     {50, 400},
	"quota keys":                      {10, 300},
	"limitRange keys":                 {10, 300},
	"ingress cidrs":                   {10, 300},
	"egress cidrs":                    {10, 500},
	"dnsNames":                        {100, 3000},
	"ports":                           {10, 500},
	"allow rules":                     {10, 300},
	"deny rules (AdminNetworkPolicy)": {10, 90},
	"strict profile projects (EgressFirewall)":  {1, 30},
	"egressIP projects":                         {1, 50},
	"admin entries":                             {5, 40},
	"invalid projects":                          {10, 200},
	"invalid projects, escaped namespaces":      {1, 4},
	"realistic projects, 63-character names":    {1, 20},
	"groups-only projects, 63-character names":  {1, 50},
	"selector labels (rules of 400 pod labels)": {1, 20},
	"invalid projects, 63-character names":      {10, 200},
}

// calScaleCounts are the larger counts of the same shapes, measured only with STRESS_SCALE=1 to
// keep the default run inside the CI test budget. Each shape keeps a small and a larger count in
// calCounts, which hold the estimate to the measurement the same way.
var calScaleCounts = map[string][]int{
	"groups-only projects":                      {100},
	"ingress cidrs":                             {1000},
	"egress cidrs":                              {2000},
	"ports":                                     {2000},
	"invalid projects, escaped namespaces":      {7},
	"realistic projects, 63-character names":    {60, 200},
	"groups-only projects, 63-character names":  {150, 300},
	"selector labels (rules of 400 pod labels)": {45},
	"invalid projects, 63-character names":      {400},
}

// calCountsFor returns the counts of shape for this run.
func calCountsFor(shape string) []int {
	if os.Getenv("STRESS_SCALE") == "" {
		return calCounts[shape]
	}
	return append(append([]int{}, calCounts[shape]...), calScaleCounts[shape]...)
}

// calDeletionShapes are also measured with namespace deletion and its circuit breaker on, which add
// static text to every shard and the declared names ($allNames) to its spoke template.
var calDeletionShapes = map[string]bool{
	"no projects": true, "invalid namespace deletion settings": true, "groups-only projects": true,
	"groups-only projects, 63-character names": true, "realistic projects, 63-character names": true,
	"invalid projects, 63-character names": true, "admin entries": true, "egressIP projects": true,
}

func TestProjectOnboarding_SizeEstimateCovers(t *testing.T) {
	t.Parallel()
	for shape := range calScaleCounts {
		if _, ok := calShapes[shape]; !ok {
			t.Errorf("calScaleCounts names %q, which is not a shape", shape)
		}
	}
	for shape, build := range calShapes {
		for _, n := range calCountsFor(shape) {
			modes := []string{"on", "off"}
			if calDeletionShapes[shape] {
				modes = append(modes, "on with namespace deletion and the breaker")
			}
			for _, prune := range modes {
				t.Run(fmt.Sprintf("%s/%d/prune %s", shape, n, prune), func(t *testing.T) {
					t.Parallel()
					labels := map[string]string(nil)
					switch prune {
					case "on":
						labels = pruneOn
					case "off":
					default:
						labels = nsdelOn
					}
					r := stressRun(t, stressOpts{allShards: true, labels: labels, mutate: func(cfg map[string]interface{}) {
						cfg["projects"] = map[string]interface{}{}
						if labels[nsdelLabel] == "true" {
							if _, set := cfg["projectDefaults"].(map[string]interface{})["maxNamespaceDeletes"]; !set {
								cfg["projectDefaults"].(map[string]interface{})["maxNamespaceDeletes"] = 5
							}
						}
						build(cfg, n)
					}})
					for _, s := range sizeOf(t, r) {
						skipped := shardSkipped(t, r.objs, s.name)
						t.Logf("%s: real %d, estimate %d (%.2f), skipped %v", s.name, s.real, s.est, float64(s.est)/float64(s.real), skipped)
						if !skipped && s.est < s.real {
							t.Errorf("%s: estimate %d is below the measured %d bytes: the size guard under-counts this shape", s.name, s.est, s.real)
						}
						if s.real > etcdRequestLimit {
							t.Errorf("%s: hub-resolved Policy is %d bytes, over the %d etcd request limit", s.name, s.real, etcdRequestLimit)
						}
					}
				})
			}
		}
	}
}

// sizeGuardScaleShapes run only with STRESS_SCALE=1, to keep the default run inside the CI test
// budget; the value names what covers the same path in every run. Every run keeps "namespace of
// 4000 escaped characters", "admin entry with a 120000-character label value", and "1000 invalid
// admin cidrs of 400 characters": the admin-tier backstop.
var sizeGuardScaleShapes = map[string]string{
	"200 realistic":                                  "TestStress_Scale_RealisticPolicySize (200 realistic projects, prune on, under the etcd request limit)",
	"400 realistic, 63-character names":              "TestProjectOnboarding_SizeGuardPerShard and SizeEstimateCovers (realistic projects, 63-character names)",
	"500 realistic, 63-character names":              "TestProjectOnboarding_SizeGuardPerShard and SizeEstimateCovers (realistic projects, 63-character names)",
	"1200 realistic, 6-character names":              "TestProjectOnboarding_SizeGuardPerShard and TestStress_Scale_RealisticSizeGuard",
	"50 x 1000 dnsNames":                             "SizeEstimateCovers (dnsNames, 3000)",
	"300 projects, 300-character escaped namespaces": "namespace of 4000 escaped characters, and SizeEstimateCovers (invalid projects, escaped namespaces)",
	"1500 invalid projects":                          "SizeEstimateCovers (invalid projects, 200, and invalid projects, 63-character names, 200)",
	"12000 invalid admin cidrs":                      "1000 invalid admin cidrs of 400 characters (the same admin-tier backstop)",
	// SizeEstimateCovers holds every one of its shapes below the etcd request limit as well, so
	// these repeat a shape it measures in every run, at a larger count.
	"30 x 50 ingress cidrs":             "SizeEstimateCovers (ingress cidrs, 300)",
	"1 x 1500 ingress cidrs":            "SizeEstimateCovers (ingress cidrs, 300)",
	"40 x 400 ports":                    "SizeEstimateCovers (ports, 500)",
	"45 rules x 400-label pod selector": "SizeEstimateCovers (selector labels (rules of 400 pod labels), 20)",
	"400 invalid, 63-character names":   "SizeEstimateCovers (invalid projects, 63-character names, 200)",
}

// Whatever the shape, the hub-resolved Policy fits the etcd request limit, or the size status says
// why projects are not rendered.
func TestProjectOnboarding_SizeGuardShapes(t *testing.T) {
	t.Parallel()
	many := func(n int, gen func(i int) map[string]interface{}) func(cfg map[string]interface{}) {
		return func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < n; i++ {
				p[fmt.Sprintf("shape-%04d", i)] = gen(i)
			}
			cfg["projects"] = p
		}
	}
	shapes := map[string]func(cfg map[string]interface{}){
		"200 realistic": many(200, stressRealistic),
		"30 x 50 ingress cidrs": many(30, func(i int) map[string]interface{} {
			p := calProject()
			calRule(p, "ext", map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"cidrs": calList(50, fmt.Sprintf("100.%d.%%d.%%d/32", 64+i))}, "ports": []interface{}{443}})
			return p
		}),
		"1 x 1500 ingress cidrs": many(1, func(i int) map[string]interface{} {
			p := calProject()
			calRule(p, "ext", map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"cidrs": calList(1500, "100.64.%d.%d/32")}, "ports": []interface{}{443}})
			return p
		}),
		"40 x 400 ports": many(40, func(i int) map[string]interface{} {
			ports := make([]interface{}, 400)
			for j := range ports {
				ports[j] = 1000 + j
			}
			p := calProject()
			calRule(p, "web", map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"sameNamespace": true}, "ports": ports})
			return p
		}),
		"50 x 1000 dnsNames": many(50, func(i int) map[string]interface{} {
			p := calProject()
			calRule(p, "ext", map[string]interface{}{"direction": "egress", "action": "allow",
				"to": map[string]interface{}{"dnsNames": calList(1000, "h%d-%d.example.com")}})
			return p
		}),
		"400 realistic, 63-character names": func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < 400; i++ {
				p[longName(i)] = stressRealistic(i)
			}
			cfg["projects"] = p
		},
		"500 realistic, 63-character names": func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < 500; i++ {
				p[longName(i)] = stressRealistic(i)
			}
			cfg["projects"] = p
		},
		"1200 realistic, 6-character names": func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < 1200; i++ {
				p[fmt.Sprintf("p-%04d", i)] = stressRealistic(i)
			}
			cfg["projects"] = p
		},
		"400 invalid, 63-character names": func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < 400; i++ {
				q := calProject()
				q["mode"] = strings.Repeat("x", 1000)
				p[longName(i)+"_"] = q
			}
			cfg["projects"] = p
		},
		"45 rules x 400-label pod selector": many(1, func(i int) map[string]interface{} {
			sel := map[string]interface{}{}
			for j := 0; j < 400; j++ {
				sel[fmt.Sprintf("example.com/l%d", j)] = "value"
			}
			p := calProject()
			for r := 0; r < 45; r++ {
				calRule(p, fmt.Sprintf("r%03d", r), map[string]interface{}{"direction": "ingress", "action": "allow",
					"from": map[string]interface{}{"pods": map[string]interface{}{"labels": sel}}})
			}
			return p
		}),
		"namespace of 4000 escaped characters": many(1, func(i int) map[string]interface{} {
			p := calProject()
			p["namespace"] = strings.Repeat("<", 4000)
			return p
		}),
		"300 projects, 300-character escaped namespaces": many(300, func(i int) map[string]interface{} {
			p := stressRealistic(i)
			p["namespace"] = strings.Repeat(">", 300)
			return p
		}),
		"admin entry with a 120000-character label value": func(cfg map[string]interface{}) {
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = map[string]interface{}{
				"e": map[string]interface{}{"appliesTo": map[string]interface{}{"labels": map[string]interface{}{"a": strings.Repeat("<", 120000)}},
					"direction": "egress", "action": "deny", "to": cidrs("198.51.100.0/24")}}
		},
		// The admin-tier backstop with few entries: each error message is cut to 400 characters,
		// so 1000 of them pass the hard limit as surely as the 12000 short ones below.
		"1000 invalid admin cidrs of 400 characters": func(cfg map[string]interface{}) {
			bad := make([]interface{}, 1000)
			for i := range bad {
				bad[i] = fmt.Sprintf("bad-%d%s", i, strings.Repeat("x", 400))
			}
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = map[string]interface{}{
				"e": adminEntry("egress", "deny", cidrs(bad...))}
		},
		"12000 invalid admin cidrs": func(cfg map[string]interface{}) {
			bad := make([]interface{}, 12000)
			for i := range bad {
				bad[i] = fmt.Sprintf("bad-%d", i)
			}
			cfg["projectDefaults"].(map[string]interface{})["adminNetworkPolicies"] = map[string]interface{}{
				"e": adminEntry("egress", "deny", cidrs(bad...))}
		},
		"1500 invalid projects": func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < 1500; i++ {
				q := calProject()
				q["mode"] = strings.Repeat("x", 1000)
				p[fmt.Sprintf("Bad_%04d", i)] = q
			}
			cfg["projects"] = p
		},
	}
	for key := range sizeGuardScaleShapes {
		if _, ok := shapes[strings.TrimSuffix(strings.TrimSuffix(key, "/prune on"), "/prune off")]; !ok {
			t.Errorf("sizeGuardScaleShapes names %q, which is not a shape", key)
		}
	}
	for name, mutate := range shapes {
		for _, prune := range []string{"on", "off"} {
			t.Run(name+"/prune "+prune, func(t *testing.T) {
				t.Parallel()
				for _, key := range []string{name, name + "/prune " + prune} {
					if cover, ok := sizeGuardScaleShapes[key]; ok && os.Getenv("STRESS_SCALE") == "" {
						t.Skipf("set STRESS_SCALE=1; covered in every run by %s", cover)
					}
				}
				labels := map[string]string(nil)
				if prune == "on" {
					labels = pruneOn
				}
				r := stressRun(t, stressOpts{allShards: true, labels: labels, mutate: mutate, allowHubErr: true})
				if name == "1000 invalid admin cidrs of 400 characters" && !strings.Contains(strings.Join(r.hubErrs, "; "), "adminNetworkPolicies errors alone") {
					t.Fatalf("want the admin-tier backstop to fail the hub template, got errors %v", r.hubErrs)
				}
				if len(r.hubErrs) > 0 {
					// The backstop: the hub template fails, so nothing is applied or pruned and no
					// oversized Policy is ever written.
					m := backstopRe.FindStringSubmatch(strings.Join(r.hubErrs, "; "))
					if m == nil {
						t.Fatalf("hub resolution failed for another reason: %v", r.hubErrs)
					}
					t.Logf("backstop: %s", m[0])
					return
				}
				for _, s := range sizeOf(t, r) {
					t.Logf("%s: real %d, estimate %d, size status %v", s.name, s.real, s.est, shardSkipped(t, r.objs, s.name))
					if s.real > etcdRequestLimit {
						t.Errorf("%s: hub-resolved Policy is %d bytes, over the %d etcd request limit", s.name, s.real, etcdRequestLimit)
					}
				}
			})
		}
	}
}
