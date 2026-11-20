//go:build integration

package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

// Sharding: project onboarding is eight Policies, policy-project-onboarding-0 to -7, rendered from
// one template. A project belongs to shard adler32(name) mod 8; each shard parses the whole config
// (so cross-project checks see every project) but renders, reports and prunes only its own.

// shardNames returns n project names, with prefix, that all belong to shard.
func shardNames(prefix string, shard, n int) []string {
	var out []string
	for i := 0; len(out) < n; i++ {
		name := fmt.Sprintf("%s-%04d", prefix, i)
		if onboardingShardOf(name) == shard {
			out = append(out, name)
		}
	}
	return out
}

// shardOfPolicy returns the shard number of a shard Policy name, -1 for another name.
func shardOfPolicy(name string) int {
	var i int
	if _, err := fmt.Sscanf(name, onboardingShardPrefix+"%d", &i); err != nil || name != fmt.Sprintf("%s%d", onboardingShardPrefix, i) {
		return -1
	}
	return i
}

// objectProject returns the project an object entry belongs to by its labels, or by its name for a
// Namespace, "" for neither.
func objectProject(o onboardingObject) string {
	meta, _ := o.obj["metadata"].(map[string]interface{})
	if p := stringMap(meta, "labels")["autoshift.io/project"]; p != "" {
		return p
	}
	return ""
}

// The mapping is adler32 of the name, mod 8, and must never change: a changed mapping moves projects
// between Policies, and every moved project's objects would be orphaned by one shard and adopted
// by another in the same sync. The pinned values are sprig adler32sum, as the hub computes it.
func TestProjectOnboarding_ShardAssignment(t *testing.T) {
	t.Parallel()
	pinned := map[string]int{
		"payments-api": 1, "reporting": 3, "legacy-app": 4, "removed-project": 7,
		"orders": 0, "customer-portal": 2, "ops-console": 3, "vpn": 5, "task-sync": 6, "inventory": 7,
	}
	for name, want := range pinned {
		if got := onboardingShardOf(name); got != want {
			t.Errorf("shard of %s = %d, want %d: the shard mapping changed", name, got, want)
		}
	}
	seen := map[int]int{}
	for i := 0; i < 400; i++ {
		seen[onboardingShardOf(fmt.Sprintf("p-%04d", i))]++
	}
	for shard := 0; shard < onboardingShards; shard++ {
		if seen[shard] < 25 {
			t.Errorf("shard %d holds %d of 400 names, want an even spread", shard, seen[shard])
		}
	}
	t.Logf("400 names per shard: %v", seen)
}

// Every project renders in exactly one shard, its own, in both ConfigurationPolicies of that shard
// only; every shard renders its projects.
func TestProjectOnboarding_ProjectInExactlyOneShard(t *testing.T) {
	t.Parallel()
	var names []string
	r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
		p := map[string]interface{}{}
		for i := 0; i < 48; i++ {
			name := fmt.Sprintf("one-%04d", i)
			names = append(names, name)
			if i%2 == 0 {
				p[name] = stressRealistic(i)
			} else {
				p[name] = stressMinimal(i)
				delete(p[name].(map[string]interface{}), "adopt")
			}
		}
		cfg["projects"] = p
	}})
	shardsUsed := map[int]bool{}
	nsIn := map[string]map[string]bool{}
	for _, o := range r.objs {
		if isStatus(o) || o.complianceType == "mustnothave" {
			continue
		}
		project := objectProject(o)
		if meta, _ := o.obj["metadata"].(map[string]interface{}); o.obj["kind"] == "AdminNetworkPolicy" && strings.HasPrefix(fmt.Sprint(meta["name"]), "autoshift-admin-") {
			if o.policy != onboardingShardPrefix+"0" {
				t.Errorf("admin %s is rendered by %s, want shard 0", stressKey(o), o.policy)
			}
			continue
		}
		if project == "" {
			t.Errorf("rendered %s without an autoshift.io/project label", stressKey(o))
			continue
		}
		want := onboardingShardOf(project)
		if got := shardOfPolicy(o.policy); got != want {
			t.Errorf("%s of project %s is rendered by %s, want shard %d", stressKey(o), project, o.policy, want)
		}
		shardsUsed[want] = true
		if o.obj["kind"] == "Namespace" {
			if nsIn[project] == nil {
				nsIn[project] = map[string]bool{}
			}
			nsIn[project][o.policy] = true
		}
	}
	for _, name := range names {
		if len(nsIn[name]) != 1 {
			t.Errorf("project %s: Namespace rendered by %d Policies, want exactly one", name, len(nsIn[name]))
		}
	}
	if len(shardsUsed) != onboardingShards {
		t.Errorf("48 projects used shards %v, want all %d", shardsUsed, onboardingShards)
	}
	if st := stressStatus(t, r.objs); len(st) != 0 {
		t.Errorf("want no status, got %v", st)
	}
}

// shardPruneLive is stale state across shards: removed projects (each with a labeled namespace and
// a binding), a binding in a current project's namespace labeled with a removed project of another
// shard, and a stale admin AdminNetworkPolicy.
func shardPruneLive(removed []string, current, foreign string) string {
	var b strings.Builder
	for _, name := range removed {
		fmt.Fprintf(&b, `apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels: {autoshift.io/project: %[1]s, autoshift.io/managed-by: project-onboarding}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-admin
  namespace: %[1]s
  labels: {autoshift.io/project: %[1]s, autoshift.io/managed-by: project-onboarding}
---
apiVersion: k8s.ovn.org/v1
kind: EgressIP
metadata:
  name: autoshift-%[1]s
  labels: {autoshift.io/project: %[1]s, autoshift.io/managed-by: project-onboarding}
---
`, name)
	}
	fmt.Fprintf(&b, `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: autoshift-stale
  namespace: %[1]s
  labels: {autoshift.io/project: %[2]s, autoshift.io/managed-by: project-onboarding}
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-admin-retired
  labels: {autoshift.io/managed-by: project-onboarding}
`, current, foreign)
	return b.String()
}

// Prune on, a shard only prunes objects of its own projects: a removed project's objects by the
// shard of the project its label names (the managed cluster hashes it the same way as the hub), an
// object in a current project's namespace by that project's shard whatever its label says, an
// admin AdminNetworkPolicy by shard 0. Each candidate is pruned exactly once.
func TestProjectOnboarding_PruneOnlyWithinShard(t *testing.T) {
	t.Parallel()
	var removed []string
	for shard := 0; shard < onboardingShards; shard++ {
		removed = append(removed, shardNames("gone", shard, 2)...)
	}
	current := shardNames("keep", 2, 1)[0]
	foreign := shardNames("gone-other", 5, 1)[0]
	for _, prune := range []bool{true, false} {
		t.Run(fmt.Sprintf("prune %v", prune), func(t *testing.T) {
			labels := map[string]string(nil)
			if prune {
				labels = pruneOn
			}
			r := stressRun(t, stressOpts{allShards: true, labels: labels, extra: shardPruneLive(removed, current, foreign), mutate: func(cfg map[string]interface{}) {
				cfg["projects"] = map[string]interface{}{current: map[string]interface{}{"groups": map[string]interface{}{"admin": "KEEP_ADMINS"}}}
			}})
			want := map[string]int{
				"RoleBinding|" + current + "|autoshift-stale": 2,
				"AdminNetworkPolicy||autoshift-admin-retired": 0,
			}
			for _, name := range removed {
				want["RoleBinding|"+name+"|autoshift-admin"] = onboardingShardOf(name)
				want["EgressIP||autoshift-"+name] = onboardingShardOf(name)
			}
			got := map[string][]string{}
			for _, o := range r.objs {
				if o.complianceType != "mustnothave" || isStatus(o) {
					continue
				}
				got[stressKey(o)] = append(got[stressKey(o)], o.policy)
				wantRemediation := "inform"
				if prune {
					wantRemediation = "enforce"
				}
				if o.remediationAction != wantRemediation {
					t.Errorf("%s pruned in %s (%s), want %s", stressKey(o), o.configPolicy, o.remediationAction, wantRemediation)
				}
			}
			for key, shard := range want {
				policies := got[key]
				if len(policies) != 1 || shardOfPolicy(policies[0]) != shard {
					t.Errorf("%s: pruned by %v, want exactly once by shard %d", key, policies, shard)
				}
			}
			for key, policies := range got {
				if _, ok := want[key]; !ok && strings.Contains(key, "gone") {
					t.Errorf("unexpected prune entry %s in %v", key, policies)
				}
			}
		})
	}
}

// A namespace of a project in another shard is still a declared namespace: an object of a removed
// project labeled into it is owned by the declaring project's shard, and a labeled namespace a
// project of another shard declares holds its label's objects (heldBy) in every shard.
func TestProjectOnboarding_CrossShardNamespaceOwnership(t *testing.T) {
	t.Parallel()
	holder := shardNames("holder", 6, 1)[0]
	renamed := shardNames("renamed", 1, 1)[0]
	live := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels: {autoshift.io/project: %[2]s, autoshift.io/managed-by: project-onboarding}
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-project-%[2]s
  labels: {autoshift.io/project: %[2]s, autoshift.io/managed-by: project-onboarding}
`, holder, renamed)
	r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, extra: live, mutate: func(cfg map[string]interface{}) {
		cfg["projects"] = map[string]interface{}{holder: map[string]interface{}{"groups": map[string]interface{}{"admin": "H_ADMINS"}}}
	}})
	if p, ok := stressPruned(r.objs)["AdminNetworkPolicy||autoshift-project-"+renamed]; ok {
		t.Errorf("the AdminNetworkPolicy of %s still selects namespace %s, which %s (shard %d) declares, but is pruned (%s)",
			renamed, holder, holder, onboardingShardOf(holder), p)
	}
	st := stressStatus(t, r.objs)
	if want := "namespace " + holder + " is owned by project " + renamed; st[holder] != want {
		t.Errorf("status[%s] = %q, want %q", holder, st[holder], want)
	}
}

// Cross-project checks see every project, whichever shard it is in: an egress IP two projects of
// different shards declare, and a namespace two such projects declare, are reported on each
// affected project in its own shard's status ConfigMap.
func TestProjectOnboarding_CrossShardDuplicates(t *testing.T) {
	t.Parallel()
	a, b := shardNames("dup-a", 3, 1)[0], shardNames("dup-b", 6, 1)[0]
	c, d := shardNames("dup-c", 0, 1)[0], shardNames("dup-d", 7, 1)[0]
	r := stressRun(t, stressOpts{allShards: true, mutate: func(cfg map[string]interface{}) {
		grp := func(n string) map[string]interface{} { return map[string]interface{}{"admin": n + "_ADMINS"} }
		cfg["projects"] = map[string]interface{}{
			a: map[string]interface{}{"groups": grp(a), "egressIP": []interface{}{"198.51.100.77"}},
			b: map[string]interface{}{"groups": grp(b), "egressIP": []interface{}{"198.51.100.77"}},
			c: map[string]interface{}{"groups": grp(c), "namespace": "shared-ns"},
			d: map[string]interface{}{"groups": grp(d), "namespace": "shared-ns"},
		}
	}})
	status := onboardingShardStatus(t, r.objs)
	check := func(project, want string) {
		t.Helper()
		shard := onboardingShardOf(project)
		if got := status[shard][project]; !strings.Contains(got, want) {
			t.Errorf("shard %d status[%s] = %q, want it to contain %q", shard, project, got, want)
		}
		for other := range status {
			if _, ok := status[other][project]; ok && other != shard {
				t.Errorf("project %s (shard %d) is reported in shard %d", project, shard, other)
			}
		}
	}
	check(a, "egressIP 198.51.100.77 is also declared by project "+b)
	check(b, "egressIP 198.51.100.77 is also declared by project "+a)
	// The first by name keeps a namespace no project is named after.
	first, second := c, d
	if d < c {
		first, second = d, c
	}
	check(second, "namespace shared-ns already declared by "+first)
	if _, ok := status[onboardingShardOf(first)][first]; ok {
		t.Errorf("%s keeps shared-ns, want no status for it, got %q", first, status[onboardingShardOf(first)][first])
	}
	for _, p := range []string{a, b, second} {
		if namespaceNamed(r.objs, p) != nil {
			t.Errorf("%s is in error, want no Namespace rendered", p)
		}
	}
	if namespaceNamed(r.objs, "shared-ns") == nil {
		t.Errorf("%s keeps shared-ns, want it rendered", first)
	}
}

// Admin network policies belong to no project: shard 0 renders, reports and prunes them; the other
// shards do not even read them, so their errors fail only shard 0's admin tier.
func TestProjectOnboarding_AdminANPOnlyInShard0(t *testing.T) {
	t.Parallel()
	r := stressRun(t, stressOpts{allShards: true, mutate: func(cfg map[string]interface{}) {
		stressDefaults(cfg)["adminNetworkPolicies"] = stressAdminEntries(5)
	}})
	var n int
	for _, o := range r.objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		if o.obj["kind"] == "AdminNetworkPolicy" && strings.HasPrefix(name, "autoshift-admin-") && o.complianceType != "mustnothave" {
			n++
			if o.policy != onboardingShardPrefix+"0" || o.remediationAction != "enforce" {
				t.Errorf("%s rendered by %s (%s), want only shard 0's enforce ConfigurationPolicy", name, o.configPolicy, o.remediationAction)
			}
		}
	}
	if n != 5 {
		t.Errorf("want 5 admin AdminNetworkPolicies, got %d", n)
	}
	bad := stressRun(t, stressOpts{allShards: true, mutate: func(cfg map[string]interface{}) {
		stressDefaults(cfg)["adminNetworkPolicies"] = map[string]interface{}{"broken": "not a map"}
	}})
	status := onboardingShardStatus(t, bad.objs)
	for shard, data := range status {
		_, has := data["projectDefaults.adminNetworkPolicies"]
		if has != (shard == 0) {
			t.Errorf("shard %d: admin tier error reported %v, want only in shard 0 (status %v)", shard, has, data)
		}
	}
}

// A config that cannot be read blocks every shard, so every shard reports it and stays
// NonCompliant (fail closed), and none sweeps.
func TestProjectOnboarding_RenderedConfigErrorInEveryShard(t *testing.T) {
	t.Parallel()
	res := runOnboardingAllShards(t, pruneOn, func(cfg map[string]interface{}) { cfg["projects"] = "oops" })
	objs := onboardingObjects(t, res)
	for shard, data := range onboardingShardStatus(t, objs) {
		if data["rendered-config"] == "" {
			t.Errorf("shard %d does not report the rendered-config error: %v", shard, data)
		}
	}
	if n := countPruned(objs); n != 0 {
		t.Errorf("want no mustnothave, got %d", n)
	}
}

// The size guard is per shard: projects that all hash to one shard fill only that shard, which
// reports the size status and stops rendering, while the other shards render their projects in
// full and stay under the limit.
func TestProjectOnboarding_SizeGuardPerShard(t *testing.T) {
	t.Parallel()
	full := shardNames("full", 4, 60)
	others := []string{}
	for shard := 0; shard < onboardingShards; shard++ {
		if shard != 4 {
			others = append(others, shardNames("calm", shard, 3)...)
		}
	}
	r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, mutate: func(cfg map[string]interface{}) {
		p := map[string]interface{}{}
		for i, name := range full {
			p[name] = stressRealistic(i)
		}
		for i, name := range others {
			p[name] = stressRealistic(100 + i)
		}
		cfg["projects"] = p
	}})
	status := onboardingShardStatus(t, r.objs)
	for shard, data := range status {
		_, skipped := data["project-onboarding.size"]
		if skipped != (shard == 4) {
			t.Errorf("shard %d: size status %v, want it only on shard 4", shard, skipped)
		}
	}
	for _, s := range sizeOf(t, r) {
		if s.real > etcdRequestLimit {
			t.Errorf("%s: %d bytes, over the etcd request limit", s.name, s.real)
		}
		if shardOfPolicy(s.name) != 4 && s.est < s.real {
			t.Errorf("%s: estimate %d below the measured %d", s.name, s.est, s.real)
		}
	}
	for _, name := range others {
		if namespaceNamed(r.objs, name) == nil {
			t.Errorf("%s is in a shard with room, want it rendered", name)
		}
	}
}

// The deployment-shaped projects, adopted by the single Policy, are adopted by the shard Policies as they
// are: every Namespace musthave with exact metadata in its shard's enforce ConfigurationPolicy,
// nothing of them pruned, under prune on and off (the golden file pins the exact content).
func TestProjectOnboarding_MigrationAdoptsNothingPruned(t *testing.T) {
	t.Parallel()
	projects := migrationRealProjects(t)
	for _, sc := range migrationScenarios {
		t.Run(sc.name, func(t *testing.T) {
			objs := migrationRun(t, sc.remediation, sc.prune)
			for _, o := range objs {
				if isStatus(o) {
					continue
				}
				meta, _ := o.obj["metadata"].(map[string]interface{})
				ns, _ := meta["namespace"].(string)
				name, _ := meta["name"].(string)
				owner := ns
				if o.obj["kind"] == "Namespace" {
					owner = name
				}
				if _, real := projects[owner]; real && o.complianceType == "mustnothave" && name != "autoshift-legacy" {
					t.Errorf("CRITICAL: %s of an adopted project is pruned", stressKey(o))
				}
				if _, real := projects[owner]; real && shardOfPolicy(o.policy) != onboardingShardOf(owner) {
					t.Errorf("%s of %s is in %s, want shard %d", stressKey(o), owner, o.policy, onboardingShardOf(owner))
				}
			}
			for name := range projects {
				ns := namespaceIn(objs, name, sc.remediation)
				if len(ns) != 1 {
					t.Errorf("%s: %d Namespace entries in the %s ConfigurationPolicy, want 1", name, len(ns), sc.remediation)
					continue
				}
				if ns[0].item["metadataComplianceType"] != "mustonlyhave" || ns[0].complianceType != "musthave" {
					t.Errorf("%s: an existing Namespace is adopted musthave with exact metadata, got %v / %v",
						name, ns[0].complianceType, ns[0].item["metadataComplianceType"])
				}
			}
		})
	}
}

// The eight shard Policies are one template: the PolicyGenerator config lists exactly
// policy-project-onboarding-0 to -7, each with the same two manifests, ConfigurationPolicy names
// derived from the Policy and no pruneObjectBehavior anywhere (removing a Policy or an object from it
// must never delete anything); the template's shard count and name pattern agree.
func TestProjectOnboarding_ShardPoliciesInSync(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "policies", "stable", "project-onboarding")
	b, err := os.ReadFile(filepath.Join(dir, "policy-generator-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pruneObjectBehavior") {
		t.Error("policy-generator-config.yaml sets pruneObjectBehavior; removing a shard Policy or an object would delete it")
	}
	var cfg struct {
		PlacementBindingDefaults struct {
			Name string `json:"name"`
		} `json:"placementBindingDefaults"`
		PolicyDefaults struct {
			RemediationAction string `json:"remediationAction"`
			Placement         struct {
				PlacementPath string `json:"placementPath"`
			} `json:"placement"`
		} `json:"policyDefaults"`
		Policies []struct {
			Name                 string `json:"name"`
			RemediationAction    string `json:"remediationAction"`
			ConsolidateManifests *bool  `json:"consolidateManifests"`
			Placement            interface{}
			Dependencies         interface{}              `json:"dependencies"`
			Manifests            []map[string]interface{} `json:"manifests"`
		} `json:"policies"`
	}
	if err := sigsyaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	// Each shard Policy, then its inform-only -test Policy (developer guide, "Gating on cluster
	// state").
	if len(cfg.Policies) != 2*onboardingShards {
		t.Fatalf("want %d policies, got %d", 2*onboardingShards, len(cfg.Policies))
	}
	if cfg.PolicyDefaults.Placement.PlacementPath != "placement.yaml" {
		t.Errorf("all shards must share placement.yaml, got %q", cfg.PolicyDefaults.Placement.PlacementPath)
	}
	if cfg.PolicyDefaults.RemediationAction != "${REMEDIATION}" {
		t.Errorf("policyDefaults.remediationAction = %q, want ${REMEDIATION}", cfg.PolicyDefaults.RemediationAction)
	}
	for i, p := range cfg.Policies {
		want := fmt.Sprintf("%s%d", onboardingShardPrefix, i/2)
		if i%2 == 1 {
			want += onboardingTestSuffix
		}
		if p.Name != want {
			t.Errorf("policy %d is %s, want %s", i, p.Name, want)
		}
		if p.Placement != nil {
			t.Errorf("%s sets its own placement; every shard selects the same clusters", p.Name)
		}
		if p.Dependencies != nil {
			t.Errorf("%s sets dependencies; neither Policy of a shard may depend on the other (README, Policies)", p.Name)
		}
		var wantManifests []map[string]interface{}
		if i%2 == 0 {
			// The manifest name keeps the earlier ConfigurationPolicy name, so the live one is
			// updated in place; PolicyGenerator honors it only with consolidateManifests false.
			if p.ConsolidateManifests == nil || *p.ConsolidateManifests {
				t.Errorf("%s: consolidateManifests must be false (the manifest names the ConfigurationPolicy)", p.Name)
			}
			if p.RemediationAction != "" {
				t.Errorf("%s: remediationAction = %q, want it inherited from policyDefaults (${REMEDIATION})", p.Name, p.RemediationAction)
			}
			wantManifests = []map[string]interface{}{{"path": "manifests", "name": want + "-enforce"}}
		} else {
			if p.RemediationAction != "inform" {
				t.Errorf("%s: remediationAction = %q, want inform", p.Name, p.RemediationAction)
			}
			wantManifests = []map[string]interface{}{{"path": "test/projects.yaml"}}
		}
		if fmt.Sprint(p.Manifests) != fmt.Sprint(wantManifests) {
			t.Errorf("%s manifests = %v, want %v", p.Name, p.Manifests, wantManifests)
		}
	}
	for _, sub := range []string{"manifests", "test"} {
		src, err := os.ReadFile(filepath.Join(dir, sub, "projects.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), fmt.Sprintf("{{hub- $shards := %d hub}}", onboardingShards)) {
			t.Errorf("%s: the template does not set $shards := %d", sub, onboardingShards)
		}
		if !strings.Contains(string(src), `(regexMatch (printf "^policy-project-onboarding-[0-7]%s$" $nameSuffix) $policyName)`) ||
			!strings.Contains(string(src), `{{hub- $nameSuffix := ternary "" "-test" (eq $want "enforce") hub}}`) {
			t.Errorf("%s: the template does not validate the Policy name against ^policy-project-onboarding-[0-7]$ (enforce) or ^policy-project-onboarding-[0-7]-test$ (inform)", sub)
		}
	}
}

// A Policy whose name carries no valid shard fails the hub template, so it renders nothing rather
// than every project or none silently. So does a Policy whose name does not match its template: a
// -test name on the enforce template, or a shard name on the inform one.
func TestProjectOnboarding_UnshardedPolicyNameFails(t *testing.T) {
	t.Parallel()
	raw := stressRaw(t, "enforce")
	cfgs, declared := stressConfigs(t)
	seed := stressSeed(t, stressOpts{}, cfgs)
	hubR, err := NewResolver(seed)
	if err != nil {
		t.Fatal(err)
	}
	ctx := HubContext{ManagedClusterName: "lint-cluster", ManagedClusterLabels: BuildSyntheticLabels(declared)}
	// Shard 3's Policy and its -test Policy both lose the shard: both fail, nothing else does.
	hub := hubR.ResolvePolicy(strings.ReplaceAll(raw, onboardingShardPrefix+"3", "policy-project-onboarding"), ctx)
	joined := strings.Join(hub.Errors, "; ")
	for _, msg := range []string{"the Policy must be named policy-project-onboarding-<shard>, shard",
		"the Policy must be named policy-project-onboarding-<shard>-test, shard"} {
		if !strings.Contains(joined, msg) {
			t.Errorf("want the renamed Policies to fail the hub template with %q, got errors %q", msg, joined)
		}
	}
	if len(hub.Errors) != 2 || !strings.HasPrefix(joined, "policy-project-onboarding") {
		t.Errorf("want only the two renamed Policies to fail, got %d errors: %q", len(hub.Errors), joined)
	}
	// The names of shard 5's Policies swapped: each template refuses the other's name.
	swapped := strings.NewReplacer(
		"  name: "+onboardingShardPrefix+"5\n", "  name: "+onboardingShardPrefix+"5"+onboardingTestSuffix+"\n",
		"  name: "+onboardingShardPrefix+"5"+onboardingTestSuffix+"\n", "  name: "+onboardingShardPrefix+"5\n",
	).Replace(raw)
	if swapped == raw {
		t.Fatal("shard 5's Policy names not found in the render")
	}
	hub = hubR.ResolvePolicy(swapped, ctx)
	if len(hub.Errors) != 2 {
		t.Errorf("want both swapped Policies of shard 5 to fail, got %d errors: %q", len(hub.Errors), strings.Join(hub.Errors, "; "))
	}
}

// A cluster-scoped object a project declares belongs to the project in its name, which is what the
// declaration is keyed on, not to the project its label names: a live EgressIP autoshift-<a> or
// AdminNetworkPolicy autoshift-project-<a> whose label names a project of another shard (current
// or removed), or carries no project label, is never pruned by any shard while <a> declares it, and
// <a>'s shard renders it.
func TestProjectOnboarding_DeclaredClusterScopedRoutedByName(t *testing.T) {
	t.Parallel()
	a := shardNames("owner", 3, 1)[0]
	b := shardNames("other", 5, 1)[0]
	gone := shardNames("gone", 6, 1)[0]
	declares := func(cfg map[string]interface{}, withB bool) {
		p := map[string]interface{}{a: map[string]interface{}{
			"groups":   map[string]interface{}{"admin": "A_ADMINS"},
			"egressIP": []interface{}{"198.51.100.40"},
			"network": map[string]interface{}{"rules": map[string]interface{}{"deny-db": map[string]interface{}{
				"direction": "egress", "action": "deny",
				"to": map[string]interface{}{"namespaces": map[string]interface{}{"names": []interface{}{"db"}}}}}},
		}}
		if withB {
			p[b] = map[string]interface{}{"groups": map[string]interface{}{"admin": "B_ADMINS"}}
		}
		cfg["projects"] = p
	}
	live := func(label string) string {
		lbl := "{autoshift.io/managed-by: project-onboarding}"
		if label != "" {
			lbl = fmt.Sprintf("{autoshift.io/project: %s, autoshift.io/managed-by: project-onboarding}", label)
		}
		return fmt.Sprintf(`apiVersion: k8s.ovn.org/v1
kind: EgressIP
metadata:
  name: autoshift-%[1]s
  labels: %[2]s
---
apiVersion: policy.networking.k8s.io/v1alpha1
kind: AdminNetworkPolicy
metadata:
  name: autoshift-project-%[1]s
  labels: %[2]s
`, a, lbl)
	}
	for name, c := range map[string]struct {
		label string
		withB bool
	}{
		"label names a current project of another shard": {b, true},
		"label names a removed project of another shard": {gone, false},
		"no project label": {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			r := stressRun(t, stressOpts{allShards: true, labels: pruneOn, extra: live(c.label), mutate: func(cfg map[string]interface{}) { declares(cfg, c.withB) }})
			for _, key := range []string{"EgressIP||autoshift-" + a, "AdminNetworkPolicy||autoshift-project-" + a} {
				var rendered []string
				for _, o := range r.objs {
					if stressKey(o) != key {
						continue
					}
					if o.complianceType == "mustnothave" {
						t.Errorf("CRITICAL: %s, declared by %s (shard 3), is pruned by %s", key, a, o.configPolicy)
						continue
					}
					rendered = append(rendered, o.configPolicy)
				}
				if len(rendered) != 1 || rendered[0] != onboardingShardPrefix+"3-enforce" {
					t.Errorf("%s rendered by %v, want only %s3-enforce", key, rendered, onboardingShardPrefix)
				}
			}
		})
	}
}

// The harness resolves only the shards onboardingShardsFor keeps (prune off; prune on resolves all
// eight). That is sound only if every other shard renders nothing but its status, cleared or holding
// only the keys every shard reports, and if dropping the other Policies changes nothing in the kept
// ones: checked against a full render, with prune on and off, for the example and stress projects
// and for the shapes that drop the most shards (no projects key, no hub config, every project
// removed, a moved namespace, stubs left out).
func TestProjectOnboarding_ShardFilterKeepsEveryRenderingShard(t *testing.T) {
	t.Parallel()
	byPolicy := func(objs []onboardingObject) map[string][]string {
		out := map[string][]string{}
		for _, o := range objs {
			out[o.policy] = append(out[o.policy], o.configPolicy+" "+o.complianceType+" "+asJSON(o.item))
		}
		return out
	}
	removeAll := func(cfg map[string]interface{}) { cfg["projects"] = map[string]interface{}{} }
	noProjectsKey := func(cfg map[string]interface{}) { delete(cfg, "projects") }
	noHubConfig := func(cfg map[string]interface{}) {
		for k := range cfg {
			delete(cfg, k)
		}
	}
	moved := func(cfg map[string]interface{}) { stressProject(cfg, "payments-api")["namespace"] = "onb-unowned" }
	scenarios := map[string]stressOpts{
		"example":            {},
		"stress":             {stubs: []string{stressPruneStubs}, mutate: stressAddAB},
		"no projects key":    {mutate: noProjectsKey},
		"no hub config":      {mutate: noHubConfig},
		"every project gone": {mutate: removeAll},
		"namespace moved":    {mutate: moved},
		"no network stubs":   {drop: []string{onboardingNetworkStubs}},
		"no ANP stubs":       {drop: []string{onboardingANPStubs}, mutate: removeAll},
	}
	for name, o := range scenarios {
		for _, prune := range []string{"off", "on"} {
			o := o
			if prune == "on" {
				o.labels = pruneOn
			}
			t.Run(name+"/prune "+prune, func(t *testing.T) {
				t.Parallel()
				full := o
				full.allShards = true
				all := stressRun(t, full)
				filtered := o
				filtered.filtered = true
				some := stressRun(t, filtered)
				cfgs, _ := stressConfigs(t)
				if o.mutate != nil {
					o.mutate(cfgs.HubConfig)
				}
				keep := onboardingShardsFor(cfgs, stressSeed(t, o, cfgs), false)
				if len(keep) == onboardingShards {
					t.Fatalf("scenario keeps every shard, so it checks nothing: %v", keep)
				}
				status := onboardingShardStatus(t, all.objs)
				common := map[string]bool{}
				for k := range status[0] {
					common[k] = true
				}
				for _, data := range status {
					for k := range common {
						if _, ok := data[k]; !ok {
							delete(common, k)
						}
					}
				}
				for shard, data := range status {
					if keep[shard] {
						continue
					}
					for k := range data {
						if !common[k] {
							t.Errorf("shard %d is not kept but reports %q: %s", shard, k, data[k])
						}
					}
				}
				for _, obj := range all.objs {
					if !keep[onboardingShardOfPolicy(obj.policy)] && !isStatus(obj) {
						t.Errorf("%s is not kept but renders %s %s", obj.policy, obj.complianceType, stressKey(obj))
					}
				}
				want, got := byPolicy(all.objs), byPolicy(some.objs)
				for policy := range want {
					if !keep[onboardingShardOfPolicy(policy)] {
						delete(want, policy)
					}
				}
				if maskNow(asJSON(want)) != maskNow(asJSON(got)) {
					t.Errorf("the kept shards render differently alone:\nfull:     %v\nfiltered: %v", want, got)
				}
			})
		}
	}
}

// The other direction of name routing: a live EgressIP autoshift-<gone>, named for a removed project
// but labeled with a current project in mode: inform, belongs to <gone> by its name. With prune on it
// is deleted, exactly once, by the enforce ConfigurationPolicy of <gone>'s shard (the label's
// inform mode does not apply); with prune off it is only reported, once, by that shard's inform one.
// AutoShift never renders such a mismatch (README, Pruning).
func TestProjectOnboarding_UndeclaredEgressIPRoutedByName(t *testing.T) {
	t.Parallel()
	a := shardNames("keeper", 2, 1)[0]
	gone := shardNames("gone", 5, 1)[0]
	live := fmt.Sprintf(`apiVersion: k8s.ovn.org/v1
kind: EgressIP
metadata:
  name: autoshift-%s
  labels: {autoshift.io/project: %s, autoshift.io/managed-by: project-onboarding}
`, gone, a)
	mutate := func(cfg map[string]interface{}) {
		cfg["projects"] = map[string]interface{}{a: map[string]interface{}{
			"groups": map[string]interface{}{"admin": "A_ADMINS"}, "mode": "inform"}}
	}
	key := "EgressIP||autoshift-" + gone
	for prune, want := range map[string]string{"on": onboardingShardPrefix + "5-enforce", "off": onboardingShardPrefix + "5" + onboardingTestSuffix} {
		t.Run("prune "+prune, func(t *testing.T) {
			o := stressOpts{allShards: true, extra: live, mutate: mutate}
			if prune == "on" {
				o.labels = pruneOn
			}
			r := stressRun(t, o)
			var got []string
			for _, obj := range r.objs {
				if stressKey(obj) != key {
					continue
				}
				if obj.complianceType != "mustnothave" {
					t.Errorf("%s is rendered (%s) by %s, want it only pruned or reported", key, obj.complianceType, obj.configPolicy)
					continue
				}
				got = append(got, obj.configPolicy)
			}
			if len(got) != 1 || got[0] != want {
				t.Errorf("%s pruned by %v, want only %s", key, got, want)
			}
		})
	}
}
