//go:build integration

package resolver

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// P1b stress: scale. The hub-resolved Policy (replicated into the cluster namespace on the hub and
// propagated to the managed cluster) carries both ConfigurationPolicies, each with the hub-resolved
// object-templates-raw. etcd rejects a request over 1.5 MiB (--max-request-bytes default), and a
// ConfigMap's data is capped at 1 MiB.

const (
	etcdRequestLimit = 1572864
	configMapLimit   = 1048576
)

// stressRealistic is a fully featured project: group override, size, strict profile, an
// in-cluster ingress allow, an in-cluster egress allow, an in-cluster deny (AdminNetworkPolicy),
// external cidrs and dnsNames (EgressFirewall), and an egress IP.
func stressRealistic(i int) map[string]interface{} {
	return map[string]interface{}{
		"team": fmt.Sprintf("team%d", i), "environment": "PROD", "costCenter": fmt.Sprint(1000 + i),
		"displayName": fmt.Sprintf("Scale project %d", i), "description": "Synthetic stress project",
		"groups":         map[string]interface{}{"admin": fmt.Sprintf("SCALE_%d_ADMINS", i)},
		"size":           "small",
		"networkProfile": "strict",
		"network": map[string]interface{}{"rules": map[string]interface{}{
			"from-web": map[string]interface{}{"direction": "ingress", "action": "allow",
				"from": map[string]interface{}{"namespaces": map[string]interface{}{"labels": map[string]interface{}{"example.com/team": "web"}}}},
			"to-db": map[string]interface{}{"direction": "egress", "action": "allow",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{fmt.Sprintf("db-%d", i)}}}, "ports": []interface{}{5432}},
			"deny-other": map[string]interface{}{"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"restricted"}}}},
			"ext": map[string]interface{}{"direction": "egress", "action": "allow",
				"to":    map[string]interface{}{"cidrs": []interface{}{"198.51.100.0/25", "198.51.100.128/25"}, "dnsNames": []interface{}{"api.example.com", "repo.example.org"}},
				"ports": []interface{}{443}},
		}},
		"egressIP": []interface{}{fmt.Sprintf("10.250.%d.%d", i/250, i%250+1)},
	}
}

// stressMinimal is the shape of a typical deployment's projects: adopt, enforce, groups only.
func stressMinimal(i int) map[string]interface{} {
	return map[string]interface{}{"team": fmt.Sprintf("team%d", i), "environment": "PROD", "adopt": true, "mode": "enforce",
		"groups": map[string]interface{}{"admin": fmt.Sprintf("SCALE_%d_ADMINS", i), "write": fmt.Sprintf("SCALE_%d_DEV", i), "read": fmt.Sprintf("SCALE_%d_VIEW", i)}}
}

// stressErrored is a project with a realistic set of hub errors.
func stressErrored(i int) map[string]interface{} {
	return map[string]interface{}{"team": fmt.Sprintf("team%d", i), "environment": "PROD", "mode": "garbage",
		"labels": map[string]interface{}{
			"bad1": map[string]interface{}{"key": "example.com/has space", "value": "x"},
			"bad2": map[string]interface{}{"key": "example.com/ok", "value": strings.Repeat("v", 70)},
		},
		"quota": map[string]interface{}{"pods": "10"}}
}

type stressScale struct {
	n                   int
	policy, enforceCP   int
	informCP, statusCM  int
	hubDur, spokeDur    string
	namespaces, objects int
}

func stressMeasure(t *testing.T, n int, gen func(int) map[string]interface{}) stressScale {
	t.Helper()
	r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
		p := map[string]interface{}{}
		for i := 0; i < n; i++ {
			p[fmt.Sprintf("scale-%04d", i)] = gen(i)
		}
		cfg["projects"] = p
	}})
	size, per := stressPolicyJSONSize(t, r.hub)
	s := stressScale{n: n, policy: size, hubDur: r.hubDur.Round(1e6).String(), spokeDur: r.spokeDur.Round(1e6).String(), objects: len(r.objs)}
	for k, v := range per {
		t.Logf("  ConfigurationPolicy %s: object-templates-raw %d bytes", k, v)
		// The largest shard's ConfigurationPolicies: <policy>-enforce and <policy>-test.
		if strings.HasSuffix(k, onboardingTestSuffix) {
			s.informCP = max(s.informCP, v)
		} else {
			s.enforceCP = max(s.enforceCP, v)
		}
	}
	for _, o := range r.objs {
		if o.obj["kind"] == "Namespace" && o.complianceType != "mustnothave" {
			s.namespaces++
		}
		if isStatus(o) && o.complianceType != "mustnothave" {
			if d, ok := o.obj["data"].(map[string]interface{}); ok {
				for k, v := range d {
					s.statusCM += len(k) + len(fmt.Sprint(v))
				}
			}
		}
	}
	t.Logf("n=%d policy=%d enforceCP=%d informCP=%d statusCM=%d namespaces=%d objects=%d hub=%s spoke=%s",
		s.n, s.policy, s.enforceCP, s.informCP, s.statusCM, s.namespaces, s.objects, s.hubDur, s.spokeDur)
	return s
}

func stressScaleCounts(t *testing.T, base []int) []int {
	if os.Getenv("STRESS_SCALE") != "" {
		return append(base, 500)
	}
	return base
}

// stressLateProjectLive is the live state of scale-0199, the last realistic project by name: its
// Namespace, a RoleBinding, a default deny, and its cluster-scoped AdminNetworkPolicy and EgressIP,
// all owned by project onboarding from an earlier run.
const stressLateProjectLive = `apiVersion: v1
kind: Namespace
metadata:
  name: scale-0199
  labels: {autoshift.io/project: scale-0199, autoshift.io/managed-by: project-onboarding}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-admin
  namespace: scale-0199
  labels: {autoshift.io/project: scale-0199, autoshift.io/managed-by: project-onboarding}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: autoshift-default-deny-egress
  namespace: scale-0199
  labels: {autoshift.io/project: scale-0199, autoshift.io/managed-by: project-onboarding}
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-project-scale-0199
  labels: {autoshift.io/project: scale-0199, autoshift.io/managed-by: project-onboarding}
---
apiVersion: k8s.ovn.org/v1
kind: EgressIP
metadata:
  name: autoshift-scale-0199
  labels: {autoshift.io/project: scale-0199, autoshift.io/managed-by: project-onboarding}
`

// Realistic projects past the size guard: 60 projects in one shard fill it, so that shard Policy
// stays under the etcd request limit, is NonCompliant through its status ConfigMap with the size
// message, and its last project by name, beyond the guard, keeps everything it owns on the cluster:
// nothing of it is pruned and its namespace is not touched. The other shards are not affected.
func TestStress_Scale_RealisticSizeGuard(t *testing.T) {
	t.Parallel()
	shard := onboardingShardOf("scale-0199")
	names := shardNames("scale", shard, 60)
	last := names[len(names)-1]
	live := strings.ReplaceAll(stressLateProjectLive, "scale-0199", last)
	r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, extra: live, mutate: func(cfg map[string]interface{}) {
		p := map[string]interface{}{}
		for i, name := range names {
			p[name] = stressRealistic(i)
		}
		cfg["projects"] = p
	}})
	size, _ := stressPolicyJSONSize(t, r.hub)
	if size > etcdRequestLimit {
		t.Errorf("hub-resolved Policy is %d bytes, over the %d etcd request limit", size, etcdRequestLimit)
	}
	status := onboardingShardStatus(t, r.objs)
	st := status[shard]
	if msg := st["project-onboarding.size"]; !strings.Contains(msg, "over the 1048576 bytes") || !strings.Contains(msg, "are not rendered") {
		t.Errorf("want the size message under project-onboarding.size of shard %d, status key holds %q", shard, msg)
	}
	for other, data := range status {
		if other != shard && len(data) != 0 {
			t.Errorf("shard %d holds no project, want no status, got %v", other, data)
		}
	}
	if !strings.Contains(st[last], "size limit") {
		t.Errorf("%s is beyond the guard, want it listed, status %q", last, st[last])
	}
	rendered := 0
	for k := range stressRendered(r.objs) {
		if strings.HasPrefix(k, "Namespace||scale-") {
			rendered++
		}
		if strings.Contains(k, last) {
			t.Errorf("%s is beyond the guard but %s is rendered (its namespace must not be touched)", last, k)
		}
	}
	if rendered == 0 || rendered == len(names) {
		t.Errorf("want the projects before the guard rendered and the rest not, %d of %d rendered", rendered, len(names))
	}
	for k, v := range stressPruned(r.objs) {
		if strings.Contains(k, "scale-") {
			t.Errorf("CRITICAL: %s is pruned (%s); a project beyond the size guard must keep its objects", k, v)
		}
	}
}

// Realistic projects: every shard Policy must stay under the etcd request limit at 200 projects
// with every project rendered (sharding spreads them over eight Policies); the test also reports
// the per-project cost of the largest shard. With STRESS_SCALE=1 it also runs 500, past the
// ceiling, where the size guard of the fullest shards stops rendering.
func TestStress_Scale_RealisticPolicySize(t *testing.T) {
	t.Parallel()
	var prev stressScale
	for _, n := range stressScaleCounts(t, []int{50, 200}) {
		s := stressMeasure(t, n, stressRealistic)
		if s.namespaces != n && n <= 200 {
			t.Errorf("n=%d: %d namespaces rendered", n, s.namespaces)
		}
		if prev.n > 0 {
			per := (s.policy - prev.policy) / (s.n - prev.n)
			fixed := s.policy - per*s.n
			t.Logf("~%d bytes per realistic project, fixed %d: etcd limit at ~%d projects", per, fixed, (etcdRequestLimit-fixed)/per)
		}
		if s.policy > etcdRequestLimit {
			t.Errorf("HIGH: %d realistic projects: hub-resolved Policy is %d bytes, over the %d etcd request limit; the replicated Policy cannot be stored", n, s.policy, etcdRequestLimit)
		}
		prev = s
	}
}

// Minimal (real-deployment shape) projects.
func TestStress_Scale_MinimalPolicySize(t *testing.T) {
	t.Parallel()
	var prev stressScale
	for _, n := range stressScaleCounts(t, []int{50, 200}) {
		s := stressMeasure(t, n, stressMinimal)
		if prev.n > 0 {
			per := (s.policy - prev.policy) / (s.n - prev.n)
			fixed := s.policy - per*s.n
			t.Logf("~%d bytes per minimal project, fixed %d: etcd limit at ~%d projects", per, fixed, (etcdRequestLimit-fixed)/per)
		}
		if s.policy > etcdRequestLimit {
			t.Errorf("HIGH: %d minimal projects: hub-resolved Policy is %d bytes, over the etcd request limit", n, s.policy)
		}
		prev = s
	}
}

// Errored projects: the status ConfigMap must stay under 1 MiB, and the Policy under 1.5 MiB.
func TestStress_Scale_ErroredStatus(t *testing.T) {
	t.Parallel()
	var prev stressScale
	for _, n := range stressScaleCounts(t, []int{50, 200}) {
		s := stressMeasure(t, n, stressErrored)
		if prev.n > 0 {
			per := (s.statusCM - prev.statusCM) / (s.n - prev.n)
			t.Logf("~%d status bytes per errored project: ConfigMap limit at ~%d projects", per, configMapLimit/per)
		}
		if s.statusCM > configMapLimit {
			t.Errorf("HIGH: %d errored projects: status ConfigMap data %d bytes, over 1 MiB", n, s.statusCM)
		}
		if s.policy > etcdRequestLimit {
			t.Errorf("HIGH: %d errored projects: hub-resolved Policy %d bytes, over the etcd request limit", n, s.policy)
		}
		prev = s
	}
}

// Breaking point (STRESS_SCALE=1): the largest project count whose hub-resolved Policy fits the
// etcd request limit, by bisection, for each project shape.
func TestStress_Scale_BreakingPoint(t *testing.T) {
	t.Parallel()
	if os.Getenv("STRESS_SCALE") == "" {
		t.Skip("set STRESS_SCALE=1")
	}
	for name, gen := range map[string]func(int) map[string]interface{}{"realistic": stressRealistic, "minimal": stressMinimal, "errored": stressErrored} {
		t.Run(name, func(t *testing.T) {
			lo, hi := 1, 600
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if stressMeasure(t, mid, gen).policy <= etcdRequestLimit {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			t.Logf("BREAKING POINT %s: %d projects fit, %d do not", name, lo, lo+1)
		})
	}
}

// stressCeiling reports whether n projects of the shape all render: no hub error and no shard
// reporting the size status.
func stressCeiling(t *testing.T, n int, name func(int) string, gen func(int) map[string]interface{}, prune bool) bool {
	t.Helper()
	labels := map[string]string(nil)
	if prune {
		labels = pruneOn
	}
	r := stressRun(t, stressOpts{allShards: true, labels: labels, allowHubErr: true, mutate: func(cfg map[string]interface{}) {
		p := map[string]interface{}{}
		for i := 0; i < n; i++ {
			p[name(i)] = gen(i)
		}
		cfg["projects"] = p
	}})
	if len(r.hubErrs) > 0 {
		return false
	}
	for _, s := range stressPolicySizes(t, r.hub) {
		if s.real > etcdRequestLimit {
			t.Errorf("n=%d: %s is %d bytes, over the etcd request limit", n, s.name, s.real)
		}
	}
	return stressStatus(t, r.objs)["project-onboarding.size"] == ""
}

// Ceilings (STRESS_SCALE=1): the largest number of projects per cluster that all render, for each
// shape, name length and prune setting, by bisection. The fullest shard decides, so the ceiling
// depends on how the names hash; the names here are sequential (p-0000 or a 63-character
// n...n-0000). Recorded in the project-onboarding README, Size limit.
func TestStress_Scale_ShardCeilings(t *testing.T) {
	t.Parallel()
	if os.Getenv("STRESS_SCALE") == "" {
		t.Skip("set STRESS_SCALE=1")
	}
	names := map[string]func(int) string{
		"short": func(i int) string { return fmt.Sprintf("p-%04d", i) },
		"63":    longName,
	}
	shapes := map[string]func(int) map[string]interface{}{"realistic": stressRealistic, "groups-only": stressMinimal}
	for shape, gen := range shapes {
		for nl, name := range names {
			for _, prune := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/prune %v", shape, nl, prune), func(t *testing.T) {
					lo, hi := 1, 3200
					if shape == "realistic" {
						hi = 600
					}
					for lo < hi {
						mid := (lo + hi + 1) / 2
						if stressCeiling(t, mid, name, gen, prune) {
							lo = mid
						} else {
							hi = mid - 1
						}
					}
					t.Logf("CEILING %s, %s names, prune %v: %d projects render, %d do not", shape, nl, prune, lo, lo+1)
				})
			}
		}
	}
}

// The total backstop: more than 3200 declared projects fail the hub template of every shard, so
// nothing is applied or pruned; 3200 resolve (STRESS_SCALE=1 also times the full run).
func TestStress_Scale_TotalBackstop(t *testing.T) {
	t.Parallel()
	gen := func(n int) func(cfg map[string]interface{}) {
		return func(cfg map[string]interface{}) {
			p := map[string]interface{}{}
			for i := 0; i < n; i++ {
				p[fmt.Sprintf("p-%04d", i)] = map[string]interface{}{"groups": map[string]interface{}{"admin": "ADMINS"}}
			}
			cfg["projects"] = p
		}
	}
	r := stressRun(t, stressOpts{allShards: true, mutate: gen(3201), allowHubErr: true})
	// Every shard Policy and its -test Policy: the backstop is in the template both render.
	if len(r.hubErrs) != 2*onboardingShards {
		t.Fatalf("3201 projects: want every shard Policy and every -test Policy to fail, got %d errors", len(r.hubErrs))
	}
	for _, e := range r.hubErrs {
		if !strings.Contains(e, "project-onboarding: 3201 projects declared, maximum 3200 per cluster") {
			t.Errorf("3201 projects: unexpected hub error %.300s", e)
		}
	}
	if os.Getenv("STRESS_SCALE") == "" {
		return
	}
	r = stressRun(t, stressOpts{allShards: true, mutate: gen(3200), allowHubErr: true})
	if len(r.hubErrs) > 0 {
		m := backstopRe.FindString(strings.Join(r.hubErrs, "; "))
		t.Logf("3200 projects: backstop %q (hub %s)", m, r.hubDur)
		if m == "" || strings.Contains(m, "projects declared") {
			t.Errorf("3200 projects: want them to resolve or hit only the size backstop, got %.300s", r.hubErrs[0])
		}
		return
	}
	t.Logf("3200 projects: hub %s, spoke %s", r.hubDur, r.spokeDur)
}
