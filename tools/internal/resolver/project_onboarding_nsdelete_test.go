//go:build integration

package resolver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// Namespace deletion: onboarding deletes the Namespace of a removed project only when it
// created that Namespace itself (marker, created-by, ownership labels), the project recorded
// deleteOnRemoval, the Namespace is not declared by any current project in any shard and is not a
// platform namespace, the cluster opted in (project-onboarding-delete-namespaces and
// project-onboarding-prune), and it holds no PersistentVolumeClaim. Every assertion that something is
// not deleted resolves all eight shards, so no shard outside a test's model can add a deletion.

const (
	nsdelLabel      = "autoshift.io/project-onboarding-delete-namespaces"
	nsdelMarker     = "autoshift.io/onboarding-created"
	nsdelCreatedBy  = "autoshift.io/onboarding-created-by"
	nsdelOnRemoval  = "autoshift.io/delete-on-removal"
	nsdelModeKey    = "autoshift.io/onboarding-mode"
	nsdelPending    = "autoshift.io/onboarding-pending-deletion-since"
	nsdelCreatedAt  = "autoshift.io/onboarding-created-at"
	nsdelUID        = "autoshift.io/onboarding-created-uid"
	nsdelBorn       = "2025-01-01T00:00:00Z" // creationTimestamp of the fixture namespaces
	nsdelLongAgo    = "2001-01-01T00:00:00Z" // a pending mark whose grace period has long passed
	nsdelKeeper     = "nsdel-keeper"
	nsdelKeeperNs   = "nsdel-keeper"
	nsdelUnrelatedN = "nsdel-unrelated"
)

// nsdelOn is a cluster with every cluster-level guard open: prune and namespace deletion on.
var nsdelOn = map[string]string{onboardingPruneLabel: "true", nsdelLabel: "true"}

// nsdelLabels returns nsdelOn with the given overrides.
func nsdelLabels(over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range nsdelOn {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// nsdelNs describes a live Namespace for the stubs.
type nsdelNs struct {
	name        string
	labels      map[string]string
	annotations map[string]string
	pvcs        int
	uid         string // metadata.uid, none when empty
	created     string // metadata.creationTimestamp, none when empty
}

// nsdelCreated is a Namespace onboarding created for project, recorded with deleteOnRemoval and the
// enforce mode, as the policy leaves it, and marked for deletion long ago: it passes every guard and
// its grace period is over, so it is deleted unless a guard under test blocks it.
func nsdelCreated(name, project string) nsdelNs {
	ns := nsdelFresh(name, project)
	ns.annotations[nsdelPending] = nsdelLongAgo
	return ns
}

// nsdelFresh is nsdelCreated before the policy marked it for deletion. Its creation record is bound
// to it: created at its creationTimestamp, and stamped with its uid.
func nsdelFresh(name, project string) nsdelNs {
	return nsdelNs{
		name:    name,
		uid:     "uid-" + name,
		created: nsdelBorn,
		labels: map[string]string{
			"autoshift.io/project":    project,
			"autoshift.io/managed-by": "project-onboarding",
			nsdelMarker:               "true",
		},
		annotations: map[string]string{
			nsdelCreatedBy: project,
			nsdelOnRemoval: "true",
			nsdelModeKey:   "enforce",
			nsdelCreatedAt: nsdelBorn,
			nsdelUID:       "uid-" + name,
		},
	}
}

func yamlMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q: %q", k, m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// nsdelStubs renders the live Namespaces, their PersistentVolumeClaims, and one claim in an
// unrelated namespace so the managed cluster always serves the PersistentVolumeClaim kind.
func nsdelStubs(nss ...nsdelNs) string {
	var b strings.Builder
	all := append([]nsdelNs{{name: nsdelUnrelatedN, pvcs: 1}}, nss...)
	for _, ns := range all {
		fmt.Fprintf(&b, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n  labels: %s\n  annotations: %s\n",
			ns.name, yamlMap(ns.labels), yamlMap(ns.annotations))
		if ns.uid != "" {
			fmt.Fprintf(&b, "  uid: %q\n", ns.uid)
		}
		if ns.created != "" {
			fmt.Fprintf(&b, "  creationTimestamp: %q\n", ns.created)
		}
		b.WriteString("---\n")
		for i := 0; i < ns.pvcs; i++ {
			fmt.Fprintf(&b, "apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: data-%d\n  namespace: %s\nspec:\n  accessModes: [ReadWriteOnce]\n  resources: {requests: {storage: 1Gi}}\n---\n", i, ns.name)
		}
	}
	return b.String()
}

// nsdelProjects replaces config.projects with keeper (so the map is never empty) plus projects.
func nsdelProjects(projects map[string]map[string]interface{}) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		p := map[string]interface{}{nsdelKeeper: map[string]interface{}{"groups": map[string]interface{}{"admin": "KEEP_ADMINS"}}}
		for name, v := range projects {
			p[name] = v
		}
		cfg["projects"] = p
	}
}

// nsdelRun resolves all eight shards against the default testdata plus the given Namespaces.
func nsdelRun(t *testing.T, remediation string, labels map[string]string, projects map[string]map[string]interface{}, nss ...nsdelNs) []onboardingObject {
	t.Helper()
	r := stressRun(t, stressOpts{allShards: true, remediation: remediation, labels: labels,
		extra: nsdelStubs(nss...), mutate: nsdelProjects(projects)})
	return r.objs
}

// nsDeletion is one mustnothave of a Namespace.
type nsDeletion struct {
	policy, configPolicy, remediation string
}

// nsDeletions maps each Namespace that gets a mustnothave, in any shard, to its entries.
func nsDeletions(objs []onboardingObject) map[string][]nsDeletion {
	out := map[string][]nsDeletion{}
	for _, o := range objs {
		if o.complianceType != "mustnothave" || o.obj["kind"] != "Namespace" {
			continue
		}
		meta, _ := o.obj["metadata"].(map[string]interface{})
		name, _ := meta["name"].(string)
		out[name] = append(out[name], nsDeletion{o.policy, o.configPolicy, o.remediationAction})
	}
	return out
}

// requireNoNamespaceDeleted fails when any shard emits a mustnothave for any Namespace.
func requireNoNamespaceDeleted(t *testing.T, objs []onboardingObject) {
	t.Helper()
	seen := map[int]bool{}
	for _, o := range objs {
		if s := onboardingShardOfPolicy(o.policy); s >= 0 {
			seen[s] = true
		}
	}
	if len(seen) != onboardingShards {
		t.Fatalf("resolved shards %v, want all %d for a negative assertion", seen, onboardingShards)
	}
	if d := nsDeletions(objs); len(d) != 0 {
		t.Errorf("want no Namespace deleted or reported for deletion in any shard, got %v", d)
	}
}

// renderedNamespaces returns every rendered (musthave) Namespace entry named name.
func renderedNamespaces(objs []onboardingObject, name string) []onboardingObject {
	var out []onboardingObject
	for _, o := range objs {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if o.obj["kind"] == "Namespace" && meta["name"] == name && o.complianceType != "mustnothave" {
			out = append(out, o)
		}
	}
	return out
}

// requireNoCreationRecord fails when a rendered Namespace entry for name carries any key of the
// creation record.
func requireNoCreationRecord(t *testing.T, objs []onboardingObject, name string) {
	t.Helper()
	entries := renderedNamespaces(objs, name)
	if len(entries) == 0 {
		t.Fatalf("no Namespace %s rendered", name)
	}
	for _, o := range entries {
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if v, ok := stringMap(meta, "labels")[nsdelMarker]; ok {
			t.Errorf("%s in %s: label %s=%q, want it absent", name, o.configPolicy, nsdelMarker, v)
		}
		ann := stringMap(meta, "annotations")
		for _, k := range []string{nsdelCreatedBy, nsdelOnRemoval, nsdelModeKey, nsdelCreatedAt, nsdelUID, nsdelPending} {
			if v, ok := ann[k]; ok {
				t.Errorf("%s in %s: annotation %s=%q, want it absent", name, o.configPolicy, k, v)
			}
		}
	}
}

func TestProjectOnboarding_NamespaceDeleteAllGuards(t *testing.T) {
	t.Parallel()
	for shard := 0; shard < onboardingShards; shard += 3 {
		gone := shardNames("nsdel-gone", shard, 1)[0]
		t.Run(gone, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRun(t, "", nsdelOn, nil, nsdelCreated(gone, gone))
			d := nsDeletions(objs)
			if len(d) != 1 || len(d[gone]) != 1 {
				t.Fatalf("want exactly one deletion, of %s, got %v", gone, d)
			}
			want := nsDeletion{fmt.Sprintf("%s%d", onboardingShardPrefix, shard), fmt.Sprintf("%s%d-enforce", onboardingShardPrefix, shard), "enforce"}
			if d[gone][0] != want {
				t.Errorf("deletion of %s = %+v, want %+v", gone, d[gone][0], want)
			}
			st := onboardingStatus(t, objs)
			if !strings.Contains(st[gone], "namespace "+gone+" was created for project "+gone+", which is no longer declared") ||
				!strings.Contains(st[gone], "it is deleted with everything in it") {
				t.Errorf("status[%s] = %q, want the deletion reported", gone, st[gone])
			}
		})
	}
}

// Each guard, missing alone, blocks the deletion.
func TestProjectOnboarding_NamespaceDeleteGuards(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-guard", 4, 1)[0]
	noRemoval := nsdelCreated(gone, gone)
	noRemoval.annotations[nsdelOnRemoval] = "false"
	unrecorded := nsdelCreated(gone, gone)
	delete(unrecorded.annotations, nsdelOnRemoval)
	withPVCs := nsdelCreated(gone, gone)
	withPVCs.pvcs = 2
	cases := []struct {
		name   string
		labels map[string]string
		ns     nsdelNs
		status string // "" for no status entry
	}{
		{"guard 1: cluster label false", nsdelLabels(map[string]string{nsdelLabel: "false"}), nsdelCreated(gone, gone),
			"it is not deleted: the cluster label project-onboarding-delete-namespaces is not 'true'"},
		{"guard 1: cluster label absent", nsdelLabels(map[string]string{nsdelLabel: "\x00absent"}), nsdelCreated(gone, gone),
			"it is not deleted: the cluster label project-onboarding-delete-namespaces is not 'true'"},
		{"guard 2: deleteOnRemoval recorded false", nsdelOn, noRemoval, ""},
		{"guard 2: deleteOnRemoval not recorded", nsdelOn, unrecorded, ""},
		{"guard 3: PersistentVolumeClaims", nsdelOn, withPVCs,
			"it is not deleted: namespace holds storage: PersistentVolumeClaim 2"},
		{"guard 4: prune off", nsdelLabels(map[string]string{onboardingPruneLabel: "false"}), nsdelCreated(gone, gone),
			"it is not deleted: the cluster label project-onboarding-prune is not 'true'"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRun(t, "", c.labels, nil, c.ns)
			requireNoNamespaceDeleted(t, objs)
			st := onboardingStatus(t, objs)
			if c.status == "" {
				if v, ok := st[gone]; ok {
					t.Errorf("status[%s] = %q, want no entry", gone, v)
				}
				return
			}
			if !strings.Contains(st[gone], c.status) {
				t.Errorf("status[%s] = %q, want it to contain %q", gone, st[gone], c.status)
			}
		})
	}
}

// The last recorded mode decides: inform (or anything but enforce) is only reported in the status.
// No ConfigurationPolicy carries a mustnothave for it, the inform one included.
func TestProjectOnboarding_NamespaceDeleteInformMode(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-inform", 2, 1)[0]
	for _, mode := range []string{"inform", "", "Enforce"} {
		mode := mode
		t.Run("mode "+mode, func(t *testing.T) {
			t.Parallel()
			ns := nsdelCreated(gone, gone)
			if mode == "" {
				delete(ns.annotations, nsdelModeKey)
			} else {
				ns.annotations[nsdelModeKey] = mode
			}
			objs := nsdelRun(t, "", nsdelOn, nil, ns)
			requireNoNamespaceDeleted(t, objs)
			if st := onboardingStatus(t, objs)[gone]; !strings.HasSuffix(st, "it would be deleted, but its last recorded mode is inform, so it is only reported") {
				t.Errorf("status[%s] = %q, want the inform report", gone, st)
			}
		})
	}
}

// Under dry run the enforce ConfigurationPolicy is inform, and the hub reads the dry run from the
// Policy annotation, so no ConfigurationPolicy carries a mustnothave: the deletion is only reported.
func TestProjectOnboarding_NamespaceDeleteDryRun(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-dry", 6, 1)[0]
	objs := nsdelRun(t, "inform", nsdelOn, nil, nsdelCreated(gone, gone))
	for _, o := range objs {
		if o.remediationAction == "enforce" {
			t.Fatalf("dry run: %s has remediationAction enforce", o.configPolicy)
		}
	}
	requireNoNamespaceDeleted(t, objs)
	if st := nsdelDryRunStatus(objs)[gone]; !strings.HasSuffix(st, "it would be deleted, but the deployment runs as a dry run, so it is only reported") {
		t.Errorf("status[%s] = %q, want the dry-run report", gone, st)
	}
}

// nsdelDryRunStatus merges the status ConfigMap data every enforce ConfigurationPolicy would write;
// under dry run they are inform, so onboardingStatus (enforce writes only) does not see them.
func nsdelDryRunStatus(objs []onboardingObject) map[string]string {
	out := map[string]string{}
	for _, o := range objs {
		if !isStatus(o) || o.complianceType != "mustonlyhave" || !strings.HasSuffix(o.configPolicy, "-enforce") {
			continue
		}
		data, _ := o.obj["data"].(map[string]interface{})
		for k, v := range data {
			s, _ := v.(string)
			if prev, ok := out[k]; ok {
				s = prev + "; " + s
			}
			out[k] = s
		}
	}
	return out
}

// requireNoInformNamespaceDelete fails when any object of an inform ConfigurationPolicy is a
// mustnothave of a Namespace, or (dryRun) when any ConfigurationPolicy carries one. A root Policy set
// to enforce in the console overrides both children, so the inform one must never hold anything that
// deletes a Namespace, and a dry run must hold nothing that would.
func requireNoInformNamespaceDelete(t *testing.T, objs []onboardingObject, dryRun bool) {
	t.Helper()
	inform := 0
	for _, o := range objs {
		if strings.HasSuffix(o.configPolicy, onboardingTestSuffix) {
			inform++
		}
		if o.complianceType != "mustnothave" || o.obj["kind"] != "Namespace" {
			continue
		}
		if dryRun || strings.HasSuffix(o.configPolicy, onboardingTestSuffix) {
			meta, _ := o.obj["metadata"].(map[string]interface{})
			t.Errorf("%s (%s) carries a mustnothave of Namespace %v", o.configPolicy, o.remediationAction, meta["name"])
		}
	}
	if inform == 0 {
		t.Fatal("no object of an inform ConfigurationPolicy resolved")
	}
}

// The inform ConfigurationPolicy only reports a deletion, in every case that deletes or would
// delete, and a dry run carries no Namespace mustnothave at all. The dry run is read from the Policy
// annotation autoshift.io/remediation: anything but exactly enforce, including a missing annotation,
// is a dry run, also when the root Policy is set to enforce by hand.
func TestProjectOnboarding_NamespaceDeleteInformNeverDeletes(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-twin", 4, 1)[0]
	informNs := nsdelCreated(gone+"-i", gone+"-i")
	informNs.annotations[nsdelModeKey] = "inform"
	nss := []nsdelNs{nsdelCreated(gone, gone), informNs}
	rootEnforce := func(raw string) string {
		return strings.ReplaceAll(raw, "\nspec:\n  disabled: false\n", "\nspec:\n  disabled: false\n  remediationAction: enforce\n")
	}
	cases := []struct {
		name        string
		remediation string
		edit        func(string) string
		dryRun      bool
	}{
		{"enforce", "", nil, false},
		{"dry run", "inform", nil, true},
		{"dry run, root Policy set to enforce", "inform", rootEnforce, true},
		{"annotation missing", "", func(raw string) string {
			return strings.ReplaceAll(raw, "    autoshift.io/remediation: enforce\n", "")
		}, true},
		{"annotation not exactly enforce", "", func(raw string) string {
			return strings.ReplaceAll(raw, "autoshift.io/remediation: enforce\n", "autoshift.io/remediation: Enforce\n")
		}, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.edit != nil {
				rem := c.remediation
				if rem == "" {
					rem = "enforce"
				}
				if raw := stressRaw(t, rem); c.edit(raw) == raw {
					t.Fatal("the edit changed nothing in the rendered Policies")
				}
			}
			r := stressRun(t, stressOpts{allShards: true, remediation: c.remediation, labels: nsdelOn,
				extra: nsdelStubs(nss...), mutate: nsdelProjects(nil), rawEdit: c.edit})
			requireNoInformNamespaceDelete(t, r.objs, c.dryRun)
			if !c.dryRun {
				requireDeleted(t, r.objs, gone)
			}
		})
	}
}

// A Namespace that a current project declares is never deleted, whichever shard declares it: the
// project was renamed (same shard or another), or another project names the namespace.
func TestProjectOnboarding_NamespaceDeleteStillDeclared(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-old", 3, 1)[0]
	for _, shard := range []int{3, 5} {
		renamed := shardNames("nsdel-new", shard, 1)[0]
		for _, mode := range []string{"rename", "other project"} {
			mode := mode
			t.Run(fmt.Sprintf("%s in shard %d", mode, shard), func(t *testing.T) {
				t.Parallel()
				project := map[string]interface{}{"groups": map[string]interface{}{"admin": "NEW_ADMINS"}, "namespace": gone}
				if mode == "other project" {
					project["adopt"] = true
				}
				objs := nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{renamed: project}, nsdelCreated(gone, gone))
				requireNoNamespaceDeleted(t, objs)
			})
		}
	}
	// The project keeps its name and moves to another namespace: not removed, so not deleted (the
	// old namespace is released instead).
	t.Run("moved namespace", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{gone: {"groups": map[string]interface{}{"admin": "A"}, "namespace": gone + "-v2"}},
			nsdelCreated(gone, gone), nsdelCreated(gone+"-v2", gone))
		requireNoNamespaceDeleted(t, objs)
	})
}

// A platform-named namespace is never deleted, whatever it carries.
func TestProjectOnboarding_NamespaceDeletePlatform(t *testing.T) {
	t.Parallel()
	var nss []nsdelNs
	for _, name := range []string{"openshift-nsdel", "kube-nsdel", "default", "openshift", "open-cluster-management-nsdel", "multicluster-engine", "hive"} {
		nss = append(nss, nsdelCreated(name, "nsdel-platform"))
	}
	requireNoNamespaceDeleted(t, nsdelRun(t, "", nsdelOn, nil, nss...))
}

// A forged or inconsistent marker never leads to a deletion: every check of the creation record and
// the ownership labels must hold.
func TestProjectOnboarding_NamespaceDeleteForgedMarker(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-forged", 1, 1)[0]
	mutations := map[string]func(ns *nsdelNs){
		"marker missing":            func(ns *nsdelNs) { delete(ns.labels, nsdelMarker) },
		"marker not true":           func(ns *nsdelNs) { ns.labels[nsdelMarker] = "yes" },
		"created-by missing":        func(ns *nsdelNs) { delete(ns.annotations, nsdelCreatedBy) },
		"created-by another":        func(ns *nsdelNs) { ns.annotations[nsdelCreatedBy] = "someone-else" },
		"managed-by missing":        func(ns *nsdelNs) { delete(ns.labels, "autoshift.io/managed-by") },
		"managed-by another":        func(ns *nsdelNs) { ns.labels["autoshift.io/managed-by"] = "other-tool" },
		"project label empty":       func(ns *nsdelNs) { ns.labels["autoshift.io/project"] = "" },
		"marker as annotation only": func(ns *nsdelNs) { delete(ns.labels, nsdelMarker); ns.annotations[nsdelMarker] = "true" },
	}
	var nss []nsdelNs
	i := 0
	for _, mutate := range mutations {
		ns := nsdelCreated(fmt.Sprintf("%s-%d", gone, i), gone)
		mutate(&ns)
		nss = append(nss, ns)
		i++
	}
	objs := nsdelRun(t, "", nsdelOn, nil, nss...)
	requireNoNamespaceDeleted(t, objs)

	// A declared, non-adopt project in a namespace onboarding did not create (it existed and is owned,
	// as before this feature): no creation record is added, and a forged one is removed.
	owner := shardNames("nsdel-owner", 0, 1)[0]
	plain := nsdelNs{name: owner, labels: map[string]string{"autoshift.io/project": owner, "autoshift.io/managed-by": "project-onboarding"}}
	forged := nsdelCreated(owner+"-f", owner)
	forged.annotations[nsdelCreatedBy] = "someone-else"
	other := shardNames("nsdel-owner", 0, 2)[1]
	projects := map[string]map[string]interface{}{
		owner: {"groups": map[string]interface{}{"admin": "O"}, "deleteOnRemoval": true},
		other: {"groups": map[string]interface{}{"admin": "O"}, "deleteOnRemoval": true, "namespace": owner + "-f"},
	}
	forged.labels["autoshift.io/project"] = other
	objs = nsdelRun(t, "", nsdelOn, projects, plain, forged)
	requireNoCreationRecord(t, objs, owner)
	requireNoCreationRecord(t, objs, owner+"-f")
}

// An adopted namespace is never deleted. While its adopt project is declared, a forged creation
// record is removed, so the namespace cannot be deleted after the project is removed either.
func TestProjectOnboarding_NamespaceDeleteAdoptedNeverDeleted(t *testing.T) {
	t.Parallel()
	adopted := shardNames("nsdel-adopted", 5, 1)[0]
	forged := nsdelCreated(adopted, adopted)
	for _, mode := range []string{"enforce", "inform"} {
		mode := mode
		t.Run("declared, mode "+mode, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{
				adopted: {"groups": map[string]interface{}{"admin": "A"}, "adopt": true, "mode": mode, "deleteOnRemoval": true},
			}, forged)
			requireNoNamespaceDeleted(t, objs)
			requireNoCreationRecord(t, objs, adopted)
			if st := onboardingStatus(t, objs)[adopted]; !strings.Contains(st, "deleteOnRemoval is ignored with adopt: true") {
				t.Errorf("status[%s] = %q, want the deleteOnRemoval note", adopted, st)
			}
		})
	}
	// After the enforce run removed the record, the project is removed: nothing is deleted.
	t.Run("removed after the record was stripped", func(t *testing.T) {
		t.Parallel()
		stripped := nsdelNs{name: adopted, labels: map[string]string{"autoshift.io/project": adopted, "autoshift.io/managed-by": "project-onboarding"}}
		objs := nsdelRun(t, "", nsdelOn, nil, stripped)
		requireNoNamespaceDeleted(t, objs)
		if v, ok := onboardingStatus(t, objs)[adopted]; ok {
			t.Errorf("status[%s] = %q, want no entry", adopted, v)
		}
	})
	// An adopt project whose namespace does not exist yet: onboarding creates it, without a record.
	t.Run("adopt creates without a record", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{
			adopted: {"groups": map[string]interface{}{"admin": "A"}, "adopt": true, "mode": "enforce"},
		})
		requireNoCreationRecord(t, objs, adopted)
	})
}

// Onboarding stamps the creation record only on a Namespace it creates, and keeps deleteOnRemoval
// and the mode current on it while the project is declared.
func TestProjectOnboarding_NamespaceCreationRecord(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-new", 7, 1)[0]
	record := func(t *testing.T, objs []onboardingObject) (map[string]string, map[string]string) {
		t.Helper()
		entries := renderedNamespaces(objs, name)
		if len(entries) != 1 {
			t.Fatalf("want one Namespace %s, got %d", name, len(entries))
		}
		meta, _ := entries[0].obj["metadata"].(map[string]interface{})
		return stringMap(meta, "labels"), stringMap(meta, "annotations")
	}
	t.Run("created", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true}})
		l, a := record(t, objs)
		if l[nsdelMarker] != "true" || a[nsdelCreatedBy] != name || a[nsdelOnRemoval] != "true" || a[nsdelModeKey] != "enforce" {
			t.Errorf("new Namespace labels %v annotations %v, want the creation record with deleteOnRemoval true and mode enforce", l, a)
		}
	})
	t.Run("deleteOnRemoval switched off", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}}}, nsdelCreated(name, name))
		l, a := record(t, objs)
		if l[nsdelMarker] != "true" || a[nsdelCreatedBy] != name || a[nsdelOnRemoval] != "false" {
			t.Errorf("Namespace labels %v annotations %v, want the record kept with deleteOnRemoval false", l, a)
		}
	})
	t.Run("invalid deleteOnRemoval", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": "yes"}})
		if st := onboardingStatus(t, objs)[name]; st != "deleteOnRemoval yes is not true or false, so it counts as false" {
			t.Errorf("status[%s] = %q, want the deleteOnRemoval error", name, st)
		}
	})
	t.Run("reserved keys", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {
			"groups":      map[string]interface{}{"admin": "A"},
			"labels":      map[string]interface{}{"m": kv(nsdelMarker, "true")},
			"annotations": map[string]interface{}{"d": kv(nsdelOnRemoval, "true"), "c": kv(nsdelCreatedBy, "x"), "p": kv(nsdelPending, nsdelLongAgo)},
		}})
		st := onboardingStatus(t, objs)[name]
		for _, want := range []string{"label " + nsdelMarker + " is reserved for project ownership",
			"annotation " + nsdelOnRemoval + " is reserved for project ownership", "annotation " + nsdelCreatedBy + " is reserved for project ownership",
			"annotation " + nsdelPending + " is reserved for project ownership"} {
			if !strings.Contains(st, want) {
				t.Errorf("status[%s] = %q, want %q", name, st, want)
			}
		}
		_, a := record(t, objs)
		if a[nsdelOnRemoval] != "false" || a[nsdelCreatedBy] != name {
			t.Errorf("annotations %v, want the policy's own record, not the catalog's", a)
		}
	})
}

// The enforce ConfigurationPolicy records inform on the Namespace of an inform project that still
// records enforce, so its removal is only reported. It is the one write of the enforce
// ConfigurationPolicy for an inform project: the live labels and annotations with the mode changed.
func TestProjectOnboarding_NamespaceRecordsInformMode(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-switch", 3, 1)[0]
	writes := func(objs []onboardingObject) []onboardingObject {
		var out []onboardingObject
		for _, o := range renderedNamespaces(objs, name) {
			if o.remediationAction == "enforce" {
				out = append(out, o)
			}
		}
		return out
	}
	t.Run("recorded enforce", func(t *testing.T) {
		t.Parallel()
		live := nsdelFresh(name, name)
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "mode": "inform", "deleteOnRemoval": true}}, live)
		w := writes(objs)
		if len(w) != 1 || w[0].configPolicy != onboardingShardPrefix+"3-enforce" || w[0].complianceType != "musthave" || w[0].item["metadataComplianceType"] != "mustonlyhave" {
			t.Fatalf("want one musthave of %s in %s3-enforce with metadataComplianceType mustonlyhave, got %v", name, onboardingShardPrefix, w)
		}
		meta, _ := w[0].obj["metadata"].(map[string]interface{})
		want := map[string]string{}
		for k, v := range live.annotations {
			want[k] = v
		}
		want[nsdelModeKey] = "inform"
		if a := stringMap(meta, "annotations"); fmt.Sprint(a) != fmt.Sprint(want) {
			t.Errorf("annotations %v, want %v", a, want)
		}
		if l := stringMap(meta, "labels"); fmt.Sprint(l) != fmt.Sprint(live.labels) {
			t.Errorf("labels %v, want the live labels %v", l, live.labels)
		}
	})
	t.Run("recorded inform", func(t *testing.T) {
		t.Parallel()
		ns := nsdelFresh(name, name)
		ns.annotations[nsdelModeKey] = "inform"
		objs := nsdelRun(t, "", nil, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "mode": "inform", "deleteOnRemoval": true}}, ns)
		if w := writes(objs); len(w) != 0 {
			t.Errorf("want no write, got %d", len(w))
		}
	})
}

// nsdelUnset leaves projectDefaults.maxNamespaceDeletes out of the config.
var nsdelUnset = struct{}{}

// nsdelRunMax is nsdelRun with projectDefaults.maxNamespaceDeletes set to max (nsdelUnset: absent).
func nsdelRunMax(t *testing.T, max interface{}, projects map[string]map[string]interface{}, nss ...nsdelNs) []onboardingObject {
	t.Helper()
	r := stressRun(t, stressOpts{allShards: true, labels: nsdelOn, extra: nsdelStubs(nss...), mutate: func(cfg map[string]interface{}) {
		nsdelProjects(projects)(cfg)
		if max != nsdelUnset {
			cfg["projectDefaults"].(map[string]interface{})["maxNamespaceDeletes"] = max
		}
	}})
	return r.objs
}

// requireDeleted fails unless exactly the namespaces in want get one enforce mustnothave each.
func requireDeleted(t *testing.T, objs []onboardingObject, want ...string) {
	t.Helper()
	d := nsDeletions(objs)
	if len(d) != len(want) {
		t.Errorf("deleted %v, want exactly %v", d, want)
	}
	for _, ns := range want {
		if len(d[ns]) != 1 || d[ns][0].remediation != "enforce" || shardOfPolicy(d[ns][0].policy) != onboardingShardOf(ns) {
			t.Errorf("%s: deletions %v, want one enforce mustnothave from shard %d", ns, d[ns], onboardingShardOf(ns))
		}
	}
}

func TestProjectOnboarding_NamespaceDeleteBreaker(t *testing.T) {
	t.Parallel()
	a, b := shardNames("nsdel-cbx", 1, 1)[0], shardNames("nsdel-cby", 6, 1)[0]
	same := shardNames("nsdel-cbs", 2, 2)
	blocked := func(k, n int) string {
		return fmt.Sprintf("it is not deleted: blocked by maxNamespaceDeletes (%d candidates > %d)", k, n)
	}
	t.Run("unset deletes every candidate", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunMax(t, nsdelUnset, nil, nsdelCreated(a, a), nsdelCreated(b, b)), a, b)
	})
	t.Run("0 reports only", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRunMax(t, 0, nil, nsdelCreated(a, a))
		requireNoNamespaceDeleted(t, objs)
		if st := onboardingStatus(t, objs)[a]; !strings.HasSuffix(st, "it is not deleted: maxNamespaceDeletes is 0") {
			t.Errorf("status[%s] = %q", a, st)
		}
	})
	t.Run("1 with one candidate deletes it", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunMax(t, 1, nil, nsdelCreated(a, a)), a)
	})
	t.Run("2 with two candidates deletes both", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunMax(t, 2, nil, nsdelCreated(a, a), nsdelCreated(b, b)), a, b)
	})
	t.Run("the largest value the pattern allows is a number", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunMax(t, 999999999, nil, nsdelCreated(a, a), nsdelCreated(b, b)), a, b)
	})
	// On a cluster the rendered config passes through toYaml, which writes 1000000 as 1e+06, and the
	// hub reads that back as a float64. The suite's rendered config is JSON, so the literal is
	// injected as a json.Number to take the same path.
	t.Run("an integral float such as 1e+06 is a number", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunMax(t, json.Number("1e+06"), nil, nsdelCreated(a, a), nsdelCreated(b, b)), a, b)
	})
	t.Run("an integral float 1.0e+00 is the number 1", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRunMax(t, json.Number("1.0e+00"), nil, nsdelCreated(a, a), nsdelCreated(b, b))
		requireNoNamespaceDeleted(t, objs)
		if st := onboardingStatus(t, objs)[a]; !strings.HasSuffix(st, blocked(2, 1)) {
			t.Errorf("status[%s] = %q, want %q", a, st, blocked(2, 1))
		}
	})
	t.Run("1 with two candidates in one shard deletes none", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRunMax(t, 1, nil, nsdelCreated(same[0], same[0]), nsdelCreated(same[1], same[1]))
		requireNoNamespaceDeleted(t, objs)
		st := onboardingStatus(t, objs)
		for _, ns := range same {
			if !strings.HasSuffix(st[ns], blocked(2, 1)) {
				t.Errorf("status[%s] = %q, want %q", ns, st[ns], blocked(2, 1))
			}
		}
	})
	t.Run("1 with two candidates across shards deletes none, both shards report", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRunMax(t, 1, nil, nsdelCreated(a, a), nsdelCreated(b, b))
		requireNoNamespaceDeleted(t, objs)
		per := onboardingShardStatus(t, objs)
		for _, ns := range []string{a, b} {
			if got := per[onboardingShardOf(ns)][ns]; !strings.HasSuffix(got, blocked(2, 1)) {
				t.Errorf("shard %d status[%s] = %q, want %q", onboardingShardOf(ns), ns, got, blocked(2, 1))
			}
		}
	})
	// A namespace whose project is declared in another shard (moved to a new namespace) is not a
	// candidate, so it does not count: every shard sees every declared name.
	t.Run("a project declared in another shard does not count", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRunMax(t, 1, map[string]map[string]interface{}{b: {"groups": map[string]interface{}{"admin": "B"}, "namespace": b + "-v2"}},
			nsdelCreated(a, a), nsdelCreated(b, b), nsdelCreated(b+"-v2", b))
		requireDeleted(t, objs, a)
	})
	t.Run("PVC and inform candidates do not count", func(t *testing.T) {
		t.Parallel()
		withPVC := nsdelCreated(same[0], same[0])
		withPVC.pvcs = 1
		inform := nsdelCreated(same[1], same[1])
		inform.annotations[nsdelModeKey] = "inform"
		objs := nsdelRunMax(t, 1, nil, nsdelCreated(a, a), withPVC, inform)
		requireDeleted(t, objs, a)
	})
	// An inform candidate is not counted, so a tripped breaker does not claim to block it.
	t.Run("a tripped breaker leaves inform candidates only reported", func(t *testing.T) {
		t.Parallel()
		inform := nsdelCreated(same[0], same[0])
		inform.annotations[nsdelModeKey] = "inform"
		objs := nsdelRunMax(t, 1, nil, nsdelCreated(a, a), nsdelCreated(b, b), inform)
		requireNoNamespaceDeleted(t, objs)
		st := onboardingStatus(t, objs)
		if !strings.HasSuffix(st[same[0]], "it would be deleted, but its last recorded mode is inform, so it is only reported") {
			t.Errorf("status[%s] = %q", same[0], st[same[0]])
		}
		for _, ns := range []string{a, b} {
			if !strings.HasSuffix(st[ns], blocked(2, 1)) {
				t.Errorf("status[%s] = %q, want %q", ns, st[ns], blocked(2, 1))
			}
		}
	})
	for _, bad := range []interface{}{-1, "1", 1.5, map[string]interface{}{"n": 1}, true, json.Number("-1e+06"), json.Number("1e+10"), json.Number("1.5e+00"), "1e+06"} {
		bad := bad
		t.Run(fmt.Sprintf("invalid %v", bad), func(t *testing.T) {
			t.Parallel()
			objs := nsdelRunMax(t, bad, nil, nsdelCreated(a, a))
			requireNoNamespaceDeleted(t, objs)
			per := onboardingShardStatus(t, objs)
			if got := per[0]["projectDefaults.maxNamespaceDeletes"]; got != "maxNamespaceDeletes is not a non-negative integer, so it counts as 0 and no namespace is deleted" {
				t.Errorf("shard 0 status projectDefaults.maxNamespaceDeletes = %q", got)
			}
			for shard := 1; shard < onboardingShards; shard++ {
				if _, ok := per[shard]["projectDefaults.maxNamespaceDeletes"]; ok {
					t.Errorf("shard %d also reports projectDefaults.maxNamespaceDeletes", shard)
				}
			}
			if st := onboardingStatus(t, objs)[a]; !strings.HasSuffix(st, "maxNamespaceDeletes is invalid, which counts as 0") {
				t.Errorf("status[%s] = %q", a, st)
			}
		})
	}
}

// nsdelRunCfg is nsdelRun with a further change to the config after the projects are set.
func nsdelRunCfg(t *testing.T, remediation string, labels map[string]string, projects map[string]map[string]interface{}, mutate func(cfg map[string]interface{}), nss ...nsdelNs) []onboardingObject {
	t.Helper()
	r := stressRun(t, stressOpts{allShards: true, remediation: remediation, labels: labels, extra: nsdelStubs(nss...), mutate: func(cfg map[string]interface{}) {
		nsdelProjects(projects)(cfg)
		if mutate != nil {
			mutate(cfg)
		}
	}})
	return r.objs
}

// nsdelGrace sets projectDefaults.namespaceDeleteGracePeriod.
func nsdelGrace(v interface{}) func(cfg map[string]interface{}) {
	return func(cfg map[string]interface{}) {
		cfg["projectDefaults"].(map[string]interface{})["namespaceDeleteGracePeriod"] = v
	}
}

// nsdelMark is a rendered Namespace that sets the pending-deletion annotation.
type nsdelMark struct {
	configPolicy, remediation, value string
}

// nsdelMarks maps each Namespace that some ConfigurationPolicy marks pending to its entries. Every
// other rendered Namespace of the policy (its own, a release, a record update) leaves the mark out.
func nsdelMarks(objs []onboardingObject) map[string][]nsdelMark {
	out := map[string][]nsdelMark{}
	for _, o := range objs {
		if o.obj["kind"] != "Namespace" || o.complianceType == "mustnothave" {
			continue
		}
		meta, _ := o.obj["metadata"].(map[string]interface{})
		if v, ok := stringMap(meta, "annotations")[nsdelPending]; ok {
			name, _ := meta["name"].(string)
			out[name] = append(out[name], nsdelMark{o.configPolicy, o.remediationAction, v})
		}
	}
	return out
}

func nsdelTs(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// nsdelPendingSince is nsdelFresh marked pending at since.
func nsdelPendingSince(name, project, since string) nsdelNs {
	ns := nsdelFresh(name, project)
	ns.annotations[nsdelPending] = since
	return ns
}

// requireMarkedNow fails unless exactly ns is marked, once, by the enforce ConfigurationPolicy of its
// shard, with a timestamp of this run.
func requireMarkedNow(t *testing.T, objs []onboardingObject, ns string, before, after time.Time) {
	t.Helper()
	m := nsdelMarks(objs)
	if len(m) != 1 || len(m[ns]) != 1 {
		t.Fatalf("marks %v, want only %s marked once", m, ns)
	}
	want := fmt.Sprintf("%s%d-enforce", onboardingShardPrefix, onboardingShardOf(ns))
	if m[ns][0].configPolicy != want || m[ns][0].remediation != "enforce" {
		t.Errorf("%s marked by %+v, want %s in enforce", ns, m[ns][0], want)
	}
	ts, err := time.Parse(time.RFC3339, m[ns][0].value)
	if err != nil || ts.Before(before.Truncate(time.Second)) || ts.After(after) {
		t.Errorf("%s marked %q, want a timestamp between %s and %s", ns, m[ns][0].value, nsdelTs(before), nsdelTs(after))
	}
}

func TestProjectOnboarding_NamespaceDeleteTwoPhase(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-phase", 3, 1)[0]
	t.Run("the first evaluation marks the namespace and deletes nothing", func(t *testing.T) {
		t.Parallel()
		before := time.Now()
		objs := nsdelRun(t, "", nsdelOn, nil, nsdelFresh(gone, gone))
		after := time.Now()
		requireNoNamespaceDeleted(t, objs)
		requireMarkedNow(t, objs, gone, before, after)
		st := onboardingStatus(t, objs)[gone]
		const marker = "it is marked for deletion now, and can be deleted from "
		i := strings.Index(st, marker)
		if i < 0 {
			t.Fatalf("status[%s] = %q, want %q", gone, st, marker)
		}
		from, err := time.Parse(time.RFC3339, st[i+len(marker):])
		if err != nil || from.Before(before.Add(24*time.Hour).Truncate(time.Second)) || from.After(after.Add(24*time.Hour)) {
			t.Errorf("status[%s] = %q, want a deletable time 24h from now", gone, st)
		}
	})
	t.Run("the grace period is not over: nothing is deleted or marked again", func(t *testing.T) {
		t.Parallel()
		since := time.Now().Add(-23 * time.Hour)
		objs := nsdelRun(t, "", nsdelOn, nil, nsdelPendingSince(gone, gone, nsdelTs(since)))
		requireNoNamespaceDeleted(t, objs)
		if m := nsdelMarks(objs); len(m) != 0 {
			t.Errorf("marks %v, want none: the namespace is already marked", m)
		}
		want := fmt.Sprintf("it is pending deletion since %s, and can be deleted from %s", nsdelTs(since), nsdelTs(since.Add(24*time.Hour)))
		if st := onboardingStatus(t, objs)[gone]; !strings.HasSuffix(st, want) {
			t.Errorf("status[%s] = %q, want %q", gone, st, want)
		}
	})
	t.Run("the grace period is over: deleted", func(t *testing.T) {
		t.Parallel()
		since := time.Now().Add(-24*time.Hour - time.Minute)
		objs := nsdelRun(t, "", nsdelOn, nil, nsdelPendingSince(gone, gone, nsdelTs(since)))
		requireDeleted(t, objs, gone)
		want := fmt.Sprintf("it has been pending deletion since %s; it is deleted with everything in it", nsdelTs(since))
		if st := onboardingStatus(t, objs)[gone]; !strings.HasSuffix(st, want) {
			t.Errorf("status[%s] = %q, want %q", gone, st, want)
		}
	})
	for _, c := range []struct {
		grace  string
		ago    time.Duration
		delete bool
	}{
		{"1h30m", 2 * time.Hour, true},
		{"1h30m", time.Hour, false},
		{"90m", 89 * time.Minute, false},
		{"45s", time.Minute, false}, // below the 1h floor: 24h is used
		{"45s", 25 * time.Hour, true},
		{"2h0m0s", 3 * time.Hour, true},
		{"48h", 25 * time.Hour, false},
	} {
		c := c
		t.Run(fmt.Sprintf("grace %s, pending %s", c.grace, c.ago), func(t *testing.T) {
			t.Parallel()
			objs := nsdelRunCfg(t, "", nsdelOn, nil, nsdelGrace(c.grace), nsdelPendingSince(gone, gone, nsdelTs(time.Now().Add(-c.ago))))
			if c.delete {
				requireDeleted(t, objs, gone)
			} else {
				requireNoNamespaceDeleted(t, objs)
			}
		})
	}
	// A pending value the policy did not write, or cannot read, counts as no mark: the namespace is
	// marked again now, never treated as long expired. An impossible date parses to the zero time,
	// which would otherwise read as expired since year 1.
	for _, bad := range []string{"garbage", "2026-13-01T00:00:00Z", "2026-02-30T00:00:00Z", "2999-01-01T00:00:00Z", "", "0001-01-01T00:00:00Z", "1763467200"} {
		bad := bad
		t.Run("pending value "+bad+" is marked again", func(t *testing.T) {
			t.Parallel()
			before := time.Now()
			objs := nsdelRun(t, "", nsdelOn, nil, nsdelPendingSince(gone, gone, bad))
			requireNoNamespaceDeleted(t, objs)
			requireMarkedNow(t, objs, gone, before, time.Now())
		})
	}
	t.Run("dry run marks nothing", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "inform", nsdelOn, nil, nsdelFresh(gone, gone))
		requireNoNamespaceDeleted(t, objs)
		if m := nsdelMarks(objs); len(m) != 0 {
			t.Errorf("dry run: marks %v, want none in any ConfigurationPolicy", m)
		}
		if st := nsdelDryRunStatus(objs)[gone]; !strings.HasSuffix(st, "it would be marked for deletion, but the deployment runs as a dry run, so it is only reported") {
			t.Errorf("status[%s] = %q", gone, st)
		}
	})
	t.Run("an inform recorded mode is not marked", func(t *testing.T) {
		t.Parallel()
		ns := nsdelFresh(gone, gone)
		ns.annotations[nsdelModeKey] = "inform"
		objs := nsdelRun(t, "", nsdelOn, nil, ns)
		if m := nsdelMarks(objs); len(m) != 0 {
			t.Errorf("marks %v, want none", m)
		}
	})
}

// requireMarkRemoved fails unless the enforce ConfigurationPolicy of ns's shard writes ns without the
// pending mark, exactly, keeping its other labels and annotations.
func requireMarkRemoved(t *testing.T, objs []onboardingObject, ns nsdelNs) {
	t.Helper()
	requireNoNamespaceDeleted(t, objs)
	if m := nsdelMarks(objs); len(m) != 0 {
		t.Errorf("marks %v, want none", m)
	}
	var writes []onboardingObject
	for _, o := range renderedNamespaces(objs, ns.name) {
		if o.remediationAction == "enforce" {
			writes = append(writes, o)
		}
	}
	if len(writes) != 1 {
		t.Fatalf("want one enforce write of %s, got %d", ns.name, len(writes))
	}
	if mt, _ := writes[0].item["metadataComplianceType"].(string); mt != "mustonlyhave" {
		t.Errorf("%s written with metadataComplianceType %q, want mustonlyhave so the mark is removed", ns.name, mt)
	}
	meta, _ := writes[0].obj["metadata"].(map[string]interface{})
	ann := stringMap(meta, "annotations")
	if _, ok := ann[nsdelPending]; ok {
		t.Errorf("%s keeps %s", ns.name, nsdelPending)
	}
	if l := stringMap(meta, "labels"); l["autoshift.io/project"] != ns.labels["autoshift.io/project"] {
		t.Errorf("%s labels %v, want the live labels kept", ns.name, l)
	}
}

// Declaring the project again removes the mark, whatever the project renders: enforce (its own
// Namespace object), inform, or invalid (a record write of the enforce ConfigurationPolicy alone).
func TestProjectOnboarding_NamespaceDeleteRedeclareCancels(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-back", 5, 1)[0]
	for label, project := range map[string]map[string]interface{}{
		"enforce":        {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true},
		"inform":         {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true, "mode": "inform"},
		"invalid":        {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true, "egressIP": []interface{}{"not-an-ip"}},
		"no admin group": {"groups": map[string]interface{}{"admin": ""}, "deleteOnRemoval": true},
	} {
		project := project
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			ns := nsdelCreated(name, name)
			requireMarkRemoved(t, nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{name: project}, ns), ns)
		})
	}
}

// An empty or missing projects map never deletes and never marks: it is as likely a broken config
// as a deliberate removal of every project. Each candidate is reported.
func TestProjectOnboarding_NamespaceDeleteEmptyProjects(t *testing.T) {
	t.Parallel()
	expired := shardNames("nsdel-empty", 1, 1)[0]
	fresh := shardNames("nsdel-empty", 6, 1)[0]
	for label, mutate := range map[string]func(cfg map[string]interface{}){
		"empty map":   func(cfg map[string]interface{}) { cfg["projects"] = map[string]interface{}{} },
		"missing key": func(cfg map[string]interface{}) { delete(cfg, "projects") },
		"null":        func(cfg map[string]interface{}) { cfg["projects"] = nil },
	} {
		mutate := mutate
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRunCfg(t, "", nsdelOn, nil, mutate, nsdelCreated(expired, expired), nsdelFresh(fresh, fresh))
			requireNoNamespaceDeleted(t, objs)
			if m := nsdelMarks(objs); len(m) != 0 {
				t.Errorf("marks %v, want none", m)
			}
			st := onboardingStatus(t, objs)
			for _, ns := range []string{expired, fresh} {
				if !strings.Contains(st[ns], "it is not deleted: config.projects is empty or missing, which never deletes a namespace") {
					t.Errorf("status[%s] = %q, want the empty-projects report", ns, st[ns])
				}
			}
		})
	}
}

// A partial map, for example a clusterset ConfigMap missing during a sync while a cluster file still
// declares a project: the projects that disappear are only marked, and the grace period protects
// them until the config is whole again.
func TestProjectOnboarding_NamespaceDeletePartialProjects(t *testing.T) {
	t.Parallel()
	names := []string{shardNames("nsdel-part", 0, 1)[0], shardNames("nsdel-part", 2, 1)[0], shardNames("nsdel-part", 7, 1)[0]}
	var nss []nsdelNs
	for _, n := range names {
		nss = append(nss, nsdelFresh(n, n))
	}
	objs := nsdelRun(t, "", nsdelOn, nil, nss...)
	requireNoNamespaceDeleted(t, objs)
	m := nsdelMarks(objs)
	for _, n := range names {
		if len(m[n]) != 1 || m[n][0].remediation != "enforce" {
			t.Errorf("%s: marks %v, want one enforce mark", n, m[n])
		}
	}
	if len(m) != len(names) {
		t.Errorf("marks %v, want only %v", m, names)
	}
}

// namespaceDeleteGracePeriod must be a positive duration of hours, minutes and seconds; anything else
// is reported in shard 0 and nothing is marked or deleted.
func TestProjectOnboarding_NamespaceDeleteGraceInvalid(t *testing.T) {
	t.Parallel()
	expired := shardNames("nsdel-grace", 4, 1)[0]
	fresh := shardNames("nsdel-grace", 5, 1)[0]
	for _, bad := range []interface{}{"1d", "24", 86400, "", "-1h", "1.5h", "24h ", " 24h", "1h1h", "1m1h", "500ms", true, map[string]interface{}{"h": 24}, "123456h"} {
		bad := bad
		t.Run(fmt.Sprintf("%v", bad), func(t *testing.T) {
			t.Parallel()
			objs := nsdelRunCfg(t, "", nsdelOn, nil, nsdelGrace(bad), nsdelCreated(expired, expired), nsdelFresh(fresh, fresh))
			requireNoNamespaceDeleted(t, objs)
			if m := nsdelMarks(objs); len(m) != 0 {
				t.Errorf("marks %v, want none", m)
			}
			per := onboardingShardStatus(t, objs)
			if got := per[0]["projectDefaults.namespaceDeleteGracePeriod"]; !strings.HasPrefix(got, "namespaceDeleteGracePeriod is not a positive duration") {
				t.Errorf("shard 0 status projectDefaults.namespaceDeleteGracePeriod = %q", got)
			}
			st := onboardingStatus(t, objs)
			for _, ns := range []string{expired, fresh} {
				if !strings.Contains(st[ns], "it is not deleted: namespaceDeleteGracePeriod is invalid, which never deletes a namespace") {
					t.Errorf("status[%s] = %q", ns, st[ns])
				}
			}
		})
	}
}

// The breaker counts the namespaces whose grace period is over and would be deleted now. Marking is
// not counted: it deletes nothing and is undone by declaring the project again.
func TestProjectOnboarding_NamespaceDeleteBreakerCountsExpired(t *testing.T) {
	t.Parallel()
	exp := shardNames("nsdel-cbe", 1, 1)[0]
	fresh := []string{shardNames("nsdel-cbf", 2, 1)[0], shardNames("nsdel-cbf", 5, 1)[0]}
	objs := nsdelRunMax(t, 1, nil, nsdelCreated(exp, exp), nsdelFresh(fresh[0], fresh[0]), nsdelFresh(fresh[1], fresh[1]))
	requireDeleted(t, objs, exp)
	m := nsdelMarks(objs)
	if len(m) != 2 || len(m[fresh[0]]) != 1 || len(m[fresh[1]]) != 1 {
		t.Errorf("marks %v, want %v marked", m, fresh)
	}
}

// nsdelEnforceWrite returns the one Namespace name that the enforce ConfigurationPolicies render, or
// fails.
func nsdelEnforceWrite(t *testing.T, objs []onboardingObject, name string) (map[string]string, map[string]string) {
	t.Helper()
	var writes []onboardingObject
	for _, o := range renderedNamespaces(objs, name) {
		if o.remediationAction == "enforce" {
			writes = append(writes, o)
		}
	}
	if len(writes) != 1 {
		t.Fatalf("want one enforce write of %s, got %d", name, len(writes))
	}
	meta, _ := writes[0].obj["metadata"].(map[string]interface{})
	return stringMap(meta, "labels"), stringMap(meta, "annotations")
}

// The creation record is bound to the namespace instance: created at its creationTimestamp, then
// stamped with its uid on a later evaluation, and only when the live namespace was created within
// 30 seconds before to 10 minutes after the recorded time.
func TestProjectOnboarding_NamespaceIdentityBinding(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-uid", 6, 1)[0]
	declared := map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true}}
	t.Run("a new namespace records its creation time", func(t *testing.T) {
		t.Parallel()
		before := time.Now()
		objs := nsdelRun(t, "", nil, declared)
		_, a := nsdelEnforceWrite(t, objs, name)
		ts, err := time.Parse(time.RFC3339, a[nsdelCreatedAt])
		if err != nil || ts.Before(before.Truncate(time.Second)) || ts.After(time.Now()) {
			t.Errorf("%s = %q, want this run's time", nsdelCreatedAt, a[nsdelCreatedAt])
		}
		if _, ok := a[nsdelUID]; ok {
			t.Errorf("a namespace that does not exist yet has no uid to stamp, got %s=%q", nsdelUID, a[nsdelUID])
		}
	})
	born, _ := time.Parse(time.RFC3339, nsdelBorn)
	for _, c := range []struct {
		label     string
		createdAt string // "" leaves the annotation out
		uid       string
		stamp     bool
	}{
		{"created at the recorded time", nsdelBorn, "u-1", true},
		{"created 9 minutes after the recorded time", nsdelTs(born.Add(-9 * time.Minute)), "u-1", true},
		{"created 11 minutes after the recorded time", nsdelTs(born.Add(-11 * time.Minute)), "u-1", false},
		{"created 29 seconds before the recorded time", nsdelTs(born.Add(29 * time.Second)), "u-1", true},
		{"created 31 seconds before the recorded time", nsdelTs(born.Add(31 * time.Second)), "u-1", false},
		{"no recorded time", "", "u-1", false},
		{"a recorded time that does not exist", "2025-02-30T00:00:00Z", "u-1", false},
		{"a recorded time that is not a timestamp", "yesterday", "u-1", false},
		{"no uid", nsdelBorn, "", false},
	} {
		c := c
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			live := nsdelFresh(name, name)
			live.uid = c.uid
			delete(live.annotations, nsdelUID)
			delete(live.annotations, nsdelCreatedAt)
			if c.createdAt != "" {
				live.annotations[nsdelCreatedAt] = c.createdAt
			}
			_, a := nsdelEnforceWrite(t, nsdelRun(t, "", nil, declared, live), name)
			got, ok := a[nsdelUID]
			if c.stamp && got != c.uid {
				t.Errorf("%s = %q, want %q", nsdelUID, got, c.uid)
			}
			if !c.stamp && ok {
				t.Errorf("%s = %q, want no stamp", nsdelUID, got)
			}
		})
	}
	// Both times impossible: toDate gives the zero time for each, so they are 0 seconds apart and
	// only the check that both are positive keeps the namespace unstamped.
	t.Run("both times impossible", func(t *testing.T) {
		t.Parallel()
		live := nsdelFresh(name, name)
		delete(live.annotations, nsdelUID)
		live.annotations[nsdelCreatedAt] = "2025-02-30T00:00:00Z"
		live.created = "2025-02-31T00:00:00Z"
		_, a := nsdelEnforceWrite(t, nsdelRun(t, "", nil, declared, live), name)
		if v, ok := a[nsdelUID]; ok {
			t.Errorf("%s = %q, want no stamp", nsdelUID, v)
		}
	})
	t.Run("an existing stamp is never replaced", func(t *testing.T) {
		t.Parallel()
		live := nsdelFresh(name, name)
		live.annotations[nsdelUID] = "uid-before-restore"
		_, a := nsdelEnforceWrite(t, nsdelRun(t, "", nil, declared, live), name)
		if a[nsdelUID] != "uid-before-restore" {
			t.Errorf("%s = %q, want the recorded stamp kept", nsdelUID, a[nsdelUID])
		}
	})
}

// A namespace whose record is not bound to it is never deleted: a restore or migration brings back
// the metadata with a new uid and a new creationTimestamp.
func TestProjectOnboarding_NamespaceDeleteRestored(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-restored", 2, 1)[0]
	const reason = "it is not deleted: its creation record is not bound to this namespace"
	restored := nsdelCreated(gone, gone)
	restored.uid = "uid-after-restore"
	restored.created = nsdelTs(time.Now().Add(-time.Hour))
	unstamped := nsdelCreated(gone, gone)
	delete(unstamped.annotations, nsdelUID)
	noUID := nsdelCreated(gone, gone)
	noUID.uid = ""
	for label, ns := range map[string]nsdelNs{"restored, new uid": restored, "never stamped": unstamped, "no uid": noUID} {
		ns := ns
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRun(t, "", nsdelOn, nil, ns)
			requireNoNamespaceDeleted(t, objs)
			if m := nsdelMarks(objs); len(m) != 0 {
				t.Errorf("marks %v, want none", m)
			}
			if st := onboardingStatus(t, objs)[gone]; !strings.Contains(st, reason) {
				t.Errorf("status[%s] = %q, want %q", gone, st, reason)
			}
		})
	}
	// Restored before the stamp was ever written, onto a cluster where the project is still declared:
	// the live namespace is far younger than the recorded time, so it is never stamped.
	t.Run("restored before the stamp, project declared", func(t *testing.T) {
		t.Parallel()
		live := nsdelFresh(gone, gone)
		delete(live.annotations, nsdelUID)
		live.uid = "uid-after-restore"
		live.created = nsdelTs(time.Now())
		_, a := nsdelEnforceWrite(t, nsdelRun(t, "", nil, map[string]map[string]interface{}{gone: {"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true}}, live), gone)
		if v, ok := a[nsdelUID]; ok {
			t.Errorf("%s = %q, want no stamp", nsdelUID, v)
		}
	})
}

// A forged record on the namespace of a declared project that does not render its own Namespace in
// the enforce ConfigurationPolicy (mode inform, invalid, no admin group, adopted inform) is removed
// by the record write, whole.
func TestProjectOnboarding_NamespaceForgedRecordStripped(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-forge", 0, 1)[0]
	for label, project := range map[string]map[string]interface{}{
		"inform":         {"groups": map[string]interface{}{"admin": "A"}, "mode": "inform"},
		"invalid":        {"groups": map[string]interface{}{"admin": "A"}, "egressIP": "not-a-list"},
		"no admin group": {"groups": map[string]interface{}{"admin": ""}},
		"adopted inform": {"groups": map[string]interface{}{"admin": "A"}, "adopt": true},
	} {
		project := project
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			forged := nsdelCreated(name, name)
			forged.annotations[nsdelCreatedBy] = "someone-else"
			if project["adopt"] == true {
				forged.annotations[nsdelCreatedBy] = name
			}
			l, a := nsdelEnforceWrite(t, nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{name: project}, forged), name)
			if _, ok := l[nsdelMarker]; ok {
				t.Errorf("labels %v keep %s", l, nsdelMarker)
			}
			for _, k := range []string{nsdelCreatedBy, nsdelOnRemoval, nsdelModeKey, nsdelCreatedAt, nsdelUID, nsdelPending} {
				if v, ok := a[k]; ok {
					t.Errorf("annotation %s=%q kept, want the forged record removed", k, v)
				}
			}
			if l["autoshift.io/project"] != name {
				t.Errorf("labels %v, want the ownership labels kept", l)
			}
		})
	}
}

// While a project is declared but does not render its own Namespace in the enforce
// ConfigurationPolicy, the record still follows it downward: deleteOnRemoval false and mode inform
// are recorded, and nothing else of the Namespace changes. A removal after that deletes nothing.
func TestProjectOnboarding_NamespaceRecordsFollowNonRendered(t *testing.T) {
	t.Parallel()
	name := shardNames("nsdel-down", 7, 1)[0]
	base := func() map[string]interface{} {
		return map[string]interface{}{"groups": map[string]interface{}{"admin": "A"}}
	}
	cases := []struct {
		label string
		edit  func(p map[string]interface{})
		drop  []string
	}{
		{"invalid", func(p map[string]interface{}) { p["egressIP"] = []interface{}{"not-an-ip"} }, nil},
		{"no admin group", func(p map[string]interface{}) { p["groups"] = map[string]interface{}{"admin": ""} }, nil},
		{"blocked by a missing EgressFirewall CRD", func(p map[string]interface{}) { p["networkProfile"] = "strict" }, []string{"project-onboarding-egressfirewall.yaml"}},
		{"invalid mode", func(p map[string]interface{}) { p["mode"] = "Enforce" }, nil},
	}
	for _, c := range cases {
		c := c
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			p := base()
			c.edit(p)
			live := nsdelFresh(name, name)
			live.annotations["example.com/keep"] = "v"
			r := stressRun(t, stressOpts{allShards: true, labels: nsdelOn, drop: c.drop, extra: nsdelStubs(live),
				mutate: nsdelProjects(map[string]map[string]interface{}{name: p})})
			if st := onboardingStatus(t, r.objs)[name]; st == "" {
				t.Fatalf("status[%s] is empty, want the project reported as not rendered", name)
			}
			l, a := nsdelEnforceWrite(t, r.objs, name)
			want := map[string]string{}
			for k, v := range live.annotations {
				want[k] = v
			}
			want[nsdelOnRemoval] = "false"
			if c.label == "invalid mode" {
				want[nsdelModeKey] = "inform"
			}
			if fmt.Sprint(a) != fmt.Sprint(want) {
				t.Errorf("annotations %v, want %v", a, want)
			}
			if fmt.Sprint(l) != fmt.Sprint(live.labels) {
				t.Errorf("labels %v, want the live labels %v", l, live.labels)
			}
		})
	}
	t.Run("never raised: deleteOnRemoval true on an invalid project", func(t *testing.T) {
		t.Parallel()
		live := nsdelFresh(name, name)
		live.annotations[nsdelOnRemoval] = "false"
		live.annotations[nsdelModeKey] = "inform"
		objs := nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"},
			"deleteOnRemoval": true, "egressIP": []interface{}{"not-an-ip"}}}, live)
		for _, o := range renderedNamespaces(objs, name) {
			if o.remediationAction == "enforce" {
				t.Errorf("enforce write of %s %v, want none: an invalid project never raises its record", name, o.obj["metadata"])
			}
		}
	})
	t.Run("dry run records nothing", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "inform", nsdelOn, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": "A"}, "egressIP": []interface{}{"not-an-ip"}}}, nsdelFresh(name, name))
		for _, o := range objs {
			if o.remediationAction == "enforce" {
				t.Fatalf("dry run: %s has remediationAction enforce", o.configPolicy)
			}
		}
	})
	// The two evaluations of a downgrade and a later removal: the first records false, so the removal
	// is silent and deletes nothing.
	t.Run("downgraded, then removed", func(t *testing.T) {
		t.Parallel()
		_, a := nsdelEnforceWrite(t, nsdelRun(t, "", nsdelOn, map[string]map[string]interface{}{name: {"groups": map[string]interface{}{"admin": ""}}}, nsdelCreated(name, name)), name)
		after := nsdelCreated(name, name)
		after.annotations = a
		objs := nsdelRun(t, "", nsdelOn, nil, after)
		requireNoNamespaceDeleted(t, objs)
		if v, ok := onboardingStatus(t, objs)[name]; ok {
			t.Errorf("status[%s] = %q, want no entry", name, v)
		}
	})
}

// nsdelStorageKinds are the storage kinds besides PersistentVolumeClaim, with their API version and
// CRD.
var nsdelStorageKinds = []struct{ kind, apiVersion, crd string }{
	{"VolumeSnapshot", "snapshot.storage.k8s.io/v1", "volumesnapshots.snapshot.storage.k8s.io"},
	{"ObjectBucketClaim", "objectbucket.io/v1alpha1", "objectbucketclaims.objectbucket.io"},
	{"DataVolume", "cdi.kubevirt.io/v1beta1", "datavolumes.cdi.kubevirt.io"},
	{"VirtualMachine", "kubevirt.io/v1", "virtualmachines.kubevirt.io"},
}

// nsdelStorage renders the CRD of each storage kind (served or not) and one object of each kind in
// an unrelated namespace, so the managed cluster serves the kind, plus count objects of kind in ns.
func nsdelStorage(served bool, ns string, counts map[string]int) string {
	var b strings.Builder
	for _, k := range nsdelStorageKinds {
		ver := k.apiVersion[strings.LastIndex(k.apiVersion, "/")+1:]
		fmt.Fprintf(&b, "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: %s\nspec:\n  versions:\n  - name: %s\n    served: %v\n---\n", k.crd, ver, served)
		fmt.Fprintf(&b, "apiVersion: %s\nkind: %s\nmetadata:\n  name: other\n  namespace: %s\n---\n", k.apiVersion, k.kind, nsdelUnrelatedN)
		for i := 0; i < counts[k.kind]; i++ {
			fmt.Fprintf(&b, "apiVersion: %s\nkind: %s\nmetadata:\n  name: s-%d\n  namespace: %s\n---\n", k.apiVersion, k.kind, i, ns)
		}
	}
	return b.String()
}

// Any storage in the namespace keeps it: a PersistentVolumeClaim in any phase, and the
// VolumeSnapshots, ObjectBucketClaims, DataVolumes and VirtualMachines of each kind whose CRD
// exists. A CRD that does not serve the version looked up keeps it too.
func TestProjectOnboarding_NamespaceDeleteStorage(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-store", 3, 1)[0]
	run := func(t *testing.T, extra string, ns nsdelNs) []onboardingObject {
		t.Helper()
		r := stressRun(t, stressOpts{allShards: true, labels: nsdelOn, extra: nsdelStubs(ns) + extra, mutate: nsdelProjects(nil)})
		return r.objs
	}
	blocked := func(t *testing.T, objs []onboardingObject, want string) {
		t.Helper()
		requireNoNamespaceDeleted(t, objs)
		if m := nsdelMarks(objs); len(m) != 0 {
			t.Errorf("marks %v, want none", m)
		}
		if st := onboardingStatus(t, objs)[gone]; !strings.HasSuffix(st, "it is not deleted: namespace holds storage: "+want) {
			t.Errorf("status[%s] = %q, want it to end with %q", gone, st, "namespace holds storage: "+want)
		}
	}
	for _, k := range nsdelStorageKinds {
		k := k
		t.Run(k.kind, func(t *testing.T) {
			t.Parallel()
			blocked(t, run(t, nsdelStorage(true, gone, map[string]int{k.kind: 1}), nsdelCreated(gone, gone)), k.kind+" 1")
		})
	}
	for _, phase := range []string{"Pending", "Bound", "Lost"} {
		phase := phase
		t.Run("PersistentVolumeClaim "+phase, func(t *testing.T) {
			t.Parallel()
			pvc := fmt.Sprintf("apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: data\n  namespace: %s\nstatus:\n  phase: %s\n---\n", gone, phase)
			blocked(t, run(t, pvc, nsdelCreated(gone, gone)), "PersistentVolumeClaim 1")
		})
	}
	t.Run("PersistentVolumeClaim terminating", func(t *testing.T) {
		t.Parallel()
		pvc := fmt.Sprintf("apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: data\n  namespace: %s\n  deletionTimestamp: %q\n  finalizers: [kubernetes.io/pvc-protection]\n---\n", gone, nsdelBorn)
		blocked(t, run(t, pvc, nsdelCreated(gone, gone)), "PersistentVolumeClaim 1")
	})
	t.Run("several kinds", func(t *testing.T) {
		t.Parallel()
		ns := nsdelCreated(gone, gone)
		ns.pvcs = 2
		blocked(t, run(t, nsdelStorage(true, gone, map[string]int{"VolumeSnapshot": 3, "VirtualMachine": 1}), ns),
			"PersistentVolumeClaim 2, VolumeSnapshot 3, VirtualMachine 1")
	})
	t.Run("a CRD that does not serve the version", func(t *testing.T) {
		t.Parallel()
		blocked(t, run(t, nsdelStorage(false, gone, nil), nsdelCreated(gone, gone)),
			"VolumeSnapshot (cannot list: its CRD does not serve snapshot.storage.k8s.io/v1), ObjectBucketClaim (cannot list: its CRD does not serve objectbucket.io/v1alpha1), DataVolume (cannot list: its CRD does not serve cdi.kubevirt.io/v1beta1), VirtualMachine (cannot list: its CRD does not serve kubevirt.io/v1)")
	})
	t.Run("storage in other namespaces does not count", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, run(t, nsdelStorage(true, gone, nil), nsdelCreated(gone, gone)), gone)
	})
}

// nsdelManagedClusters renders the ManagedCluster CRD and one ManagedCluster per name.
func nsdelManagedClusters(names ...string) string {
	b := "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: managedclusters.cluster.open-cluster-management.io\nspec:\n  versions:\n  - name: v1\n    served: true\n---\n"
	for _, n := range names {
		b += fmt.Sprintf("apiVersion: cluster.open-cluster-management.io/v1\nkind: ManagedCluster\nmetadata:\n  name: %s\nspec:\n  hubAcceptsClient: true\n---\n", n)
	}
	return b
}

// The hub's own namespaces are never deleted, whatever they carry: the namespace of this Policy,
// and on a hub every namespace named after a ManagedCluster (local-cluster and each spoke), which
// the hub uses for that cluster. Each is refused silently, like a platform namespace.
func TestProjectOnboarding_NamespaceDeleteHubNamespaces(t *testing.T) {
	t.Parallel()
	spoke := shardNames("nsdel-spoke", 1, 1)[0]
	other := shardNames("nsdel-free", 1, 1)[0]
	nss := []nsdelNs{nsdelCreated("policies-autoshift", "policies-autoshift"), nsdelCreated("local-cluster", "local-cluster"),
		nsdelCreated(spoke, spoke), nsdelFresh(spoke+"-fresh", spoke+"-fresh"), nsdelCreated(other, other)}
	// eso-hub-bootstrap.yaml holds an unlabeled policies-autoshift Namespace; this test seeds its own.
	// The ManagedCluster local-cluster comes from cluster-openshift-resources.yaml.
	r := stressRun(t, stressOpts{allShards: true, labels: nsdelOn, drop: []string{"eso-hub-bootstrap.yaml"},
		extra: nsdelStubs(nss...) + nsdelManagedClusters(spoke, spoke+"-fresh"), mutate: nsdelProjects(nil)})
	requireDeleted(t, r.objs, other)
	if m := nsdelMarks(r.objs); len(m) != 0 {
		t.Errorf("marks %v, want none", m)
	}
	st := onboardingStatus(t, r.objs)
	for _, ns := range []string{"policies-autoshift", "local-cluster", spoke, spoke + "-fresh"} {
		if v, ok := st[ns]; ok {
			t.Errorf("status[%s] = %q, want no entry: refused like a platform namespace", ns, v)
		}
	}
	// Without the ManagedCluster CRD (a managed cluster that is not a hub), the name means nothing.
	t.Run("no ManagedCluster CRD", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelOn, nil, nsdelCreated(spoke, spoke))
		requireDeleted(t, objs, spoke)
	})
}

// The rename of the README: without namespace:, the old namespace is marked (the old project is
// gone); with namespace: <old>, it is declared and never marked, and once its ownership label names
// the new project, the old project's creation record is removed.
func TestProjectOnboarding_NamespaceDeleteRename(t *testing.T) {
	t.Parallel()
	old := shardNames("nsdel-ren-old", 2, 1)[0]
	renamed := shardNames("nsdel-ren-new", 4, 1)[0]
	project := func(ns string) map[string]map[string]interface{} {
		p := map[string]interface{}{"groups": map[string]interface{}{"admin": "A"}, "deleteOnRemoval": true}
		if ns != "" {
			p["namespace"] = ns
		}
		return map[string]map[string]interface{}{renamed: p}
	}
	t.Run("without namespace: the old namespace is marked", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelOn, project(""), nsdelFresh(old, old))
		requireNoNamespaceDeleted(t, objs)
		if m := nsdelMarks(objs); len(m) != 1 || len(m[old]) != 1 {
			t.Errorf("marks %v, want %s marked", m, old)
		}
	})
	t.Run("with namespace: old, it is never marked", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelOn, project(old), nsdelCreated(old, old))
		requireNoNamespaceDeleted(t, objs)
		if m := nsdelMarks(objs); len(m) != 0 {
			t.Errorf("marks %v, want none", m)
		}
		if st := onboardingStatus(t, objs)[renamed]; !strings.Contains(st, "namespace "+old+" is owned by project "+old) {
			t.Errorf("status[%s] = %q, want the ownership block", renamed, st)
		}
	})
	t.Run("relabeled: the old record is removed", func(t *testing.T) {
		t.Parallel()
		ns := nsdelFresh(old, old)
		ns.labels["autoshift.io/project"] = renamed
		requireNoCreationRecord(t, nsdelRun(t, "", nsdelOn, project(old), ns), old)
	})
}

// requireUnmarked fails unless the enforce ConfigurationPolicy of ns's shard writes ns exactly
// (mustonlyhave) without the pending mark and with every other live key, and nothing marks it.
func requireUnmarked(t *testing.T, objs []onboardingObject, ns nsdelNs) {
	t.Helper()
	requireNoNamespaceDeleted(t, objs)
	if m := nsdelMarks(objs); len(m) != 0 {
		t.Errorf("marks %v, want none", m)
	}
	var writes []onboardingObject
	for _, o := range renderedNamespaces(objs, ns.name) {
		if o.remediationAction == "enforce" {
			writes = append(writes, o)
		}
	}
	if len(writes) != 1 || writes[0].item["metadataComplianceType"] != "mustonlyhave" ||
		writes[0].configPolicy != fmt.Sprintf("%s%d-enforce", onboardingShardPrefix, onboardingShardOf(ns.name)) {
		t.Fatalf("want one mustonlyhave enforce write of %s from its shard, got %v", ns.name, writes)
	}
	meta, _ := writes[0].obj["metadata"].(map[string]interface{})
	want := map[string]string{}
	for k, v := range ns.annotations {
		if k != nsdelPending {
			want[k] = v
		}
	}
	if a := stringMap(meta, "annotations"); fmt.Sprint(a) != fmt.Sprint(want) {
		t.Errorf("annotations %v, want %v", a, want)
	}
	if l := stringMap(meta, "labels"); fmt.Sprint(l) != fmt.Sprint(ns.labels) {
		t.Errorf("labels %v, want %v", l, ns.labels)
	}
}

// A cluster-level guard that blocks the deletion removes the mark, so turning the guard back on
// always starts a fresh grace period: an operator's kill switch never leaves expired marks behind.
// The status keeps the pending times of the removed mark.
func TestProjectOnboarding_NamespaceDeleteGuardResetsMark(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-flap", 6, 1)[0]
	for label, labels := range map[string]map[string]string{
		"delete-namespaces off":    nsdelLabels(map[string]string{nsdelLabel: "false"}),
		"delete-namespaces absent": nsdelLabels(map[string]string{nsdelLabel: "\x00absent"}),
		"prune off":                nsdelLabels(map[string]string{onboardingPruneLabel: "false"}),
	} {
		labels := labels
		t.Run(label+", then on again", func(t *testing.T) {
			t.Parallel()
			ns := nsdelCreated(gone, gone)
			objs := nsdelRun(t, "", labels, nil, ns)
			requireUnmarked(t, objs, ns)
			st := onboardingStatus(t, objs)[gone]
			for _, want := range []string{"it is pending deletion since " + nsdelLongAgo + ", and can be deleted from ",
				"it is not deleted: the cluster label", "its mark is removed, so a new grace period starts once that is resolved"} {
				if !strings.Contains(st, want) {
					t.Errorf("status[%s] = %q, want %q", gone, st, want)
				}
			}
			// The next evaluation, guard on again: the namespace is marked now, not deleted.
			before := time.Now()
			objs = nsdelRun(t, "", nsdelOn, nil, nsdelFresh(gone, gone))
			requireNoNamespaceDeleted(t, objs)
			requireMarkedNow(t, objs, gone, before, time.Now())
		})
	}
	t.Run("an unmarked namespace is not written", func(t *testing.T) {
		t.Parallel()
		objs := nsdelRun(t, "", nsdelLabels(map[string]string{nsdelLabel: "false"}), nil, nsdelFresh(gone, gone))
		for _, o := range renderedNamespaces(objs, gone) {
			t.Errorf("unexpected write of %s in %s", gone, o.configPolicy)
		}
	})
	t.Run("an empty projects map removes the mark too", func(t *testing.T) {
		t.Parallel()
		ns := nsdelCreated(gone, gone)
		requireUnmarked(t, nsdelRunCfg(t, "", nsdelOn, nil, func(cfg map[string]interface{}) { cfg["projects"] = map[string]interface{}{} }, ns), ns)
	})
	t.Run("a guard on the namespace itself keeps the mark", func(t *testing.T) {
		t.Parallel()
		ns := nsdelCreated(gone, gone)
		ns.pvcs = 1
		objs := nsdelRun(t, "", nsdelOn, nil, ns)
		requireNoNamespaceDeleted(t, objs)
		for _, o := range renderedNamespaces(objs, gone) {
			t.Errorf("unexpected write of %s in %s", gone, o.configPolicy)
		}
	})
}

// The dry-run exit of the README: dry run with the label off writes nothing; the first evaluation
// after the dry run, label still off, removes every mark, including one older than the dry run;
// with the label on again each namespace is marked now and gets a full grace period.
func TestProjectOnboarding_NamespaceDeleteDryRunExit(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-exit", 2, 1)[0]
	off := nsdelLabels(map[string]string{nsdelLabel: "false"})
	ns := nsdelCreated(gone, gone)
	objs := nsdelRun(t, "inform", off, nil, ns)
	requireNoNamespaceDeleted(t, objs)
	for _, o := range objs {
		if o.remediationAction == "enforce" {
			t.Fatalf("dry run: %s has remediationAction enforce", o.configPolicy)
		}
	}
	for _, o := range renderedNamespaces(objs, gone) {
		t.Errorf("dry run: write of %s in %s, want none", gone, o.configPolicy)
	}
	requireUnmarked(t, nsdelRun(t, "", off, nil, ns), ns)
	before := time.Now()
	objs = nsdelRun(t, "", nsdelOn, nil, nsdelFresh(gone, gone))
	requireNoNamespaceDeleted(t, objs)
	requireMarkedNow(t, objs, gone, before, time.Now())
}

// A grace period below 1h is reported and the 24h default applies, to marks that exist already too.
func TestProjectOnboarding_NamespaceDeleteGraceFloor(t *testing.T) {
	t.Parallel()
	gone := shardNames("nsdel-floor", 0, 1)[0]
	for _, low := range []string{"30m", "59m59s", "1s", "0s", "0h0m0s"} {
		low := low
		t.Run(low, func(t *testing.T) {
			t.Parallel()
			objs := nsdelRunCfg(t, "", nsdelOn, nil, nsdelGrace(low), nsdelPendingSince(gone, gone, nsdelTs(time.Now().Add(-2*time.Hour))))
			requireNoNamespaceDeleted(t, objs)
			if got := onboardingShardStatus(t, objs)[0]["projectDefaults.namespaceDeleteGracePeriod"]; got != "namespaceDeleteGracePeriod is shorter than the minimum of 1h, so the default 24h is used" {
				t.Errorf("shard 0 status = %q", got)
			}
			requireDeleted(t, nsdelRunCfg(t, "", nsdelOn, nil, nsdelGrace(low), nsdelPendingSince(gone, gone, nsdelTs(time.Now().Add(-25*time.Hour)))), gone)
		})
	}
	t.Run("1h is the minimum", func(t *testing.T) {
		t.Parallel()
		requireDeleted(t, nsdelRunCfg(t, "", nsdelOn, nil, nsdelGrace("1h"), nsdelPendingSince(gone, gone, nsdelTs(time.Now().Add(-61*time.Minute)))), gone)
	})
}
